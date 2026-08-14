//go:build integration

package integrationtest

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

type seededInstrument struct {
	seededEntity
	Name string `json:"name"`
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
