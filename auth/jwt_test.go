package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeTokenValid(t *testing.T) {
	signer := newES256Signer(t, "key-1")
	token := signer.token(t, defaultClaims())

	decoded, err := decodeToken(token)
	if err != nil {
		t.Fatalf("decodeToken: %v", err)
	}
	if decoded.header.Algorithm != algorithmES256 {
		t.Errorf("alg = %q, want ES256", decoded.header.Algorithm)
	}
	if decoded.header.KeyID != "key-1" {
		t.Errorf("kid = %q, want key-1", decoded.header.KeyID)
	}
	if decoded.signingInput != token[:strings.LastIndexByte(token, '.')] {
		t.Errorf("signingInput = %q, want the header.payload prefix", decoded.signingInput)
	}
}

func TestDecodeTokenMalformed(t *testing.T) {
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
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeToken(token); !errors.Is(err, ErrMalformedJWT) {
				t.Errorf("decodeToken(%q) error = %v, want ErrMalformedJWT", token, err)
			}
		})
	}
}

func TestVerifySignatureRoundTrip(t *testing.T) {
	signers := map[string]testSigner{
		"ES256": newES256Signer(t, "es"),
		"RS256": newRS256Signer(t, "rs"),
		"EdDSA": newEdDSASigner(t, "ed"),
	}
	for name, signer := range signers {
		t.Run(name, func(t *testing.T) {
			decoded, err := decodeToken(signer.token(t, defaultClaims()))
			if err != nil {
				t.Fatalf("decodeToken: %v", err)
			}
			if err := verifySignature(decoded, signer.publicJWK); err != nil {
				t.Errorf("verifySignature: %v, want nil", err)
			}
		})
	}
}

func TestVerifySignatureTampered(t *testing.T) {
	signer := newES256Signer(t, "es")
	decoded, err := decodeToken(signer.token(t, defaultClaims()))
	if err != nil {
		t.Fatalf("decodeToken: %v", err)
	}
	// Flip a byte of the signing input so the signature no longer matches.
	decoded.signingInput += "x"
	if err := verifySignature(decoded, signer.publicJWK); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("verifySignature = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifySignatureWrongKey(t *testing.T) {
	signer := newES256Signer(t, "es")
	other := newES256Signer(t, "es")
	decoded, err := decodeToken(signer.token(t, defaultClaims()))
	if err != nil {
		t.Fatalf("decodeToken: %v", err)
	}
	if err := verifySignature(decoded, other.publicJWK); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("verifySignature with a different key = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifySignatureAlgorithmBoundToKey(t *testing.T) {
	// The token header claims ES256, but the matched key is RSA. Verification
	// must use the key's algorithm and fail, never trust the header's claim.
	signer := newES256Signer(t, "es")
	rsaKey := newRS256Signer(t, "es").publicJWK
	decoded, err := decodeToken(signer.token(t, defaultClaims()))
	if err != nil {
		t.Fatalf("decodeToken: %v", err)
	}
	if err := verifySignature(decoded, rsaKey); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("verifySignature = %v, want ErrInvalidSignature", err)
	}
}

func TestJSONWebKeyAlgorithmInference(t *testing.T) {
	cases := []struct {
		key  jsonWebKey
		want algorithm
	}{
		{jsonWebKey{Algorithm: algorithmES256}, algorithmES256},
		{jsonWebKey{KeyType: "RSA"}, algorithmRS256},
		{jsonWebKey{KeyType: "EC", Curve: "P-256"}, algorithmES256},
		{jsonWebKey{KeyType: "OKP", Curve: "Ed25519"}, algorithmEdDSA},
		{jsonWebKey{KeyType: "oct"}, ""},
	}
	for _, testCase := range cases {
		if got := testCase.key.algorithm(); got != testCase.want {
			t.Errorf("algorithm(%+v) = %q, want %q", testCase.key, got, testCase.want)
		}
	}
}
