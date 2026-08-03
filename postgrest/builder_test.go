package postgrest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
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

	rows, response, err := postgrest.Collect[instrument](
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From("instruments").
			Select("id, name"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(rows) != 2 || rows[0].Name != "violin" {
		t.Errorf("rows = %+v", rows)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
	}
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

// TestCollectEmptyResultYieldsEmptySlice pins Collect's documented
// empty-result contract: a JSON [] decodes to an empty, non-nil slice, so
// callers range over results without a nil check.
func TestCollectEmptyResultYieldsEmptySlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()

	rows, _, err := postgrest.Collect[instrument](
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From("instruments").
			Select(""),
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

			_, response, err := postgrest.Collect[instrument](
				t.Context(),
				newTestClient(t, server),
				postgrest.
					From("instruments").
					Select(""),
			)
			if err != nil {
				t.Fatalf("Collect: %v", err)
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

	if _, _, err := postgrest.Collect[instrument](
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From("instruments").
			Select(""),
	); err != nil {
		t.Fatalf("Collect: %v", err)
	}
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

	if _, _, err := postgrest.Collect[instrument](
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From("instruments").
			Select(`"full name", id`),
	); err != nil {
		t.Fatalf("Collect: %v", err)
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

	rows, response, err := postgrest.Collect[instrument](
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From("missing").
			Select(""),
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.HTTPStatus != http.StatusNotFound || typedError.Code != "42P01" {
		t.Errorf("typedError = %+v", typedError)
	}
	if rows != nil {
		t.Errorf("rows = %+v, want nil on error", rows)
	}
	if response != (postgrest.Response{}) {
		t.Errorf("response = %+v, want zero value on error", response)
	}
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

	_, _, err := postgrest.Collect[instrument](
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From("instruments").
			Select(""),
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

	rows, response, err := postgrest.Collect[instrument](
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From("instruments").
			Select(""),
	)

	if err == nil || !strings.Contains(err.Error(), "decoding response") {
		t.Fatalf("want wrapped decoding failure, got %v", err)
	}
	var typedError *postgrest.Error
	if errors.As(err, &typedError) {
		t.Errorf("decode failure must not be an *Error: %v", typedError)
	}
	if rows != nil {
		t.Errorf("rows = %+v, want nil on error", rows)
	}
	if response != (postgrest.Response{}) {
		t.Errorf("response = %+v, want zero value on error", response)
	}
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

	_, _, err := postgrest.Collect[instrument](
		t.Context(),
		newTestClient(t, server),
		postgrest.
			From("").
			Select("id"),
	)

	if !errors.Is(err, postgrest.ErrMissingTable) {
		t.Errorf("want ErrMissingTable, got %v", err)
	}
}

// TestCollectReportsMissingClientWithoutIO pins ErrMissingClient's contract:
// a nil client is rejected before anything else is inspected, so the
// sentinel is reported instead of a panic on the absent client.
func TestCollectReportsMissingClientWithoutIO(t *testing.T) {
	_, _, err := postgrest.Collect[instrument](
		t.Context(),
		nil,
		postgrest.From("instruments").Select("id"),
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

	_, _, err := postgrest.Collect[instrument](
		ctx,
		newTestClient(t, server),
		postgrest.
			From("instruments").
			Select(""),
	)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled in chain, got %v", err)
	}
}

// TestBuildersForkIndependently pins builder immutability at the wire: one
// QueryBuilder forked into two divergent chains sends two independent
// requests, neither observing the other. The backing-slice aliasing
// subtlety underneath is pinned by the internal request package's tests.
func TestBuildersForkIndependently(t *testing.T) {
	selects := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		selects <- request.URL.Query().Get("select")
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	base := postgrest.From("instruments")
	if _, _, err := postgrest.Collect[instrument](t.Context(), client, base.Select("id")); err != nil {
		t.Fatalf("first chain: %v", err)
	}
	if _, _, err := postgrest.Collect[instrument](t.Context(), client, base.Select("name")); err != nil {
		t.Fatalf("second chain: %v", err)
	}

	first, second := <-selects, <-selects
	if first != "id" || second != "name" {
		t.Errorf("selects = %q, %q; want id then name", first, second)
	}
}
