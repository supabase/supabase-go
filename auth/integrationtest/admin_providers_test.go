package integrationtest

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth"
)

// uniqueIdentifier builds a provider identifier that no earlier run can have
// left behind, carrying the "custom:" prefix the server requires.
func uniqueIdentifier(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("custom:go-sdk-%d", time.Now().UnixNano())
}

// TestAdminCustomProviderLifecycle walks one provider through its whole life:
// create, get, list with and without the type filter, update, delete, gone.
// The provider is OAuth 2.0 because the server validates OAuth2 endpoints
// without contacting them at admin time - it resolves their hostnames, so the
// placeholder URLs use a real public host no request ever reaches - where
// OIDC creation fetches the issuer's discovery document and needs a live one.
func TestAdminCustomProviderLifecycle(t *testing.T) {
	admin := newAdmin(t)
	identifier := uniqueIdentifier(t)

	definition := auth.NewCustomProvider(identifier, "Go SDK Lifecycle", "client-id-1", "client-secret-1").
		OAuth2("https://example.com/authorize", "https://example.com/token", "https://example.com/userinfo").
		Scopes("profile", "email")
	created, err := admin.CreateCustomProvider(t.Context(), definition)
	if err != nil {
		t.Fatalf("CreateCustomProvider: %v", err)
	}
	if created.Identifier() != identifier || created.Type() != "oauth2" {
		t.Errorf("created = (%q, %q), want the registered identifier typed oauth2", created.Identifier(), created.Type())
	}
	if created.Name() != "Go SDK Lifecycle" || created.ClientID() != "client-id-1" {
		t.Errorf("created = (%q, %q), want the registered name and client id", created.Name(), created.ClientID())
	}
	if created.AuthorizationURL() != "https://example.com/authorize" || created.TokenURL() != "https://example.com/token" || created.UserinfoURL() != "https://example.com/userinfo" {
		t.Errorf("endpoints = (%q, %q, %q), want the registered URLs", created.AuthorizationURL(), created.TokenURL(), created.UserinfoURL())
	}
	// The definition deviated from no defaults, so the server's own apply.
	if !created.Enabled() || !created.PKCEEnabled() || created.EmailOptional() {
		t.Errorf("defaults = (enabled %t, pkce %t, email optional %t), want the server's (true, true, false)", created.Enabled(), created.PKCEEnabled(), created.EmailOptional())
	}
	if created.CreatedAt().IsZero() {
		t.Error("CreatedAt is zero, want the registration timestamp set")
	}

	fetched, err := admin.GetCustomProvider(t.Context(), identifier)
	if err != nil {
		t.Fatalf("GetCustomProvider: %v", err)
	}
	if fetched.ID() != created.ID() {
		t.Errorf("fetched ID = %q, want the created provider %q", fetched.ID(), created.ID())
	}

	listed, err := admin.ListCustomProviders(t.Context())
	if err != nil {
		t.Fatalf("ListCustomProviders: %v", err)
	}
	if !containsProvider(listed, identifier) {
		t.Errorf("listing does not contain %q, want it present", identifier)
	}
	oidcOnly, err := admin.ListCustomProviders(t.Context(), auth.WithOIDCOnly())
	if err != nil {
		t.Fatalf("ListCustomProviders with WithOIDCOnly: %v", err)
	}
	if containsProvider(oidcOnly, identifier) {
		t.Errorf("OIDC-only listing contains the oauth2 provider %q, want it filtered out", identifier)
	}

	updated, err := admin.UpdateCustomProvider(t.Context(), identifier,
		auth.WithName("Go SDK Lifecycle (disabled)"), auth.WithEnabled(false))
	if err != nil {
		t.Fatalf("UpdateCustomProvider: %v", err)
	}
	if updated.Name() != "Go SDK Lifecycle (disabled)" || updated.Enabled() {
		t.Errorf("updated = (%q, enabled %t), want the new name with sign-ins off", updated.Name(), updated.Enabled())
	}
	if updated.ClientID() != "client-id-1" {
		t.Errorf("ClientID = %q, want the unchosen field left alone", updated.ClientID())
	}

	if err := admin.DeleteCustomProvider(t.Context(), identifier); err != nil {
		t.Fatalf("DeleteCustomProvider: %v", err)
	}
	_, err = admin.GetCustomProvider(t.Context(), identifier)
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("GetCustomProvider after delete = %v, want *auth.Error", err)
	}
	if serverError.HTTPStatus != http.StatusNotFound || serverError.Code != "custom_provider_not_found" {
		t.Errorf("Error = %+v, want HTTP 404 custom_provider_not_found", serverError)
	}
}

// containsProvider reports whether providers holds one with the identifier.
func containsProvider(providers []auth.CustomProvider, identifier string) bool {
	for _, provider := range providers {
		if provider.Identifier() == identifier {
			return true
		}
	}
	return false
}

// TestAdminCreateOIDCProviderValidationFailure proves OIDC registration
// validates at admin time: the server fetches the issuer's discovery document
// before storing anything, and an issuer that serves none - here a resolvable
// public host with no OIDC deployment - is a clean rejection for the
// administrator rather than a latent failure at a user's sign-in.
func TestAdminCreateOIDCProviderValidationFailure(t *testing.T) {
	admin := newAdmin(t)

	definition := auth.NewCustomProvider(uniqueIdentifier(t), "No Discovery Here", "client-id", "client-secret").
		OIDC("https://example.com")
	_, err := admin.CreateCustomProvider(t.Context(), definition)
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("CreateCustomProvider = %v, want *auth.Error", err)
	}
	if serverError.Code != "validation_failed" {
		t.Errorf("Code = %q, want validation_failed", serverError.Code)
	}
}

// TestAdminCreateCustomProviderDuplicateIdentifier proves an identifier
// registers once: the second registration is the server's conflict.
func TestAdminCreateCustomProviderDuplicateIdentifier(t *testing.T) {
	admin := newAdmin(t)
	identifier := uniqueIdentifier(t)

	definition := auth.NewCustomProvider(identifier, "Go SDK Duplicate", "client-id", "client-secret").
		OAuth2("https://example.com/authorize", "https://example.com/token", "https://example.com/userinfo")
	if _, err := admin.CreateCustomProvider(t.Context(), definition); err != nil {
		t.Fatalf("CreateCustomProvider: %v", err)
	}

	_, err := admin.CreateCustomProvider(t.Context(), definition)
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("second CreateCustomProvider = %v, want *auth.Error", err)
	}
	if serverError.Code != "conflict" {
		t.Errorf("Code = %q, want conflict", serverError.Code)
	}

	if err := admin.DeleteCustomProvider(t.Context(), identifier); err != nil {
		t.Errorf("DeleteCustomProvider: %v", err)
	}
}
