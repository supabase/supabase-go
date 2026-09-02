package postgrest_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/supabase/supabase-go/postgrest"
)

// rowWithChannel is a row type encoding/json cannot marshal, used to prove an
// encode failure is parked at build time and surfaces at the executing
// function rather than on the wire.
type rowWithChannel struct {
	Channel chan int `json:"channel"`
}

// captured records the request a [captureServer] received.
type captured struct {
	request *http.Request
	body    []byte
}

// captureServer starts a test server that records the request it receives -
// a clone for its method, path and headers, plus the body separately since a
// clone does not copy the drained body - and answers with the given status
// and response body. The record is read after the request completes.
func captureServer(t *testing.T, status int, responseBody string) (*httptest.Server, *captured) {
	t.Helper()
	record := &captured{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		record.body, _ = io.ReadAll(request.Body)
		record.request = request.Clone(request.Context())
		if responseBody != "" {
			writer.Header().Set("Content-Type", "application/json")
		}
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(responseBody))
	}))
	t.Cleanup(server.Close)
	return server, record
}

// TestInsertSendsRowsAsJSONArray pins the insert wire shape through Execute:
// a POST to the table path carrying the rows as one JSON array under the JSON
// media type, with the apikey injected and - since Execute reads nothing back -
// no Prefer header at all.
func TestInsertSendsRowsAsJSONArray(t *testing.T) {
	server, record := captureServer(t, http.StatusCreated, "")

	response, err := postgrest.Execute(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments").Insert(
			instrument{ID: 1, Name: "violin"},
			instrument{ID: 2, Name: "flute"},
		),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.HTTPStatus != http.StatusCreated {
		t.Errorf("HTTPStatus = %d, want 201", response.HTTPStatus)
	}
	if got, want := record.request.Method, http.MethodPost; got != want {
		t.Errorf("method = %q, want %q", got, want)
	}
	if got, want := record.request.URL.Path, "/rest/v1/instruments"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := string(record.body), `[{"id":1,"name":"violin"},{"id":2,"name":"flute"}]`; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	if got, want := record.request.Header.Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got := record.request.Header.Values("Prefer"); len(got) != 0 {
		t.Errorf("Prefer = %q, want none on an Execute", got)
	}
	if got, want := record.request.Header.Get("apikey"), "TEST_API_KEY"; got != want {
		t.Errorf("apikey = %q, want %q", got, want)
	}
}

// TestInsertZeroRowsSendsEmptyArray pins the zero-row contract: Insert with no
// arguments sends the empty array, never JSON null, leaving the server to rule
// on it.
func TestInsertZeroRowsSendsEmptyArray(t *testing.T) {
	server, record := captureServer(t, http.StatusCreated, "")

	if _, err := postgrest.Execute(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments").Insert(),
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := string(record.body), "[]"; got != want {
		t.Errorf("body = %q, want %q (zero rows must send an empty array, never null)", got, want)
	}
}

// TestCollectAddsRepresentationPreferenceToWrites pins the execution-layer
// representation choice from both sides: Collect over a write adds Prefer:
// return=representation and decodes the returned rows, while Collect over a
// read adds no Prefer at all - the regression pin that keeps reads
// byte-identical on the wire.
func TestCollectAddsRepresentationPreferenceToWrites(t *testing.T) {
	testCases := []struct {
		name       string
		query      postgrest.Query[instrument]
		wantPrefer bool
	}{
		{
			name:       "insert is a write",
			query:      postgrest.From[instrument]("instruments").Insert(instrument{ID: 1, Name: "violin"}),
			wantPrefer: true,
		},
		{
			name:       "update is a write",
			query:      postgrest.From[instrument]("instruments").Eq("id", 1).Update(map[string]any{"name": "violin"}),
			wantPrefer: true,
		},
		{
			name:       "delete is a write",
			query:      postgrest.From[instrument]("instruments").Eq("id", 1).Delete(),
			wantPrefer: true,
		},
		{
			name:       "read is unchanged",
			query:      postgrest.From[instrument]("instruments").Select("id, name"),
			wantPrefer: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server, record := captureServer(t, http.StatusOK, `[{"id":1,"name":"violin"}]`)

			rows, _, err := postgrest.Collect(t.Context(), newTestClient(t, server), testCase.query)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			if len(rows) != 1 || rows[0] != (instrument{ID: 1, Name: "violin"}) {
				t.Errorf("rows = %+v, want the decoded row", rows)
			}
			prefer := record.request.Header.Values("Prefer")
			switch {
			case testCase.wantPrefer && !slices.Contains(prefer, "return=representation"):
				t.Errorf("Prefer = %q, want it to carry return=representation", prefer)
			case !testCase.wantPrefer && len(prefer) != 0:
				t.Errorf("Prefer = %q, want none on a read", prefer)
			}
		})
	}
}

// TestInsertReturningProjectsColumns pins Returning as a pure projection: it
// writes the cleaned select parameter with replace semantics, an empty string
// means every column, and - executed through Execute - the parameter travels
// with no return preference.
func TestInsertReturningProjectsColumns(t *testing.T) {
	base := postgrest.From[instrument]("instruments").Insert(instrument{ID: 1, Name: "violin"})
	testCases := []struct {
		name       string
		builder    postgrest.MutationBuilder[instrument]
		wantSelect string
	}{
		{
			name:       "columns cleaned",
			builder:    base.Returning("id, name"),
			wantSelect: "id,name",
		},
		{
			name:       "later returning replaces earlier",
			builder:    base.Returning("id").Returning("name"),
			wantSelect: "name",
		},
		{
			name:       "empty means all columns",
			builder:    base.Returning(""),
			wantSelect: "*",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server, record := captureServer(t, http.StatusCreated, "")

			if _, err := postgrest.Execute(t.Context(), newTestClient(t, server), testCase.builder); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got := record.request.URL.Query().Get("select"); got != testCase.wantSelect {
				t.Errorf("select = %q, want %q", got, testCase.wantSelect)
			}
			if got := record.request.Header.Values("Prefer"); len(got) != 0 {
				t.Errorf("Prefer = %q, want none under Execute", got)
			}
		})
	}
}

// TestInsertMarshalFailureSurfacesAtExecute pins deferred build failure: a row
// type encoding/json cannot marshal fails at the executing function, not on
// the wire, and no request is made.
func TestInsertMarshalFailureSurfacesAtExecute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("no HTTP request should be made when the payload cannot be marshalled")
	}))
	defer server.Close()

	_, err := postgrest.Execute(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[rowWithChannel]("instruments").Insert(rowWithChannel{}),
	)
	if err == nil {
		t.Fatal("Execute succeeded, want a marshal error")
	}
	if !strings.Contains(err.Error(), "encoding insert rows") {
		t.Errorf("err = %v, want it to mention encoding insert rows", err)
	}
}

// TestExecuteReturnsResponseWithoutDecoding pins that Execute decodes no body:
// a 201 with an empty body yields the response metadata and a nil error, where
// a read function would fail to decode the absent array.
func TestExecuteReturnsResponseWithoutDecoding(t *testing.T) {
	server, _ := captureServer(t, http.StatusCreated, "")

	response, err := postgrest.Execute(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments").Insert(instrument{ID: 1, Name: "violin"}),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.HTTPStatus != http.StatusCreated {
		t.Errorf("HTTPStatus = %d, want 201", response.HTTPStatus)
	}
	if response.Count != -1 {
		t.Errorf("Count = %d, want -1 (no Content-Range served)", response.Count)
	}
}

// TestExecuteSentinels pins that Execute reports the same pre-flight sentinels
// as the read functions, without any I/O: a nil client and an empty table name
// each fail before a request is built.
func TestExecuteSentinels(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		_, err := postgrest.Execute(
			t.Context(),
			nil,
			postgrest.From[instrument]("instruments").Insert(instrument{ID: 1, Name: "violin"}),
		)
		if !errors.Is(err, postgrest.ErrMissingClient) {
			t.Errorf("want ErrMissingClient, got %v", err)
		}
	})
	t.Run("empty table", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Error("no HTTP request should be made for an empty table name")
		}))
		defer server.Close()

		_, err := postgrest.Execute(
			t.Context(),
			newTestClient(t, server),
			postgrest.From[instrument]("").Insert(instrument{ID: 1, Name: "violin"}),
		)
		if !errors.Is(err, postgrest.ErrMissingTable) {
			t.Errorf("want ErrMissingTable, got %v", err)
		}
	})
}

// TestUpdateSendsPatchWithChanges pins the update wire shape through Execute: a
// PATCH to the table path carrying the changes as one JSON object under the
// JSON media type, the row-choosing filter in the query string, and - since
// Execute reads nothing back - no Prefer header at all.
func TestUpdateSendsPatchWithChanges(t *testing.T) {
	server, record := captureServer(t, http.StatusNoContent, "")

	response, err := postgrest.Execute(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments").
			Eq("id", 1).
			Update(map[string]any{"name": "viola"}),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.HTTPStatus != http.StatusNoContent {
		t.Errorf("HTTPStatus = %d, want 204", response.HTTPStatus)
	}
	if got, want := record.request.Method, http.MethodPatch; got != want {
		t.Errorf("method = %q, want %q", got, want)
	}
	if got, want := string(record.body), `{"name":"viola"}`; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	if got, want := record.request.URL.RawQuery, "id=eq.1"; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
	if got, want := record.request.Header.Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got := record.request.Header.Values("Prefer"); len(got) != 0 {
		t.Errorf("Prefer = %q, want none on an Execute", got)
	}
}

// TestUpdateReturningProjectsColumns pins Returning on a mutation exactly as the
// insert case pins it: the cleaned select parameter with replace semantics, an
// empty string meaning every column, and - under Execute - no return preference.
func TestUpdateReturningProjectsColumns(t *testing.T) {
	base := postgrest.From[instrument]("instruments").Update(map[string]any{"name": "viola"})
	testCases := []struct {
		name       string
		builder    postgrest.MutationBuilder[instrument]
		wantSelect string
	}{
		{
			name:       "columns cleaned",
			builder:    base.Returning("id, name"),
			wantSelect: "id,name",
		},
		{
			name:       "later returning replaces earlier",
			builder:    base.Returning("id").Returning("name"),
			wantSelect: "name",
		},
		{
			name:       "empty means all columns",
			builder:    base.Returning(""),
			wantSelect: "*",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server, record := captureServer(t, http.StatusNoContent, "")

			if _, err := postgrest.Execute(t.Context(), newTestClient(t, server), testCase.builder); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got := record.request.URL.Query().Get("select"); got != testCase.wantSelect {
				t.Errorf("select = %q, want %q", got, testCase.wantSelect)
			}
			if got := record.request.Header.Values("Prefer"); len(got) != 0 {
				t.Errorf("Prefer = %q, want none under Execute", got)
			}
		})
	}
}

// TestDeleteSendsDeleteWithoutBody pins the delete wire shape through Execute: a
// DELETE to the table path carrying no body and no JSON media type, the
// row-choosing filter in the query string, and - since Execute reads nothing
// back - no Prefer header at all.
func TestDeleteSendsDeleteWithoutBody(t *testing.T) {
	server, record := captureServer(t, http.StatusNoContent, "")

	response, err := postgrest.Execute(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[instrument]("instruments").
			Eq("id", 1).
			Delete(),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.HTTPStatus != http.StatusNoContent {
		t.Errorf("HTTPStatus = %d, want 204", response.HTTPStatus)
	}
	if got, want := record.request.Method, http.MethodDelete; got != want {
		t.Errorf("method = %q, want %q", got, want)
	}
	if len(record.body) != 0 {
		t.Errorf("body = %q, want empty (a delete sends no body)", record.body)
	}
	if got, want := record.request.URL.RawQuery, "id=eq.1"; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
	if got := record.request.Header.Get("Content-Type"); got != "" {
		t.Errorf("Content-Type = %q, want none (a delete sends no body)", got)
	}
	if got := record.request.Header.Values("Prefer"); len(got) != 0 {
		t.Errorf("Prefer = %q, want none on an Execute", got)
	}
}
