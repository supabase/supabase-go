package supabase_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/supabase"
)

// TestNewValidationPropagates pins that New surfaces the configuration
// package's sentinels unchanged, so callers match them with errors.Is as the
// doc comment promises.
func TestNewValidationPropagates(t *testing.T) {
	if _, err := supabase.New("", "k"); !errors.Is(err, configuration.ErrMissingURL) {
		t.Fatalf("want ErrMissingURL, got %v", err)
	}
	if _, err := supabase.New("https://PROJECT_ID.supabase.co", ""); !errors.Is(err, configuration.ErrMissingKey) {
		t.Fatalf("want ErrMissingKey, got %v", err)
	}
}

// TestAuthAccessorReturnsStableHandle pins that Auth hands back the one
// handle New constructed, identical across calls, keeping domain navigation
// context-free and error-free.
func TestAuthAccessorReturnsStableHandle(t *testing.T) {
	client, err := supabase.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	first := client.Auth()
	if first == nil {
		t.Fatal("Auth returned nil")
	}
	if second := client.Auth(); second != first {
		t.Error("Auth returned a different handle on the second call")
	}
}

// traceMarker is the context key stampingTransport reads the test's marker
// value through.
type traceMarker struct{}

// stampingTransport simulates an instrumented transport such as
// otelhttp.NewTransport: it reads a value from the request's context and
// writes it to the wire as the traceparent header, delegating the round trip
// to the default transport. It clones before writing, honoring the
// [http.RoundTripper] contract that the request is never mutated.
type stampingTransport struct{}

func (stampingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if value, ok := request.Context().Value(traceMarker{}).(string); ok {
		request = request.Clone(request.Context())
		request.Header.Set("traceparent", value)
	}
	return http.DefaultTransport.RoundTrip(request)
}

// TestInjectedTransportReceivesCallContext pins the trace-propagation
// contract the sdk-compliance manifest claims: every call's context reaches
// the transport injected through WithHTTPClient, and headers stamped there
// reach the wire, through both composed domains of one root client. This is
// exactly what an OpenTelemetry otelhttp transport does - read the active
// span from the request context, write the traceparent header - simulated
// here without the dependency so the guarantee stays pinned in the fast tier.
func TestInjectedTransportReceivesCallContext(t *testing.T) {
	received := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received[request.URL.Path] = request.Header.Get("traceparent")
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/auth/v1/user" {
			_, _ = writer.Write([]byte(`{"id":"USER_ID"}`))
			return
		}
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, err := supabase.New(server.URL, "TEST_API_KEY",
		configuration.WithHTTPClient(&http.Client{Transport: stampingTransport{}}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.WithValue(t.Context(), traceMarker{}, "SOME-MARKER")

	if _, _, err := postgrest.Collect(ctx, client.Database(),
		postgrest.From[struct{}]("instruments")); err != nil {
		t.Fatalf("Collect through Database(): %v", err)
	}
	if _, err := client.Auth().GetUser(ctx, "END_USER_JWT"); err != nil {
		t.Fatalf("GetUser through Auth(): %v", err)
	}

	for _, path := range []string{"/rest/v1/instruments", "/auth/v1/user"} {
		if got := received[path]; got != "SOME-MARKER" {
			t.Errorf("traceparent on %s = %q, want the stamped marker", path, got)
		}
	}
}
