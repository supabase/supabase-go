package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/supabase/supabase-go/auth"
)

func ExampleNew() {
	client, err := auth.New("https://PROJECT_ID.supabase.co", "API_KEY")
	fmt.Println(client != nil && err == nil)
	// Output: true
}

// ExampleClient_GetClaims verifies an end-user token and branches on the ways
// verification can fail, so a caller sees how each maps to an HTTP response.
func ExampleClient_GetClaims() {
	client, err := auth.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	claims, _, _, err := client.GetClaims(context.Background(), "END_USER_ACCESS_TOKEN")
	switch {
	case errors.Is(err, auth.ErrExpiredJWT):
		fmt.Println("token expired - ask the client to refresh")
	case errors.Is(err, auth.ErrMalformedJWT), errors.Is(err, auth.ErrInvalidSignature):
		fmt.Println("token forged or corrupt - reject")
	case err != nil:
		// Server-side verification failed, or the key set could not be fetched.
		var authError *auth.Error
		if errors.As(err, &authError) {
			fmt.Println("auth server rejected the token:", authError.HTTPStatus)
			return
		}
		fmt.Println(err)
	default:
		fmt.Printf("verified user %s acting as %s\n", claims.Subject(), claims.Role())
	}
}

// ExampleClient_GetUser fetches the authenticated user's current profile from
// the Auth server. Prefer it over GetClaims only when a decision needs
// server-fresh data - here, whether the email is confirmed right now, not
// whether it was confirmed when the token was minted.
func ExampleClient_GetUser() {
	client, err := auth.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	user, err := client.GetUser(context.Background(), "END_USER_ACCESS_TOKEN")
	if err != nil {
		fmt.Println(err)
		return
	}
	if user.EmailConfirmedAt().IsZero() {
		fmt.Println("email not yet confirmed")
		return
	}
	fmt.Println("welcome back", user.Email())
}

// claimsContextKey types the request-context key the middleware stores verified
// claims under, so handlers retrieve them without string-typed lookups.
type claimsContextKey struct{}

// Example_middleware is the canonical backend pattern: HTTP middleware verifies
// the inbound bearer token once, rejects anything invalid and hands verified
// claims to the wrapped handler through the request context. The protected
// handler then authorizes from those claims and can attach the same verified
// token to a Database client so the query runs under the user's Row Level
// Security policies.
func Example_middleware() {
	client, err := auth.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	authenticate := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			token, ok := bearerToken(request)
			if !ok {
				http.Error(writer, "missing bearer token", http.StatusUnauthorized)
				return
			}
			claims, _, _, err := client.GetClaims(request.Context(), token)
			if err != nil {
				// Every verification failure is a 401: never leak which check failed.
				http.Error(writer, "invalid token", http.StatusUnauthorized)
				return
			}
			request = request.WithContext(context.WithValue(request.Context(), claimsContextKey{}, claims))
			next.ServeHTTP(writer, request)
		})
	}

	deleteAccount := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		claims := request.Context().Value(claimsContextKey{}).(*auth.Claims)

		// Step up: a destructive action demands a second factor.
		if claims.AuthenticatorAssuranceLevel() != "aal2" {
			http.Error(writer, "multi-factor authentication required", http.StatusForbidden)
			return
		}

		// claims.Subject() is the verified user ID. Attach the request's token to
		// a Database client through its access-token provider to delete only this
		// user's rows under their Row Level Security policies.
		_, _ = fmt.Fprintf(writer, "deleting account for %s", claims.Subject())
	})

	http.Handle("/account/delete", authenticate(deleteAccount))
}

// bearerToken returns the token from an Authorization: Bearer header.
func bearerToken(request *http.Request) (string, bool) {
	header := request.Header.Get("Authorization")
	value, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || value == "" {
		return "", false
	}
	return value, true
}
