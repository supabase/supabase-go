package integrationtest

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/integration-testing/testkit"
)

// newAdmin wires the admin surface of an Auth client holding the local
// stack's secret key.
func newAdmin(t *testing.T) *auth.Admin {
	t.Helper()
	projectURL, secretKey := testkit.SecretCredentials(t)
	client, err := auth.New(projectURL, secretKey)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return client.Admin()
}

// uniqueEmail returns a lower-case address unique to this test run, the shape
// testkit.SignUpUser uses, so admin-created users never collide across runs
// against the shared stack.
func uniqueEmail(t *testing.T) string {
	t.Helper()
	safeName := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	return fmt.Sprintf("admin-%d-%s@example.com", time.Now().UnixNano(), safeName)
}

// TestAdminUserLifecycle proves the whole spine on one user: create returns
// the user as stored, get round-trips it by id, delete removes it and get
// then answers 404.
func TestAdminUserLifecycle(t *testing.T) {
	admin := newAdmin(t)
	email := uniqueEmail(t)

	created, err := admin.CreateUser(t.Context(), auth.UserAttributes{
		Email:        email,
		Password:     "integration-password",
		EmailConfirm: true,
		UserMetadata: map[string]any{"display_name": "Lifecycle"},
		AppMetadata:  map[string]any{"team": "integration"},
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.ID() == "" {
		t.Fatal("created user has no id")
	}
	if created.Email() != email {
		t.Errorf("Email = %q, want %q", created.Email(), email)
	}
	if created.EmailConfirmedAt().IsZero() {
		t.Error("EmailConfirmedAt is zero, want it set - the attributes marked the email confirmed")
	}
	if created.UserMetadata()["display_name"] != "Lifecycle" {
		t.Errorf("UserMetadata = %v, want display_name Lifecycle", created.UserMetadata())
	}
	if created.AppMetadata()["team"] != "integration" {
		t.Errorf("AppMetadata = %v, want team integration", created.AppMetadata())
	}

	fetched, err := admin.GetUser(t.Context(), created.ID())
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if fetched.ID() != created.ID() || fetched.Email() != email {
		t.Errorf("fetched = (%q, %q), want the created user", fetched.ID(), fetched.Email())
	}

	if err := admin.DeleteUser(t.Context(), created.ID()); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	_, err = admin.GetUser(t.Context(), created.ID())
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("GetUser after delete = %v, want *auth.Error", err)
	}
	if serverError.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", serverError.HTTPStatus)
	}
}

// TestAdminSoftDeleteUser proves deactivation keeps the user readable: the
// soft-deleted user answers get with DeletedAt set and an obfuscated email, a
// repeat soft delete succeeds while changing neither value, and the bare
// delete, which sends no body and leaves the mode to the server's default,
// then removes the record.
func TestAdminSoftDeleteUser(t *testing.T) {
	admin := newAdmin(t)
	email := uniqueEmail(t)

	created, err := admin.CreateUser(t.Context(), auth.UserAttributes{
		Email:        email,
		Password:     "integration-password",
		EmailConfirm: true,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if !created.DeletedAt().IsZero() {
		t.Fatalf("DeletedAt = %v on the created user, want the zero time", created.DeletedAt())
	}

	if err := admin.DeleteUser(t.Context(), created.ID(), auth.WithSoftDelete()); err != nil {
		t.Fatalf("DeleteUser(WithSoftDelete): %v", err)
	}

	deactivated, err := admin.GetUser(t.Context(), created.ID())
	if err != nil {
		t.Fatalf("GetUser after soft delete: %v", err)
	}
	if deactivated.DeletedAt().IsZero() {
		t.Error("DeletedAt is zero, want it set")
	}
	if deactivated.Email() == email {
		t.Errorf("Email = %q, want it obfuscated away from the created address", deactivated.Email())
	}

	if err := admin.DeleteUser(t.Context(), created.ID(), auth.WithSoftDelete()); err != nil {
		t.Errorf("repeat soft delete = %v, want success", err)
	}
	unchanged, err := admin.GetUser(t.Context(), created.ID())
	if err != nil {
		t.Fatalf("GetUser after repeat soft delete: %v", err)
	}
	if !unchanged.DeletedAt().Equal(deactivated.DeletedAt()) {
		t.Errorf("DeletedAt = %v after the repeat, want %v unchanged", unchanged.DeletedAt(), deactivated.DeletedAt())
	}
	if unchanged.Email() != deactivated.Email() {
		t.Errorf("Email = %q after the repeat, want %q unchanged", unchanged.Email(), deactivated.Email())
	}

	if err := admin.DeleteUser(t.Context(), created.ID()); err != nil {
		t.Fatalf("DeleteUser after soft delete: %v", err)
	}
	_, err = admin.GetUser(t.Context(), created.ID())
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("GetUser after hard delete = %v, want *auth.Error", err)
	}
	if serverError.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", serverError.HTTPStatus)
	}
}

// TestAdminRejectsPublishableKey proves the trust boundary: the same calls on
// a publishable-key client are rejected by the server as unauthorized.
func TestAdminRejectsPublishableKey(t *testing.T) {
	projectURL, publishableKey := testkit.Credentials(t)
	client, err := auth.New(projectURL, publishableKey)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	admin := client.Admin()

	operations := []struct {
		name string
		call func() error
	}{
		{"CreateUser", func() error {
			_, err := admin.CreateUser(t.Context(), auth.UserAttributes{Email: uniqueEmail(t)})
			return err
		}},
		{"GetUser", func() error {
			_, err := admin.GetUser(t.Context(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
			return err
		}},
		{"DeleteUser", func() error {
			return admin.DeleteUser(t.Context(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			err := operation.call()
			var serverError *auth.Error
			if !errors.As(err, &serverError) {
				t.Fatalf("%s = %v, want *auth.Error", operation.name, err)
			}
			if serverError.HTTPStatus != http.StatusUnauthorized && serverError.HTTPStatus != http.StatusForbidden {
				t.Errorf("HTTPStatus = %d, want 401 or 403", serverError.HTTPStatus)
			}
		})
	}
}
