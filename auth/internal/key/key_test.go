package key_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
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
	for _, alg := range []string{"ES256", "RS256", "EdDSA", "Ed25519", "Ed448"} {
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
			if err := signingKey.Verify(signer.Algorithm(), signingInput, signature); err != nil {
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
	if err := signingKey.Verify(signer.Algorithm(), signingInput+"x", signature); err == nil {
		t.Error("Verify = nil, want an error for a tampered signing input")
	}
}

func TestVerifyTamperedSignature(t *testing.T) {
	signer := testkit.NewES256(t, "es")
	signingKey := onlyKey(t, testkit.JWKSetDocument(t, signer))
	signingInput, signature := splitToken(t, signer.TamperedToken(t, testkit.DefaultClaims()))

	if err := signingKey.Verify(signer.Algorithm(), signingInput, signature); err == nil {
		t.Error("Verify = nil, want an error for a tampered signature")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	signer := testkit.NewES256(t, "es")
	other := testkit.NewES256(t, "es")
	signingKey := onlyKey(t, testkit.JWKSetDocument(t, other))
	signingInput, signature := splitToken(t, signer.Token(t, testkit.DefaultClaims()))

	if err := signingKey.Verify(signer.Algorithm(), signingInput, signature); err == nil {
		t.Error("Verify with a different key = nil, want an error")
	}
}

// jwkSetDocumentWith builds a JWK Set document of the signers' JWKs with
// overrides applied to each, a nil value removing the member.
func jwkSetDocumentWith(t *testing.T, overrides map[string]any, signers ...testkit.Signer) []byte {
	t.Helper()
	keys := make([]map[string]any, 0, len(signers))
	for _, signer := range signers {
		jwk := signer.PublicJWK()
		for name, value := range overrides {
			if value == nil {
				delete(jwk, name)
			} else {
				jwk[name] = value
			}
		}
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
			signingKey := onlyKey(t, jwkSetDocumentWith(t, map[string]any{"alg": nil}, signer))
			signingInput, signature := splitToken(t, signer.Token(t, testkit.DefaultClaims()))
			if err := signingKey.Verify(signer.Algorithm(), signingInput, signature); err != nil {
				t.Errorf("Verify: %v, want nil via inferred algorithm", err)
			}
		})
	}
}

// outcome names what an error from Verify tells its caller.
func outcome(err error) string {
	switch {
	case err == nil:
		return "verifies"
	case errors.Is(err, key.ErrUnusable):
		return "unusable"
	default:
		return "does not verify"
	}
}

// TestVerifyBinding pins the algorithm a key binds from its kty, crv and alg,
// which the token header must name: a key binding none implemented here is
// unusable, and a header naming another algorithm does not verify.
func TestVerifyBinding(t *testing.T) {
	ecSigner := testkit.NewES256(t, "kid-1")
	rsaSigner := testkit.NewRS256(t, "kid-1")
	edSigner := testkit.NewEdDSA(t, "kid-1")
	ed448PublicKey := base64.RawURLEncoding.EncodeToString(make([]byte, 57))
	testCases := []struct {
		name         string
		signer       testkit.Signer
		jwkOverrides map[string]any
		want         string
	}{
		{"Ed25519 key whose alg is Ed25519", edSigner, map[string]any{"alg": "Ed25519"}, "verifies"},
		{"header naming Ed25519", edSigner.ProposingAlgorithm("Ed25519"), nil, "verifies"},
		{"header naming Ed448 for an Ed25519 key", edSigner.ProposingAlgorithm("Ed448"), nil, "does not verify"},
		{"header naming ES256 for an RSA key", rsaSigner.ProposingAlgorithm("ES256"), nil, "does not verify"},
		{"RSA key whose alg is RS384", rsaSigner, map[string]any{"alg": "RS384"}, "unusable"},
		{"RSA key whose alg is ES256", rsaSigner, map[string]any{"alg": "ES256"}, "unusable"},
		{"EC key on P-384", ecSigner, map[string]any{"crv": "P-384"}, "unusable"},
		{"symmetric key", ecSigner, map[string]any{"kty": "oct"}, "unusable"},
		{"Ed448 key", edSigner, map[string]any{"crv": "Ed448", "x": ed448PublicKey}, "unusable"},
		{"Ed25519 key whose alg is Ed448", edSigner, map[string]any{"alg": "Ed448"}, "unusable"},
		{"Ed448 key whose alg is Ed25519", edSigner, map[string]any{"crv": "Ed448", "alg": "Ed25519", "x": ed448PublicKey}, "unusable"},
		{"EC key whose material does not import", ecSigner, map[string]any{"x": "%"}, "unusable"},
		{"RSA key whose material does not import", rsaSigner, map[string]any{"n": "%"}, "unusable"},
		{"Ed25519 key of the wrong size", edSigner, map[string]any{"x": base64.RawURLEncoding.EncodeToString(make([]byte, 31))}, "unusable"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			signingKey := onlyKey(t, jwkSetDocumentWith(t, testCase.jwkOverrides, testCase.signer))
			signingInput, signature := splitToken(t, testCase.signer.Token(t, testkit.DefaultClaims()))
			err := signingKey.Verify(testCase.signer.Algorithm(), signingInput, signature)
			if got := outcome(err); got != testCase.want {
				t.Errorf("Verify = %v (%s), want %s", err, got, testCase.want)
			}
		})
	}
}

func TestVerifyShortSignatureDoesNotVerify(t *testing.T) {
	// A signature of the wrong length fails the check: the key itself is usable.
	signer := testkit.NewES256(t, "es")
	signingKey := onlyKey(t, testkit.JWKSetDocument(t, signer))
	signingInput, signature := splitToken(t, signer.Token(t, testkit.DefaultClaims()))
	if err := signingKey.Verify(signer.Algorithm(), signingInput, signature[:63]); outcome(err) != "does not verify" {
		t.Errorf("Verify = %v, want a signature that does not verify", err)
	}
}

func TestParseSetMalformed(t *testing.T) {
	if _, err := key.ParseSet([]byte("not json")); err == nil {
		t.Error("ParseSet error = nil, want the decode error")
	}
}

// TestParseSetKeysArray pins how ParseSet reads a JSON document by its keys
// array, and by the lack of one.
func TestParseSetKeysArray(t *testing.T) {
	testCases := []struct {
		document string
		wantErr  bool
	}{
		{`{"keys":[]}`, false},
		{`{}`, false},
		{`{"keys":null}`, false},
		{`null`, false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.document, func(t *testing.T) {
			keys, err := key.ParseSet([]byte(testCase.document))
			if (err != nil) != testCase.wantErr {
				t.Errorf("ParseSet error = %v, want an error: %v", err, testCase.wantErr)
			}
			if len(keys) != 0 {
				t.Errorf("ParseSet returned %d keys, want none", len(keys))
			}
		})
	}
}
