package postgrest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/postgrest/internal/respond"
	"github.com/supabase/supabase-go/postgrest/internal/testkit"
)

type instrument struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// newTestClient wires a postgrest.Client at the given test server, so tests
// exercise the same transport (apikey header injection included) consumers use.
func newTestClient(t *testing.T, server *httptest.Server) *postgrest.Client {
	t.Helper()
	projectConfiguration, err := configuration.New(core.ModulePathPostgrest, server.URL, "TEST_API_KEY")
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	return postgrest.NewFromConfiguration(projectConfiguration)
}

// newSingularityServer answers every request as PostgREST refuses a singular
// request whose result held no row or more than one: HTTP 406 with code
// PGRST116 and the given details. The server closes when the test ends.
func newSingularityServer(t *testing.T, details string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotAcceptable)
		respond.JSON(t, writer, map[string]any{
			"code":    "PGRST116",
			"message": "JSON object requested, multiple (or no) rows returned",
			"details": details,
			"hint":    nil,
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// TestCollectDecodesRows pins the read happy path end to end: rows decode
// into the caller's type and the request reaches the wire with the cleaned
// select list, the /rest/v1 path, the injected apikey header and the
// plural-form Accept header (application/json - the singular form belongs
// to CollectSingle alone).
func TestCollectDecodesRows(t *testing.T) {
	var observed *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed = request.Clone(request.Context())
		writer.Header().Set("Content-Type", "application/json")
		respond.JSON(t, writer, []instrument{{ID: 1, Name: "violin"}, {ID: 2, Name: "flute"}})
	}))
	defer server.Close()

	rows, response, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From[instrument]("instruments").
			Select("id, name"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(rows) != 2 || rows[0].Name != "violin" {
		t.Errorf("rows = %+v", rows)
	}
	testkit.AssertOKResponse(t, response)
	if got, want := observed.URL.Path, "/rest/v1/instruments"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := observed.URL.Query().Get("select"), "id,name"; got != want {
		t.Errorf("select = %q, want %q (whitespace should be stripped)", got, want)
	}
	if got := observed.Header.Get("apikey"); got != "TEST_API_KEY" {
		t.Errorf("apikey header = %q; transport injection is not wired", got)
	}
	if got := observed.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
}

// TestCollectBareFrom pins the select-less read: a QueryBuilder is already a
// complete query, so Collect accepts a bare From and the request reaches the
// wire with an empty query string - no select parameter is invented, leaving
// the all-columns projection to PostgREST's documented default of *.
func TestCollectBareFrom(t *testing.T) {
	var observed *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed = request.Clone(request.Context())
		writer.Header().Set("Content-Type", "application/json")
		respond.JSON(t, writer, []instrument{{ID: 1, Name: "violin"}, {ID: 2, Name: "flute"}})
	}))
	defer server.Close()

	rows, response, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(rows) != 2 || rows[0].Name != "violin" {
		t.Errorf("rows = %+v", rows)
	}
	testkit.AssertOKResponse(t, response)
	if got, want := observed.Method, http.MethodGet; got != want {
		t.Errorf("method = %q, want %q", got, want)
	}
	if got, want := observed.URL.Path, "/rest/v1/instruments"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got := observed.URL.RawQuery; got != "" {
		t.Errorf("query = %q, want empty (a bare From sends no select parameter)", got)
	}
}

// TestQueryBuilderPromotedModifierSkipsSelect pins promotion at the root:
// every FilterBuilder method is available directly on a QueryBuilder, so a
// modifier chains off From without a Select and no select parameter appears
// on the wire.
func TestQueryBuilderPromotedModifierSkipsSelect(t *testing.T) {
	testCases := []struct {
		name  string
		query postgrest.Query[instrument]
		want  string
	}{
		{
			name:  "limit",
			query: postgrest.From[instrument]("instruments").Limit(3),
			want:  "limit=3",
		},
		{
			name:  "order refined through its wrappers",
			query: postgrest.From[instrument]("instruments").Order("name").Descending(),
			want:  "order=name.desc",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var observed *http.Request
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				observed = request.Clone(request.Context())
				respond.Body(t, writer, []byte(`[]`))
			}))
			defer server.Close()

			rows, response, err := postgrest.Collect(t.Context(), newTestClient(t, server), testCase.query)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			if len(rows) != 0 {
				t.Errorf("len(rows) = %d, want 0", len(rows))
			}
			testkit.AssertOKResponse(t, response)
			if got := observed.URL.RawQuery; got != testCase.want {
				t.Errorf("query = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestCollectSendsRangeValuesThroughScalarFilters pins the wire text of a
// Range given to the filter methods whose value is typed any: the literal
// rides bare in single-value position and double-quoted inside list
// position, where its commas would otherwise split the elements.
func TestCollectSendsRangeValuesThroughScalarFilters(t *testing.T) {
	b := postgrest.From[instrument]("instruments")
	testCases := []struct {
		name  string
		query postgrest.Query[instrument]
		want  string
	}{
		{
			name:  "eq carries the literal bare",
			query: b.Eq("active_during", postgrest.NewRange[int]().FromInclusive(2).ToExclusive(7)),
			want:  "eq.[2,7)",
		},
		{
			name: "in double-quotes each range element",
			query: b.In("active_during",
				postgrest.NewRange[int]().FromInclusive(1).ToExclusive(3),
				postgrest.NewRange[int]().FromInclusive(4).ToExclusive(6)),
			want: `in.("[1,3)","[4,6)")`,
		},
		{
			name:  "isdistinct carries the empty range bare",
			query: b.IsDistinct("active_during", postgrest.EmptyRange()),
			want:  "isdistinct.empty",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var observed *http.Request
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				observed = request.Clone(request.Context())
				respond.Body(t, writer, []byte(`[]`))
			}))
			defer server.Close()

			rows, response, err := postgrest.Collect(t.Context(), newTestClient(t, server), testCase.query)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			if len(rows) != 0 {
				t.Errorf("len(rows) = %d, want 0", len(rows))
			}
			testkit.AssertOKResponse(t, response)
			if got := observed.URL.Query().Get("active_during"); got != testCase.want {
				t.Errorf("active_during = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestCollectEmptyResultYieldsEmptySlice pins Collect's documented
// empty-result contract: a JSON [] decodes to an empty, non-nil slice, so
// callers range over results without a nil check.
func TestCollectEmptyResultYieldsEmptySlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		respond.Body(t, writer, []byte(`[]`))
	}))
	defer server.Close()

	rows, response, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if rows == nil {
		t.Error("rows = nil, want empty non-nil slice")
	}
	if len(rows) != 0 {
		t.Errorf("len(rows) = %d, want 0", len(rows))
	}
	testkit.AssertOKResponse(t, response)
}

// TestCollectPreservesRawRowBytes pins the json.RawMessage half of the
// documented dynamic-container contract: Collect[json.RawMessage] defers
// per-row decoding, each element carrying its row's JSON verbatim so
// consumers can route or decode rows individually. The map[string]any half
// is demonstrated by ExampleCollect_schemaDriven.
func TestCollectPreservesRawRowBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		respond.Body(t, writer, []byte(`[{"id":1,"name":"violin"},{"id":2,"name":"flute"}]`))
	}))
	defer server.Close()

	rows, response, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[json.RawMessage]("instruments"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2", len(rows))
	}
	if got, want := string(rows[0]), `{"id":1,"name":"violin"}`; got != want {
		t.Errorf("rows[0] = %s, want %s (row bytes must pass through verbatim)", got, want)
	}
	testkit.AssertOKResponse(t, response)
}

// TestCollectReturnsResponseMetadata pins Response's wiring from the
// Content-Range header: a reported total populates Count and an absent or
// unknown one is -1. Parser edge cases live in TestParseContentRangeTotal;
// this test proves the header value actually flows through the pipeline.
func TestCollectReturnsResponseMetadata(t *testing.T) {
	testCases := []struct {
		name         string
		contentRange string
		wantCount    int64
	}{
		{name: "total reported", contentRange: "0-1/2", wantCount: 2},
		{name: "total unknown", contentRange: "0-1/*", wantCount: -1},
		{name: "header absent", contentRange: "", wantCount: -1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if testCase.contentRange != "" {
					writer.Header().Set("Content-Range", testCase.contentRange)
				}
				respond.Body(t, writer, []byte(`[{"id":1,"name":"violin"},{"id":2,"name":"flute"}]`))
			}))
			defer server.Close()

			rows, response, err := postgrest.Collect(
				t.Context(),
				newTestClient(t, server),
				postgrest.From[instrument]("instruments"),
			)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			if len(rows) != 2 {
				t.Errorf("row count = %d, want 2", len(rows))
			}
			if response.Count != testCase.wantCount {
				t.Errorf("Count = %d, want %d", response.Count, testCase.wantCount)
			}
			if response.HTTPStatus != http.StatusOK {
				t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
			}
		})
	}
}

// TestCollectEmptySelectMeansAllColumns pins Select's documented contract
// that an empty column list selects all columns, exactly as "*" does.
func TestCollectEmptySelectMeansAllColumns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.URL.Query().Get("select"); got != "*" {
			t.Errorf("select = %q, want *", got)
		}
		respond.Body(t, writer, []byte(`[]`))
	}))
	defer server.Close()

	rows, response, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From[instrument]("instruments").

			// Method-Under-Test:
			// An explicit inclusion of query `select=*`, rather than relying upon the identical,
			// implicit default the PostgREST service implements when select is not present in the query
			Select(""),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("len(rows) = %d, want 0", len(rows))
	}
	testkit.AssertOKResponse(t, response)
}

// TestCollectPreservesQuotedIdentifiersInSelect pins the other half of
// Select's cleaning contract: whitespace is stripped from the column list
// except inside double-quoted identifiers, which must reach the wire intact.
func TestCollectPreservesQuotedIdentifiersInSelect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got, want := request.URL.Query().Get("select"), `"full name",id`; got != want {
			t.Errorf("select = %q, want %q (quoted whitespace must survive)", got, want)
		}
		respond.Body(t, writer, []byte(`[]`))
	}))
	defer server.Close()

	rows, response, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From[instrument]("instruments").
			Select(`"full name", id`),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("len(rows) = %d, want 0", len(rows))
	}
	testkit.AssertOKResponse(t, response)
}

// TestCollectAppliesRange pins behavior, especially around edge cases, when
// using the Range method. This includes how it interacts with previous calls
// to either itself or Limit.
func TestCollectAppliesRange(t *testing.T) {
	testCases := []struct {
		name       string
		perform    func(builder postgrest.QueryBuilder[json.RawMessage]) postgrest.Query[json.RawMessage]
		wantOffset string
		wantLimit  string
	}{
		{
			name: "first page",
			perform: func(builder postgrest.QueryBuilder[json.RawMessage]) postgrest.Query[json.RawMessage] {
				return builder.Range(0, 9)
			},
			wantOffset: "0",
			wantLimit:  "10",
		},
		{
			name: "interior window",
			perform: func(builder postgrest.QueryBuilder[json.RawMessage]) postgrest.Query[json.RawMessage] {
				return builder.Range(1, 3)
			},
			wantOffset: "1",
			wantLimit:  "3",
		},
		{
			name: "single row",
			perform: func(builder postgrest.QueryBuilder[json.RawMessage]) postgrest.Query[json.RawMessage] {
				return builder.Range(2, 2)
			},
			wantOffset: "2",
			wantLimit:  "1",
		},
		{
			name: "empty window", // impotent, zero limit is forwarded
			perform: func(builder postgrest.QueryBuilder[json.RawMessage]) postgrest.Query[json.RawMessage] {
				return builder.Range(2, 1)
			},
			wantOffset: "2",
			wantLimit:  "0",
		},
		{
			name: "negative cap", // negative limit is forwarded verbatim
			perform: func(builder postgrest.QueryBuilder[json.RawMessage]) postgrest.Query[json.RawMessage] {
				return builder.Range(3, 1)
			},
			wantOffset: "3",
			wantLimit:  "-1",
		},
		{
			name: "negative start", // negative offset is forwarded verbatim
			perform: func(builder postgrest.QueryBuilder[json.RawMessage]) postgrest.Query[json.RawMessage] {
				return builder.Range(-1, 1)
			},
			wantOffset: "-1",
			wantLimit:  "3",
		},
		{
			name: "overrides limit",
			perform: func(builder postgrest.QueryBuilder[json.RawMessage]) postgrest.Query[json.RawMessage] {
				return builder.Limit(5).Range(1, 9)
			},
			wantOffset: "1",
			wantLimit:  "9",
		},
		{
			name: "partially overridden by limit",
			perform: func(builder postgrest.QueryBuilder[json.RawMessage]) postgrest.Query[json.RawMessage] {
				return builder.Range(10, 19).Limit(5)
			},
			wantOffset: "10",
			wantLimit:  "5",
		},
		{
			name: "overrides self",
			perform: func(builder postgrest.QueryBuilder[json.RawMessage]) postgrest.Query[json.RawMessage] {
				return builder.Range(0, 9).Range(10, 17)
			},
			wantOffset: "10",
			wantLimit:  "8",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				values, err := url.ParseQuery(request.URL.RawQuery)
				if err != nil {
					t.Fatalf("request query parse: %v, for raw query %q", err, request.URL.RawQuery)
				}
				if len(values) != 2 {
					t.Fatalf("request query parse want value count got %d, want %d, for raw query %q", len(values), 2, request.URL.RawQuery)
				}

				offsetValues := values["offset"]
				if len(offsetValues) != 1 || offsetValues[0] != testCase.wantOffset {
					t.Fatalf("offsetValues got = %q, want single %q", offsetValues, testCase.wantOffset)
				}

				limitValues := values["limit"]
				if len(limitValues) != 1 || limitValues[0] != testCase.wantLimit {
					t.Fatalf("limitValues got = %q, want single %q", limitValues, testCase.wantLimit)
				}

				respond.Body(t, writer, []byte(`[]`))
			}))
			defer server.Close()

			rows, response, err := postgrest.Collect(
				t.Context(),
				newTestClient(t, server),
				testCase.perform(postgrest.From[json.RawMessage]("some table")),
			)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			if len(rows) != 0 {
				t.Errorf("row count = %d, want 0", len(rows))
			}
			testkit.AssertOKResponse(t, response)
		})
	}
}

// TestCollectReturnsTypedErrorForPostgRESTFailure pins the failure half of
// the return contract: a non-2xx answer surfaces as an *Error carrying the
// parsed body, while rows and Response stay zero - a failing status lives on
// the error, never on Response.
func TestCollectReturnsTypedErrorForPostgRESTFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusNotFound)
		respond.Body(t, writer, []byte(`{"message":"relation \"public.missing\" does not exist","code":"42P01","details":null,"hint":null}`))
	}))
	defer server.Close()

	rows, response, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("missing"),
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.HTTPStatus != http.StatusNotFound || typedError.Code != "42P01" {
		t.Errorf("typedError = %+v", typedError)
	}
	testkit.AssertNoResults(t, rows, response)
}

// TestCollectPreservesUnparsableErrorBody pins newError's fallback: a
// non-2xx body that is not the documented PostgREST error JSON (HTML from an
// intermediary, for example) still surfaces as an *Error, with the raw body
// preserved in Message so no diagnostic information is lost.
func TestCollectPreservesUnparsableErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		respond.Body(t, writer, []byte(`<html>upstream unavailable</html>`))
	}))
	defer server.Close()

	rows, response, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments"),
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.HTTPStatus != http.StatusServiceUnavailable {
		t.Errorf("HTTPStatus = %d, want 503", typedError.HTTPStatus)
	}
	if typedError.Message != `<html>upstream unavailable</html>` {
		t.Errorf("Message = %q, want the raw body preserved", typedError.Message)
	}
	if typedError.Code != "" {
		t.Errorf("Code = %q, want empty for an unparsable body", typedError.Code)
	}
	testkit.AssertNoResults(t, rows, response)
}

// TestCollectWrapsDecodeFailure pins the remaining failure class: a 2xx
// answer whose body does not decode is a wrapped failure, not an *Error
// (that type means the server answered with an error), and rows and
// Response stay zero.
func TestCollectWrapsDecodeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		respond.Body(t, writer, []byte(`{not json`))
	}))
	defer server.Close()

	rows, response, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments"),
	)

	if err == nil || !strings.Contains(err.Error(), "decoding response") {
		t.Fatalf("want wrapped decoding failure, got %v", err)
	}
	if typedError, ok := errors.AsType[*postgrest.Error](err); ok {
		t.Errorf("decode failure must not be an *Error: %v", typedError)
	}
	testkit.AssertNoResults(t, rows, response)
}

// TestCollectReportsMissingTableWithoutIO pins ErrMissingTable's contract:
// an empty table name is rejected before any request is sent, so the
// sentinel costs no network round trip and the handler proves the absence
// of I/O by failing the test if reached.
func TestCollectReportsMissingTableWithoutIO(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("no HTTP request should be made for an empty table name")
	}))
	defer server.Close()

	rows, response, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From[instrument]("").
			Select("id"),
	)

	if !errors.Is(err, postgrest.ErrMissingTable) {
		t.Errorf("want ErrMissingTable, got %v", err)
	}
	testkit.AssertNoResults(t, rows, response)
}

// TestCollectReportsMissingClientWithoutIO pins ErrMissingClient's contract:
// a nil client is rejected before anything else is inspected, so the
// sentinel is reported instead of a panic on the absent client.
func TestCollectReportsMissingClientWithoutIO(t *testing.T) {
	_, _, err := postgrest.Collect(
		t.Context(),
		nil,
		postgrest.From[instrument]("instruments").Select("id"),
	)

	if !errors.Is(err, postgrest.ErrMissingClient) {
		t.Errorf("want ErrMissingClient, got %v", err)
	}
}

// TestCollectHonoursContextCancellation pins the context contract:
// cancelling the caller's context aborts the in-flight request and the
// cause stays matchable with errors.Is through the wrapped chain.
func TestCollectHonoursContextCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-started
		cancel()
	}()

	rows, response, err := postgrest.Collect(
		ctx,
		newTestClient(t, server),
		postgrest.From[instrument]("instruments"),
	)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled in chain, got %v", err)
	}
	testkit.AssertNoResults(t, rows, response)
}

// TestBuildersForkIndependently pins builder immutability at the wire: one
// QueryBuilder forked into divergent chains - two projections and a promoted
// modifier - sends independent requests, none observing another. The
// backing-slice aliasing subtlety underneath is pinned by the internal
// request package's tests.
func TestBuildersForkIndependently(t *testing.T) {
	queries := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		queries <- request.URL.RawQuery
		respond.Body(t, writer, []byte(`[]`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	base := postgrest.From[instrument]("instruments")
	for _, fork := range []postgrest.Query[instrument]{base.Select("id"), base.Select("name"), base.Limit(1)} {
		rows, response, err := postgrest.Collect(t.Context(), client, fork)
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if len(rows) != 0 {
			t.Errorf("len(rows) = %d, want 0", len(rows))
		}
		testkit.AssertOKResponse(t, response)
	}

	first, second, third := <-queries, <-queries, <-queries
	if first != "select=id" || second != "select=name" || third != "limit=1" {
		t.Errorf("queries = %q, %q, %q; want select=id, select=name then limit=1", first, second, third)
	}
}

// TestCollectSingleDecodesRow pins the singular read end to end: the request
// reaches the wire with the singular-form Accept header and the lone JSON
// object decodes into the caller's type directly, no array wrapper involved.
func TestCollectSingleDecodesRow(t *testing.T) {
	var observed *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed = request.Clone(request.Context())
		writer.Header().Set("Content-Type", "application/vnd.pgrst.object+json")
		respond.JSON(t, writer, instrument{ID: 1, Name: "violin"})
	}))
	defer server.Close()

	row, response, err := postgrest.CollectSingle(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments").Eq("id", 1),
	)
	if err != nil {
		t.Fatalf("CollectSingle: %v", err)
	}
	if row != (instrument{ID: 1, Name: "violin"}) {
		t.Errorf("row = %+v", row)
	}
	testkit.AssertOKResponse(t, response)
	if got, want := observed.Header.Get("Accept"), "application/vnd.pgrst.object+json"; got != want {
		t.Errorf("Accept = %q, want %q", got, want)
	}
}

// TestCollectSingleReturnsZeroValuesOnError pins the failure contract: the
// server's 406 PGRST116 answer surfaces as *Error with its fields untouched,
// wrapping the sentinel its details stand for in either wording PostgREST
// has used, and the returned row and Response are zero values.
func TestCollectSingleReturnsZeroValuesOnError(t *testing.T) {
	testCases := []struct {
		name    string
		details string
		want    error
	}{
		{"no row", "The result contains 0 rows", postgrest.ErrNoRows},
		{"no row, older wording", "Results contain 0 rows, application/vnd.pgrst.object+json requires 1 row", postgrest.ErrNoRows},
		{"two rows", "The result contains 2 rows", postgrest.ErrTooManyRows},
		{"ten rows", "The result contains 10 rows", postgrest.ErrTooManyRows},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			row, response, err := postgrest.CollectSingle(
				t.Context(),
				newTestClient(t, newSingularityServer(t, testCase.details)),
				postgrest.From[instrument]("instruments"),
			)

			var typedError *postgrest.Error
			if !errors.As(err, &typedError) {
				t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
			}
			if typedError.Code != "PGRST116" || typedError.HTTPStatus != http.StatusNotAcceptable || typedError.Details != testCase.details {
				t.Errorf("error = %+v, want the server's fields untouched", typedError)
			}
			if !errors.Is(err, testCase.want) {
				t.Errorf("err = %v, want it to wrap %v", err, testCase.want)
			}
			if row != (instrument{}) {
				t.Errorf("row = %+v, want zero value", row)
			}
			if response != (postgrest.Response{}) {
				t.Errorf("response = %+v, want zero value", response)
			}
		})
	}
}

// TestCollectSingleMaybeDecodesRow pins the tolerant singular read end to
// end: the request reaches the wire with the default plural Accept header -
// never the singular form - and the lone row the Content-Range header counts
// is unwrapped alongside true.
func TestCollectSingleMaybeDecodesRow(t *testing.T) {
	var observed *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed = request.Clone(request.Context())
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Range", "0-0/*")
		respond.JSON(t, writer, []instrument{{ID: 1, Name: "violin"}})
	}))
	defer server.Close()

	row, found, response, err := postgrest.CollectSingleMaybe(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments").Eq("id", 1),
	)
	if err != nil {
		t.Fatalf("CollectSingleMaybe: %v", err)
	}
	if !found {
		t.Error("found = false, want true")
	}
	if row != (instrument{ID: 1, Name: "violin"}) {
		t.Errorf("row = %+v", row)
	}
	testkit.AssertOKResponse(t, response)
	if got, want := observed.Header.Get("Accept"), "application/json"; got != want {
		t.Errorf("Accept = %q, want %q", got, want)
	}
}

// TestCollectSingleMaybeNoRows pins absence as an ordinary outcome: a result
// the Content-Range header counts as no row yields the zero value alongside
// false with no error, and the Response still carries the successful
// request's metadata.
func TestCollectSingleMaybeNoRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Range", "*/*")
		respond.Body(t, writer, []byte(`[]`))
	}))
	defer server.Close()

	row, found, response, err := postgrest.CollectSingleMaybe(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments").Eq("id", 404),
	)
	if err != nil {
		t.Fatalf("CollectSingleMaybe: %v", err)
	}
	if found {
		t.Error("found = true, want false")
	}
	if row != (instrument{}) {
		t.Errorf("row = %+v, want zero value", row)
	}
	testkit.AssertOKResponse(t, response)
}

// TestCollectSingleMaybeTooManyRows pins cardinality enforcement on a read:
// a result the Content-Range header counts as more than one row fails with
// ErrTooManyRows naming the count, built on the client and so no *Error,
// with zero values throughout.
func TestCollectSingleMaybeTooManyRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Range", "0-1/*")
		respond.JSON(t, writer, []instrument{{ID: 1, Name: "violin"}, {ID: 2, Name: "flute"}})
	}))
	defer server.Close()

	row, found, response, err := postgrest.CollectSingleMaybe(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments"),
	)

	if !errors.Is(err, postgrest.ErrTooManyRows) {
		t.Fatalf("err = %v, want ErrTooManyRows", err)
	}
	if !strings.HasSuffix(err.Error(), "(2 rows)") {
		t.Errorf("err = %q, want the count named", err)
	}
	if _, isServerError := errors.AsType[*postgrest.Error](err); isServerError {
		t.Errorf("err = %v, want no *postgrest.Error built on the client", err)
	}
	if found {
		t.Error("found = true, want false")
	}
	if row != (instrument{}) {
		t.Errorf("row = %+v, want zero value", row)
	}
	if response != (postgrest.Response{}) {
		t.Errorf("response = %+v, want zero value", response)
	}
}

// TestCollectSingleMaybeReturnsZeroValuesOnError pins that server failures
// pass through unchanged rather than being mistaken for cardinality
// outcomes: a non-2xx answer surfaces as *Error with zero values throughout.
func TestCollectSingleMaybeReturnsZeroValuesOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		respond.Body(t, writer, []byte(`{"code":"22P02","message":"invalid input syntax"}`))
	}))
	defer server.Close()

	row, found, response, err := postgrest.CollectSingleMaybe(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments"),
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "22P02" || typedError.HTTPStatus != http.StatusBadRequest {
		t.Errorf("error = %+v", typedError)
	}
	if found {
		t.Error("found = true, want false")
	}
	if row != (instrument{}) {
		t.Errorf("row = %+v, want zero value", row)
	}
	if response != (postgrest.Response{}) {
		t.Errorf("response = %+v, want zero value", response)
	}
}

// TestCollectSingleMaybeCountsReadsFromContentRange pins where a read's row
// count comes from: the range of the Content-Range header, and the body's
// elements only without one, so that a function's single array value, which
// the header counts as one row, is the row whole rather than its first
// element.
func TestCollectSingleMaybeCountsReadsFromContentRange(t *testing.T) {
	testCases := []struct {
		name         string
		contentRange string
		body         string
		wantRow      string
		wantFound    bool
		wantTooMany  bool
	}{
		{"one row by the header", "0-0/*", `[{"id":1}]`, `{"id":1}`, true, false},
		{"two rows by the header", "0-1/57", `[{"id":1},{"id":2}]`, "", false, true},
		{"no row by the header", "*/*", `[]`, "", false, false},
		{"two rows by the body without a header", "", `[{"id":1},{"id":2}]`, "", false, true},
		{"no row by the body without a header", "", `[]`, "", false, false},
		{"one row whose body is a function's array value", "0-0/*", `[1,2,3]`, `[1,2,3]`, true, false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if testCase.contentRange != "" {
					writer.Header().Set("Content-Range", testCase.contentRange)
				}
				respond.Body(t, writer, []byte(testCase.body))
			}))
			defer server.Close()

			row, found, response, err := postgrest.CollectSingleMaybe(
				t.Context(),
				newTestClient(t, server),
				postgrest.RPC[json.RawMessage]("numbers").Rows(),
			)
			if testCase.wantTooMany {
				if !errors.Is(err, postgrest.ErrTooManyRows) {
					t.Fatalf("err = %v, want ErrTooManyRows", err)
				}
				if found || row != nil || response != (postgrest.Response{}) {
					t.Errorf("CollectSingleMaybe = (%s, %v, %+v), want zero values", row, found, response)
				}
				return
			}
			if err != nil {
				t.Fatalf("CollectSingleMaybe: %v", err)
			}
			if found != testCase.wantFound {
				t.Errorf("found = %v, want %v", found, testCase.wantFound)
			}
			if string(row) != testCase.wantRow {
				t.Errorf("row = %s, want %s", row, testCase.wantRow)
			}
			if response.HTTPStatus != http.StatusOK {
				t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
			}
		})
	}
}

// TestCollectSingleMaybeKeepsAFunctionCallPlural pins that a function call is
// a read whatever its method: the POST reaches the wire with the default
// plural Accept header, never the singular form.
func TestCollectSingleMaybeKeepsAFunctionCallPlural(t *testing.T) {
	var observed *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed = request.Clone(request.Context())
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Range", "0-0/*")
		respond.JSON(t, writer, []instrument{{ID: 1, Name: "violin"}})
	}))
	defer server.Close()

	row, found, _, err := postgrest.CollectSingleMaybe(
		t.Context(),
		newTestClient(t, server),
		postgrest.RPC[instrument]("pick_one").Rows(),
	)
	if err != nil {
		t.Fatalf("CollectSingleMaybe: %v", err)
	}
	if !found || row != (instrument{ID: 1, Name: "violin"}) {
		t.Errorf("CollectSingleMaybe = (%+v, %v), want the row found", row, found)
	}
	if observed.Method != http.MethodPost {
		t.Errorf("Method = %s, want POST", observed.Method)
	}
	if got, want := observed.Header.Get("Accept"), "application/json"; got != want {
		t.Errorf("Accept = %q, want %q", got, want)
	}
}

// TestCollectSingleMaybeWriteIsCountedByTheServer pins the write end to end:
// the update reaches the wire with the singular Accept header, asking for its
// representation, so the server counts the rows it affects, and the one
// object it answers with decodes as the row alongside true.
func TestCollectSingleMaybeWriteIsCountedByTheServer(t *testing.T) {
	var observed *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed = request.Clone(request.Context())
		writer.Header().Set("Content-Type", "application/vnd.pgrst.object+json")
		respond.JSON(t, writer, instrument{ID: 1, Name: "viola"})
	}))
	defer server.Close()

	row, found, response, err := postgrest.CollectSingleMaybe(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments").Eq("id", 1).Update(map[string]any{"name": "viola"}),
	)
	if err != nil {
		t.Fatalf("CollectSingleMaybe: %v", err)
	}
	if !found || row != (instrument{ID: 1, Name: "viola"}) {
		t.Errorf("CollectSingleMaybe = (%+v, %v), want the updated row found", row, found)
	}
	testkit.AssertOKResponse(t, response)
	if observed.Method != http.MethodPatch {
		t.Errorf("Method = %s, want PATCH", observed.Method)
	}
	if got, want := observed.Header.Get("Accept"), "application/vnd.pgrst.object+json"; got != want {
		t.Errorf("Accept = %q, want %q", got, want)
	}
	if prefer := observed.Header.Values("Prefer"); !slices.Contains(prefer, "return=representation") {
		t.Errorf("Prefer = %q, want it to carry return=representation", prefer)
	}
}

// TestCollectSingleMaybeWriteAffectingNoRow pins the server's 406 for a write
// that affected no row, whose details carry "0 rows" as whole words in either
// wording PostgREST has used, as the empty result with no error and a
// Response carrying that status and no count. A count of ten rows is not no
// row.
func TestCollectSingleMaybeWriteAffectingNoRow(t *testing.T) {
	testCases := []struct {
		name      string
		details   string
		wantEmpty bool
	}{
		{"no row", "The result contains 0 rows", true},
		{"no row, older wording", "Results contain 0 rows, application/vnd.pgrst.object+json requires 1 row", true},
		{"ten rows", "The result contains 10 rows", false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			row, found, response, err := postgrest.CollectSingleMaybe(
				t.Context(),
				newTestClient(t, newSingularityServer(t, testCase.details)),
				postgrest.From[instrument]("instruments").Eq("id", 404).Delete(),
			)
			if !testCase.wantEmpty {
				if !errors.Is(err, postgrest.ErrTooManyRows) {
					t.Fatalf("err = %v, want ErrTooManyRows", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("CollectSingleMaybe: %v", err)
			}
			if found || row != (instrument{}) {
				t.Errorf("CollectSingleMaybe = (%+v, %v), want the empty result", row, found)
			}
			if want := (postgrest.Response{HTTPStatus: http.StatusNotAcceptable, Count: -1}); response != want {
				t.Errorf("response = %+v, want %+v", response, want)
			}
		})
	}
}

// TestCollectSingleMaybeWriteAffectingMoreThanOneRow pins the server's 406
// for a write it rolled back as the *Error with its fields untouched,
// wrapping ErrTooManyRows, with zero values throughout.
func TestCollectSingleMaybeWriteAffectingMoreThanOneRow(t *testing.T) {
	row, found, response, err := postgrest.CollectSingleMaybe(
		t.Context(),
		newTestClient(t, newSingularityServer(t, "The result contains 2 rows")),
		postgrest.From[instrument]("instruments").Eq("name", "violin").Update(map[string]any{"name": "viola"}),
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "PGRST116" || typedError.HTTPStatus != http.StatusNotAcceptable || typedError.Details != "The result contains 2 rows" {
		t.Errorf("error = %+v, want the server's fields untouched", typedError)
	}
	if !errors.Is(err, postgrest.ErrTooManyRows) {
		t.Errorf("err = %v, want it to wrap ErrTooManyRows", err)
	}
	if found || row != (instrument{}) || response != (postgrest.Response{}) {
		t.Errorf("CollectSingleMaybe = (%+v, %v, %+v), want zero values", row, found, response)
	}
}

// TestCollectSingleMaybeWriteOtherNotAcceptableUnchanged pins that a 406 of
// any other code surfaces as the *Error it is, wrapping nothing.
func TestCollectSingleMaybeWriteOtherNotAcceptableUnchanged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotAcceptable)
		respond.Body(t, writer, []byte(`{"code":"PGRST107","message":"None of these media types are available: text/csv","details":null,"hint":null}`))
	}))
	defer server.Close()

	_, found, response, err := postgrest.CollectSingleMaybe(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments").Eq("id", 1).Delete(),
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "PGRST107" || typedError.HTTPStatus != http.StatusNotAcceptable {
		t.Errorf("error = %+v", typedError)
	}
	if errors.Is(err, postgrest.ErrTooManyRows) || errors.Is(err, postgrest.ErrNoRows) || typedError.Unwrap() != nil {
		t.Errorf("err = %v, want it to wrap nothing", err)
	}
	if found || response != (postgrest.Response{}) {
		t.Errorf("CollectSingleMaybe = (%v, %+v), want zero values", found, response)
	}
}
