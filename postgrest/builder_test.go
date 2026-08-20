package postgrest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
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

// TestCollectDecodesRows pins the read happy path end to end: rows decode
// into the caller's type and the request reaches the wire with the cleaned
// select list, the /rest/v1 path, the injected apikey header and the
// plural-form Accept header (the SDK never requests
// application/vnd.pgrst.object+json).
func TestCollectDecodesRows(t *testing.T) {
	var observed *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed = request.Clone(request.Context())
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode([]instrument{{ID: 1, Name: "violin"}, {ID: 2, Name: "flute"}})
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
		_ = json.NewEncoder(writer).Encode([]instrument{{ID: 1, Name: "violin"}, {ID: 2, Name: "flute"}})
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
				_, _ = writer.Write([]byte(`[]`))
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

// TestCollectEmptyResultYieldsEmptySlice pins Collect's documented
// empty-result contract: a JSON [] decodes to an empty, non-nil slice, so
// callers range over results without a nil check.
func TestCollectEmptyResultYieldsEmptySlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`[]`))
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
		_, _ = writer.Write([]byte(`[{"id":1,"name":"violin"},{"id":2,"name":"flute"}]`))
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
				_, _ = writer.Write([]byte(`[{"id":1,"name":"violin"},{"id":2,"name":"flute"}]`))
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
		_, _ = writer.Write([]byte(`[]`))
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
		_, _ = writer.Write([]byte(`[]`))
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

				_, _ = writer.Write([]byte(`[]`))
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
		_, _ = writer.Write([]byte(`{"message":"relation \"public.missing\" does not exist","code":"42P01","details":null,"hint":null}`))
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
		_, _ = writer.Write([]byte(`<html>upstream unavailable</html>`))
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
		_, _ = writer.Write([]byte(`{not json`))
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
	var typedError *postgrest.Error
	if errors.As(err, &typedError) {
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
		_, _ = writer.Write([]byte(`[]`))
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
