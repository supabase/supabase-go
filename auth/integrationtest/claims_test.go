package integrationtest

import (
	"errors"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/integrationsupport"
)

// TestGetClaimsVerifiesSignedUpUser signs up a real user and verifies the
// issued access token locally against the stack's published signing key. The
// pinned CLI signs access tokens with an asymmetric ES256 key, so this
// exercises the JWK Set verification path end to end, not the server fallback.
func TestGetClaimsVerifiesSignedUpUser(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)
	user := integrationsupport.SignUpUser(t, projectURL, apiKey)
	client := newAuthClient(t)

	if algorithm := tokenAlgorithm(t, user.AccessToken); algorithm != "ES256" {
		t.Fatalf("access token alg = %q, want ES256 - the local stack is not signing asymmetrically", algorithm)
	}

	claims, err := client.GetClaims(t.Context(), user.AccessToken)
	if err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	if claims.Subject() != user.ID {
		t.Errorf("Subject = %q, want %q", claims.Subject(), user.ID)
	}
	if claims.Role() != "authenticated" {
		t.Errorf("Role = %q, want authenticated", claims.Role())
	}
	if claims.Email() != user.Email {
		t.Errorf("Email = %q, want %q", claims.Email(), user.Email)
	}
}

// TestGetClaimsRejectsTamperedToken proves local verification rejects a forged
// token: a real token whose signature has been altered fails against the
// published key rather than being accepted.
func TestGetClaimsRejectsTamperedToken(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)
	user := integrationsupport.SignUpUser(t, projectURL, apiKey)
	client := newAuthClient(t)

	_, err := client.GetClaims(t.Context(), tamperSignature(user.AccessToken))
	if !errors.Is(err, auth.ErrInvalidSignature) {
		t.Errorf("GetClaims(tampered) = %v, want ErrInvalidSignature", err)
	}
}

// TestGetUserRoundTrip fetches the signed-up user's profile from the Auth
// server and confirms a garbage token is rejected.
func TestGetUserRoundTrip(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)
	user := integrationsupport.SignUpUser(t, projectURL, apiKey)
	client := newAuthClient(t)

	fetched, err := client.GetUser(t.Context(), user.AccessToken)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if fetched.ID() != user.ID {
		t.Errorf("ID = %q, want %q", fetched.ID(), user.ID)
	}
	if fetched.Email() != user.Email {
		t.Errorf("Email = %q, want %q", fetched.Email(), user.Email)
	}

	var authError *auth.Error
	if _, err := client.GetUser(t.Context(), "not-a-real-token"); !errors.As(err, &authError) {
		t.Errorf("GetUser(garbage) = %v, want *auth.Error", err)
	}
}
