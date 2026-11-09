package asuser

import (
	"context"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/integration-testing/testkit"
)

// TestAutoRefreshAgainstStack runs AutoRefresh against the local stack until
// the server-issued access token comes within the refresh lead, and asserts
// that the refresh lands with a new token the server verifies. It waits on
// the wall clock for about ten seconds, and requires the stack's access
// tokens to expire within 100 seconds of issue, failing at once when they
// do not.
func TestAutoRefreshAgainstStack(t *testing.T) {
	client := newAuthClient(t)
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)
	session := signIn(t, client, user)
	signedInToken, err := session.AccessToken(t.Context())
	if err != nil {
		t.Fatalf("AccessToken after sign-in: %v", err)
	}
	signedIn, err := client.GetClaims(t.Context(), signedInToken)
	if err != nil {
		t.Fatalf("GetClaims after sign-in: %v", err)
	}
	if lifetime := time.Until(signedIn.Claims().ExpiresAt()); lifetime > 100*time.Second {
		t.Fatalf("access token expires %s after sign-in, want at most 100s: [auth] jwt_expiry in integration-testing/supabase/config.toml must stay at 100", lifetime.Round(time.Second))
	}

	refreshed := make(chan auth.SessionEvent, 1)
	remove := session.AddEventListener(func(event auth.SessionEvent) {
		if event.Name() == auth.SessionEventTokenRefreshed {
			select {
			case refreshed <- event:
			default:
			}
		}
	})
	defer remove()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- session.AutoRefresh(ctx) }()

	select {
	case <-refreshed:
	case <-time.After(40 * time.Second):
		t.Fatal("no SessionEventTokenRefreshed within 40 seconds of sign-in")
	}
	refreshedToken, err := session.AccessToken(t.Context())
	if err != nil {
		t.Fatalf("AccessToken after the refresh: %v", err)
	}
	if refreshedToken == signedInToken {
		t.Error("AccessToken after the refresh is the token signed in with, want a new one")
	}
	result, err := client.GetClaims(t.Context(), refreshedToken)
	if err != nil {
		t.Fatalf("GetClaims with the refreshed token: %v", err)
	}
	if result.Claims().Subject() != user.ID {
		t.Errorf("the refreshed token's subject = %s, want user %s", result.Claims().Subject(), user.ID)
	}
	if !result.Claims().ExpiresAt().After(signedIn.Claims().ExpiresAt()) {
		t.Errorf("the refreshed token expires at %s, want later than the signed-in token's %s", result.Claims().ExpiresAt(), signedIn.Claims().ExpiresAt())
	}

	cancel()
	select {
	case err := <-returned:
		if err != nil {
			t.Errorf("AutoRefresh returned %v after cancellation, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AutoRefresh did not return within 5 seconds of cancellation")
	}
	if _, err := session.SignOut(t.Context()); err != nil {
		t.Errorf("SignOut: %v", err)
	}
}
