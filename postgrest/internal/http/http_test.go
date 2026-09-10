package http_test

import (
	"context"
	"errors"
	netHttp "net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"

	"github.com/supabase/supabase-go/postgrest/internal/http"
	"github.com/supabase/supabase-go/postgrest/internal/request"
)

// newClient builds a Client sending to the test server, with automatic
// retries off so a failure surfaces on the attempt that caused it.
func newClient(t *testing.T, server *httptest.Server) http.Client {
	t.Helper()
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	return http.New(server.Client(), *baseURL, false)
}

// TestDoReportsNon2xxAsResultNotError pins the package's side of the error
// boundary: a completed exchange comes back as a Result whatever its status
// code, body intact, leaving user-facing failure shaping to the caller.
func TestDoReportsNon2xxAsResultNotError(t *testing.T) {
	server := httptest.NewServer(netHttp.HandlerFunc(func(writer netHttp.ResponseWriter, _ *netHttp.Request) {
		writer.WriteHeader(netHttp.StatusNotFound)
		_, _ = writer.Write([]byte(`{"message":"missing"}`))
	}))
	t.Cleanup(server.Close)

	result, err := newClient(t, server).Do(t.Context(), request.New(netHttp.MethodGet, "things"))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if result.StatusCode != netHttp.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, netHttp.StatusNotFound)
	}
	if got, want := string(result.Body), `{"message":"missing"}`; got != want {
		t.Errorf("Body = %q, want %q", got, want)
	}
}

// TestDoPropagatesResolverErrorsVerbatim pins the token side of the error
// boundary: a resolver's error ends the exchange as the call's error,
// unwrapped, before any request is sent.
func TestDoPropagatesResolverErrorsVerbatim(t *testing.T) {
	server := httptest.NewServer(netHttp.HandlerFunc(func(netHttp.ResponseWriter, *netHttp.Request) {
		t.Error("the server was reached, want resolution to fail the exchange before any I/O")
	}))
	t.Cleanup(server.Close)

	sentinel := errors.New("resolution failed")
	client := newClient(t, server).WithTokenResolver(func(context.Context) (string, error) {
		return "", sentinel
	})

	_, err := client.Do(t.Context(), request.New(netHttp.MethodGet, "things"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(err, sentinel) = false, want true: %v", err)
	}
}

// TestWithMethodsDeriveCopiesLeavingTheReceiverUntouched pins the value
// contract the With methods share: the derived copy carries the change and
// the client it came from does not, exercised through WithAccept as the
// representative and observed on the wire. The first request also pins the
// construction-time Accept default.
func TestWithMethodsDeriveCopiesLeavingTheReceiverUntouched(t *testing.T) {
	var (
		mutex   sync.Mutex
		accepts []string
	)
	server := httptest.NewServer(netHttp.HandlerFunc(func(writer netHttp.ResponseWriter, received *netHttp.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		accepts = append(accepts, received.Header.Get("Accept"))
		writer.WriteHeader(netHttp.StatusOK)
	}))
	t.Cleanup(server.Close)

	base := newClient(t, server)
	derived := base.WithAccept("application/vnd.pgrst.object+json")

	for _, client := range []http.Client{base, derived, base} {
		if _, err := client.Do(t.Context(), request.New(netHttp.MethodGet, "things")); err != nil {
			t.Fatalf("Do: %v", err)
		}
	}

	mutex.Lock()
	defer mutex.Unlock()
	want := []string{"application/json", "application/vnd.pgrst.object+json", "application/json"}
	if !slices.Equal(accepts, want) {
		t.Errorf("Accept sequence = %q, want %q", accepts, want)
	}
}
