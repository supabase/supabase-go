package auth_test

import (
	"context"
	"testing"

	"github.com/supabase/supabase-go/auth/internal/testkit"
	"github.com/supabase/supabase-go/core/configuration"
)

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
