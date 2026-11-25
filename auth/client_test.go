package auth_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/internal/testkit"
)

func TestGetClaimsVerifiesAsymmetricLocally(t *testing.T) {
	signers := map[string]testkit.Signer{
		"ES256": testkit.NewES256(t, "kid-1"),
		"RS256": testkit.NewRS256(t, "kid-1"),
		"EdDSA": testkit.NewEdDSA(t, "kid-1"),
	}
	for name, signer := range signers {
		t.Run(name, func(t *testing.T) {
			mock := newMockAuthServer(t, signer)
			client := mock.client(t)

			result, err := client.GetClaims(context.Background(), signer.Token(t, testkit.DefaultClaims()))
			if err != nil {
				t.Fatalf("GetClaims: %v", err)
			}
			claims, header, signature := result.Claims(), result.Header(), result.Signature()
			if claims.Subject() != "11111111-1111-1111-1111-111111111111" {
				t.Errorf("Subject = %q", claims.Subject())
			}
			if header.Algorithm() != name || header.KeyID() != "kid-1" || header.Type() != "JWT" {
				t.Errorf("header = (%q, %q, %q), want (%q, kid-1, JWT)", header.Algorithm(), header.KeyID(), header.Type(), name)
			}
			if len(signature) == 0 {
				t.Error("signature is empty, want the token's raw signature bytes")
			}
			if hits := mock.userHits.Load(); hits != 0 {
				t.Errorf("user endpoint hit %d times, want 0 for local verification", hits)
			}
			if fetches := mock.jwkSetFetches.Load(); fetches != 1 {
				t.Errorf("JWK Set fetched %d times, want 1", fetches)
			}
		})
	}
}

// TestGetClaimsReturnsSignatureCopy pins the returned signature to the token's
// actual signature bytes and proves it is a copy: corrupting one read's bytes
// must not bleed into the next.
func TestGetClaimsReturnsSignatureCopy(t *testing.T) {
	signer := testkit.NewES256(t, "kid-1")
	mock := newMockAuthServer(t, signer)
	client := mock.client(t)
	jwt := signer.Token(t, testkit.DefaultClaims())

	expected, err := base64.RawURLEncoding.DecodeString(jwt[strings.LastIndexByte(jwt, '.')+1:])
	if err != nil {
		t.Fatalf("decoding the token's signature segment: %v", err)
	}

	result, err := client.GetClaims(context.Background(), jwt)
	if err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	first := result.Signature()
	if !bytes.Equal(first, expected) {
		t.Fatalf("signature = %x, want the token's signature segment %x", first, expected)
	}

	first[0] ^= 0xFF
	if second := result.Signature(); !bytes.Equal(second, expected) {
		t.Error("second read differs - returned bytes are not a copy")
	}
}

func TestGetClaimsRejectsTamperedSignature(t *testing.T) {
	signer := testkit.NewES256(t, "kid-1")
	mock := newMockAuthServer(t, signer)
	client := mock.client(t)

	_, err := client.GetClaims(context.Background(), signer.TamperedToken(t, testkit.DefaultClaims()))
	if !errors.Is(err, auth.ErrInvalidSignature) {
		t.Errorf("GetClaims = %v, want ErrInvalidSignature", err)
	}
	if hits := mock.userHits.Load(); hits != 0 {
		t.Errorf("user endpoint hit %d times, want 0 - a bad signature must not fall back", hits)
	}
}

func TestGetClaimsRejectsExpired(t *testing.T) {
	signer := testkit.NewES256(t, "kid-1")
	mock := newMockAuthServer(t, signer)
	client := mock.client(t)

	claims := testkit.DefaultClaims()
	claims["exp"] = int64(1_600_000_000) // 2020
	if _, err := client.GetClaims(context.Background(), signer.Token(t, claims)); !errors.Is(err, auth.ErrExpiredJWT) {
		t.Errorf("GetClaims (past exp) = %v, want ErrExpiredJWT", err)
	}

	delete(claims, "exp")
	if _, err := client.GetClaims(context.Background(), signer.Token(t, claims)); !errors.Is(err, auth.ErrExpiredJWT) {
		t.Errorf("GetClaims (absent exp) = %v, want ErrExpiredJWT", err)
	}
}

// TestGetClaimsNotBeforeClaim pins how GetClaims treats a token's nbf claim
// on each verification route: the outcome and the requests made in reaching
// it.
func TestGetClaimsNotBeforeClaim(t *testing.T) {
	signer := testkit.NewES256(t, "kid-1")
	legacyToken := func(t *testing.T, claims map[string]any) string {
		t.Helper()
		return testkit.CraftToken(t, "HS256", "", claims)
	}
	now := time.Now()
	testCases := []struct {
		name              string
		mint              func(t *testing.T, claims map[string]any) string
		notBefore         time.Time
		wantErr           error
		wantUserHits      int32
		wantJWKSetFetches int32
	}{
		{"in the future, verified locally", signer.Token, now.Add(time.Hour), nil, 0, 1},
		{"in the future, routed to the server", legacyToken, now.Add(time.Hour), nil, 1, 0},
		{"in the past", signer.Token, now.Add(-time.Hour), nil, 0, 1},
		{"at the current second", signer.Token, now, nil, 0, 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			mock := newMockAuthServer(t, signer)
			client := mock.client(t)
			claims := testkit.DefaultClaims()
			claims["nbf"] = testCase.notBefore.Unix()

			result, err := client.GetClaims(context.Background(), testCase.mint(t, claims))
			if hits := mock.userHits.Load(); hits != testCase.wantUserHits {
				t.Errorf("user endpoint hit %d times, want %d", hits, testCase.wantUserHits)
			}
			if fetches := mock.jwkSetFetches.Load(); fetches != testCase.wantJWKSetFetches {
				t.Errorf("JWK Set fetched %d times, want %d", fetches, testCase.wantJWKSetFetches)
			}
			if testCase.wantErr != nil {
				if result != nil || !errors.Is(err, testCase.wantErr) {
					t.Errorf("GetClaims = (%v, %v), want (nil, %v)", result, err, testCase.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetClaims: %v", err)
			}
			notBefore, found := result.Claims().CustomClaim("nbf")
			if !found || notBefore != float64(testCase.notBefore.Unix()) {
				t.Errorf("CustomClaim(nbf) = (%v, %v), want (%d, true)", notBefore, found, testCase.notBefore.Unix())
			}
		})
	}
}

func TestGetClaimsRejectsMalformedAndEmpty(t *testing.T) {
	mock := newMockAuthServer(t)
	client := mock.client(t)

	if _, err := client.GetClaims(context.Background(), ""); !errors.Is(err, auth.ErrMissingJWT) {
		t.Errorf("GetClaims(empty) = %v, want ErrMissingJWT", err)
	}
	if _, err := client.GetClaims(context.Background(), "not-a-jwt"); !errors.Is(err, auth.ErrMalformedJWT) {
		t.Errorf("GetClaims(garbage) = %v, want ErrMalformedJWT", err)
	}
}

func TestGetClaimsRoutesLegacyTokenToServer(t *testing.T) {
	mock := newMockAuthServer(t)
	client := mock.client(t)

	// A legacy HS256 token cannot be verified locally, so the claims are trusted
	// only after the Auth server confirms the token with a 200.
	result, err := client.GetClaims(context.Background(), testkit.CraftToken(t, "HS256", "", testkit.DefaultClaims()))
	if err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	claims, header, signature := result.Claims(), result.Header(), result.Signature()
	if claims.Email() != "player@example.com" {
		t.Errorf("Email = %q", claims.Email())
	}
	if header.Algorithm() != "HS256" || len(signature) == 0 {
		t.Errorf("envelope = (%q, %d signature bytes), want HS256 with signature bytes on the server route too", header.Algorithm(), len(signature))
	}
	if hits := mock.userHits.Load(); hits != 1 {
		t.Errorf("user endpoint hit %d times, want 1", hits)
	}
	if fetches := mock.jwkSetFetches.Load(); fetches != 0 {
		t.Errorf("JWK Set fetched %d times, want 0 for a legacy token", fetches)
	}
}

func TestGetClaimsRoutesKeylessTokenToServer(t *testing.T) {
	mock := newMockAuthServer(t)
	client := mock.client(t)

	// Asymmetric alg but no key id: nothing to look up, so it defers to server.
	if _, err := client.GetClaims(context.Background(), testkit.CraftToken(t, "ES256", "", testkit.DefaultClaims())); err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	if hits := mock.userHits.Load(); hits != 1 {
		t.Errorf("user endpoint hit %d times, want 1", hits)
	}
}

func TestGetClaimsUnknownKeyIDFallsBackToServer(t *testing.T) {
	published := testkit.NewES256(t, "kid-known")
	foreign := testkit.NewES256(t, "kid-unknown")
	mock := newMockAuthServer(t, published)
	client := mock.client(t)

	if _, err := client.GetClaims(context.Background(), foreign.Token(t, testkit.DefaultClaims())); err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	if hits := mock.userHits.Load(); hits != 1 {
		t.Errorf("user endpoint hit %d times, want 1 (fallback for unknown kid)", hits)
	}

	// A second unknown-kid token arriving straight after must not force another
	// JWK Set fetch: the miss is answered from the fetch just made, and only
	// the server round trip repeats.
	if _, err := client.GetClaims(context.Background(), foreign.Token(t, testkit.DefaultClaims())); err != nil {
		t.Fatalf("GetClaims (second): %v", err)
	}
	if hits := mock.userHits.Load(); hits != 2 {
		t.Errorf("user endpoint hit %d times, want 2", hits)
	}
	if fetches := mock.jwkSetFetches.Load(); fetches != 1 {
		t.Errorf("JWK Set fetched %d times across an unknown-kid storm, want 1", fetches)
	}
}

func TestGetClaimsJWKSetFetchFailureSurfaces(t *testing.T) {
	signer := testkit.NewES256(t, "kid-1")
	mock := newMockAuthServer(t, signer)
	mock.jwkSetStatus = 500
	client := mock.client(t)

	// A discovery failure fails the call rather than falling back: the server's
	// response surfaces as *auth.Error, shaped by the fetch closure.
	_, err := client.GetClaims(context.Background(), signer.Token(t, testkit.DefaultClaims()))
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("GetClaims error = %v, want *auth.Error", err)
	}
	if serverError.HTTPStatus != 500 {
		t.Errorf("HTTPStatus = %d, want 500", serverError.HTTPStatus)
	}
	if hits := mock.userHits.Load(); hits != 0 {
		t.Errorf("user endpoint hit %d times, want 0 - a discovery failure must not fall back", hits)
	}
}

func TestGetClaimsServerRejectionSurfaces(t *testing.T) {
	mock := newMockAuthServer(t)
	mock.userStatus = 401
	mock.userBody = `{"code":"bad_jwt","message":"invalid token"}`
	client := mock.client(t)

	_, err := client.GetClaims(context.Background(), testkit.CraftToken(t, "HS256", "", testkit.DefaultClaims()))
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("GetClaims error = %v, want *auth.Error", err)
	}
	if serverError.HTTPStatus != 401 || serverError.Code != "bad_jwt" {
		t.Errorf("Error = %+v, want HTTP 401 code bad_jwt", serverError)
	}
}

func TestGetUser(t *testing.T) {
	mock := newMockAuthServer(t)
	mock.userBody = `{"id":"user-id","email":"player@example.com","role":"authenticated"}`
	client := mock.client(t)

	result, err := client.GetUser(context.Background(), testkit.CraftToken(t, "HS256", "", testkit.DefaultClaims()))
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	user := result.User()
	if user.ID() != "user-id" || user.Email() != "player@example.com" {
		t.Errorf("user = %+v", user)
	}
}

func TestGetUserRejection(t *testing.T) {
	mock := newMockAuthServer(t)
	mock.userStatus = 403
	mock.userBody = `{"code":"forbidden","message":"no"}`
	client := mock.client(t)

	_, err := client.GetUser(context.Background(), "any-token")
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("GetUser error = %v, want *auth.Error", err)
	}
	if serverError.HTTPStatus != 403 {
		t.Errorf("HTTPStatus = %d, want 403", serverError.HTTPStatus)
	}
}

func TestGetUserMissingToken(t *testing.T) {
	mock := newMockAuthServer(t)
	client := mock.client(t)

	if _, err := client.GetUser(context.Background(), ""); !errors.Is(err, auth.ErrMissingJWT) {
		t.Errorf("GetUser(empty) = %v, want ErrMissingJWT", err)
	}
}
