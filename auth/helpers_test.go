package auth

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
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// base64URL encodes bytes as a JWT segment: base64url without padding.
func base64URL(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

// testSigner mints JWTs for one signing key and publishes the matching public
// JWK, so a test can drive verification end to end without the Auth server.
type testSigner struct {
	algorithm string
	keyID     string
	publicJWK jsonWebKey
	sign      func(signingInput string) []byte
}

// token builds and signs a JWT carrying claims, with the signer's algorithm and
// key id in the header.
func (s testSigner) token(t *testing.T, claims map[string]any) string {
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

// newES256Signer builds an ES256 signer with a freshly generated P-256 key.
func newES256Signer(t *testing.T, keyID string) testSigner {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating ES256 key: %v", err)
	}
	return testSigner{
		algorithm: "ES256",
		keyID:     keyID,
		publicJWK: jsonWebKey{
			KeyType:   "EC",
			KeyID:     keyID,
			Algorithm: "ES256",
			Curve:     "P-256",
			X:         base64URL(privateKey.X.FillBytes(make([]byte, 32))),
			Y:         base64URL(privateKey.Y.FillBytes(make([]byte, 32))),
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

// newRS256Signer builds an RS256 signer with a freshly generated RSA key.
func newRS256Signer(t *testing.T, keyID string) testSigner {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RS256 key: %v", err)
	}
	return testSigner{
		algorithm: "RS256",
		keyID:     keyID,
		publicJWK: jsonWebKey{
			KeyType:   "RSA",
			KeyID:     keyID,
			Algorithm: "RS256",
			Modulus:   base64URL(privateKey.N.Bytes()),
			Exponent:  base64URL(big.NewInt(int64(privateKey.E)).Bytes()),
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

// newEdDSASigner builds an EdDSA signer with a freshly generated Ed25519 key.
func newEdDSASigner(t *testing.T, keyID string) testSigner {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating EdDSA key: %v", err)
	}
	return testSigner{
		algorithm: "EdDSA",
		keyID:     keyID,
		publicJWK: jsonWebKey{
			KeyType:   "OKP",
			KeyID:     keyID,
			Algorithm: "EdDSA",
			Curve:     "Ed25519",
			X:         base64URL(publicKey),
		},
		sign: func(signingInput string) []byte {
			return ed25519.Sign(privateKey, []byte(signingInput))
		},
	}
}

// craftToken builds a structurally valid JWT with the given algorithm, key id
// and claims but a dummy signature. It suits routing cases where verification
// never reaches the signature - a legacy HS256 token, or one without a key id.
func craftToken(t *testing.T, algorithm, keyID string, claims map[string]any) string {
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

// tamperedToken mints a valid token and then corrupts its signature so
// verification fails while the token stays structurally valid.
func tamperedToken(t *testing.T, signer testSigner, claims map[string]any) string {
	t.Helper()
	valid := signer.token(t, claims)
	lastDot := strings.LastIndexByte(valid, '.')
	signature := []byte(valid[lastDot+1:])
	if signature[0] == 'A' {
		signature[0] = 'B'
	} else {
		signature[0] = 'A'
	}
	return valid[:lastDot+1] + string(signature)
}

// defaultClaims returns a claim set with the registered claims a valid token
// carries, expiring an hour out. Callers overwrite entries to vary a case.
func defaultClaims() map[string]any {
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

// mockAuthServer stands in for the Auth server, serving the JWK Set discovery
// endpoint and the user endpoint. Its counters and configurable responses let a
// test assert which path a call took.
type mockAuthServer struct {
	server *httptest.Server

	keys          []jsonWebKey
	jwkSetFetches atomic.Int32
	userHits      atomic.Int32
	userStatus    int
	userBody      string
}

// newMockAuthServer starts a server publishing keys at the discovery endpoint
// and answering the user endpoint with 200 and an empty user object by default.
func newMockAuthServer(t *testing.T, keys ...jsonWebKey) *mockAuthServer {
	t.Helper()
	mock := &mockAuthServer{keys: keys, userStatus: http.StatusOK, userBody: "{}"}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/v1/.well-known/jwks.json", func(writer http.ResponseWriter, _ *http.Request) {
		mock.jwkSetFetches.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"keys": mock.keys})
	})
	mux.HandleFunc("/auth/v1/user", func(writer http.ResponseWriter, _ *http.Request) {
		mock.userHits.Add(1)
		writer.WriteHeader(mock.userStatus)
		_, _ = writer.Write([]byte(mock.userBody))
	})
	mock.server = httptest.NewServer(mux)
	t.Cleanup(mock.server.Close)
	return mock
}

// client builds an auth client wired to the mock server.
func (m *mockAuthServer) client(t *testing.T) *Client {
	t.Helper()
	client, err := New(m.server.URL, "test-anon-key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}
