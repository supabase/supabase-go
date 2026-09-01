package request_test

// cSpell:ignore موارد Finstruments Fincrement Fadmin Fauth Fusers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/supabase/supabase-go/postgrest/internal/request"
)

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return parsed
}

func assemble(t *testing.T, r request.Request) *http.Request {
	t.Helper()
	httpRequest, err := r.HTTPRequest(t.Context(), mustParseURL(t, "https://example.test/rest/v1"))
	if err != nil {
		t.Fatalf("HTTPRequest: %v", err)
	}
	return httpRequest
}

func requestURL(t *testing.T, r request.Request) string {
	t.Helper()
	return assemble(t, r).URL.String()
}

// TestWithParameterDoesNotMutateReceiver pins the model's immutability
// contract: WithParameter returns a new value and the receiver is unchanged,
// so a Request can be stored and forked safely.
func TestWithParameterDoesNotMutateReceiver(t *testing.T) {
	base := request.New(http.MethodGet, "instruments")

	first := base.WithParameter("select", "id")
	second := base.WithParameter("select", "name")

	if got, want := requestURL(t, base), "https://example.test/rest/v1/instruments"; got != want {
		t.Errorf("base URL = %q, want %q (base was mutated by a fork)", got, want)
	}
	if got, want := requestURL(t, first), "https://example.test/rest/v1/instruments?select=id"; got != want {
		t.Errorf("first fork URL = %q, want %q", got, want)
	}
	if got, want := requestURL(t, second), "https://example.test/rest/v1/instruments?select=name"; got != want {
		t.Errorf("second fork URL = %q, want %q", got, want)
	}
}

// TestForksFromSharedIntermediateAreIndependent pins fork independence for
// chains sharing a cloned backing-slice ancestry - the append-aliasing bug
// that WithParameter's slices.Clone exists to prevent.
func TestForksFromSharedIntermediateAreIndependent(t *testing.T) {
	intermediate := request.New(http.MethodGet, "instruments").WithParameter("select", "id")

	first := intermediate.WithParameter("limit", "1")
	second := intermediate.WithParameter("offset", "2")

	if got, want := requestURL(t, first), "https://example.test/rest/v1/instruments?select=id&limit=1"; got != want {
		t.Errorf("first chain URL = %q, want %q", got, want)
	}
	if got, want := requestURL(t, second), "https://example.test/rest/v1/instruments?select=id&offset=2"; got != want {
		t.Errorf("second chain URL = %q, want %q", got, want)
	}
}

// TestWithParameterPreservesRepeatedKeys pins duplicate-key preservation:
// PostgREST combines repeated filter keys with AND (age=gte.18&age=lte.65 is
// a range filter), so the model must keep duplicates, never collapse them.
func TestWithParameterPreservesRepeatedKeys(t *testing.T) {
	ranged := request.New(http.MethodGet, "people").
		WithParameter("age", "gte.18").
		WithParameter("age", "lte.65")

	if got, want := requestURL(t, ranged), "https://example.test/rest/v1/people?age=gte.18&age=lte.65"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestWithParameterReplacing(t *testing.T) {
	requests := []request.Request{
		request.New(http.MethodGet, "path").
			WithParameterReplacing("key", "A"),

		request.New(http.MethodGet, "path").
			WithParameterReplacing("key", "Z").
			WithParameterReplacing("key", "A"),

		request.New(http.MethodGet, "path").
			WithParameter("key", "Z").
			WithParameterReplacing("key", "A"),

		request.New(http.MethodGet, "path").
			WithParameter("key", "Z").
			WithParameterReplacing("key", "X").
			WithParameterReplacing("key", "A"),
	}

	want := "https://example.test/rest/v1/path?key=A"

	for _, request := range requests {
		if got := requestURL(t, request); got != want {
			t.Errorf("URL = %q, want %q", got, want)
		}
	}
}

func TestWithParameterJoining(t *testing.T) {
	testCases := []struct {
		name    string
		perform func(request.Request) request.Request
		want    string
	}{
		{
			name: "single call",
			perform: func(request request.Request) request.Request {
				return request.WithParameterJoining("key", "A")
			},
			want: "A",
		},
		{
			name: "multiple calls with discrete values",
			perform: func(request request.Request) request.Request {
				return request.
					WithParameterJoining("key", "A").
					WithParameterJoining("key", "B").
					WithParameterJoining("key", "C")
			},
			want: "A,B,C",
		},
		{
			name: "multiple calls with same value",
			perform: func(request request.Request) request.Request {
				return request.
					WithParameterJoining("key", "A").
					WithParameterJoining("key", "A")
			},
			want: "A,A",
		},
		{
			name: "multiple calls seeded by WithParameter",
			perform: func(request request.Request) request.Request {
				return request.
					WithParameter("key", "A").
					WithParameterJoining("key", "B1").
					WithParameterJoining("key", "B2")
			},
			want: "A,B1,B2",
		},
		{
			name: "multiple calls seeded by WithParameterReplacing",
			perform: func(request request.Request) request.Request {
				return request.
					WithParameterReplacing("key", "A").
					WithParameterJoining("key", "B1").
					WithParameterJoining("key", "B2")
			},
			want: "A,B1,B2",
		},
		{
			name: "multiple calls seeded by WithParameter then WithParameterReplacing",
			perform: func(request request.Request) request.Request {
				return request.
					WithParameter("key", "A").
					WithParameterReplacing("key", "C").
					WithParameterJoining("key", "B1").
					WithParameterJoining("key", "B2")
			},
			want: "C,B1,B2",
		},
		{
			name: "multiple calls seeded by WithParameter twice",
			perform: func(request request.Request) request.Request {
				return request.
					WithParameter("key", "A1").
					WithParameter("key", "A2").        // now: key=A1&key=A2
					WithParameterJoining("key", "B1"). // now: key=A1,A2,B1
					WithParameterJoining("key", "B2")
			},
			want: "A1,A2,B1,B2",
		},
	}

	base := request.New(http.MethodGet, "path")
	synthesize := func(value string) string { return "https://example.test/rest/v1/path?key=" + value }

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got, want := requestURL(t, testCase.perform(base)), synthesize(testCase.want); got != want {
				t.Errorf("URL = %q, want %q", got, want)
			}
		})
	}
}

func TestWithParameterValueAppended(t *testing.T) {
	testCases := []struct {
		name    string
		perform func(request.Request) request.Request
		want    string
	}{
		{
			name: "single call when key does not exist",
			perform: func(request request.Request) request.Request {
				return request.WithParameterValueAppended("key", "A")
			},
			want: "",
		},
		{
			name: "multiple calls when key does not exist",
			perform: func(request request.Request) request.Request {
				return request.
					WithParameterValueAppended("key", "A").
					WithParameterValueAppended("key", "B")
			},
			want: "",
		},
		{
			name: "single call when only one instance of key",
			perform: func(request request.Request) request.Request {
				return request.
					WithParameter("key", "A").
					WithParameterValueAppended("key", "B")
			},
			want: "?key=AB",
		},
		{
			name: "single call when multiple instances of key",
			perform: func(request request.Request) request.Request {
				return request.
					WithParameter("key", "A").
					WithParameter("key", "B").
					WithParameterValueAppended("key", "C")
			},
			want: "?key=A&key=BC",
		},
	}

	base := request.New(http.MethodGet, "path")
	synthesize := func(value string) string { return "https://example.test/rest/v1/path" + value }

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got, want := requestURL(t, testCase.perform(base)), synthesize(testCase.want); got != want {
				t.Errorf("URL = %q, want %q", got, want)
			}
		})
	}
}

// TestHTTPRequestCarriesContextAndMethod pins request assembly: the caller's
// context rides the HTTP request, the method is preserved and no headers are
// injected - the Accept header must be set at execution.
func TestHTTPRequestCarriesContextAndMethod(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, "present")

	httpRequest, err := request.New(http.MethodGet, "instruments").
		HTTPRequest(ctx, mustParseURL(t, "https://example.test/rest/v1"))
	if err != nil {
		t.Fatalf("HTTPRequest: %v", err)
	}
	if httpRequest.Method != http.MethodGet {
		t.Errorf("method = %q, want %q", httpRequest.Method, http.MethodGet)
	}
	if got := httpRequest.Header.Get("Accept"); got != "" {
		t.Errorf("Accept = %q, want no Accept header", got)
	}
	if values, present := httpRequest.Header["Accept"]; present {
		t.Errorf("Accept header is present with values %q, want absent", values)
	}
	if httpRequest.Context().Value(contextKey{}) != "present" {
		t.Error("context was not propagated onto the HTTP request")
	}
}

// TestPathEscaping pins that the relation path, stored unescaped by New, is
// escaped at assembly time as Path documents.
func TestPathEscaping(t *testing.T) {
	httpRequest, err := request.New(http.MethodGet, "odd table").
		HTTPRequest(t.Context(), mustParseURL(t, "https://example.test/rest/v1"))
	if err != nil {
		t.Fatalf("HTTPRequest: %v", err)
	}
	if got, want := httpRequest.URL.String(), "https://example.test/rest/v1/odd%20table"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

// TestQueryStringRendering asserts the exact query text HTTPRequest renders for
// single parameter pairs, characters of PostgREST's dialect included.
func TestQueryStringRendering(t *testing.T) {
	testCases := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{
			name:  "select list comma",
			key:   "select",
			value: "id,name",
			want:  "select=id,name",
		},
		{
			name:  "order terms",
			key:   "order",
			value: "acquired_year.desc,name",
			want:  "order=acquired_year.desc,name",
		},
		{
			name:  "embed alias and parentheses",
			key:   "select",
			value: "name,section:orchestral_sections(name)",
			want:  "select=name,section:orchestral_sections(name)",
		},
		{
			name:  "wildcard",
			key:   "select",
			value: "*",
			want:  "select=*",
		},
		{
			name:  "quoted in-list",
			key:   "name",
			value: `in.("x","y")`,
			want:  "name=in.(%22x%22,%22y%22)",
		},
		{
			name:  "quoted identifier with space",
			key:   "select",
			value: `"full name"`,
			want:  "select=%22full%20name%22",
		},
		{
			name:  "structural characters",
			key:   "value",
			value: "a&b=c+d%e",
			want:  "value=a%26b%3Dc%2Bd%25e",
		},
		{
			name:  "semicolon",
			key:   "value",
			value: "a;b",
			want:  "value=a%3Bb",
		},
		{
			name:  "non-ASCII octets",
			key:   "name",
			value: "eq.é",
			want:  "name=eq.%C3%A9",
		},
		{
			name:  "four-byte UTF-8 emoji",
			key:   "name",
			value: "eq.💩",
			want:  "name=eq.%F0%9F%92%A9",
		},
		{
			name:  "invalid UTF-8 octet",
			key:   "value",
			value: "\xff",
			want:  "value=%FF",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rendered := requestURL(t, request.New(http.MethodGet, "instruments").
				WithParameter(testCase.key, testCase.value))
			if got, want := rendered, "https://example.test/rest/v1/instruments?"+testCase.want; got != want {
				t.Errorf("URL = %q, want %q", got, want)
			}
		})
	}
}

// TestQueryStringPairOrdering asserts the order pairs render in.
func TestQueryStringPairOrdering(t *testing.T) {
	rendered := requestURL(t, request.New(http.MethodGet, "instruments").
		WithParameter("select", "id").
		WithParameter("order", "name").
		WithParameter("limit", "1"))
	if got, want := rendered, "https://example.test/rest/v1/instruments?select=id&order=name&limit=1"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestTableNameEscaping(t *testing.T) {
	testCases := []struct {
		name  string
		table string
		want  string
	}{
		{
			name:  "a very normal standard table name",
			table: "instruments",
			want:  "rest/v1/instruments",
		},
		{
			name:  "table name with unreserved character joining",
			table: "table_2024",
			want:  "rest/v1/table_2024",
		},
		{
			name:  "a dot within a segment is ordinary data",
			table: "foo.bar",
			want:  "rest/v1/foo.bar",
		},
		{
			name:  "colon and dash remain literal",
			table: "2026-06-30T11:23:31",
			want:  "rest/v1/2026-06-30T11:23:31",
		},
		{
			name:  "plus remains literal", // differing from query-string layer
			table: "a+b",
			want:  "rest/v1/a+b",
		},
		{
			name:  "non-ASCII",
			table: "موارد", // resources
			want:  "rest/v1/%D9%85%D9%88%D8%A7%D8%B1%D8%AF",
		},
		{
			name:  "embedded space",
			table: "odd table",
			want:  "rest/v1/odd%20table",
		},
		{
			name:  "risk of leaking tail into query string",
			table: "a?b",
			want:  "rest/v1/a%3Fb",
		},
		{
			name:  "risk of dropping tail into unsent fragment",
			table: "a#b",
			want:  "rest/v1/a%23b",
		},
		{
			name:  "LF control character",
			table: "a\nb",
			want:  "rest/v1/a%0Ab",
		},
		{
			name:  "embedded slash",
			table: "a/b",
			want:  "rest/v1/a%2Fb",
		},
		{
			name:  "should not collapse double slash",
			table: "a//b",
			want:  "rest/v1/a%2F%2Fb",
		},
		{
			name:  "should not swallow leading slash",
			table: "/instruments",
			want:  "rest/v1/%2Finstruments",
		},
		{
			name:  "trailing slash",
			table: "instruments/",
			want:  "rest/v1/instruments%2F",
		},
		{
			name:  "should not allow route to RPC",
			table: "rpc/increment_secret",
			want:  "rest/v1/rpc%2Fincrement_secret",
		},
		{
			name:  "single dot should not be API root",
			table: ".",
			want:  "rest/v1/%2E",
		},
		{
			name:  "double dot should not climb up one level above PostgREST mount",
			table: "..",
			want:  "rest/v1/%2E%2E",
		},
		{
			name:  "double double dot should not climb up to host root",
			table: "../..",
			want:  "rest/v1/..%2F..",
		},
		{
			name:  "embedded double dot should not cancel leading part",
			table: "a/../b",
			want:  "rest/v1/a%2F..%2Fb",
		},
		{
			name:  "prefixed double dot should not steer to sibling path under same gateway prefix",
			table: "../admin",
			want:  "rest/v1/..%2Fadmin",
		},
		{
			name:  "curated double dots should not navigate to admin API",
			table: "../../auth/v1/admin/users",
			want:  "rest/v1/..%2F..%2Fauth%2Fv1%2Fadmin%2Fusers",
		},
		{
			name:  "trailing percent style name",
			table: "50%off",
			want:  "rest/v1/50%25off",
		},
		{
			name:  "invalid hex digits after percent should not matter as taken verbatim after the percent escape",
			table: "%zz",
			want:  "rest/v1/%25zz",
		},
		{
			name:  "hex encoded slash should not matter as taken verbatim after the percent escape",
			table: "%2F",
			want:  "rest/v1/%252F",
		},
		{
			name:  "hex encoded double dots should not matter as taken verbatim after the percent escapes",
			table: "%2e%2e",
			want:  "rest/v1/%252e%252e",
		},
		{
			name:  "empty",
			table: "",
			want:  "rest/v1",
		},
		{
			name:  "single space",
			table: " ",
			want:  "rest/v1/%20",
		},
		{
			name:  "multiple spaces",
			table: "   ",
			want:  "rest/v1/%20%20%20",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rendered := requestURL(t, request.New(http.MethodGet, testCase.table))
			if got, want := rendered, "https://example.test/"+testCase.want; got != want {
				t.Errorf("URL = %q, want %q", got, want)
			}
		})
	}
}

// TestWithMethodReplacesMethodWithoutMutatingReceiver pins that WithMethod
// sets the method the assembled request sends and leaves the receiver
// unchanged, so the GET that New opens forks into a write independently.
func TestWithMethodReplacesMethodWithoutMutatingReceiver(t *testing.T) {
	base := request.New(http.MethodGet, "instruments")
	post := base.WithMethod(http.MethodPost)

	if got, want := assemble(t, base).Method, http.MethodGet; got != want {
		t.Errorf("base method = %q, want %q (receiver was mutated by a fork)", got, want)
	}
	if got, want := assemble(t, post).Method, http.MethodPost; got != want {
		t.Errorf("forked method = %q, want %q", got, want)
	}
}

// TestWithBody pins the body contract: a Request with a body carries the exact
// bytes under the JSON media type PostgREST requires on a write, and a Request
// with no body carries neither. A nil body is no body.
func TestWithBody(t *testing.T) {
	testCases := []struct {
		name            string
		perform         func(request.Request) request.Request
		wantBodyPresent bool
		wantBody        string
		wantContentType string
	}{
		{
			name:            "no body",
			perform:         func(r request.Request) request.Request { return r },
			wantBodyPresent: false,
			wantContentType: "",
		},
		{
			name:            "object array",
			perform:         func(r request.Request) request.Request { return r.WithBody([]byte(`[{"id":1}]`)) },
			wantBodyPresent: true,
			wantBody:        `[{"id":1}]`,
			wantContentType: "application/json",
		},
		{
			name:            "empty array from zero rows",
			perform:         func(r request.Request) request.Request { return r.WithBody([]byte("[]")) },
			wantBodyPresent: true,
			wantBody:        "[]",
			wantContentType: "application/json",
		},
		{
			name:            "nil body is no body",
			perform:         func(r request.Request) request.Request { return r.WithBody(nil) },
			wantBodyPresent: false,
			wantContentType: "",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assembled := assemble(t, testCase.perform(request.New(http.MethodPost, "instruments")))
			switch {
			case testCase.wantBodyPresent && assembled.Body == nil:
				t.Fatal("request carries no body, want one")
			case !testCase.wantBodyPresent && assembled.Body != nil:
				t.Error("request carries a body, want none")
			case testCase.wantBodyPresent:
				body, err := io.ReadAll(assembled.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if got := string(body); got != testCase.wantBody {
					t.Errorf("body = %q, want %q", got, testCase.wantBody)
				}
			}
			if got := assembled.Header.Get("Content-Type"); got != testCase.wantContentType {
				t.Errorf("Content-Type = %q, want %q", got, testCase.wantContentType)
			}
		})
	}
}

// TestWithBodyCopiesInput pins that WithBody snapshots the caller's slice, so a
// later change to that slice cannot alter what the Request sends.
func TestWithBodyCopiesInput(t *testing.T) {
	payload := []byte("[1]")
	req := request.New(http.MethodPost, "instruments").WithBody(payload)
	payload[1] = '9' // the caller changes its slice after handing it over

	body, err := io.ReadAll(assemble(t, req).Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got, want := string(body), "[1]"; got != want {
		t.Errorf("body = %q, want %q (WithBody did not copy its input)", got, want)
	}
}

// TestWithErrorShortCircuitsHTTPRequest pins that a deferred build failure is
// returned by HTTPRequest before any assembly and in place of any request, and
// that it takes precedence over an error assembly would itself raise.
func TestWithErrorShortCircuitsHTTPRequest(t *testing.T) {
	sentinel := errors.New("deferred build failure")

	httpRequest, err := request.New(http.MethodPost, "instruments").
		WithError(sentinel).
		HTTPRequest(t.Context(), mustParseURL(t, "https://example.test/rest/v1"))
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want %v", err, sentinel)
	}
	if httpRequest != nil {
		t.Error("HTTPRequest returned a request alongside a deferred error, want nil")
	}

	// An invalid method makes assembly itself fail, yet the deferred error must
	// still be the one returned: it is checked first.
	_, err = request.New("bad method", "instruments").
		WithError(sentinel).
		HTTPRequest(t.Context(), mustParseURL(t, "https://example.test/rest/v1"))
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want %v (deferred error must precede assembly)", err, sentinel)
	}
}

// TestWriteModelMethodsDoNotMutateReceiver pins the copy-on-write contract for
// the method, body and error fields together: forking a base Request leaves the
// base assembling exactly as it did before the fork.
func TestWriteModelMethodsDoNotMutateReceiver(t *testing.T) {
	base := request.New(http.MethodGet, "instruments")

	_ = base.WithMethod(http.MethodPost)
	_ = base.WithBody([]byte("[]"))
	_ = base.WithError(errors.New("deferred"))

	assembled, err := base.HTTPRequest(t.Context(), mustParseURL(t, "https://example.test/rest/v1"))
	if err != nil {
		t.Fatalf("base HTTPRequest: %v (a fork mutated the receiver)", err)
	}
	if got, want := assembled.Method, http.MethodGet; got != want {
		t.Errorf("base method = %q, want %q", got, want)
	}
	if assembled.Body != nil {
		t.Error("base carries a body after a fork added one")
	}
	if got := assembled.Header.Get("Content-Type"); got != "" {
		t.Errorf("base Content-Type = %q, want none", got)
	}
}
