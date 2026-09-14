package integrationtest

import (
	"encoding/base64"
	"strings"
	"testing"
)

// cSpell:disable

// jwt was captured from a local integration test run, as an example static
// fixture for testing with. It is a JSON Web Token (JWT), meaning that it's
// encoded in the form [Header].[Payload].[Signature], where each of those parts
// is base64 encoded. Header and Payload are both JSON (textual) while the
// Signature is a raw, binary cryptographic hash.
const jwt string = "eyJhbGciOiJFUzI1NiIsImtpZCI6ImI4MTI2OWYxLTIxZDgtNGYyZS1iNzE5LWMyMjQwYTg0MGQ5MCIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJodHRwOi8vMTI3LjAuMC4xOjU0MzIxL2F1dGgvdjEiLCJzdWIiOiIzZDYzNDc5Zi02OWQ2LTRhMGUtOGY3Ni03ZTE0ZjA1ZDMxNjIiLCJhdWQiOiJhdXRoZW50aWNhdGVkIiwiZXhwIjoxNzg5MDMzODgyLCJpYXQiOjE3ODkwMzAyODIsImVtYWlsIjoidXNlci0xNzg5MDMwMjgyMjk3NTc3NTgxLXRlc3RnZXRjbGFpbXNyZWplY3RzdGFtcGVyZWR0b2tlbkBleGFtcGxlLmNvbSIsInBob25lIjoiIiwiYXBwX21ldGFkYXRhIjp7InByb3ZpZGVyIjoiZW1haWwiLCJwcm92aWRlcnMiOlsiZW1haWwiXX0sInVzZXJfbWV0YWRhdGEiOnsiZW1haWwiOiJ1c2VyLTE3ODkwMzAyODIyOTc1Nzc1ODEtdGVzdGdldGNsYWltc3JlamVjdHN0YW1wZXJlZHRva2VuQGV4YW1wbGUuY29tIiwiZW1haWxfdmVyaWZpZWQiOnRydWUsInBob25lX3ZlcmlmaWVkIjpmYWxzZSwic3ViIjoiM2Q2MzQ3OWYtNjlkNi00YTBlLThmNzYtN2UxNGYwNWQzMTYyIn0sInJvbGUiOiJhdXRoZW50aWNhdGVkIiwiYWFsIjoiYWFsMSIsImFtciI6W3sibWV0aG9kIjoicGFzc3dvcmQiLCJ0aW1lc3RhbXAiOjE3ODkwMzAyODJ9XSwic2Vzc2lvbl9pZCI6Ijc3ZjAwYjAzLTYxZGUtNDFhOS05ZWY1LTA5MGNlNmI3YjIwMiIsImlzX2Fub255bW91cyI6ZmFsc2V9.-EV4NqS-Z5mynoM7mE9z8a-g8TIwIpWjCUz5704e4oKzbLNosBAZPUXgllAMgqAS0d1D-GgW1GX4rI9IaZWMPA"

// cSpell:enable

func TestTamper(t *testing.T) {
	tampered := tamperSignature(jwt)
	if tampered == jwt {
		t.Fatal("tamperSignature returned the token unchanged")
	}
	originalParts := strings.Split(jwt, ".")
	tamperedParts := strings.Split(tampered, ".")
	if len(tamperedParts) != 3 {
		t.Fatalf("tampered token has %d parts, want 3", len(tamperedParts))
	}
	if tamperedParts[0] != originalParts[0] || tamperedParts[1] != originalParts[1] {
		t.Fatal("header and payload must be untouched")
	}
	if _, err := base64.RawURLEncoding.DecodeString(tamperedParts[2]); err != nil {
		t.Fatalf("tampered signature is no longer valid base64url: %v", err)
	}
}

// TestTamperSignaturePanicsOnEmptySignature pins the failure mode for tokens
// that no integration run should ever produce: any token that is empty or ends
// with a dot leaves an empty signature slice, and indexing it panics.
// Loud failure is acceptable in a fixture helper (fail early).
func TestTamperSignaturePanicsOnEmptySignature(t *testing.T) {
	testCases := []struct {
		name  string
		token string
	}{
		{name: "empty token", token: ""},
		{name: "empty parts", token: ".."},
		{name: "empty last part", token: "A.B."},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("tamperSignature returned without panicking")
				}
			}()
			tamperSignature(testCase.token)
		})
	}
}

// TestTamperSignatureNeedsOnlyANonEmptyFinalPart pins that tamperSignature
// never requires a three-part JWT: it flips the first character after the last
// dot, wherever that is.
func TestTamperSignatureNeedsOnlyANonEmptyFinalPart(t *testing.T) {
	testCases := []struct {
		name  string
		token string
		want  string
	}{
		{name: "two parts", token: "A.B", want: "A.A"},
		// A dot-less token is all signature. This row also exercises the 'A' to
		// 'B' flip branch, which the real token's '-'-leading signature never
		// takes.
		{name: "no dots", token: "AB", want: "BB"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := tamperSignature(testCase.token); got != testCase.want {
				t.Errorf("tamperSignature(%q) = %q, want %q", testCase.token, got, testCase.want)
			}
		})
	}
}
