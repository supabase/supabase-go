package asproject

import (
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/signout"
	"github.com/supabase/supabase-go/integration-testing/testkit"
)

// definitiveRefreshCodes are the codes the server answers a refresh of a
// revoked session with, its choice between them being its own.
var definitiveRefreshCodes = []string{"refresh_token_not_found", "session_not_found", "session_expired", "refresh_token_already_used"}

// TestRefreshAfterRevocation is the boundary between the project and a
// signed-in user: the project revokes a session with the secret key and the
// user's Session observes it. A refresh is rejected definitively while the
// unexpired access token still reads, a bare SignOut of a revoked session
// reports success because its goal state is already reached and
// WithOthersScope surfaces the same answer as the error it is, leaving its
// session live.
func TestRefreshAfterRevocation(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)
	client := newAuthClient(t)
	admin := newAdmin(t)

	adopted, err := client.SessionFromTokens(t.Context(), user.AccessToken, user.RefreshToken)
	if err != nil {
		t.Fatalf("SessionFromTokens: %v", err)
	}
	session := adopted.Session()
	signIn := func() *auth.Session {
		result, err := client.SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: user.Email, Password: testkit.Password})
		if err != nil {
			t.Fatalf("SignInWithPassword: %v", err)
		}
		return result.Session()
	}
	first, second := signIn(), signIn()
	secondAccessToken, err := second.AccessToken(t.Context())
	if err != nil {
		t.Fatalf("AccessToken of the second session: %v", err)
	}

	if result, err := admin.SignOut(t.Context(), user.AccessToken); err != nil || result == nil {
		t.Fatalf("Admin.SignOut = (%v, %v), want the empty result", result, err)
	}
	var serverError *auth.Error
	if result, err := client.GetUser(t.Context(), user.AccessToken); result != nil || !errors.As(err, &serverError) || serverError.HTTPStatus != http.StatusForbidden || serverError.Code != "session_not_found" {
		t.Errorf("GetUser after the revocation = (%v, %v), want *auth.Error with HTTP 403 and code session_not_found", result, err)
	}

	refreshed, err := session.Refresh(t.Context())
	if refreshed != nil || !errors.As(err, &serverError) || serverError.HTTPStatus != http.StatusBadRequest || !slices.Contains(definitiveRefreshCodes, serverError.Code) {
		t.Errorf("Refresh after revocation = (%v, %v), want *auth.Error with HTTP 400 and one of %v", refreshed, err, definitiveRefreshCodes)
	}
	if errors.Is(err, auth.ErrSessionExpired) {
		t.Error("Refresh reported ErrSessionExpired while the access token is unexpired")
	}
	if accessToken, err := session.AccessToken(t.Context()); err != nil || accessToken != user.AccessToken {
		t.Errorf("AccessToken() after the rejection = (%q, %v), want the unexpired token kept", accessToken, err)
	}
	if profile := session.User(); profile == nil || profile.ID() != user.ID || session.RefreshToken() != user.RefreshToken {
		t.Errorf("the rejected session holds (%+v, %q), want the adopted user and refresh token kept", profile, session.RefreshToken())
	}

	if result, err := first.SignOut(t.Context()); err != nil || result == nil {
		t.Errorf("a bare SignOut of a revoked session = (%v, %v), want the empty result", result, err)
	}
	if accessToken, err := first.AccessToken(t.Context()); accessToken != "" || !errors.Is(err, auth.ErrSignedOut) {
		t.Errorf("AccessToken() after that SignOut = (%q, %v), want ErrSignedOut", accessToken, err)
	}
	if first.RefreshToken() != "" || first.User() != nil {
		t.Error("the signed-out session still holds credentials")
	}

	result, err := second.SignOut(t.Context(), signout.WithOthersScope())
	if result != nil || !errors.As(err, &serverError) || serverError.HTTPStatus != http.StatusForbidden || serverError.Code != "session_not_found" {
		t.Errorf("SignOut with WithOthersScope of a revoked session = (%v, %v), want *auth.Error with HTTP 403 and code session_not_found", result, err)
	}
	if accessToken, err := second.AccessToken(t.Context()); err != nil || accessToken != secondAccessToken {
		t.Errorf("AccessToken() after that SignOut = (%q, %v), want the session kept live", accessToken, err)
	}

	if _, err := admin.DeleteUser(t.Context(), user.ID); err != nil {
		t.Errorf("DeleteUser: %v", err)
	}
}
