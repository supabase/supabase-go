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
