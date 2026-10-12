package integrationtest

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/supabase/supabase-go/auth"
)

// TestAdminInviteUser proves invite creates the user and marks the
// invitation: the returned user carries the email, the metadata and an
// invited-at timestamp, and the 200 itself proves the stack accepted the
// invitation email for delivery.
func TestAdminInviteUser(t *testing.T) {
	admin := newAdmin(t)
	email := uniqueEmail(t)

	user, err := admin.InviteUser(t.Context(), email,
		auth.WithUserMetadata(map[string]any{"workspace": "integration"}))
	if err != nil {
		t.Fatalf("InviteUser: %v", err)
	}

	if user.Email() != email {
		t.Errorf("Email = %q, want %q", user.Email(), email)
	}
	if user.InvitedAt().IsZero() {
		t.Error("InvitedAt is zero, want the invitation timestamp set")
	}
	if user.UserMetadata()["workspace"] != "integration" {
		t.Errorf("UserMetadata = %v, want workspace integration", user.UserMetadata())
	}

	if err := admin.DeleteUser(t.Context(), user.ID()); err != nil {
		t.Errorf("DeleteUser: %v", err)
	}
}

// TestAdminGenerateLinks mints one link of every kind against a single user:
// signup creates them, recovery and magiclink act on them existing, and the
// email-change pair covers both delivery targets. Each link must carry its
// verification artifacts and name its kind.
func TestAdminGenerateLinks(t *testing.T) {
	admin := newAdmin(t)
	ctx := t.Context()
	email := uniqueEmail(t)

	signup, err := admin.GenerateLink(ctx, auth.NewLinkRequest(email).
		Signup("integration-password").
		UserMetadata(map[string]any{"workspace": "integration"}))
	if err != nil {
		t.Fatalf("GenerateLink(Signup): %v", err)
	}

	if signup.User().ID() == "" {
		t.Fatal("signup link carries no user, want the created user")
	}
	if signup.VerificationType() != "signup" {
		t.Errorf("VerificationType = %q, want signup", signup.VerificationType())
	}
	if signup.EmailOTP() == "" || signup.HashedToken() == "" {
		t.Errorf("verification artifacts = (%q, %q), want both non-empty", signup.EmailOTP(), signup.HashedToken())
	}
	actionURL, err := url.Parse(signup.ActionLink())
	if err != nil {
		t.Fatalf("ActionLink %q does not parse: %v", signup.ActionLink(), err)
	}
	if actionURL.Query().Get("token") == "" || actionURL.Query().Get("type") != "signup" {
		t.Errorf("ActionLink query = %q, want a token and type=signup", actionURL.RawQuery)
	}

	recovery, err := admin.GenerateLink(ctx, auth.NewLinkRequest(email).Recovery())
	if err != nil {
		t.Fatalf("GenerateLink(Recovery): %v", err)
	}
	if recovery.VerificationType() != "recovery" {
		t.Errorf("VerificationType = %q, want recovery", recovery.VerificationType())
	}

	magic, err := admin.GenerateLink(ctx, auth.NewLinkRequest(email).MagicLink())
	if err != nil {
		t.Fatalf("GenerateLink(MagicLink): %v", err)
	}
	if magic.VerificationType() != "magiclink" {
		t.Errorf("VerificationType = %q, want magiclink", magic.VerificationType())
	}

	// The email-change pair needs secure email change, which the local
	// stack's config.toml enables via double_confirm_changes.
	newEmail := uniqueEmail(t)
	toCurrent, err := admin.GenerateLink(ctx, auth.NewLinkRequest(email).EmailChangeToCurrent(newEmail))
	if err != nil {
		t.Fatalf("GenerateLink(EmailChangeToCurrent): %v", err)
	}
	if toCurrent.VerificationType() != "email_change_current" {
		t.Errorf("VerificationType = %q, want email_change_current", toCurrent.VerificationType())
	}

	toNew, err := admin.GenerateLink(ctx, auth.NewLinkRequest(email).EmailChangeToNew(newEmail))
	if err != nil {
		t.Fatalf("GenerateLink(EmailChangeToNew): %v", err)
	}
	if toNew.VerificationType() != "email_change_new" {
		t.Errorf("VerificationType = %q, want email_change_new", toNew.VerificationType())
	}

	if err := admin.DeleteUser(ctx, signup.User().ID()); err != nil {
		t.Errorf("DeleteUser: %v", err)
	}
}

// TestGenerateLinkUnknownUser proves the modes that demand an existing user
// surface the server's rejection: recovery for an address that was never
// signed up answers HTTP 404.
func TestGenerateLinkUnknownUser(t *testing.T) {
	admin := newAdmin(t)

	_, err := admin.GenerateLink(t.Context(), auth.NewLinkRequest(uniqueEmail(t)).Recovery())
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("GenerateLink = %v, want *auth.Error", err)
	}
	if serverError.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", serverError.HTTPStatus)
	}
}

// TestGenerateLinkZeroValueLiteral pins the one escape the link request's
// typestate cannot close: a composite literal of a mode type compiles while
// carrying neither email nor mode. The server is the gate that catches it,
// rejecting the request at its first validation with HTTP 400 before acting
// on anything.
func TestGenerateLinkZeroValueLiteral(t *testing.T) {
	admin := newAdmin(t)

	_, err := admin.GenerateLink(t.Context(), auth.SignupLinkRequest{})
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("GenerateLink(SignupLinkRequest{}) = %v, want *auth.Error", err)
	}
	if serverError.HTTPStatus != http.StatusBadRequest {
		t.Errorf("HTTPStatus = %d, want 400", serverError.HTTPStatus)
	}
}
