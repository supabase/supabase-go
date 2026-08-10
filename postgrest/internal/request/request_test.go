package request_test

// cSpell:ignore موارد

import (
	"context"
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

func requestURL(t *testing.T, r request.Request) string {
	t.Helper()
	httpRequest, err := r.HTTPRequest(t.Context(), mustParseURL(t, "https://example.test/rest/v1"))
	if err != nil {
		t.Fatalf("HTTPRequest: %v", err)
	}
	return httpRequest.URL.String()
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

// TestHTTPRequestCarriesContextMethodAndAcceptHeader pins request assembly:
// the caller's context rides the HTTP request, the method is preserved and
// Accept asks for JSON.
func TestHTTPRequestCarriesContextMethodAndAcceptHeader(t *testing.T) {
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
	if got := httpRequest.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want application/json", got)
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
			want:  "rest/v1/a/b", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "should not collapse double slash",
			table: "a//b",
			want:  "rest/v1/a/b", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "should not swallow leading slash",
			table: "/instruments",
			want:  "rest/v1/instruments", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "trailing slash",
			table: "instruments/",
			want:  "rest/v1/instruments/", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "should not allow route to RPC",
			table: "rpc/increment_secret",
			want:  "rest/v1/rpc/increment_secret", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "single dot should not be API root",
			table: ".",
			want:  "rest/v1", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "double dot should not climb up one level above PostgREST mount",
			table: "..",
			want:  "rest", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "double double dot should not climb up to host root",
			table: "../..",
			want:  "", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "embedded double dot should not cancel leading part",
			table: "a/../b",
			want:  "rest/v1/b", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "prefixed double dot should not steer to sibling path under same gateway prefix",
			table: "../admin",
			want:  "rest/admin", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "curated double dots should not navigate to admin API",
			table: "../../auth/v1/admin/users",
			want:  "auth/v1/admin/users", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "trailing percent style name",
			table: "50%off",
			want:  "rest/v1", // TODO fix implementation so this gets correctly escaped (discarded because setPath fails on invalid escape)
		},
		{
			name:  "invalid hex digits after percent should not matter as taken verbatim after the percent escape",
			table: "%zz",
			want:  "rest/v1", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "hex encoded slash should not matter as taken verbatim after the percent escape",
			table: "%2F",
			want:  "rest/v1/%2F", // TODO fix implementation so this gets correctly escaped
		},
		{
			name:  "hex encoded double dots should not matter as taken verbatim after the percent escapes",
			table: "%2e%2e",
			want:  "rest/v1/%2e%2e", // TODO fix implementation so this gets correctly escaped
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
