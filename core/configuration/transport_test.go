package configuration_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
)

func TestTransportInjectsHeaders(t *testing.T) {
	server, received := recordingServer(t)
	projectConfiguration, err := configuration.New(core.ModulePathRoot, server.URL, "test-key",
		configuration.WithHeader("X-App-Version", "1.0.0+user.generated"))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	sendGet(t, projectConfiguration.HTTPClient(), server.URL, nil)

	if received.Get("apikey") != "test-key" {
		t.Errorf("apikey = %q, want %q", received.Get("apikey"), "test-key")
	}

	// The build is in-tree, so build information records no version for the
	// SDK's modules and the header carries the unknown-version value.
	clientInfo := received.Get("X-Client-Info")
	if !strings.HasPrefix(clientInfo, "supabase-go/0.0.0; ") {
		t.Errorf("X-Client-Info = %q, want prefix %q", clientInfo, "supabase-go/0.0.0; ")
	}

	if received.Get("Authorization") != "" {
		t.Errorf("Authorization = %q, want it absent because the transport never sets it", received.Get("Authorization"))
	}
	if received.Get("X-App-Version") != "1.0.0+user.generated" {
		t.Errorf("X-App-Version = %q, want %q", received.Get("X-App-Version"), "1.0.0+user.generated")
	}
}

func TestWithHTTPClientDoesNotMutateInput(t *testing.T) {
	custom := &http.Client{}
	_, err := configuration.New(core.ModulePathRoot, "https://PROJECT_ID.supabase.co", "k", configuration.WithHTTPClient(custom))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	if custom.Transport != nil {
		t.Error("input client's Transport was mutated")
	}
	if custom.CheckRedirect != nil {
		t.Error("input client's CheckRedirect was mutated")
	}
}

// TestWithHTTPClientPreservesTimeout guards the request_timeout capability's
// only moving part: the clone taken by the transport wrap must keep the
// caller's construction-time Timeout, or the deadline would silently vanish.
// The server never answers, so only the Timeout can end the request, and the
// error it ends with must still report itself as a timeout.
func TestWithHTTPClientPreservesTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		select {
		case <-release:
		case <-request.Context().Done(): // the client hung up first, as intended
		}
	}))
	defer server.Close()
	defer close(release) // unblocks the handler before Close waits on it

	projectConfiguration, err := configuration.New(core.ModulePathRoot, server.URL, "API_KEY",
		configuration.WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	_, err = projectConfiguration.HTTPClient().Do(request)
	urlError, ok := errors.AsType[*url.Error](err)
	if !ok {
		t.Fatalf("Do error = %v, want a *url.Error", err)
	}
	if !urlError.Timeout() {
		t.Errorf("Timeout() = false, want true: %v", urlError)
	}
}

func TestTransportHeaderPrecedence(t *testing.T) {
	server, received := recordingServer(t)
	projectConfiguration, err := configuration.New(core.ModulePathRoot, server.URL, "project-key",
		configuration.WithHeader("X-Dummy-Header", "default"))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	sendGet(t, projectConfiguration.HTTPClient(), server.URL, func(request *http.Request) {
		request.Header.Set("Authorization", "Bearer user-token")
		request.Header.Set("X-Dummy-Header", "per-request")
		request.Header.Set("apikey", "attacker-key")
		request.Header.Set("X-Client-Info", "pretender")
	})

	// A per-request end-user token passes through untouched.
	if received.Get("Authorization") != "Bearer user-token" {
		t.Errorf("Authorization = %q, want the per-request token preserved", received.Get("Authorization"))
	}
	// A per-request header beats the WithHeader default.
	if received.Get("X-Dummy-Header") != "per-request" {
		t.Errorf("X-Dummy-Header = %q, want the per-request value to win", received.Get("X-Dummy-Header"))
	}
	// apikey is always the project key and cannot be overridden.
	if received.Get("apikey") != "project-key" {
		t.Errorf("apikey = %q, want the project key forced", received.Get("apikey"))
	}
	// An X-Client-Info the application supplies replaces the SDK's value.
	if received.Get("X-Client-Info") != "pretender" {
		t.Errorf("X-Client-Info = %q, want the per-request value to win", received.Get("X-Client-Info"))
	}
}

// TestWithHeaderClientInfoReplacesTheSDKValue covers the cross-SDK client info
// spec's rule for an X-Client-Info header set through the client's global
// headers: it replaces the SDK's value on every request, matched without
// regard to the case of the header name.
func TestWithHeaderClientInfoReplacesTheSDKValue(t *testing.T) {
	server, received := recordingServer(t)
	projectConfiguration, err := configuration.New(core.ModulePathRoot, server.URL, "project-key",
		configuration.WithHeader("x-client-info", "wrapper-of-supabase-go/1.0.0"))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	sendGet(t, projectConfiguration.HTTPClient(), server.URL, nil)

	if got, want := received.Get("X-Client-Info"), "wrapper-of-supabase-go/1.0.0"; got != want {
		t.Errorf("X-Client-Info = %q, want the WithHeader value %q", got, want)
	}
	if got := received.Values("X-Client-Info"); len(got) != 1 {
		t.Errorf("X-Client-Info sent %d times, want once: %q", len(got), got)
	}
}

// TestTransportFailureError proves the error Do returns when a request gets
// no answer leaves the query string, which holds the filter values of a
// PostgREST read, out of its text. The error is a *url.Error, as http.Client
// returns, that still wraps the transport's failure, and its URL keeps only
// the scheme, host and path of the request's URL.
func TestTransportFailureError(t *testing.T) {
	projectConfiguration, err := configuration.New(core.ModulePathRoot, "https://PROJECT_ID.supabase.co", "API_KEY",
		configuration.WithHTTPClient(&http.Client{Transport: failingTransport{}}))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, "https://PROJECT_ID.supabase.co/rest/v1/players?select=id&email=eq.player%40example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	_, err = projectConfiguration.HTTPClient().Do(request)
	if !errors.Is(err, errBrokenWire) {
		t.Fatalf("Do error = %v, want errors.Is %v", err, errBrokenWire)
	}
	if _, ok := errors.AsType[*url.Error](err); !ok {
		t.Errorf("Do error type = %T, want *url.Error", err)
	}
	want := `Get "https://PROJECT_ID.supabase.co/rest/v1/players": broken wire`
	if err.Error() != want {
		t.Errorf("Do error = %q, want %q", err, want)
	}
}

func recordingServer(t *testing.T) (*httptest.Server, *http.Header) {
	t.Helper()
	var received http.Header
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received = request.Header.Clone()
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, &received
}

func sendGet(t *testing.T, doer configuration.HTTPClient, requestURL string, mutate func(*http.Request)) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if mutate != nil {
		mutate(request)
	}
	response, err := doer.Do(request)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_, err = io.Copy(io.Discard, response.Body)
	err = errors.Join(err, response.Body.Close())
	if err != nil {
		t.Fatalf("reading the response body: %v", err)
	}
}
