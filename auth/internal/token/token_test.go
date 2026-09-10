package token_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth/internal/testkit"
	"github.com/supabase/supabase-go/auth/internal/token"
)

// base64URL encodes bytes as a JWT segment: base64url without padding.
func base64URL(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

func TestDecodeValid(t *testing.T) {
	signer := testkit.NewES256(t, "key-1")
	jwt := signer.Token(t, testkit.DefaultClaims())

	decoded, err := token.Decode(jwt)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Algorithm() != "ES256" {
		t.Errorf("Algorithm = %q, want ES256", decoded.Algorithm())
	}
	if decoded.KeyID() != "key-1" {
		t.Errorf("KeyID = %q, want key-1", decoded.KeyID())
	}
	if decoded.SigningInput() != jwt[:strings.LastIndexByte(jwt, '.')] {
		t.Errorf("SigningInput = %q, want the header.payload prefix", decoded.SigningInput())
	}
	if len(decoded.Signature()) == 0 {
		t.Error("Signature is empty, want the decoded signature bytes")
	}
}

func TestDecodeMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"one part":          "OnlyOnePart",
		"two parts":         "header.payload",
		"four parts":        "a.b.c.d",
		"empty header":      ".payload.signature",
		"empty signature":   "header.payload.",
		"non-base64 header": "not*base64.cGF5bG9hZA.c2ln",
		"non-json header":   base64URL([]byte("not json")) + "." + base64URL([]byte(`{}`)) + ".c2ln",
	}
	for name, jwt := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := token.Decode(jwt); err == nil {
				t.Errorf("Decode(%q) error = nil, want a malformed-token error", jwt)
			}
		})
	}
}

// claimsOf builds a token carrying claims and parses them back out, so a case
// exercises the full wire round trip.
func claimsOf(t *testing.T, claims map[string]any) token.Claims {
	t.Helper()
	decoded, err := token.Decode(testkit.CraftToken(t, "ES256", "key-1", claims))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	parsed, err := decoded.Claims()
	if err != nil {
		t.Fatalf("Claims: %v", err)
	}
	return parsed
}

func TestClaimsTypedFields(t *testing.T) {
	claims := claimsOf(t, map[string]any{
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
	})

	if claims.Issuer() != "https://project.supabase.co/auth/v1" {
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

func TestClaimsAudienceArray(t *testing.T) {
	claims := claimsOf(t, map[string]any{
		"aud": []string{"authenticated", "other"},
		"exp": int64(1_800_000_000),
	})
	got := claims.Audience()
	if len(got) != 2 || got[0] != "authenticated" || got[1] != "other" {
		t.Errorf("Audience = %v, want [authenticated other]", got)
	}
}

func TestClaimsCustomClaims(t *testing.T) {
	claims := claimsOf(t, map[string]any{
		"sub":          "user-id",
		"exp":          int64(1_800_000_000),
		"app_metadata": map[string]any{"provider": "email"},
		"amr":          []map[string]any{{"method": "password", "timestamp": 1_700_000_000}},
		"tenant":       "acme",
	})

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

func TestClaimsAudienceCopied(t *testing.T) {
	claims := claimsOf(t, map[string]any{
		"aud": []string{"a", "b"},
		"exp": int64(1_800_000_000),
	})
	got := claims.Audience()
	got[0] = "mutated"
	if claims.Audience()[0] != "a" {
		t.Error("mutating the returned audience changed the claims")
	}
}

func TestClaimsPayloadNotJSON(t *testing.T) {
	// A structurally valid token whose payload decodes but is not a JSON
	// object: Decode succeeds, Claims must fail.
	jwt := base64URL([]byte(`{"alg":"ES256"}`)) + "." + base64URL([]byte("not json")) + "." + base64URL([]byte("sig"))
	decoded, err := token.Decode(jwt)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, err := decoded.Claims(); err == nil {
		t.Error("Claims() error = nil, want a malformed-token error")
	}
}

func TestClaimsMissingIssuedAt(t *testing.T) {
	claims := claimsOf(t, map[string]any{
		"sub": "user-id",
		"exp": int64(1_800_000_000),
	})
	if !claims.IssuedAt().IsZero() {
		t.Errorf("IssuedAt = %v, want zero", claims.IssuedAt())
	}
}
