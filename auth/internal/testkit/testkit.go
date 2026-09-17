// Package testkit mints signed JWTs and publishes each signer's matching
// public JWK, so tests across the auth module drive verification end to end
// without an Auth server. It knows nothing of the packages under test: signers
// speak only the wire formats, publishing each JWK as a plain map ready for a
// JWK Set document or an httptest discovery endpoint.
package testkit

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"math/big"
	"strings"
	"testing"
	"time"
)

// base64URL encodes bytes as a JWT segment: base64url without padding.
func base64URL(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

// Signer mints JWTs for one signing key and publishes the matching public
// JWK.
type Signer struct {
	algorithm string
	keyID     string
	publicJWK map[string]any
	sign      func(signingInput string) []byte
}

// KeyID returns the key id the signer stamps into token headers and its
// published JWK.
func (s Signer) KeyID() string { return s.keyID }

// PublicJWK returns a copy of the signer's public JWK, ready to serialize
// into a JWK Set document.
func (s Signer) PublicJWK() map[string]any { return maps.Clone(s.publicJWK) }

// Token builds and signs a JWT carrying claims, with the signer's algorithm
// and key id in the header.
func (s Signer) Token(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": s.algorithm, "typ": "JWT"}
	if s.keyID != "" {
		header["kid"] = s.keyID
	}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshaling header: %v", err)
	}
	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshaling claims: %v", err)
	}
	signingInput := base64URL(headerBytes) + "." + base64URL(claimsBytes)
	return signingInput + "." + base64URL(s.sign(signingInput))
}

// TamperedToken mints a valid token and then corrupts its signature so
// verification fails while the token stays structurally valid.
func (s Signer) TamperedToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	valid := s.Token(t, claims)
	lastDot := strings.LastIndexByte(valid, '.')
	signature := []byte(valid[lastDot+1:])
	if signature[0] == 'A' {
		signature[0] = 'B'
	} else {
		signature[0] = 'A'
	}
	return valid[:lastDot+1] + string(signature)
}

// NewES256 builds an ES256 signer with a freshly generated P-256 key.
func NewES256(t *testing.T, keyID string) Signer {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating ES256 key: %v", err)
	}

	// The SEC 1 uncompressed point: one form byte, then the two fixed-width
	// 32-byte coordinates - exactly the full-size values a JWK requires.
	publicKeyPoint, err := privateKey.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("encoding ES256 public key: %v", err)
	}

	return Signer{
		algorithm: "ES256",
		keyID:     keyID,
		publicJWK: map[string]any{
			"kty": "EC",
			"kid": keyID,
			"alg": "ES256",
			"crv": "P-256",
			"x":   base64URL(publicKeyPoint[1:33]),
			"y":   base64URL(publicKeyPoint[33:65]),
		},
		sign: func(signingInput string) []byte {
			digest := sha256.Sum256([]byte(signingInput))
			r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
			if err != nil {
				t.Fatalf("signing ES256: %v", err)
			}
			signature := make([]byte, 64)
			r.FillBytes(signature[:32])
			s.FillBytes(signature[32:])
			return signature
		},
	}
}

// NewRS256 builds an RS256 signer with a freshly generated RSA key.
func NewRS256(t *testing.T, keyID string) Signer {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RS256 key: %v", err)
	}
	return Signer{
		algorithm: "RS256",
		keyID:     keyID,
		publicJWK: map[string]any{
			"kty": "RSA",
			"kid": keyID,
			"alg": "RS256",
			"n":   base64URL(privateKey.N.Bytes()),
			"e":   base64URL(big.NewInt(int64(privateKey.E)).Bytes()),
		},
		sign: func(signingInput string) []byte {
			digest := sha256.Sum256([]byte(signingInput))
			signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
			if err != nil {
				t.Fatalf("signing RS256: %v", err)
			}
			return signature
		},
	}
}

// NewEdDSA builds an EdDSA signer with a freshly generated Ed25519 key.
func NewEdDSA(t *testing.T, keyID string) Signer {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating EdDSA key: %v", err)
	}
	return Signer{
		algorithm: "EdDSA",
		keyID:     keyID,
		publicJWK: map[string]any{
			"kty": "OKP",
			"kid": keyID,
			"alg": "EdDSA",
			"crv": "Ed25519",
			"x":   base64URL(publicKey),
		},
		sign: func(signingInput string) []byte {
			return ed25519.Sign(privateKey, []byte(signingInput))
		},
	}
}

// CraftToken builds a structurally valid JWT with the given algorithm, key id
// and claims but a dummy signature. It suits routing cases where verification
// never reaches the signature - a legacy HS256 token, or one without a key id.
func CraftToken(t *testing.T, algorithm, keyID string, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": algorithm, "typ": "JWT"}
	if keyID != "" {
		header["kid"] = keyID
	}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshaling header: %v", err)
	}
	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshaling claims: %v", err)
	}
	return base64URL(headerBytes) + "." + base64URL(claimsBytes) + "." + base64URL([]byte("signature"))
}

// DefaultClaims returns a claim set with the registered claims a valid token
// carries, expiring an hour out. Callers overwrite entries to vary a case.
func DefaultClaims() map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":          "https://project.supabase.co/auth/v1",
		"sub":          "11111111-1111-1111-1111-111111111111",
		"aud":          "authenticated",
		"exp":          now.Add(time.Hour).Unix(),
		"iat":          now.Unix(),
		"role":         "authenticated",
		"aal":          "aal1",
		"session_id":   "22222222-2222-2222-2222-222222222222",
		"email":        "player@example.com",
		"is_anonymous": false,
	}
}

// JWKSetDocument serializes the signers' public keys as a JWK Set document,
// the JSON shape the discovery endpoint serves.
func JWKSetDocument(t *testing.T, signers ...Signer) []byte {
	t.Helper()
	keys := make([]map[string]any, 0, len(signers))
	for _, signer := range signers {
		keys = append(keys, signer.PublicJWK())
	}
	document, err := json.Marshal(map[string]any{"keys": keys})
	if err != nil {
		t.Fatalf("marshaling JWK Set document: %v", err)
	}
	return document
}
