package auth_test

import (
	"context"
	"errors"
	"testing"

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

			claims, err := client.GetClaims(context.Background(), signer.Token(t, testkit.DefaultClaims()))
			if err != nil {
				t.Fatalf("GetClaims: %v", err)
			}
			if claims.Subject() != "11111111-1111-1111-1111-111111111111" {
				t.Errorf("Subject = %q", claims.Subject())
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
	claims, err := client.GetClaims(context.Background(), testkit.CraftToken(t, "HS256", "", testkit.DefaultClaims()))
	if err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	if claims.Email() != "player@example.com" {
		t.Errorf("Email = %q", claims.Email())
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
	mock.userBody = `{"error_code":"bad_jwt","msg":"invalid token"}`
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

	user, err := client.GetUser(context.Background(), testkit.CraftToken(t, "HS256", "", testkit.DefaultClaims()))
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.ID() != "user-id" || user.Email() != "player@example.com" {
		t.Errorf("user = %+v", user)
	}
}

func TestGetUserRejection(t *testing.T) {
	mock := newMockAuthServer(t)
	mock.userStatus = 403
	mock.userBody = `{"error_code":"forbidden","msg":"no"}`
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
