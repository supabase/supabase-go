package asuser

import (
	"net/http"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/integration-testing/testkit"
)

// TestUpdateUser changes a signed-in user's metadata through the session,
// which replaces its profile and tells its listeners, and then their password
// through the client, after which only the new password signs in.
func TestUpdateUser(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)
	client := newAuthClient(t)
	signedIn, err := client.SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: user.Email, Password: testkit.Password})
	if err != nil {
		t.Fatalf("SignInWithPassword: %v", err)
	}
	session := signedIn.Session()

	updated := make(chan auth.SessionEvent, 1)
	remove := session.AddEventListener(func(event auth.SessionEvent) {
		if event.Name() == auth.SessionEventUserUpdated {
			select {
			case updated <- event:
			default:
			}
		}
	})
	defer remove()

	result, err := session.UpdateUser(t.Context(), auth.UserUpdate{UserMetadata: map[string]any{"plan": "pro"}})
	if err != nil {
		t.Fatalf("Session.UpdateUser: %v", err)
	}
	if result.User().UserMetadata()["plan"] != "pro" {
		t.Errorf("the result's metadata = %v, want the plan the update set", result.User().UserMetadata())
	}
	if session.User().UserMetadata()["plan"] != "pro" {
		t.Errorf("the session's metadata = %v, want the plan the update set", session.User().UserMetadata())
	}
	select {
	case <-updated:
	case <-time.After(5 * time.Second):
		t.Fatal("no SessionEventUserUpdated within five seconds of the update")
	}

	accessToken, err := session.AccessToken(t.Context())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	const newPassword = "integration-password-rotated"
	if _, err = client.UpdateUser(t.Context(), accessToken, auth.UserUpdate{Password: newPassword}); err != nil {
		t.Fatalf("Client.UpdateUser: %v", err)
	}
	if _, err = client.SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: user.Email, Password: newPassword}); err != nil {
		t.Errorf("SignInWithPassword with the new password: %v", err)
	}
	stale, err := client.SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: user.Email, Password: testkit.Password})
	assertServerError(t, stale, err, http.StatusBadRequest, "invalid_credentials")
}

// TestUpdateUserEmailWaitsOnConfirmation moves a signed-in user to a new
// address, which waits on confirmation: the profile the result and the
// session hold keeps the old address, with the new one in NewEmail.
func TestUpdateUserEmailWaitsOnConfirmation(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)
	client := newAuthClient(t)
	signedIn, err := client.SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: user.Email, Password: testkit.Password})
	if err != nil {
		t.Fatalf("SignInWithPassword: %v", err)
	}
	session := signedIn.Session()

	newEmail := testkit.UniqueEmail(t)
	result, err := session.UpdateUser(t.Context(), auth.UserUpdate{Email: newEmail})
	if err != nil {
		t.Fatalf("Session.UpdateUser: %v", err)
	}
	for holder, profile := range map[string]*auth.User{"result": result.User(), "session": session.User()} {
		if profile.Email() != user.Email {
			t.Errorf("the %s's Email = %q, want the old address %q until the new one is confirmed", holder, profile.Email(), user.Email)
		}
		if profile.NewEmail() != newEmail {
			t.Errorf("the %s's NewEmail = %q, want %q", holder, profile.NewEmail(), newEmail)
		}
	}
}
