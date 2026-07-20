package postgrest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestExecuteDecodesRows(t *testing.T) {
	var observed *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed = request.Clone(request.Context())
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode([]instrument{{ID: 1, Name: "violin"}, {ID: 2, Name: "flute"}})
	}))
	defer server.Close()

	var rows []instrument
	response, err := newTestClient(t, server).From("instruments").Select("id, name").Execute(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Execute: %v", err)
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

func TestExecuteReturnsResponseMetadata(t *testing.T) {
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

			var rows []instrument
			response, err := newTestClient(t, server).From("instruments").Select("").Execute(context.Background(), &rows)
			if err != nil {
				t.Fatalf("Execute: %v", err)
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

func TestExecuteEmptySelectMeansAllColumns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.URL.Query().Get("select"); got != "*" {
			t.Errorf("select = %q, want *", got)
		}
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()

	var rows []instrument
	if _, err := newTestClient(t, server).From("instruments").Select("").Execute(context.Background(), &rows); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestExecuteReturnsTypedErrorForPostgRESTFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte(`{"message":"relation \"public.missing\" does not exist","code":"42P01","details":null,"hint":null}`))
	}))
	defer server.Close()

	var rows []instrument
	response, err := newTestClient(t, server).From("missing").Select("").Execute(context.Background(), &rows)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.HTTPStatus != http.StatusNotFound || typedError.Code != "42P01" {
		t.Errorf("typedError = %+v", typedError)
	}
	if response != (postgrest.Response{}) {
		t.Errorf("response = %+v, want zero value on error", response)
	}
}

func TestExecuteReportsMissingTableWithoutIO(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("no HTTP request should be made for an empty table name")
	}))
	defer server.Close()

	var rows []instrument
	_, err := newTestClient(t, server).From("").Select("id").Execute(context.Background(), &rows)
	if !errors.Is(err, postgrest.ErrMissingTable) {
		t.Errorf("want ErrMissingTable, got %v", err)
	}
}

func TestExecuteHonoursContextCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	var rows []instrument
	_, err := newTestClient(t, server).From("instruments").Select("").Execute(ctx, &rows)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled in chain, got %v", err)
	}
}

func TestBuildersForkIndependently(t *testing.T) {
	selects := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		selects <- request.URL.Query().Get("select")
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()

	// One QueryBuilder, two divergent chains: immutability means neither
	// chain can observe the other.
	base := newTestClient(t, server).From("instruments")
	var rows []instrument
	if _, err := base.Select("id").Execute(context.Background(), &rows); err != nil {
		t.Fatalf("first chain: %v", err)
	}
	if _, err := base.Select("name").Execute(context.Background(), &rows); err != nil {
		t.Fatalf("second chain: %v", err)
	}

	first, second := <-selects, <-selects
	if first != "id" || second != "name" {
		t.Errorf("selects = %q, %q; want id then name", first, second)
	}
}
