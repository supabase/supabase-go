package exchange_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/supabase/supabase-go/postgrest/internal/exchange"
	"github.com/supabase/supabase-go/postgrest/internal/request"
	"github.com/supabase/supabase-go/postgrest/internal/respond"
)

// newClient builds a Client sending to the test server, with automatic
// retries off so a failure surfaces on the attempt that caused it.
func newClient(t *testing.T, server *httptest.Server) exchange.Client {
	t.Helper()
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	return exchange.New(server.Client(), *baseURL, false)
}

// TestDoReportsNon2xxAsResultNotError pins the package's side of the error
// boundary: a completed exchange comes back as a Result whatever its status
// code, body intact, leaving user-facing failure shaping to the caller.
func TestDoReportsNon2xxAsResultNotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
		respond.Body(t, writer, []byte(`{"message":"missing"}`))
	}))
	t.Cleanup(server.Close)

	result, err := newClient(t, server).Do(t.Context(), request.New(http.MethodGet, "things"))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if result.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusNotFound)
	}
	if got, want := string(result.Body), `{"message":"missing"}`; got != want {
		t.Errorf("Body = %q, want %q", got, want)
	}
}

// clientFunc is a configuration.HTTPClient that answers through the function
// itself.
type clientFunc func(*http.Request) (*http.Response, error)

func (f clientFunc) Do(httpRequest *http.Request) (*http.Response, error) { return f(httpRequest) }

// failingBody is a response body read from its Reader whose Close returns
// closeErr.
type failingBody struct {
	io.Reader
	closeErr error
}

func (b failingBody) Close() error { return b.closeErr }

// TestDoFailsOnABodyThatCannotBeReadInFull pins that a response body that
// cannot be read in full or closed fails the exchange, and is not retried
// even when its status would be.
func TestDoFailsOnABodyThatCannotBeReadInFull(t *testing.T) {
	broken := errors.New("broken")
	cases := []struct {
		name   string
		status int
		body   io.ReadCloser
	}{
		{"a body that cannot be closed", http.StatusOK, failingBody{Reader: strings.NewReader("[]"), closeErr: broken}},
		{"a retryable answer whose body cannot be drained", http.StatusServiceUnavailable, failingBody{Reader: iotest.ErrReader(broken)}},
		{"a retryable answer whose body cannot be closed", http.StatusServiceUnavailable, failingBody{Reader: strings.NewReader(""), closeErr: broken}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			requests := 0
			transport := clientFunc(func(*http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: testCase.status, Body: testCase.body}, nil
			})
			client := exchange.New(transport, url.URL{Scheme: "http", Host: "postgrest.test"}, true)

			result, err := client.Do(t.Context(), request.New(http.MethodGet, "things"))
			if result.StatusCode != 0 || !errors.Is(err, broken) {
				t.Errorf("Do = (%+v, %v), want the body's failure", result, err)
			}
			if requests != 1 {
				t.Errorf("requests = %d, want 1", requests)
			}
		})
	}
}

// TestDoPropagatesResolverErrorsVerbatim pins the token side of the error
// boundary: a resolver's error ends the exchange as the call's error,
// unwrapped, before any request is sent.
func TestDoPropagatesResolverErrorsVerbatim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the server was reached, want resolution to fail the exchange before any I/O")
	}))
	t.Cleanup(server.Close)

	sentinel := errors.New("resolution failed")
	client := newClient(t, server).WithTokenResolver(func(context.Context) (string, error) {
		return "", sentinel
	})

	_, err := client.Do(t.Context(), request.New(http.MethodGet, "things"))
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
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, received *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		accepts = append(accepts, received.Header.Get("Accept"))
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	base := newClient(t, server)
	derived := base.WithAccept("application/vnd.pgrst.object+json")

	for _, client := range []exchange.Client{base, derived, base} {
		if _, err := client.Do(t.Context(), request.New(http.MethodGet, "things")); err != nil {
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
