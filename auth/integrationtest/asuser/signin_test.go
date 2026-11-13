package asuser

import (
	"context"
	"net/http"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/messaging"
	"github.com/supabase/supabase-go/auth/otp"
	"github.com/supabase/supabase-go/auth/signup"
	"github.com/supabase/supabase-go/integration-testing/testkit"
)

// unregisteredPhone is a number no account on the stack holds: without an
// SMS provider, the stack registers no phone numbers.
const unregisteredPhone = "15555550100"

// TestSignUpByEmail registers an address on the stack, which confirms email
// accounts at once, and proves the answer: the user as stored, with the
// metadata the sign-up stated, and a session whose access token verifies as
// that user. The account then signs in by password, and the server's rulings
// on the same address again and on a short password surface as *auth.Error.
func TestSignUpByEmail(t *testing.T) {
	client := newAuthClient(t)
	email := testkit.UniqueEmail(t)

	result, err := client.SignUpByEmail(t.Context(), email, testkit.Password,
		signup.WithUserMetadata(map[string]any{"display_name": "Ada"}))
	if err != nil {
		t.Fatalf("SignUpByEmail: %v", err)
	}
	user := result.User()
	if user.ID() == "" || user.Email() != email || user.Role() != "authenticated" || user.Audience() != "authenticated" {
		t.Errorf("User() = (id %q, email %q, role %q, audience %q), want a new id, %q and authenticated", user.ID(), user.Email(), user.Role(), user.Audience(), email)
	}
	if user.EmailConfirmedAt().IsZero() {
		t.Error("EmailConfirmedAt() is zero, want the stack's confirmation at sign-up")
	}
	if user.UserMetadata()["display_name"] != "Ada" || user.AppMetadata()["provider"] != "email" {
		t.Errorf("metadata = (user %v, app %v), want display_name Ada and provider email", user.UserMetadata(), user.AppMetadata())
	}
	if identities := user.Identities(); len(identities) != 1 || identities[0].Provider() != "email" || identities[0].UserID() != user.ID() {
		t.Errorf("Identities() = %+v, want the user's one email identity", identities)
	}

	session := result.Session()
	if session == nil {
		t.Fatal("Session() = nil, want the session of an account the stack confirmed at once")
	}
	if profile := session.User(); profile.ID() != user.ID() || profile.Email() != email {
		t.Errorf("Session().User() = (%q, %q), want (%q, %q)", profile.ID(), profile.Email(), user.ID(), email)
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
	if claims := verified.Claims(); claims.Subject() != user.ID() || claims.Email() != email || claims.Role() != "authenticated" {
		t.Errorf("claims = (sub %q, email %q, role %q), want (%q, %q, authenticated)", claims.Subject(), claims.Email(), claims.Role(), user.ID(), email)
	}

	signedIn, err := client.SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: email, Password: testkit.Password})
	if err != nil {
		t.Fatalf("SignInWithPassword as the new account: %v", err)
	}
	if id := signedIn.Session().User().ID(); id != user.ID() {
		t.Errorf("the password sign-in's user = %q, want the new account %q", id, user.ID())
	}

	t.Run("an address with an account is user_already_exists", func(t *testing.T) {
		again, err := client.SignUpByEmail(t.Context(), email, testkit.Password)
		assertServerError(t, again, err, http.StatusUnprocessableEntity, "user_already_exists")
	})

	t.Run("a password below the minimum length is weak_password", func(t *testing.T) {
		short, err := client.SignUpByEmail(t.Context(), testkit.UniqueEmail(t), "short")
		assertServerError(t, short, err, http.StatusUnprocessableEntity, "weak_password")
	})
}

// TestSignUpByPhone proves a registration by phone reaches the server's
// phone route, which the stack keeps closed without an SMS provider, with
// phone_provider_disabled. A WhatsApp channel, which needs a provider that
// delivers over WhatsApp, is refused before that with validation_failed,
// which proves the chosen channel reaches the wire.
func TestSignUpByPhone(t *testing.T) {
	client := newAuthClient(t)

	bySMS, err := client.SignUpByPhone(t.Context(), unregisteredPhone, testkit.Password)
	assertServerError(t, bySMS, err, http.StatusBadRequest, "phone_provider_disabled")

	overWhatsApp, err := client.SignUpByPhone(t.Context(), unregisteredPhone, testkit.Password, signup.WithChannel(messaging.ChannelWhatsApp))
	assertServerError(t, overWhatsApp, err, http.StatusBadRequest, "validation_failed")
}

// TestSignInWithOTPByEmail sends a code to a fresh address, which creates its
// account, and proves the server's rulings around it. A second send within
// the minimum interval between sends to one address, a second on the stack,
// is over_email_send_rate_limit. Without user creation, an address with no
// account is otp_disabled while an address with one is sent its code.
func TestSignInWithOTPByEmail(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	client := newAuthClient(t)
	email := testkit.UniqueEmail(t)

	result, err := client.SignInWithOTPByEmail(t.Context(), email)
	if err != nil || result == nil {
		t.Fatalf("SignInWithOTPByEmail = (%v, %v), want the empty result", result, err)
	}
	again, err := client.SignInWithOTPByEmail(t.Context(), email)
	assertServerError(t, again, err, http.StatusTooManyRequests, "over_email_send_rate_limit")

	t.Run("without user creation an address with no account is otp_disabled", func(t *testing.T) {
		refused, err := client.SignInWithOTPByEmail(t.Context(), testkit.UniqueEmail(t), otp.WithoutUserCreation())
		assertServerError(t, refused, err, http.StatusUnprocessableEntity, "otp_disabled")
	})

	t.Run("without user creation an address with an account is sent its code", func(t *testing.T) {
		user := testkit.SignUpUser(t, projectURL, apiKey)
		sent, err := client.SignInWithOTPByEmail(t.Context(), user.Email, otp.WithoutUserCreation())
		if err != nil || sent == nil {
			t.Errorf("SignInWithOTPByEmail = (%v, %v), want the empty result", sent, err)
		}
	})
}

// TestSignInWithOTPByPhone proves a send to a phone reaches the server's
// phone route, which the stack keeps closed without an SMS provider, with
// phone_provider_disabled. Without user creation, a number with no account
// is refused before that with otp_disabled, which proves create_user reaches
// the wire.
func TestSignInWithOTPByPhone(t *testing.T) {
	client := newAuthClient(t)

	sent, err := client.SignInWithOTPByPhone(t.Context(), unregisteredPhone)
	assertServerError(t, sent, err, http.StatusBadRequest, "phone_provider_disabled")

	refused, err := client.SignInWithOTPByPhone(t.Context(), unregisteredPhone, otp.WithoutUserCreation())
	assertServerError(t, refused, err, http.StatusUnprocessableEntity, "otp_disabled")
}

// TestVerifyOTPRefusesAWrongCode proves that every route of verification
// refuses a code that matches nothing with otp_expired: a code sent to a
// registered address, a phone code of either kind and a token hash.
func TestVerifyOTPRefusesAWrongCode(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)
	client := newAuthClient(t)

	cases := []struct {
		name   string
		verify func(ctx context.Context) (*auth.VerifyOTPResult, error)
	}{
		{
			name: "a code sent to an email",
			verify: func(ctx context.Context) (*auth.VerifyOTPResult, error) {
				return client.VerifyOTPByEmail(ctx, user.Email, "000000", otp.TypeEmail)
			},
		},
		{
			name: "a code sent to a phone",
			verify: func(ctx context.Context) (*auth.VerifyOTPResult, error) {
				return client.VerifyOTPByPhone(ctx, unregisteredPhone, "000000", otp.TypeSMS)
			},
		},
		{
			name: "a code confirming a phone change",
			verify: func(ctx context.Context) (*auth.VerifyOTPResult, error) {
				return client.VerifyOTPByPhone(ctx, unregisteredPhone, "000000", otp.TypePhoneChange)
			},
		},
		{
			name: "a token hash",
			verify: func(ctx context.Context) (*auth.VerifyOTPResult, error) {
				return client.VerifyOTPByTokenHash(ctx, "not-a-token-hash", otp.TypeEmail)
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := testCase.verify(t.Context())
			assertServerError(t, result, err, http.StatusForbidden, "otp_expired")
		})
	}
}
