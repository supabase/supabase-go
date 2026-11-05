package auth_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/internal/testkit"
	"github.com/supabase/supabase-go/core/configuration"
)

// roundTripFunc is an http.RoundTripper that answers through the function
// itself.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// closeFailingBody is a response body read from its Reader whose Close fails
// with err.
type closeFailingBody struct {
	io.Reader
	err error
}

func (b closeFailingBody) Close() error { return b.err }

// TestRequestFailsOnABodyThatCannotBeClosed proves that a response body that
// cannot be closed fails the call, as one that cannot be read does.
func TestRequestFailsOnABodyThatCannotBeClosed(t *testing.T) {
	broken := errors.New("broken")
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: closeFailingBody{strings.NewReader("{}"), broken}}, nil
	})
	client, err := auth.New("https://PROJECT_ID.supabase.co", "test-anon-key", configuration.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := client.GetUser(t.Context(), "any-token")
	if result != nil || !errors.Is(err, broken) {
		t.Errorf("GetUser = (%v, %v), want the failure to close the body", result, err)
	}
}

// TestEveryRequestStatesAPIVersion proves that each request path the package
// sends - an admin call, the user fetch and the JWK Set fetch - states Auth
// API version 2024-01-01, and that a configured header of the same name never
// displaces it.
func TestEveryRequestStatesAPIVersion(t *testing.T) {
	competingHeader := configuration.WithHeader("X-Supabase-Api-Version", "2023-01-01")

	recorder := newAdminServer(t)
	admin := recorder.admin(t, competingHeader)
	if _, err := admin.GetUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"); err != nil {
		t.Fatalf("Admin.GetUser: %v", err)
	}
	if len(recorder.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(recorder.requests))
	}
	if version := recorder.requests[0].apiVersion; version != "2024-01-01" {
		t.Errorf("admin request API version = %q, want 2024-01-01", version)
	}

	signer := testkit.NewES256(t, "kid-1")
	mock := newMockAuthServer(t, signer)
	client := mock.client(t, competingHeader)
	if _, err := client.GetClaims(context.Background(), signer.Token(t, testkit.DefaultClaims())); err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	if mock.jwkSetAPIVersion != "2024-01-01" {
		t.Errorf("JWK Set fetch API version = %q, want 2024-01-01", mock.jwkSetAPIVersion)
	}
	if _, err := client.GetUser(context.Background(), "any-token"); err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if mock.userAPIVersion != "2024-01-01" {
		t.Errorf("user request API version = %q, want 2024-01-01", mock.userAPIVersion)
	}
}
