package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/core/pagination"
)

// ExampleClient_GetClaims verifies an end-user token and branches on the ways
// verification can fail, so a caller sees how each maps to an HTTP response.
func ExampleClient_GetClaims() {
	client, err := auth.New("https://PROJECT_ID.supabase.co", "sb_publishable_...")
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
	client, err := auth.New("https://PROJECT_ID.supabase.co", "sb_publishable_...")
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
//
// Note: There is no formal "middleware" type in the standard library. This example
// follows the conventional decorator shape, whereby the wrapper is itself an
// [http.Handler] and so wrappers nest.
//
//	func middleware(next http.Handler) http.Handler
func Example_middleware() {
	client, err := auth.New("https://PROJECT_ID.supabase.co", "sb_publishable_...")
	if err != nil {
		fmt.Println(err)
		return
	}

	// authenticate is a middleware that deals only with authentication, the
	// "who are you, and is this token genuine?" question that is identical for
	// every protected endpoint, thus exists as reusable middleware. May also be
	// referred to as the layer that performs universal identity checks.
	authenticate := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			token, ok := bearerToken(request)
			if !ok {
				http.Error(writer, "missing bearer token", http.StatusUnauthorized)
				return
			}
			claims, _, _, err := client.GetClaims(request.Context(), token)
			if err != nil {
				// Every verification failure is a 401 to ensure wire responses
				// remain opaque, intentionally stripping away the client's
				// GetClaims returned error detail. This prevents attackers from
				// probing to explore which of their forgeries got the furthest.
				http.Error(writer, "invalid token", http.StatusUnauthorized)
				return
			}
			request = request.WithContext(context.WithValue(request.Context(), claimsContextKey{}, claims))
			next.ServeHTTP(writer, request)
		})
	}

	// deleteAccount is a handler that has two jobs to do:
	// 1. Authorization check ("is this user allowed to do this particular thing?"),
	//    being a per-endpoint policy rather than a universal property of the token.
	//    In this case the check is to ensure that a second factor was used.
	// 2. The account deletion operation, now that we know the authorization
	//    check has succeeded.
	// May also be referred to as the layer that asserts per-route policy.
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

// ExampleClient_Admin provisions a teammate account from a back-office job.
// The secret key comes from the environment, never from source: it bypasses
// Row Level Security, so it belongs to server configuration alone. Admin
// creation sends no confirmation email - EmailConfirm marks the address
// trusted, so the account is usable immediately.
func ExampleClient_Admin() {
	secretKey := os.Getenv("SUPABASE_SECRET_KEY")
	client, err := auth.New("https://PROJECT_ID.supabase.co", secretKey)
	if err != nil {
		fmt.Println(err)
		return
	}
	admin := client.Admin()

	user, err := admin.CreateUser(context.Background(), auth.UserAttributes{
		Email:        "ada@example.com",
		Password:     "correct horse battery staple",
		EmailConfirm: true,
		AppMetadata:  map[string]any{"team": "platform"},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("provisioned", user.Email())
}

// ExampleAdmin_CreateUser imports an account from a legacy authentication
// system. PasswordHash carries the stored bcrypt, scrypt or argon2 hash, so
// the user keeps their password across the migration - set it instead of
// Password, never alongside it.
func ExampleAdmin_CreateUser() {
	client, err := auth.New("https://PROJECT_ID.supabase.co", "sb_secret_...")
	if err != nil {
		fmt.Println(err)
		return
	}
	admin := client.Admin()

	user, err := admin.CreateUser(context.Background(), auth.UserAttributes{
		Email:        "imported@example.com",
		EmailConfirm: true,
		PasswordHash: "$2y$10$HASH_FROM_THE_LEGACY_STORE",
		UserMetadata: map[string]any{"migrated_from": "legacy-auth"},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("migrated", user.Email())
}

// ExampleAdmin_UpdateUser suspends a user by banning sign-ins for three
// days, then lifts the ban early with the literal "none". Only the set
// fields change: everything else on the user stays as it is.
func ExampleAdmin_UpdateUser() {
	client, err := auth.New("https://PROJECT_ID.supabase.co", "sb_secret_...")
	if err != nil {
		fmt.Println(err)
		return
	}
	admin := client.Admin()
	ctx := context.Background()

	banned, err := admin.UpdateUser(ctx, "USER_ID", auth.UserAttributes{BanDuration: "72h"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("banned until", banned.BannedUntil())

	restored, err := admin.UpdateUser(ctx, "USER_ID", auth.UserAttributes{BanDuration: "none"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("ban lifted:", restored.BannedUntil().IsZero())
}

// ExampleAdmin_ListUsers walks every page of the project's users, the shape
// of a nightly reconciliation against a billing system. The walk leans on
// the server's page-size default and asks for each page by number. NextPage
// reports false on the last page, which ends the walk.
func ExampleAdmin_ListUsers() {
	client, err := auth.New("https://PROJECT_ID.supabase.co", "sb_secret_...")
	if err != nil {
		fmt.Println(err)
		return
	}
	admin := client.Admin()
	ctx := context.Background()

	for number := 1; ; {
		page, err := admin.ListUsers(ctx, pagination.WithPage(number))
		if err != nil {
			fmt.Println(err)
			return
		}
		for _, user := range page.Users() {
			fmt.Println(user.ID(), user.Email())
		}
		next, ok := page.NextPage()
		if !ok {
			break
		}
		number = next
	}
	fmt.Println("reconciled")
}

// ExampleAdmin_DeleteUser separates closing an account from erasing it. The
// soft delete ends every session and factor and obfuscates the contact
// details, but keeps the row readable for audit. The bare call makes no
// choice, so the server's default applies: the hard delete, for the moment
// the record itself must go, such as a data-protection erasure request.
func ExampleAdmin_DeleteUser() {
	client, err := auth.New("https://PROJECT_ID.supabase.co", "sb_secret_...")
	if err != nil {
		fmt.Println(err)
		return
	}
	admin := client.Admin()
	ctx := context.Background()

	if err := admin.DeleteUser(ctx, "USER_ID", auth.WithSoftDelete()); err != nil {
		fmt.Println(err)
		return
	}

	// Later, an erasure request arrives for the same account.
	if err := admin.DeleteUser(ctx, "USER_ID"); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("erased")
}
