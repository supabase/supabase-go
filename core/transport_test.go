package core_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/supabase/supabase-go/core"
)

func TestTransportInjectsHeaders(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		got = request.Header.Clone()
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	configuration, err := core.NewConfiguration(server.URL, "test-key",
		core.WithHeaders(map[string]string{"X-Client-Info": "supabase-go/test"}))
	if err != nil {
		t.Fatalf("NewConfiguration: %v", err)
	}

	response, err := configuration.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()

	if got.Get("apikey") != "test-key" {
		t.Errorf("apikey = %q, want %q", got.Get("apikey"), "test-key")
	}
	if got.Get("Authorization") != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", got.Get("Authorization"), "Bearer test-key")
	}
	if got.Get("X-Client-Info") != "supabase-go/test" {
		t.Errorf("X-Client-Info = %q, want %q", got.Get("X-Client-Info"), "supabase-go/test")
	}
}

func TestWithHTTPClientDoesNotMutateInput(t *testing.T) {
	custom := &http.Client{}
	configuration, err := core.NewConfiguration("https://project.supabase.co", "k", core.WithHTTPClient(custom))
	if err != nil {
		t.Fatalf("NewConfiguration: %v", err)
	}
	if custom.Transport != nil {
		t.Error("input client's Transport was mutated")
	}
	if configuration.Client() == custom {
		t.Error("Configuration reused the caller's client instead of cloning it")
	}
}
