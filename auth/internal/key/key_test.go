package key_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/supabase/supabase-go/auth/internal/key"
	"github.com/supabase/supabase-go/auth/internal/testkit"
)

// onlyKey parses a JWK Set document and returns its single key.
func onlyKey(t *testing.T, document []byte) key.Key {
	t.Helper()
	keys, err := key.ParseSet(document)
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("ParseSet returned %d keys, want 1", len(keys))
	}
	return keys[0]
}

// splitToken separates a compact JWT into the signing input a signature covers
// and the decoded signature bytes.
func splitToken(t *testing.T, jwt string) (signingInput string, signature []byte) {
	t.Helper()
	lastDot := strings.LastIndexByte(jwt, '.')
	signature, err := base64.RawURLEncoding.DecodeString(jwt[lastDot+1:])
	if err != nil {
		t.Fatalf("decoding signature: %v", err)
	}
	return jwt[:lastDot], signature
}

func TestSupported(t *testing.T) {
	for _, alg := range []string{"ES256", "RS256", "EdDSA"} {
		if !key.Supported(alg) {
			t.Errorf("Supported(%q) = false, want true", alg)
		}
	}
	for _, alg := range []string{"HS256", "HS512", "none", ""} {
		if key.Supported(alg) {
			t.Errorf("Supported(%q) = true, want false", alg)
		}
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	signers := map[string]testkit.Signer{
		"ES256": testkit.NewES256(t, "es"),
		"RS256": testkit.NewRS256(t, "rs"),
		"EdDSA": testkit.NewEdDSA(t, "ed"),
	}
	for name, signer := range signers {
		t.Run(name, func(t *testing.T) {
			signingKey := onlyKey(t, testkit.JWKSetDocument(t, signer))
			if signingKey.ID() != signer.KeyID() {
				t.Errorf("ID = %q, want %q", signingKey.ID(), signer.KeyID())
			}
			signingInput, signature := splitToken(t, signer.Token(t, testkit.DefaultClaims()))
			if err := signingKey.Verify(signingInput, signature); err != nil {
				t.Errorf("Verify: %v, want nil", err)
			}
		})
	}
}

func TestVerifyTamperedSigningInput(t *testing.T) {
	signer := testkit.NewES256(t, "es")
	signingKey := onlyKey(t, testkit.JWKSetDocument(t, signer))
	signingInput, signature := splitToken(t, signer.Token(t, testkit.DefaultClaims()))

	// Flip a byte of the signing input so the signature no longer matches.
	if err := signingKey.Verify(signingInput+"x", signature); err == nil {
		t.Error("Verify = nil, want an error for a tampered signing input")
	}
}

func TestVerifyTamperedSignature(t *testing.T) {
	signer := testkit.NewES256(t, "es")
	signingKey := onlyKey(t, testkit.JWKSetDocument(t, signer))
	signingInput, signature := splitToken(t, signer.TamperedToken(t, testkit.DefaultClaims()))

	if err := signingKey.Verify(signingInput, signature); err == nil {
		t.Error("Verify = nil, want an error for a tampered signature")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	signer := testkit.NewES256(t, "es")
	other := testkit.NewES256(t, "es")
	signingKey := onlyKey(t, testkit.JWKSetDocument(t, other))
	signingInput, signature := splitToken(t, signer.Token(t, testkit.DefaultClaims()))

	if err := signingKey.Verify(signingInput, signature); err == nil {
		t.Error("Verify with a different key = nil, want an error")
	}
}

func TestVerifyAlgorithmBoundToKey(t *testing.T) {
	// The token header claims ES256, but the matched key is RSA. Verification
	// must use the key's algorithm and fail, never trust the header's claim.
	signer := testkit.NewES256(t, "es")
	rsaKey := onlyKey(t, testkit.JWKSetDocument(t, testkit.NewRS256(t, "es")))
	signingInput, signature := splitToken(t, signer.Token(t, testkit.DefaultClaims()))

	if err := rsaKey.Verify(signingInput, signature); err == nil {
		t.Error("Verify = nil, want an error - the key's algorithm must govern")
	}
}

// jwkSetDocumentWithoutAlg strips the alg field from each signer's JWK, so
// verification exercises the key-type inference.
func jwkSetDocumentWithoutAlg(t *testing.T, signers ...testkit.Signer) []byte {
	t.Helper()
	keys := make([]map[string]any, 0, len(signers))
	for _, signer := range signers {
		jwk := signer.PublicJWK()
		delete(jwk, "alg")
		keys = append(keys, jwk)
	}
	document, err := json.Marshal(map[string]any{"keys": keys})
	if err != nil {
		t.Fatalf("marshaling JWK Set document: %v", err)
	}
	return document
}

func TestVerifyInfersAlgorithmFromKeyType(t *testing.T) {
	signers := map[string]testkit.Signer{
		"EC P-256 infers ES256":    testkit.NewES256(t, "es"),
		"RSA infers RS256":         testkit.NewRS256(t, "rs"),
		"OKP Ed25519 infers EdDSA": testkit.NewEdDSA(t, "ed"),
	}
	for name, signer := range signers {
		t.Run(name, func(t *testing.T) {
			signingKey := onlyKey(t, jwkSetDocumentWithoutAlg(t, signer))
			signingInput, signature := splitToken(t, signer.Token(t, testkit.DefaultClaims()))
			if err := signingKey.Verify(signingInput, signature); err != nil {
				t.Errorf("Verify: %v, want nil via inferred algorithm", err)
			}
		})
	}
}

func TestVerifyRejectsSymmetricKeyType(t *testing.T) {
	// A symmetric key type infers no algorithm, so nothing verifies against it.
	signingKey := onlyKey(t, []byte(`{"keys":[{"kty":"oct","kid":"sym"}]}`))
	if err := signingKey.Verify("header.payload", []byte("signature")); err == nil {
		t.Error("Verify = nil, want an error for a key type that infers no algorithm")
	}
}

func TestParseSetMalformed(t *testing.T) {
	if _, err := key.ParseSet([]byte("not json")); err == nil {
		t.Error("ParseSet error = nil, want the decode error")
	}
}
