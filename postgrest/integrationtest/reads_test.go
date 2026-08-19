//go:build integration

package integrationtest

// cSpell:ignore Fworld Fleading

import (
	"errors"
	"net/http"
	"os"
	"slices"
	"sync"
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

// justNamedEntity is used when we have a seeded entity that does not have
// a column called id and therefore doesn't map to [seededEntity].
type justNamedEntity struct {
	Name string `json:"name"`
}

type seededInstrument struct {
	namedEntity
	AcquiredYear *int `json:"acquired_year"`
}

type seededIndexSlice struct {
	seededEntity
	DecimalNumber string `json:"decimal number"`
	Character     string `json:"character"`
	Phonetic      string `json:"phonetic"`
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
		postgrest.From[seededInstrument]("instruments"),
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

// TestCollectAllColumnsWithoutSelect proves the select-less read against
// real PostgREST: a bare From sends no select parameter and the server's
// documented default of * answers with every column, populating a field the
// projection never named.
func TestCollectAllColumnsWithoutSelect(t *testing.T) {
	client := newIntegrationClient(t)

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.From[seededInstrument]("instruments"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// No count was requested, so PostgREST reports an unknown total ("0-2/*").
	testkit.AssertOKResponse(t, response)
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3 (seed drifted?)", len(rows))
	}
	years := map[string]*int{}
	for _, row := range rows {
		years[row.Name] = row.AcquiredYear
	}
	for name, want := range map[string]int{"violin": 2015, "viola": 2020} {
		got, present := years[name]
		if !present {
			t.Errorf("seeded row %q missing from result set", name)
			continue
		}
		if got == nil || *got != want {
			t.Errorf("%s acquired_year = %v, want %d (every column should arrive without a select)", name, got, want)
		}
	}
	if got, present := years["cello"]; !present {
		t.Error(`seeded row "cello" missing from result set`)
	} else if got != nil {
		t.Errorf("cello acquired_year = %d, want null", *got)
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
		postgrest.From[seededInstrument]("does_not_exist"),
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
		name  string
		table string
	}{
		{
			name:  "embedded space",
			table: "odd table",
		},
		{
			name:  "risk of leaking tail into query string",
			table: "a?b",
		},
		{
			name:  "should not allow route to RPC",
			table: "rpc/d",
		},
		{
			name:  "should not collapse double slash",
			table: "hello//world",
		},
		{
			name:  "should not swallow leading slash",
			table: "/leading",
		},
		{
			name:  "trailing slash",
			table: "trailing/",
		},
		{
			name:  "embedded ampersand",
			table: "e&f",
		},
		{
			name:  "double double dot",
			table: "../..",
		},
		{
			name:  "embedded double dot should not cancel leading part",
			table: "g/../h",
		},
		{
			name:  "trailing percent style name",
			table: "50%off",
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
		{
			name:  "colon and dash",
			table: "2026-06-30T11:23:31",
		},
		{
			name:  "embedded plus",
			table: "a+b",
		},
	}

	client := newIntegrationClient(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rows, response, err := postgrest.Collect(
				t.Context(),
				client,
				postgrest.
					From[namedEntity](testCase.table).
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
		postgrest.From[struct{}]("."),
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
		postgrest.From[struct{}](".."),
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

func newIndexSlice(id int, decimalNumber, character, phonetic string) seededIndexSlice {
	return seededIndexSlice{
		seededEntity:  seededEntity{ID: id},
		DecimalNumber: decimalNumber,
		Character:     character,
		Phonetic:      phonetic,
	}
}

var indexSlices = sync.OnceValue(func() []seededIndexSlice {
	return []seededIndexSlice{
		newIndexSlice(1, "01", "A", "Alpha"),
		newIndexSlice(2, "02", "B", "Bravo"),
		newIndexSlice(3, "03", "C", "Charlie"),
		newIndexSlice(4, "04", "D", "Delta"),
		newIndexSlice(5, "05", "E", "Echo"),
		newIndexSlice(6, "06", "F", "Foxtrot"),
		newIndexSlice(7, "07", "G", "Golf"),
		newIndexSlice(8, "08", "H", "Hotel"),
		newIndexSlice(9, "09", "I", "India"),
		newIndexSlice(10, "10", "J", "Juliet"),
		newIndexSlice(11, "11", "K", "Kilo"),
		newIndexSlice(12, "12", "L", "Lima"),
		newIndexSlice(13, "13", "M", "Mike"),
		newIndexSlice(14, "14", "N", "November"),
		newIndexSlice(15, "15", "O", "Oscar"),
		newIndexSlice(16, "16", "P", "Papa"),
		newIndexSlice(17, "17", "Q", "Quebec"),
		newIndexSlice(18, "18", "R", "Romeo"),
		newIndexSlice(19, "19", "S", "Sierra"),
		newIndexSlice(20, "20", "T", "Tango"),
		newIndexSlice(21, "21", "U", "Uniform"),
		newIndexSlice(22, "22", "V", "Victor"),
		newIndexSlice(23, "23", "W", "Whiskey"),
		newIndexSlice(24, "24", "X", "X-Ray"),
		newIndexSlice(25, "25", "Y", "Yankee"),
		newIndexSlice(26, "26", "Z", "Zulu"),
	}
})

// indexSlice returns the value that is in the sequences table for this
// id (not zero-based - that is, the first record when ordered ascending
// is id 1). Tests must use this function to obtain these values rather
// than directly accessing indexSlices (for risk of mutating by accident).
func indexSlice(id int) seededIndexSlice {
	return indexSlices()[id-1]
}

func TestCollectAppliesRange(t *testing.T) {
	testCases := []struct {
		name         string
		perform      func(builder postgrest.OrderedFilterBuilder[seededIndexSlice]) postgrest.Query[seededIndexSlice]
		wantRowCount int
		wantFirst    seededIndexSlice
		wantLast     seededIndexSlice
	}{
		{
			name: "single row",
			perform: func(builder postgrest.OrderedFilterBuilder[seededIndexSlice]) postgrest.Query[seededIndexSlice] {
				return builder.Range(16, 16)
			},
			wantRowCount: 1,
			wantFirst:    indexSlice(17),
			wantLast:     indexSlice(17),
		},
		{
			name: "no rows",
			perform: func(builder postgrest.OrderedFilterBuilder[seededIndexSlice]) postgrest.Query[seededIndexSlice] {
				return builder.Range(100, 199)
			},
			wantRowCount: 0,
			wantFirst:    seededIndexSlice{},
			wantLast:     seededIndexSlice{},
		},
		{
			name: "middle ascending",
			perform: func(builder postgrest.OrderedFilterBuilder[seededIndexSlice]) postgrest.Query[seededIndexSlice] {
				return builder.Range(3, 12)
			},
			wantRowCount: 10,
			wantFirst:    indexSlice(4),
			wantLast:     indexSlice(13),
		},
		{
			name: "middle descending",
			perform: func(builder postgrest.OrderedFilterBuilder[seededIndexSlice]) postgrest.Query[seededIndexSlice] {
				return builder.Descending().Range(3, 6)
			},
			wantRowCount: 4,
			wantFirst:    indexSlice(23),
			wantLast:     indexSlice(20),
		},
		{
			name: "first page of ten",
			perform: func(builder postgrest.OrderedFilterBuilder[seededIndexSlice]) postgrest.Query[seededIndexSlice] {
				return builder.Range(0, 9)
			},
			wantRowCount: 10,
			wantFirst:    indexSlice(1),
			wantLast:     indexSlice(10),
		},
		{
			name: "last page of ten", // imperfect alignment (partially filled)
			perform: func(builder postgrest.OrderedFilterBuilder[seededIndexSlice]) postgrest.Query[seededIndexSlice] {
				return builder.Range(20, 29)
			},
			wantRowCount: 6,
			wantFirst:    indexSlice(21),
			wantLast:     indexSlice(26),
		},
		{
			name: "last page of thirteen", // perfect alignment
			perform: func(builder postgrest.OrderedFilterBuilder[seededIndexSlice]) postgrest.Query[seededIndexSlice] {
				return builder.Range(13, 25)
			},
			wantRowCount: 13,
			wantFirst:    indexSlice(14),
			wantLast:     indexSlice(26),
		},
		{
			name: "negative offset", // (surprisingly) allowed, effectively offset zero
			perform: func(builder postgrest.OrderedFilterBuilder[seededIndexSlice]) postgrest.Query[seededIndexSlice] {
				return builder.Range(-2, 4)
			},
			wantRowCount: 5,
			wantFirst:    indexSlice(1),
			wantLast:     indexSlice(5),
		},
	}

	client := newIntegrationClient(t)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rows, response, err := postgrest.Collect(
				t.Context(),
				client,
				testCase.perform(postgrest.From[seededIndexSlice]("sequences").Order("id")),
			)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}

			// No count was requested, so PostgREST reports an unknown total ("0-2/*").
			testkit.AssertOKResponse(t, response)
			rowCount := len(rows)
			if rowCount != testCase.wantRowCount {
				t.Fatalf("row count = %d, want %d", rowCount, testCase.wantRowCount)
			}
			if rowCount > 0 {
				if rows[0] != testCase.wantFirst {
					t.Fatalf("first row unexpected - want %q, got %q", rows[0], testCase.wantFirst)
				}
				if rows[rowCount-1] != testCase.wantLast {
					t.Fatalf("last row unexpected - want %q, got %q", rows[rowCount-1], testCase.wantLast)
				}
			}
		})
	}
}

func TestUnsatisfiableRangeReturnsTypedError(t *testing.T) {
	client := newIntegrationClient(t)

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.
			From[seededIndexSlice]("sequences").
			Order("id").
			Range(4, 3),
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "PGRST103" {
		t.Errorf("Code = %q, want PGRST103 (unknown relation)", typedError.Code)
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

func TestFiltersWithReserved(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{"just comma", ","},
		{"just dot", "."},
		{"just colon", ":"},
		{"just asterisk", "*"},
		{"just opening parenthesis", "("},
		{"just closing parenthesis", ")"},
		{"just dollar", "$"},
		{"just opening square bracket", "["},
		{"just closing square bracket", "]"},
		{"just semi-colon", ";"},
		{"just opening brace", "{"},
		{"just closing brace", "}"},
		{"just caret", "^"},
		{"just percent", "%"},
		{"just left angle bracket", "<"},
		{"just right angle bracket", ">"},
		{"just equals", "="},
		{"just plus", "+"},
		{"just minus", "-"},
		{"just double quote", `"`},
		{"just single quote", "'"},
		{"just backslash", `\`},
		{"just backspace", "\b"},
		{"just form feed", "\f"},
		{"just newline", "\n"},
		{"just carriage return", "\r"},
		{"just tab", "\t"},
		{"just multitudinous", "众"},
		{"just poop", "💩"},
		{"a comma b", "a,b"},
		{"opening brace inside", "brace{inside"},
		{"closing brace inside", "brace}inside"},
		{"opening paren inside", "paren(open"},
		{"closing paren inside", "close)paren"},
		{"double quoted inside", `say "hi"`},
		{"double quoted", `"The IKEA Effect"`},
		{"backslash inside", `back\slash`},
		{"space padded", " padded "},
		{"empty", ""},
		{"wildcard version tag", "v1.2:rc*"},
	}

	testFilters := []struct {
		name    string
		perform func(builder postgrest.FilterBuilder[justNamedEntity], value string) postgrest.Query[justNamedEntity]
	}{
		{
			"Eq",
			func(builder postgrest.FilterBuilder[justNamedEntity], value string) postgrest.Query[justNamedEntity] {
				return builder.Eq("text", value)
			},
		},
		{
			"Contains",
			func(builder postgrest.FilterBuilder[justNamedEntity], value string) postgrest.Query[justNamedEntity] {
				return builder.Contains("array", value)
			},
		},
	}

	client := newIntegrationClient(t)

	for _, testCase := range testCases {
		for _, testFilter := range testFilters {
			t.Run(testCase.name+" via "+testFilter.name, func(t *testing.T) {
				rows, response, err := postgrest.Collect(
					t.Context(),
					client,
					testFilter.perform(postgrest.From[justNamedEntity]("⚠ reserved ⚠").Select("name"), testCase.value),
				)
				if err != nil {
					t.Fatalf("Collect: %v", err)
				}

				// No count was requested, so PostgREST reports an unknown total ("0-2/*").
				testkit.AssertOKResponse(t, response)
				if len(rows) != 1 {
					t.Fatalf("row count = %d, want 1 (seed drifted?)", len(rows))
				}

				if rows[0].Name != testCase.name {
					t.Errorf("want %q, got %q", testCase.name, rows[0].Name)
				}
			})
		}
	}
}
