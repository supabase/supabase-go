package integrationtest

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/core/pagination"
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

// TestAdminUpdateUser walks one user through each update the attributes
// express: a new email, a metadata merge that leaves untouched keys alone, a
// nil-valued key as a delete, role with application metadata, then a ban
// raised and lifted.
func TestAdminUpdateUser(t *testing.T) {
	admin := newAdmin(t)
	email := uniqueEmail(t)

	created, err := admin.CreateUser(t.Context(), auth.UserAttributes{
		Email:        email,
		Password:     "integration-password",
		EmailConfirm: true,
		UserMetadata: map[string]any{"display_name": "Before", "locale": "en"},
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	updatedEmail := "updated-" + email
	afterEmail, err := admin.UpdateUser(t.Context(), created.ID(), auth.UserAttributes{Email: updatedEmail})
	if err != nil {
		t.Fatalf("UpdateUser(email): %v", err)
	}
	if afterEmail.Email() != updatedEmail {
		t.Errorf("Email = %q, want %q", afterEmail.Email(), updatedEmail)
	}

	afterMerge, err := admin.UpdateUser(t.Context(), created.ID(), auth.UserAttributes{
		UserMetadata: map[string]any{"display_name": "After"},
	})
	if err != nil {
		t.Fatalf("UpdateUser(metadata merge): %v", err)
	}
	if afterMerge.UserMetadata()["display_name"] != "After" {
		t.Errorf("display_name = %v, want After", afterMerge.UserMetadata()["display_name"])
	}
	if afterMerge.UserMetadata()["locale"] != "en" {
		t.Errorf("locale = %v, want the untouched en - an update merges rather than replaces", afterMerge.UserMetadata()["locale"])
	}

	afterDelete, err := admin.UpdateUser(t.Context(), created.ID(), auth.UserAttributes{
		UserMetadata: map[string]any{"locale": nil},
	})
	if err != nil {
		t.Fatalf("UpdateUser(metadata key delete): %v", err)
	}
	if _, present := afterDelete.UserMetadata()["locale"]; present {
		t.Errorf("locale = %v, want the key deleted by its nil value", afterDelete.UserMetadata()["locale"])
	}
	if afterDelete.UserMetadata()["display_name"] != "After" {
		t.Errorf("display_name = %v, want After surviving the delete", afterDelete.UserMetadata()["display_name"])
	}

	afterRole, err := admin.UpdateUser(t.Context(), created.ID(), auth.UserAttributes{
		Role:        "editor",
		AppMetadata: map[string]any{"team": "integration"},
	})
	if err != nil {
		t.Fatalf("UpdateUser(role and app metadata): %v", err)
	}
	if afterRole.Role() != "editor" {
		t.Errorf("Role = %q, want editor", afterRole.Role())
	}
	if afterRole.AppMetadata()["team"] != "integration" {
		t.Errorf("AppMetadata = %v, want team integration", afterRole.AppMetadata())
	}

	banned, err := admin.UpdateUser(t.Context(), created.ID(), auth.UserAttributes{BanDuration: "876000h"})
	if err != nil {
		t.Fatalf("UpdateUser(ban): %v", err)
	}
	if !banned.BannedUntil().After(time.Now()) {
		t.Errorf("BannedUntil = %v, want a future instant", banned.BannedUntil())
	}

	lifted, err := admin.UpdateUser(t.Context(), created.ID(), auth.UserAttributes{BanDuration: "none"})
	if err != nil {
		t.Fatalf("UpdateUser(ban lift): %v", err)
	}
	if !lifted.BannedUntil().IsZero() {
		t.Errorf("BannedUntil = %v after the lift, want the zero time", lifted.BannedUntil())
	}

	fetched, err := admin.GetUser(t.Context(), created.ID())
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if fetched.Email() != updatedEmail || fetched.Role() != "editor" {
		t.Errorf("fetched = (%q, %q), want the updated email and role", fetched.Email(), fetched.Role())
	}

	if err := admin.DeleteUser(t.Context(), created.ID()); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
}

// TestAdminListUsersPagination proves the page walk against the live stack:
// a two-per-page listing has full pages, a true next page and a last page,
// and walking every page finds each created user exactly where the pages
// say.
func TestAdminListUsersPagination(t *testing.T) {
	admin := newAdmin(t)

	prefix := fmt.Sprintf("admin-list-%d", time.Now().UnixNano())
	seen := make(map[string]bool, 3)
	identifiers := make([]string, 0, 3)
	for i := range 3 {
		email := fmt.Sprintf("%s-%d@example.com", prefix, i)
		user, err := admin.CreateUser(t.Context(), auth.UserAttributes{
			Email:        email,
			Password:     "integration-password",
			EmailConfirm: true,
		})
		if err != nil {
			t.Fatalf("CreateUser(%d): %v", i, err)
		}
		seen[email] = false
		identifiers = append(identifiers, user.ID())
	}

	first, err := admin.ListUsers(t.Context(), pagination.WithSize(2))
	if err != nil {
		t.Fatalf("ListUsers(first page): %v", err)
	}
	if len(first.Users()) != 2 {
		t.Errorf("first page = %d users, want a full page of 2", len(first.Users()))
	}
	if first.Total() < 3 {
		t.Errorf("Total = %d, want at least the 3 created users", first.Total())
	}
	if number, ok := first.NextPage(); !ok || number != 2 {
		t.Errorf("NextPage = (%d, %t), want (2, true)", number, ok)
	}
	if number, ok := first.LastPage(); !ok || number < 2 {
		t.Errorf("LastPage = (%d, %t), want a page of at least 2", number, ok)
	}

	for number, pages := 1, 0; ; pages++ {
		if pages > 1000 {
			t.Fatal("the page walk passed 1000 pages without ending")
		}
		page, err := admin.ListUsers(t.Context(), pagination.WithPage(number), pagination.WithSize(2))
		if err != nil {
			t.Fatalf("ListUsers(page %d): %v", number, err)
		}
		if len(page.Users()) > 2 {
			t.Errorf("page %d = %d users, want at most the restated size of 2", number, len(page.Users()))
		}
		for _, user := range page.Users() {
			if _, created := seen[user.Email()]; created {
				seen[user.Email()] = true
			}
		}
		next, ok := page.NextPage()
		if !ok {
			break
		}
		number = next
	}
	for email, found := range seen {
		if !found {
			t.Errorf("created user %s never appeared across the pages", email)
		}
	}

	for _, identifier := range identifiers {
		if err := admin.DeleteUser(t.Context(), identifier); err != nil {
			t.Errorf("DeleteUser(%s): %v", identifier, err)
		}
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
