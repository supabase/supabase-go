package postgrest_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/supabase/supabase-go/postgrest"
)

// TestRPCRowsSendsPost pins the rows-shape wire form from both execution sides:
// through Execute a POST to rpc/<function> carries the arguments as one JSON
// object under the JSON media type with no Prefer header, and through Collect
// the same call additionally carries the execution-injected return=representation.
func TestRPCRowsSendsPost(t *testing.T) {
	t.Run("through Execute carries no Prefer", func(t *testing.T) {
		server, record := captureServer(t, http.StatusOK, "")

		if _, err := postgrest.Execute(
			t.Context(),
			newTestClient(t, server),
			postgrest.RPC[map[string]any]("add_them").Arguments(map[string]any{"a": 1, "b": 2}).Rows(),
		); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if got, want := record.request.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := record.request.URL.Path, "/rest/v1/rpc/add_them"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := string(record.body), `{"a":1,"b":2}`; got != want {
			t.Errorf("body = %s, want %s", got, want)
		}
		if got, want := record.request.Header.Get("Content-Type"), "application/json"; got != want {
			t.Errorf("Content-Type = %q, want %q", got, want)
		}
		if got := record.request.Header.Values("Prefer"); len(got) != 0 {
			t.Errorf("Prefer = %q, want none on an Execute", got)
		}
	})

	t.Run("through Collect carries return=representation", func(t *testing.T) {
		server, record := captureServer(t, http.StatusOK, "[]")

		if _, _, err := postgrest.Collect(
			t.Context(),
			newTestClient(t, server),
			postgrest.RPC[map[string]any]("add_them").Arguments(map[string]any{"a": 1, "b": 2}).Rows(),
		); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if prefer := record.request.Header.Values("Prefer"); !slices.Contains(prefer, "return=representation") {
			t.Errorf("Prefer = %q, want it to carry return=representation", prefer)
		}
	})
}

// TestRPCRowsReadOnlySendsGet pins the read-only rows form: ReadOnly turns the
// call into a GET whose arguments travel in the query string - a string bare, a
// number verbatim, an array in the {1,2} literal form - with no body, no JSON
// media type and no Prefer header.
func TestRPCRowsReadOnlySendsGet(t *testing.T) {
	server, record := captureServer(t, http.StatusOK, "[]")

	if _, _, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.
			RPC[map[string]any]("search_pieces").
			Arguments(map[string]any{"name": "cello", "count": 2, "ids": []int{1, 2}}).
			Rows().
			ReadOnly(),
	); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got, want := record.request.Method, http.MethodGet; got != want {
		t.Errorf("method = %q, want %q", got, want)
	}
	if got, want := record.request.URL.Path, "/rest/v1/rpc/search_pieces"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	query := record.request.URL.Query()
	if got, want := query.Get("name"), "cello"; got != want {
		t.Errorf("name = %q, want %q (a string travels bare)", got, want)
	}
	if got, want := query.Get("count"), "2"; got != want {
		t.Errorf("count = %q, want %q (a number travels verbatim)", got, want)
	}
	if got, want := query.Get("ids"), "{1,2}"; got != want {
		t.Errorf("ids = %q, want %q (an array travels as a literal)", got, want)
	}
	if len(record.body) != 0 {
		t.Errorf("body = %q, want empty (a GET sends no body)", record.body)
	}
	if got := record.request.Header.Get("Content-Type"); got != "" {
		t.Errorf("Content-Type = %q, want none (a GET sends no body)", got)
	}
	if got := record.request.Header.Values("Prefer"); len(got) != 0 {
		t.Errorf("Prefer = %q, want none on a GET read", got)
	}
}

// TestRPCValueDecodesWholeBody pins the value shape through CollectRaw in both
// transports: a synthesized bare 3 decodes whole into an int, with no row-array
// unwrapping, whether the call is a POST or its read-only GET.
func TestRPCValueDecodesWholeBody(t *testing.T) {
	testCases := []struct {
		name string
		call postgrest.RawQuery[int]
	}{
		{
			name: "Value POST",
			call: postgrest.RPC[int]("add_them").Arguments(map[string]any{"a": 1, "b": 2}).Value(),
		},
		{
			name: "Value ReadOnly GET",
			call: postgrest.RPC[int]("add_them").Arguments(map[string]any{"a": 1, "b": 2}).Value().ReadOnly(),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server, _ := captureServer(t, http.StatusOK, "3")

			value, response, err := postgrest.CollectRaw(t.Context(), newTestClient(t, server), testCase.call)
			if err != nil {
				t.Fatalf("CollectRaw: %v", err)
			}
			if value != 3 {
				t.Errorf("value = %d, want 3", value)
			}
			if response.HTTPStatus != http.StatusOK {
				t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
			}
		})
	}
}

// TestCollectRawDecodesTableQueryWholeArray pins that CollectRaw is not scoped
// to functions: a table read's array body decodes wholesale into a slice
// through an ordinary From builder, whose type parameter names the whole-body
// target the array decodes into.
func TestCollectRawDecodesTableQueryWholeArray(t *testing.T) {
	server, _ := captureServer(t, http.StatusOK, `[{"id":1,"name":"violin"},{"id":2,"name":"flute"}]`)

	rows, _, err := postgrest.CollectRaw(
		t.Context(),
		newTestClient(t, server),
		postgrest.From[[]instrument]("instruments"),
	)
	if err != nil {
		t.Fatalf("CollectRaw: %v", err)
	}
	want := []instrument{{ID: 1, Name: "violin"}, {ID: 2, Name: "flute"}}
	if !slices.Equal(rows, want) {
		t.Errorf("rows = %+v, want %+v", rows, want)
	}
}

// TestRPCVoidExecutes pins the void call: Execute sends a POST to rpc/<function>
// carrying the empty-object body, decodes nothing and reports the response
// metadata, with no Prefer header.
func TestRPCVoidExecutes(t *testing.T) {
	server, record := captureServer(t, http.StatusNoContent, "")

	response, err := postgrest.Execute(
		t.Context(),
		newTestClient(t, server),
		postgrest.RPCVoid("refresh_reporting_view"),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.HTTPStatus != http.StatusNoContent {
		t.Errorf("HTTPStatus = %d, want 204", response.HTTPStatus)
	}
	if got, want := record.request.Method, http.MethodPost; got != want {
		t.Errorf("method = %q, want %q", got, want)
	}
	if got, want := record.request.URL.Path, "/rest/v1/rpc/refresh_reporting_view"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := string(record.body), "{}"; got != want {
		t.Errorf("body = %q, want %q (no arguments sends an empty object)", got, want)
	}
	if got := record.request.Header.Values("Prefer"); len(got) != 0 {
		t.Errorf("Prefer = %q, want none on an Execute", got)
	}
}

// TestRPCNoArguments pins the never-filler contract: omitting Arguments sends
// the empty object under the POST forms and a bare query under ReadOnly, so the
// function runs on its parameter defaults either way.
func TestRPCNoArguments(t *testing.T) {
	t.Run("POST form sends an empty object", func(t *testing.T) {
		server, record := captureServer(t, http.StatusOK, "[]")

		if _, _, err := postgrest.Collect(
			t.Context(),
			newTestClient(t, server),
			postgrest.RPC[map[string]any]("current_repertoire").Rows(),
		); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if got, want := string(record.body), "{}"; got != want {
			t.Errorf("body = %q, want %q", got, want)
		}
	})

	t.Run("ReadOnly form sends a bare query", func(t *testing.T) {
		server, record := captureServer(t, http.StatusOK, "[]")

		if _, _, err := postgrest.Collect(
			t.Context(),
			newTestClient(t, server),
			postgrest.RPC[map[string]any]("current_repertoire").Rows().ReadOnly(),
		); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if got := record.request.URL.RawQuery; got != "" {
			t.Errorf("query = %q, want empty (no arguments sends a bare query)", got)
		}
		if len(record.body) != 0 {
			t.Errorf("body = %q, want empty", record.body)
		}
	})
}

// TestRPCArgumentsReplace pins replace-on-write on both entries: a later
// Arguments supersedes an earlier one, so only the final object travels.
func TestRPCArgumentsReplace(t *testing.T) {
	t.Run("on the generic entry", func(t *testing.T) {
		server, record := captureServer(t, http.StatusOK, "[]")

		if _, _, err := postgrest.Collect(
			t.Context(),
			newTestClient(t, server),
			postgrest.
				RPC[map[string]any]("f").
				Arguments(map[string]any{"a": 1}).
				Arguments(map[string]any{"b": 2}).
				Rows(),
		); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if got, want := string(record.body), `{"b":2}`; got != want {
			t.Errorf("body = %s, want %s (a later Arguments replaces an earlier one)", got, want)
		}
	})

	t.Run("on the void entry", func(t *testing.T) {
		server, record := captureServer(t, http.StatusNoContent, "")

		if _, err := postgrest.Execute(
			t.Context(),
			newTestClient(t, server),
			postgrest.
				RPCVoid("f").
				Arguments(map[string]any{"a": 1}).
				Arguments(map[string]any{"b": 2}),
		); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if got, want := string(record.body), `{"b":2}`; got != want {
			t.Errorf("body = %s, want %s (a later Arguments replaces an earlier one)", got, want)
		}
	})
}

// TestRPCReadOnlyArgumentsMustBeObject pins the read-only argument contract: an
// array (or any non-object) cannot become named query parameters, so the call
// parks a deferred error that surfaces at the executing function with no I/O.
func TestRPCReadOnlyArgumentsMustBeObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("no HTTP request should be made when read-only arguments are not an object")
	}))
	defer server.Close()

	_, _, err := postgrest.Collect(
		t.Context(),
		newTestClient(t, server),
		postgrest.RPC[map[string]any]("f").Arguments([]int{1, 2, 3}).Rows().ReadOnly(),
	)
	if err == nil {
		t.Fatal("Collect succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "must be a JSON object") {
		t.Errorf("err = %v, want it to mention the JSON object requirement", err)
	}
}

// TestRPCFunctionNameIsEscaped pins that a function name travels as one escaped
// path segment, so a name carrying URL structure names the function literally
// rather than steering the request.
func TestRPCFunctionNameIsEscaped(t *testing.T) {
	server, record := captureServer(t, http.StatusNoContent, "")

	if _, err := postgrest.Execute(
		t.Context(),
		newTestClient(t, server),
		postgrest.RPCVoid("a/b"),
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, want := record.request.URL.EscapedPath(), "/rest/v1/rpc/a%2Fb"; got != want {
		t.Errorf("escaped path = %q, want %q (a function name is one escaped segment)", got, want)
	}
}

// TestRPCEmptyFunctionName pins that an empty function name fails with
// ErrMissingFunction before any I/O, distinct from the empty-table sentinel,
// through each executing function that reaches an RPC call.
func TestRPCEmptyFunctionName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("no HTTP request should be made for an empty function name")
	}))
	defer server.Close()

	t.Run("void call through Execute", func(t *testing.T) {
		_, err := postgrest.Execute(t.Context(), newTestClient(t, server), postgrest.RPCVoid(""))
		if !errors.Is(err, postgrest.ErrMissingFunction) {
			t.Errorf("want ErrMissingFunction, got %v", err)
		}
	})

	t.Run("rows call through Collect", func(t *testing.T) {
		_, _, err := postgrest.Collect(t.Context(), newTestClient(t, server), postgrest.RPC[map[string]any]("").Rows())
		if !errors.Is(err, postgrest.ErrMissingFunction) {
			t.Errorf("want ErrMissingFunction, got %v", err)
		}
	})

	t.Run("value call through CollectRaw", func(t *testing.T) {
		_, _, err := postgrest.CollectRaw[int](t.Context(), newTestClient(t, server), postgrest.RPC[int]("").Value())
		if !errors.Is(err, postgrest.ErrMissingFunction) {
			t.Errorf("want ErrMissingFunction, got %v", err)
		}
	})
}
