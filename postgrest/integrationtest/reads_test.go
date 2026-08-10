//go:build integration

package integrationtest

// cSpell:ignore Fworld Fleading

import (
	"errors"
	"net/http"
	"os"
	"slices"
	"testing"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/postgrest/internal/testkit"
)

// newIntegrationClient wires a client at the local Supabase stack started by
// scripts/integration-test.sh, failing the test when the required environment
// variables are absent.
func newIntegrationClient(t *testing.T) *postgrest.Client {
	t.Helper()
	projectURL := os.Getenv("SUPABASE_URL")
	apiKey := os.Getenv("SUPABASE_PUBLISHABLE_KEY")
	if projectURL == "" || apiKey == "" {
		t.Fatal("SUPABASE_URL and SUPABASE_PUBLISHABLE_KEY must be set for integration tests - run scripts/integration-test.sh")
	}
	projectConfiguration, err := configuration.New(core.ModulePathPostgrest, projectURL, apiKey)
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	return postgrest.NewFromConfiguration(projectConfiguration)
}

type seededEntity struct {
	ID int `json:"id"`
}

type namedEntity struct {
	seededEntity
	Name string `json:"name"`
}

type seededInstrument struct {
	namedEntity
}

// TestSelectAllColumns proves the read path against real PostgREST: seeded
// rows decode, the status is 200 and a request without a count preference
// reports an unknown total as -1, confirming live the Content-Range behavior
// the unit tests synthesize.
func TestSelectAllColumns(t *testing.T) {
	client := newIntegrationClient(t)

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[seededInstrument]("instruments").
			Select(""),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// No count was requested, so PostgREST reports an unknown total ("0-2/*").
	testkit.AssertOKResponse(t, response)
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3 (seed drifted?)", len(rows))
	}
	names := map[string]bool{}
	for _, row := range rows {
		names[row.Name] = true
	}
	for _, want := range []string{"violin", "viola", "cello"} {
		if !names[want] {
			t.Errorf("seeded row %q missing from result set %v", want, names)
		}
	}
}

// TestSelectColumnSubset proves the cleaned select list is accepted by the
// real server and that a narrower row type decodes the projection.
func TestSelectColumnSubset(t *testing.T) {
	client := newIntegrationClient(t)

	type nameOnly struct {
		Name string `json:"name"`
	}

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[nameOnly]("instruments").
			Select("name"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	testkit.AssertOKResponse(t, response)
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3 (seed drifted?)", len(rows))
	}
	for index, row := range rows {
		if row.Name == "" {
			t.Errorf("rows[%d].Name is empty", index)
		}
	}
}

// TestCollectAppliesLimit proves that the [postgrest.Limit] method applies the
// specified limit when that limit is more than one and that limit is less than
// the number of rows in the seeded data.
func TestCollectAppliesLimit(t *testing.T) {
	client := newIntegrationClient(t)

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[seededInstrument]("instruments").
			Select("").
			Limit(2), // the function under test
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// No count was requested, so PostgREST reports an unknown total ("0-2/*").
	testkit.AssertOKResponse(t, response)
	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2 (seed drifted or wrong limit applied?)", len(rows))
	}
}

func TestCollectAppliesZeroLimit(t *testing.T) {
	client := newIntegrationClient(t)

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[seededInstrument]("instruments").
			Select("").
			Limit(0), // the function under test
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// No count was requested, so PostgREST reports an unknown total ("0-2/*").
	testkit.AssertOKResponse(t, response)
	if len(rows) != 0 {
		t.Fatalf("row count = %d, want 0 (wrong limit applied?)", len(rows))
	}
}

func TestCollectForwardsNegativeLimit(t *testing.T) {
	client := newIntegrationClient(t)

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[seededInstrument]("instruments").
			Select("").
			Limit(-1), // the function under test
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "PGRST103" {
		t.Errorf("Code = %q, want PGRST103 (invalid range was specified for Limits and Pagination)", typedError.Code)
	}
	if typedError.HTTPStatus != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("HTTPStatus = %d, want 416", typedError.HTTPStatus)
	}

	if rows != nil {
		t.Errorf("rows = %+v, want nil on error", rows)
	}
	if response != (postgrest.Response{}) {
		t.Errorf("response = %+v, want zero value on error", response)
	}
}

func TestCollectAppliesSingleColumnOrder(t *testing.T) {
	testCases := []struct {
		name    string
		perform func(builder postgrest.OrderedFilterBuilder[seededInstrument]) postgrest.Query[seededInstrument]
		want    []string
	}{
		{
			name: "implicit defaults",
			perform: func(builder postgrest.OrderedFilterBuilder[seededInstrument]) postgrest.Query[seededInstrument] {
				return builder
			},
			want: []string{"violin", "viola", "cello"}, // 2015, 2020, null
		},
		{
			name: "descending",
			perform: func(builder postgrest.OrderedFilterBuilder[seededInstrument]) postgrest.Query[seededInstrument] {
				return builder.Descending() // .desc
			},
			want: []string{"cello", "viola", "violin"}, // null, 2020, 2015
		},
		{
			name: "ascending with nulls first",
			perform: func(builder postgrest.OrderedFilterBuilder[seededInstrument]) postgrest.Query[seededInstrument] {
				return builder.NullsFirst() // .nullsfirst
			},
			want: []string{"cello", "violin", "viola"}, // null, 2015, 2020
		},
		{
			name: "descending with nulls last",
			perform: func(builder postgrest.OrderedFilterBuilder[seededInstrument]) postgrest.Query[seededInstrument] {
				return builder.Descending().NullsLast() // .desc.nullslast
			},
			want: []string{"viola", "violin", "cello"}, // 2020, 2015, null
		},
	}

	client := newIntegrationClient(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rows, response, err := postgrest.Collect(
				t.Context(),
				client,
				testCase.perform(postgrest.
					From[seededInstrument]("instruments").
					Select("").
					Order("acquired_year")),
			)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}

			// No count was requested, so PostgREST reports an unknown total ("0-2/*").
			testkit.AssertOKResponse(t, response)
			if len(rows) != 3 {
				t.Fatalf("row count = %d, want 3 (seed drifted?)", len(rows))
			}

			for index, row := range rows {
				if testCase.want[index] != row.Name {
					t.Errorf("Row %d, want %q, got %q", index, testCase.want[index], row.Name)
				}
			}
		})
	}
}

func TestCollectAppliesMultiColumnOrder(t *testing.T) {
	testCases := []struct {
		name    string
		perform func(builder postgrest.FilterBuilder[seededEntity]) postgrest.Query[seededEntity]
		want    []int
	}{
		{
			// Tie-break activates the second term
			name: "order order",
			perform: func(builder postgrest.FilterBuilder[seededEntity]) postgrest.Query[seededEntity] {
				return builder.Order("section").Order("seat") // order=section,seat
			},
			want: []int{4, 2, 1, 3, 5, 6},
		},
		{
			// A refinement binds the newest term, not the first
			name: "order order descending",
			perform: func(builder postgrest.FilterBuilder[seededEntity]) postgrest.Query[seededEntity] {
				return builder.Order("section").Order("seat").Descending() // order=section,seat.desc
			},
			want: []int{2, 4, 3, 1, 6, 5},
		},
		{
			// A refined first term keeps its refinement when a second term follows
			name: "order descending order",
			perform: func(builder postgrest.FilterBuilder[seededEntity]) postgrest.Query[seededEntity] {
				return builder.Order("section").Descending().Order("seat") // order=section.desc,seat
			},
			want: []int{5, 6, 1, 3, 4, 2},
		},
		{
			// Null placement defaults inside a lower-precedence term
			name: "order order nulls",
			perform: func(builder postgrest.FilterBuilder[seededEntity]) postgrest.Query[seededEntity] {
				return builder.Order("section").Order("rating") // order=section,rating
			},
			want: []int{4, 2, 3, 1, 6, 5},
		},
		{
			// NullsFirst refines the lower-precedence term
			name: "order order nulls refine",
			perform: func(builder postgrest.FilterBuilder[seededEntity]) postgrest.Query[seededEntity] {
				return builder.Order("section").Order("rating").NullsFirst() // order=section,rating.nullsfirst
			},
			want: []int{2, 4, 3, 1, 5, 6},
		},
		{
			// Nulls at the first term with the second term ordering the null group
			name: "order order nulls 2",
			perform: func(builder postgrest.FilterBuilder[seededEntity]) postgrest.Query[seededEntity] {
				return builder.Order("rating").Order("tenure") // order=rating,tenure
			},
			want: []int{6, 3, 4, 1, 5, 2},
		},
	}

	client := newIntegrationClient(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rows, response, err := postgrest.Collect(
				t.Context(),
				client,
				testCase.perform(postgrest.
					From[seededEntity]("players").
					Select("id")),
			)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}

			// No count was requested, so PostgREST reports an unknown total ("0-2/*").
			testkit.AssertOKResponse(t, response)
			if len(rows) != 6 {
				t.Fatalf("row count = %d, want 6 (seed drifted?)", len(rows))
			}

			rowIds := make([]int, len(rows))
			for index, row := range rows {
				rowIds[index] = row.ID
			}

			if !slices.Equal(testCase.want, rowIds) {
				t.Errorf("want %q, got %q", testCase.want, rowIds)
			}
		})
	}
}

// TestMissingRelationReturnsTypedError proves error parsing against a real
// PostgREST error response. The stack is version-pinned by the harness, so
// the exact protocol shape (PGRST205, HTTP 404) is asserted deliberately: a
// failure here on a pin bump is upstream drift worth reviewing.
func TestMissingRelationReturnsTypedError(t *testing.T) {
	client := newIntegrationClient(t)

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[seededInstrument]("does_not_exist").
			Select(""),
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "PGRST205" {
		t.Errorf("Code = %q, want PGRST205 (unknown relation)", typedError.Code)
	}
	if typedError.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", typedError.HTTPStatus)
	}
	if rows != nil {
		t.Errorf("rows = %+v, want nil on error", rows)
	}
	if response != (postgrest.Response{}) {
		t.Errorf("response = %+v, want zero value on error", response)
	}
}

func TestDelimitedIdentifierTableNames(t *testing.T) {
	testCases := []struct {
		name        string
		shapedInput string // TODO remove once path escaping fixed
		table       string
	}{
		{
			name:        "embedded space",
			shapedInput: "odd%20table",
			table:       "odd table",
		},
		{
			name:  "risk of leaking tail into query string",
			table: "a?b",
		},
		{
			name:        "should not allow route to RPC",
			shapedInput: "rpc%2Fd",
			table:       "rpc/d",
		},
		{
			name:        "should not collapse double slash",
			shapedInput: "hello%2F%2Fworld",
			table:       "hello//world",
		},
		{
			name:        "should not swallow leading slash",
			shapedInput: "%2Fleading",
			table:       "/leading",
		},
		{
			name:        "trailing slash",
			shapedInput: "trailing%2F",
			table:       "trailing/",
		},
		{
			name:        "embedded ampersand",
			shapedInput: "e&f",
			table:       "e&f",
		},
		{
			name:        "double double dot",
			shapedInput: "..%2F..",
			table:       "../..",
		},
		{
			name:        "embedded double dot should not cancel leading part",
			shapedInput: "g%2F..%2Fh",
			table:       "g/../h",
		},
		{
			name:        "trailing percent style name",
			shapedInput: "50%25off",
			table:       "50%off",
		},
		{
			name:  "double quote",
			table: `"`,
		},
		{
			name:  "single quote",
			table: "'",
		},
		{
			name:  "backtick",
			table: "`",
		},
	}

	client := newIntegrationClient(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			table := testCase.table
			if testCase.shapedInput != "" {
				table = testCase.shapedInput
			}

			rows, response, err := postgrest.Collect(
				t.Context(),
				client,
				postgrest.
					From[namedEntity](table).
					Select("name"),
			)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}

			// No count was requested, so PostgREST reports an unknown total ("0-2/*").
			testkit.AssertOKResponse(t, response)
			if len(rows) != 1 {
				t.Fatalf("row count = %d, want 1 (seed drifted?)", len(rows))
			}

			if rows[0].Name != testCase.table {
				t.Errorf("want %q, got %q", testCase.table, rows[0].Name)
			}
		})
	}
}

func TestDelimitedIdentifierTableNameSingleDot(t *testing.T) {
	client := newIntegrationClient(t)

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[struct{}]("%2E"). // TODO use "." once path escaping fixed
			Select(""),
	)

	if rows != nil {
		t.Errorf("rows = %+v, want nil on error", rows)
	}
	if response != (postgrest.Response{}) {
		t.Errorf("response = %+v, want zero value on error", response)
	}
	if err == nil {
		t.Fatal("Collect: want an error - Kong normalizes `.` to the PostgREST OpenAPI root")
	}
	// The root answers HTTP 200 with the OpenAPI object, so the SDK fails while
	// decoding it into a row slice rather than returning an *Error. A decode
	// failure (not an HTTP error) is the observable signature of `.` here, and
	// the contrast with the double-dot 404 is the point of having both tests
	// (see TestDelimitedIdentifierTableNameDoubleDot).
	var typedError *postgrest.Error
	if errors.As(err, &typedError) {
		t.Fatalf("got *postgrest.Error %v, want a decoding error (200 OpenAPI object)", typedError)
	}
}

func TestDelimitedIdentifierTableNameDoubleDot(t *testing.T) {
	client := newIntegrationClient(t)

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[struct{}]("%2E%2E"). // TODO use ".." once path escaping fixed
			Select(""),
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	// This 404 is Kong's router error, not PostgREST's: `..` normalizes to
	// `/rest/` upstream, matching no route. Kong's body carries no PostgREST
	// code, so Code is empty - the discriminator from a genuine PGRST205.
	if typedError.Code != "" {
		t.Errorf("Code = %q, want empty (Kong router 404 carries no PostgREST code)", typedError.Code)
	}
	if typedError.Message != "no Route matched with those values" {
		t.Errorf("Message = %q, want Kong's no-route message", typedError.Message)
	}
	if typedError.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", typedError.HTTPStatus)
	}
	if rows != nil {
		t.Errorf("rows = %+v, want nil on error", rows)
	}
	if response != (postgrest.Response{}) {
		t.Errorf("response = %+v, want zero value on error", response)
	}
}
