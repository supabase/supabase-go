package auth

import (
	"context"
	"errors"
	"testing"
)

func TestGetClaimsVerifiesAsymmetricLocally(t *testing.T) {
	signers := map[string]testSigner{
		"ES256": newES256Signer(t, "kid-1"),
		"RS256": newRS256Signer(t, "kid-1"),
		"EdDSA": newEdDSASigner(t, "kid-1"),
	}
	for name, signer := range signers {
		t.Run(name, func(t *testing.T) {
			mock := newMockAuthServer(t, signer.publicJWK)
			client := mock.client(t)

			claims, err := client.GetClaims(context.Background(), signer.token(t, defaultClaims()))
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
	signer := newES256Signer(t, "kid-1")
	mock := newMockAuthServer(t, signer.publicJWK)
	client := mock.client(t)

	_, err := client.GetClaims(context.Background(), tamperedToken(t, signer, defaultClaims()))
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("GetClaims = %v, want ErrInvalidSignature", err)
	}
	if hits := mock.userHits.Load(); hits != 0 {
		t.Errorf("user endpoint hit %d times, want 0 - a bad signature must not fall back", hits)
	}
}

func TestGetClaimsRejectsExpired(t *testing.T) {
	signer := newES256Signer(t, "kid-1")
	mock := newMockAuthServer(t, signer.publicJWK)
	client := mock.client(t)

	claims := defaultClaims()
	claims["exp"] = int64(1_600_000_000) // 2020
	if _, err := client.GetClaims(context.Background(), signer.token(t, claims)); !errors.Is(err, ErrExpiredJWT) {
		t.Errorf("GetClaims (past exp) = %v, want ErrExpiredJWT", err)
	}

	delete(claims, "exp")
	if _, err := client.GetClaims(context.Background(), signer.token(t, claims)); !errors.Is(err, ErrExpiredJWT) {
		t.Errorf("GetClaims (absent exp) = %v, want ErrExpiredJWT", err)
	}
}

func TestGetClaimsRejectsMalformedAndEmpty(t *testing.T) {
	mock := newMockAuthServer(t)
	client := mock.client(t)

	if _, err := client.GetClaims(context.Background(), ""); !errors.Is(err, ErrMissingJWT) {
		t.Errorf("GetClaims(empty) = %v, want ErrMissingJWT", err)
	}
	if _, err := client.GetClaims(context.Background(), "not-a-jwt"); !errors.Is(err, ErrMalformedJWT) {
		t.Errorf("GetClaims(garbage) = %v, want ErrMalformedJWT", err)
	}
}

func TestGetClaimsRoutesLegacyTokenToServer(t *testing.T) {
	mock := newMockAuthServer(t)
	client := mock.client(t)

	// A legacy HS256 token cannot be verified locally, so the claims are trusted
	// only after the Auth server confirms the token with a 200.
	claims, err := client.GetClaims(context.Background(), craftToken(t, "HS256", "", defaultClaims()))
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
	if _, err := client.GetClaims(context.Background(), craftToken(t, "ES256", "", defaultClaims())); err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	if hits := mock.userHits.Load(); hits != 1 {
		t.Errorf("user endpoint hit %d times, want 1", hits)
	}
}

func TestGetClaimsUnknownKeyIDFallsBackToServer(t *testing.T) {
	published := newES256Signer(t, "kid-known")
	foreign := newES256Signer(t, "kid-unknown")
	mock := newMockAuthServer(t, published.publicJWK)
	client := mock.client(t)

	if _, err := client.GetClaims(context.Background(), foreign.token(t, defaultClaims())); err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	if hits := mock.userHits.Load(); hits != 1 {
		t.Errorf("user endpoint hit %d times, want 1 (fallback for unknown kid)", hits)
	}
}

func TestGetClaimsServerRejectionSurfaces(t *testing.T) {
	mock := newMockAuthServer(t)
	mock.userStatus = 401
	mock.userBody = `{"error_code":"bad_jwt","msg":"invalid token"}`
	client := mock.client(t)

	_, err := client.GetClaims(context.Background(), craftToken(t, "HS256", "", defaultClaims()))
	var authError *Error
	if !errors.As(err, &authError) {
		t.Fatalf("GetClaims error = %v, want *auth.Error", err)
	}
	if authError.HTTPStatus != 401 || authError.Code != "bad_jwt" {
		t.Errorf("Error = %+v, want HTTP 401 code bad_jwt", authError)
	}
}

func TestGetUser(t *testing.T) {
	mock := newMockAuthServer(t)
	mock.userBody = `{"id":"user-id","email":"player@example.com","role":"authenticated"}`
	client := mock.client(t)

	user, err := client.GetUser(context.Background(), craftToken(t, "HS256", "", defaultClaims()))
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
	var authError *Error
	if !errors.As(err, &authError) {
		t.Fatalf("GetUser error = %v, want *auth.Error", err)
	}
	if authError.HTTPStatus != 403 {
		t.Errorf("HTTPStatus = %d, want 403", authError.HTTPStatus)
	}
}

func TestGetUserMissingToken(t *testing.T) {
	mock := newMockAuthServer(t)
	client := mock.client(t)

	if _, err := client.GetUser(context.Background(), ""); !errors.Is(err, ErrMissingJWT) {
		t.Errorf("GetUser(empty) = %v, want ErrMissingJWT", err)
	}
}
