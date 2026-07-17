package configuration_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/supabase/supabase-go/core/configuration"
)

func TestTransportInjectsHeaders(t *testing.T) {
	server, received := recordingServer(t)
	projectConfiguration, err := configuration.New(server.URL, "test-key",
		configuration.WithHeader("X-App-Version", "1.0.0+user.generated"))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	sendGet(t, projectConfiguration.HTTPClient(), server.URL, nil)

	if received.Get("apikey") != "test-key" {
		t.Errorf("apikey = %q, want %q", received.Get("apikey"), "test-key")
	}
	if received.Get("X-Client-Info") != "supabase-go" {
		t.Errorf("X-Client-Info = %q, want %q", received.Get("X-Client-Info"), "supabase-go")
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
	projectConfiguration, err := configuration.New("https://PROJECT_ID.supabase.co", "k", configuration.WithHTTPClient(custom))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	if custom.Transport != nil {
		t.Error("input client's Transport was mutated")
	}
	if projectConfiguration.HTTPClient() == custom {
		t.Error("Configuration reused the caller's client instead of cloning it")
	}
}

func TestTransportHeaderPrecedence(t *testing.T) {
	server, received := recordingServer(t)
	projectConfiguration, err := configuration.New(server.URL, "project-key",
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
	// X-Client-Info is for Supabase SDK telemetry needs, so should remain as set by this SDK, not the user
	if received.Get("X-Client-Info") == "pretender" {
		t.Errorf("X-Client-Info = %q, but wanted SDK default", received.Get("X-Client-Info"))
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

func sendGet(t *testing.T, doer configuration.HTTPClient, url string, mutate func(*http.Request)) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
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
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
}
