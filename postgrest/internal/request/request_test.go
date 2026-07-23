package request_test

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
	httpRequest, err := r.HTTPRequest(context.Background(), mustParseURL(t, "https://example.test/rest/v1"))
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

	if got, want := requestURL(t, first), "https://example.test/rest/v1/instruments?limit=1&select=id"; got != want {
		t.Errorf("first chain URL = %q, want %q", got, want)
	}
	if got, want := requestURL(t, second), "https://example.test/rest/v1/instruments?offset=2&select=id"; got != want {
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

// TestHTTPRequestCarriesContextMethodAndAcceptHeader pins request assembly:
// the caller's context rides the HTTP request, the method is preserved and
// Accept asks for JSON.
func TestHTTPRequestCarriesContextMethodAndAcceptHeader(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "present")

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
		HTTPRequest(context.Background(), mustParseURL(t, "https://example.test/rest/v1"))
	if err != nil {
		t.Fatalf("HTTPRequest: %v", err)
	}
	if got, want := httpRequest.URL.String(), "https://example.test/rest/v1/odd%20table"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}
