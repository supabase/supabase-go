// Package asuser exercises the auth module against the live local Supabase
// stack that scripts/integration-test.sh starts, as a signed-in user: every
// client here holds the publishable key, and the secret key is absent from the
// package's environment.
package asuser

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/integration-testing/testkit"
)

// newAuthClient wires an Auth client at the local Supabase stack.
func newAuthClient(t *testing.T) *auth.Client {
	t.Helper()
	projectURL, apiKey := testkit.Credentials(t)
	client, err := auth.New(projectURL, apiKey)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return client
}

// tokenAlgorithm returns the alg from a JWT's header, so a test can assert the
// local stack signs access tokens asymmetrically.
func tokenAlgorithm(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("access token is not a three-part JWT")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decoding token header: %v", err)
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		t.Fatalf("parsing token header: %v", err)
	}
	return header.Algorithm
}

// tamperSignature flips one character of a JWT's signature, leaving it
// structurally valid but cryptographically invalid. This is a simplistic
// mutation, as it modifies the base64 encoding of the underlying raw signature,
// therefore "structurally valid" means that the three-part JWT remains validly
// formed ([Header].[Payload].[Signature]) and that the third part (Signature)
// remains valid base64 which when decoded has a different first byte in the
// raw, binary cryptographic hash.
func tamperSignature(token string) string {
	lastDot := strings.LastIndexByte(token, '.')
	signature := []byte(token[lastDot+1:])
	if signature[0] == 'A' {
		signature[0] = 'B'
	} else {
		signature[0] = 'A'
	}
	return token[:lastDot+1] + string(signature)
}
