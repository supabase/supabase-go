package asproject

import (
	"context"
	"net/http"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/otp"
	"github.com/supabase/supabase-go/integration-testing/testkit"
)

// redeemOnce redeems a code through verify and asserts the session it
// answers with: a session for the user with userID and email, whose access
// token verifies as theirs. It then asserts that a second redemption is
// refused with otp_expired, and returns the session.
func redeemOnce(t *testing.T, client *auth.Client, verify func(ctx context.Context) (*auth.VerifyOTPResult, error), userID, email string) *auth.Session {
	t.Helper()
	result, err := verify(t.Context())
	if err != nil {
		t.Fatalf("redeeming the code: %v", err)
	}
	session := result.Session()
	if session == nil {
		t.Fatal("Session() = nil, want the signed-in session")
	}
	if user := session.User(); user.ID() != userID || user.Email() != email {
		t.Errorf("Session().User() = (%q, %q), want (%q, %q)", user.ID(), user.Email(), userID, email)
	}
	if session.RefreshToken() == "" {
		t.Error("RefreshToken() is empty, want the new session's")
	}
	accessToken, err := session.AccessToken(t.Context())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	verified, err := client.GetClaims(t.Context(), accessToken)
	if err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	if claims := verified.Claims(); claims.Subject() != userID || claims.Email() != email {
		t.Errorf("claims = (sub %q, email %q), want (%q, %q)", claims.Subject(), claims.Email(), userID, email)
	}

	again, err := verify(t.Context())
	assertServerError(t, again, err, http.StatusForbidden, "otp_expired")
	return session
}

// TestSignInWithOTPByEmailCreatesTheAccount sends a code to a fresh address
// and proves the account the send created, confirmed and carrying the
// metadata the send stated, then signs it in with a code. The stack has no
// mail server, so the project mints the codes: the server refuses a recovery
// link for an address with no account, so minting one proves the account
// exists, and a magic link carries the code the email would have.
func TestSignInWithOTPByEmailCreatesTheAccount(t *testing.T) {
	client := newAuthClient(t)
	admin := newAdmin(t)
	email := testkit.UniqueEmail(t)

	if _, err := client.SignInWithOTPByEmail(t.Context(), email, otp.WithUserMetadata(map[string]any{"display_name": "Ada"})); err != nil {
		t.Fatalf("SignInWithOTPByEmail: %v", err)
	}
	recovery, err := admin.GenerateLink(t.Context(), auth.NewLinkRequest(email).Recovery())
	if err != nil {
		t.Fatalf("GenerateLink(Recovery) for the address the send created: %v", err)
	}
	account := recovery.User()
	if account.Email() != email || account.EmailConfirmedAt().IsZero() || account.UserMetadata()["display_name"] != "Ada" {
		t.Errorf("the created account = (%q, confirmed at %v, metadata %v), want %q, confirmed, with display_name Ada", account.Email(), account.EmailConfirmedAt(), account.UserMetadata(), email)
	}

	link, err := admin.GenerateLink(t.Context(), auth.NewLinkRequest(email).MagicLink())
	if err != nil {
		t.Fatalf("GenerateLink(MagicLink): %v", err)
	}
	redeemOnce(t, client, func(ctx context.Context) (*auth.VerifyOTPResult, error) {
		return client.VerifyOTPByEmail(ctx, email, link.EmailOTP(), otp.TypeEmail)
	}, account.ID(), email)
}

// TestVerifyOTPSignsIn redeems each kind of code an email carries, by code
// or by token hash, and proves the session each answers with and what each
// changes on the account. The stack has no mail server, so the project
// mints every code.
func TestVerifyOTPSignsIn(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	client := newAuthClient(t)
	admin := newAdmin(t)

	t.Run("a sign-up confirmation confirms the address", func(t *testing.T) {
		email := testkit.UniqueEmail(t)
		link, err := admin.GenerateLink(t.Context(), auth.NewLinkRequest(email).Signup(testkit.Password))
		if err != nil {
			t.Fatalf("GenerateLink(Signup): %v", err)
		}
		if !link.User().EmailConfirmedAt().IsZero() {
			t.Fatal("EmailConfirmedAt() is set before the confirmation is redeemed, want zero")
		}
		session := redeemOnce(t, client, func(ctx context.Context) (*auth.VerifyOTPResult, error) {
			return client.VerifyOTPByTokenHash(ctx, link.HashedToken(), otp.TypeEmail)
		}, link.User().ID(), email)
		if session.User().EmailConfirmedAt().IsZero() {
			t.Error("EmailConfirmedAt() is zero, want the confirmation recorded")
		}
	})

	t.Run("a recovery code signs the user in", func(t *testing.T) {
		user := testkit.SignUpUser(t, projectURL, apiKey)
		link, err := admin.GenerateLink(t.Context(), auth.NewLinkRequest(user.Email).Recovery())
		if err != nil {
			t.Fatalf("GenerateLink(Recovery): %v", err)
		}
		redeemOnce(t, client, func(ctx context.Context) (*auth.VerifyOTPResult, error) {
			return client.VerifyOTPByEmail(ctx, user.Email, link.EmailOTP(), otp.TypeRecovery)
		}, user.ID, user.Email)
	})

	t.Run("an invitation confirms the invited address", func(t *testing.T) {
		email := testkit.UniqueEmail(t)
		link, err := admin.GenerateLink(t.Context(), auth.NewLinkRequest(email).Invite())
		if err != nil {
			t.Fatalf("GenerateLink(Invite): %v", err)
		}
		session := redeemOnce(t, client, func(ctx context.Context) (*auth.VerifyOTPResult, error) {
			return client.VerifyOTPByTokenHash(ctx, link.HashedToken(), otp.TypeInvite)
		}, link.User().ID(), email)
		if user := session.User(); user.InvitedAt().IsZero() || user.EmailConfirmedAt().IsZero() {
			t.Errorf("the invited user = (invited at %v, confirmed at %v), want both set", user.InvitedAt(), user.EmailConfirmedAt())
		}
	})

	t.Run("an email change moves the account to the new address", func(t *testing.T) {
		user := testkit.SignUpUser(t, projectURL, apiKey)
		newEmail := testkit.UniqueEmail(t)
		link, err := admin.GenerateLink(t.Context(), auth.NewLinkRequest(user.Email).EmailChangeToCurrent(newEmail))
		if err != nil {
			t.Fatalf("GenerateLink(EmailChangeToCurrent): %v", err)
		}
		session := redeemOnce(t, client, func(ctx context.Context) (*auth.VerifyOTPResult, error) {
			return client.VerifyOTPByTokenHash(ctx, link.HashedToken(), otp.TypeEmailChange)
		}, user.ID, newEmail)
		if pending := session.User().NewEmail(); pending != "" {
			t.Errorf("NewEmail() = %q, want no change pending once it completed", pending)
		}
	})
}
