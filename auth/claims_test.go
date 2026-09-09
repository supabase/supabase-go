package auth

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestParseClaimsTypedFields(t *testing.T) {
	raw := map[string]any{
		"iss":          "https://project.supabase.co/auth/v1",
		"sub":          "user-id",
		"aud":          "authenticated",
		"exp":          int64(1_800_000_000),
		"iat":          int64(1_700_000_000),
		"role":         "authenticated",
		"aal":          "aal2",
		"session_id":   "session-id",
		"email":        "player@example.com",
		"phone":        "+15552368",
		"is_anonymous": true,
	}
	claimsBytes, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}

	claims, err := parseClaims(claimsBytes)
	if err != nil {
		t.Fatalf("parseClaims: %v", err)
	}

	if claims.Issuer() != raw["iss"] {
		t.Errorf("Issuer = %q", claims.Issuer())
	}
	if claims.Subject() != "user-id" {
		t.Errorf("Subject = %q", claims.Subject())
	}
	if got := claims.Audience(); len(got) != 1 || got[0] != "authenticated" {
		t.Errorf("Audience = %v", got)
	}
	if !claims.ExpiresAt().Equal(time.Unix(1_800_000_000, 0).UTC()) {
		t.Errorf("ExpiresAt = %v", claims.ExpiresAt())
	}
	if !claims.IssuedAt().Equal(time.Unix(1_700_000_000, 0).UTC()) {
		t.Errorf("IssuedAt = %v", claims.IssuedAt())
	}
	if claims.Role() != "authenticated" {
		t.Errorf("Role = %q", claims.Role())
	}
	if claims.AuthenticatorAssuranceLevel() != "aal2" {
		t.Errorf("AuthenticatorAssuranceLevel = %q", claims.AuthenticatorAssuranceLevel())
	}
	if claims.SessionID() != "session-id" {
		t.Errorf("SessionID = %q", claims.SessionID())
	}
	if claims.Email() != "player@example.com" {
		t.Errorf("Email = %q", claims.Email())
	}
	if claims.Phone() != "+15552368" {
		t.Errorf("Phone = %q", claims.Phone())
	}
	if !claims.IsAnonymous() {
		t.Error("IsAnonymous = false, want true")
	}
}

func TestParseClaimsAudienceArray(t *testing.T) {
	claimsBytes := []byte(`{"aud":["authenticated","other"],"exp":1800000000}`)
	claims, err := parseClaims(claimsBytes)
	if err != nil {
		t.Fatalf("parseClaims: %v", err)
	}
	got := claims.Audience()
	if len(got) != 2 || got[0] != "authenticated" || got[1] != "other" {
		t.Errorf("Audience = %v, want [authenticated other]", got)
	}
}

func TestParseClaimsCustomClaims(t *testing.T) {
	claimsBytes := []byte(`{
		"sub":"user-id",
		"exp":1800000000,
		"app_metadata":{"provider":"email"},
		"amr":[{"method":"password","timestamp":1700000000}],
		"tenant":"acme"
	}`)
	claims, err := parseClaims(claimsBytes)
	if err != nil {
		t.Fatalf("parseClaims: %v", err)
	}

	tenant, ok := claims.CustomClaim("tenant")
	if !ok || tenant != "acme" {
		t.Errorf("CustomClaim(tenant) = %v, %v", tenant, ok)
	}
	if _, ok := claims.CustomClaim("app_metadata"); !ok {
		t.Error("CustomClaim(app_metadata) not found, want present")
	}
	if _, ok := claims.CustomClaim("amr"); !ok {
		t.Error("CustomClaim(amr) not found, want present")
	}
	// A claim mapped to a typed accessor is not surfaced as a custom claim.
	if _, ok := claims.CustomClaim("sub"); ok {
		t.Error("CustomClaim(sub) found, want absent")
	}
}

func TestParseClaimsAudienceCopied(t *testing.T) {
	claims, err := parseClaims([]byte(`{"aud":["a","b"],"exp":1800000000}`))
	if err != nil {
		t.Fatalf("parseClaims: %v", err)
	}
	got := claims.Audience()
	got[0] = "mutated"
	if claims.Audience()[0] != "a" {
		t.Error("mutating the returned audience changed the claims")
	}
}

func TestParseClaimsMalformed(t *testing.T) {
	if _, err := parseClaims([]byte("not json")); !errors.Is(err, ErrMalformedJWT) {
		t.Errorf("parseClaims = %v, want ErrMalformedJWT", err)
	}
}

func TestParseClaimsMissingIssuedAt(t *testing.T) {
	claims, err := parseClaims([]byte(`{"sub":"user-id","exp":1800000000}`))
	if err != nil {
		t.Fatalf("parseClaims: %v", err)
	}
	if !claims.IssuedAt().IsZero() {
		t.Errorf("IssuedAt = %v, want zero", claims.IssuedAt())
	}
}
