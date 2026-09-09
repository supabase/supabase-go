package integrationtest

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/integrationsupport"
	"github.com/supabase/supabase-go/postgrest"
)

// cSpell:ignore alice

// constantAccessToken returns a provider resolving to the given token, the
// documented attachment for a token already in hand.
func constantAccessToken(token string) configuration.AccessTokenProvider {
	return func(context.Context) (string, error) { return token, nil }
}

// practiceLog maps the row type of the public.practice_logs table. The id is a
// server-defaulted UUID, so omitzero leaves it out of an insert.
type practiceLog struct {
	ID      string `json:"id,omitzero"`
	UserID  string `json:"user_id"`
	Piece   string `json:"piece"`
	Minutes int    `json:"minutes"`
}

// TestAccessTokenRLSIsolation is the readiness signal: two signed-up users each
// insert their own rows through derived clients, and each reads back exactly
// their own - the database applying each user's Row Level Security policies from
// the attached token.
func TestAccessTokenRLSIsolation(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)
	alice := integrationsupport.SignUpUser(t, projectURL, apiKey)
	bob := integrationsupport.SignUpUser(t, projectURL, apiKey)

	aliceClient := newIntegrationClient(t).WithAccessTokenProvider(constantAccessToken(alice.AccessToken))
	bobClient := newIntegrationClient(t).WithAccessTokenProvider(constantAccessToken(bob.AccessToken))

	if _, err := postgrest.Execute(
		t.Context(), aliceClient,
		postgrest.From[practiceLog]("practice_logs").Insert(
			practiceLog{UserID: alice.ID, Piece: "Prelude in C", Minutes: 30},
			practiceLog{UserID: alice.ID, Piece: "Slow Waltz", Minutes: 20},
		),
	); err != nil {
		t.Fatalf("Alice insert: %v", err)
	}
	if _, err := postgrest.Execute(
		t.Context(), bobClient,
		postgrest.From[practiceLog]("practice_logs").Insert(
			practiceLog{UserID: bob.ID, Piece: "Evening Ballad", Minutes: 45},
		),
	); err != nil {
		t.Fatalf("Bob insert: %v", err)
	}

	aliceRows, _, err := postgrest.Collect(t.Context(), aliceClient, postgrest.From[practiceLog]("practice_logs"))
	if err != nil {
		t.Fatalf("Alice Collect: %v", err)
	}
	if len(aliceRows) != 2 {
		t.Fatalf("Alice saw %d rows, want exactly 2 (only her own)", len(aliceRows))
	}
	for _, row := range aliceRows {
		if row.UserID != alice.ID {
			t.Errorf("Alice saw a row owned by %q, want only her own %q", row.UserID, alice.ID)
		}
	}

	bobRows, _, err := postgrest.Collect(t.Context(), bobClient, postgrest.From[practiceLog]("practice_logs"))
	if err != nil {
		t.Fatalf("Bob Collect: %v", err)
	}
	if len(bobRows) != 1 {
		t.Fatalf("Bob saw %d rows, want exactly 1 (only his own)", len(bobRows))
	}
	if bobRows[0].UserID != bob.ID {
		t.Errorf("Bob saw a row owned by %q, want his own %q", bobRows[0].UserID, bob.ID)
	}
}

// TestAccessTokenAnonymousBaseSeesNothing proves the base publishable-key client
// carries no user identity and Row Level Security filters it silently: after a
// user inserts a row, the base client's read returns an empty slice at HTTP 200,
// not a permission error - and no token bled onto the base client.
func TestAccessTokenAnonymousBaseSeesNothing(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)
	user := integrationsupport.SignUpUser(t, projectURL, apiKey)
	userClient := newIntegrationClient(t).WithAccessTokenProvider(constantAccessToken(user.AccessToken))

	if _, err := postgrest.Execute(
		t.Context(), userClient,
		postgrest.From[practiceLog]("practice_logs").Insert(
			practiceLog{UserID: user.ID, Piece: "Solo Study", Minutes: 25},
		),
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	base := newIntegrationClient(t)
	rows, response, err := postgrest.Collect(t.Context(), base, postgrest.From[practiceLog]("practice_logs"))
	if err != nil {
		t.Fatalf("base Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200 (Row Level Security filters silently, it does not error)", response.HTTPStatus)
	}
	if len(rows) != 0 {
		t.Errorf("base client saw %d rows, want 0 (no anonymous policy grants any)", len(rows))
	}
}

// TestAccessTokenCrossTenantInsertRejected proves the with-check insert policy
// stops one user forging another's row: Alice inserting a row that claims Bob's
// id is refused with the RLS violation, HTTP 403 code 42501 - the grant-versus-
// policy failure asymmetry, pinned.
func TestAccessTokenCrossTenantInsertRejected(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)
	alice := integrationsupport.SignUpUser(t, projectURL, apiKey)
	bob := integrationsupport.SignUpUser(t, projectURL, apiKey)
	aliceClient := newIntegrationClient(t).WithAccessTokenProvider(constantAccessToken(alice.AccessToken))

	_, err := postgrest.Execute(
		t.Context(), aliceClient,
		postgrest.From[practiceLog]("practice_logs").Insert(
			practiceLog{UserID: bob.ID, Piece: "Impostor Sonata", Minutes: 10},
		),
	)
	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.HTTPStatus != http.StatusForbidden {
		t.Errorf("HTTPStatus = %d, want 403", typedError.HTTPStatus)
	}
	if typedError.Code != "42501" {
		t.Errorf("Code = %q, want 42501 (row-level security with-check violation)", typedError.Code)
	}
}

// TestAccessTokenCrossTenantUpdateInvisible proves the using clause filters a
// cross-tenant update invisibly rather than erroring: Alice's update of Bob's
// row matches nothing (nil error, zero rows), and Bob reads his row back
// unchanged. This silent-filter semantic is the classic consumer surprise, so
// it earns its own test.
func TestAccessTokenCrossTenantUpdateInvisible(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)
	alice := integrationsupport.SignUpUser(t, projectURL, apiKey)
	bob := integrationsupport.SignUpUser(t, projectURL, apiKey)
	aliceClient := newIntegrationClient(t).WithAccessTokenProvider(constantAccessToken(alice.AccessToken))
	bobClient := newIntegrationClient(t).WithAccessTokenProvider(constantAccessToken(bob.AccessToken))

	bobRow, _, err := postgrest.CollectSingle(
		t.Context(), bobClient,
		postgrest.From[practiceLog]("practice_logs").
			Insert(practiceLog{UserID: bob.ID, Piece: "Bob's Ballad", Minutes: 40}).
			Returning("id"),
	)
	if err != nil {
		t.Fatalf("Bob insert: %v", err)
	}

	updated, _, err := postgrest.Collect(
		t.Context(), aliceClient,
		postgrest.From[practiceLog]("practice_logs").
			Eq("id", bobRow.ID).
			Update(map[string]any{"piece": "Hijacked"}),
	)
	if err != nil {
		t.Fatalf("Alice update: %v", err)
	}
	if len(updated) != 0 {
		t.Errorf("Alice's cross-tenant update returned %d rows, want 0 (the row is invisible to her)", len(updated))
	}

	readBack, _, err := postgrest.CollectSingle(
		t.Context(), bobClient,
		postgrest.From[practiceLog]("practice_logs").Eq("id", bobRow.ID),
	)
	if err != nil {
		t.Fatalf("Bob read-back: %v", err)
	}
	if readBack.Piece != "Bob's Ballad" {
		t.Errorf("Bob's piece = %q, want it unchanged as %q", readBack.Piece, "Bob's Ballad")
	}
}

// TestAccessTokenPerReadProviderOverridesClientProvider proves the precedence
// contract on the wire: a client derived for Alice, given Bob's token as a
// per-read option for one call, returns Bob's rows, and the next call without
// the option is Alice again - so the option scopes to its call and leaks no
// state.
func TestAccessTokenPerReadProviderOverridesClientProvider(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)
	alice := integrationsupport.SignUpUser(t, projectURL, apiKey)
	bob := integrationsupport.SignUpUser(t, projectURL, apiKey)

	aliceClient := newIntegrationClient(t).WithAccessTokenProvider(constantAccessToken(alice.AccessToken))
	bobClient := newIntegrationClient(t).WithAccessTokenProvider(constantAccessToken(bob.AccessToken))

	if _, err := postgrest.Execute(t.Context(), aliceClient,
		postgrest.From[practiceLog]("practice_logs").Insert(practiceLog{UserID: alice.ID, Piece: "Alice's Waltz", Minutes: 15})); err != nil {
		t.Fatalf("Alice insert: %v", err)
	}
	if _, err := postgrest.Execute(t.Context(), bobClient,
		postgrest.From[practiceLog]("practice_logs").Insert(practiceLog{UserID: bob.ID, Piece: "Bob's March", Minutes: 35})); err != nil {
		t.Fatalf("Bob insert: %v", err)
	}

	asBob, _, err := postgrest.Collect(
		t.Context(), aliceClient,
		postgrest.From[practiceLog]("practice_logs"),
		postgrest.WithAccessTokenProvider(constantAccessToken(bob.AccessToken)),
	)
	if err != nil {
		t.Fatalf("Collect with option: %v", err)
	}
	if len(asBob) != 1 || asBob[0].UserID != bob.ID {
		t.Fatalf("option-scoped call saw %+v, want only Bob's rows", asBob)
	}

	asAlice, _, err := postgrest.Collect(t.Context(), aliceClient, postgrest.From[practiceLog]("practice_logs"))
	if err != nil {
		t.Fatalf("Collect without option: %v", err)
	}
	if len(asAlice) != 1 || asAlice[0].UserID != alice.ID {
		t.Fatalf("client-default call saw %+v, want only Alice's rows", asAlice)
	}
}

// TestAccessTokenRPCCarriesClaims proves the attached token's claims reach a
// Postgres function on both RPC transports: current_user_id() decodes to the
// caller's id through CollectRaw over a POST and over a read-only GET alike.
func TestAccessTokenRPCCarriesClaims(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)
	alice := integrationsupport.SignUpUser(t, projectURL, apiKey)
	aliceClient := newIntegrationClient(t).WithAccessTokenProvider(constantAccessToken(alice.AccessToken))

	t.Run("via POST", func(t *testing.T) {
		id, _, err := postgrest.CollectRaw(
			t.Context(), aliceClient,
			postgrest.RPC[string]("current_user_id").Value(),
		)
		if err != nil {
			t.Fatalf("CollectRaw: %v", err)
		}
		if id != alice.ID {
			t.Errorf("current_user_id = %q, want Alice's id %q", id, alice.ID)
		}
	})

	t.Run("via read-only GET", func(t *testing.T) {
		id, _, err := postgrest.CollectRaw(
			t.Context(), aliceClient,
			postgrest.RPC[string]("current_user_id").Value().ReadOnly(),
		)
		if err != nil {
			t.Fatalf("CollectRaw: %v", err)
		}
		if id != alice.ID {
			t.Errorf("current_user_id (GET) = %q, want Alice's id %q", id, alice.ID)
		}
	})
}

// TestAccessTokenOpaqueTokenRejectedByServer is the opacity contract's
// server-side backstop: a provider resolving to a double-prefixed "Bearer <jwt>"
// (the classic mistake) is sent verbatim and rejected every time, so the call
// spends its whole re-send budget - the provider is asked four times (initial
// plus three renewal re-asks) - before surfacing HTTP 401. It doubles as the
// wire-level budget-exhaustion proof.
func TestAccessTokenOpaqueTokenRejectedByServer(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)
	alice := integrationsupport.SignUpUser(t, projectURL, apiKey)

	var providerCalls atomic.Int64
	doublePrefixed := "Bearer " + alice.AccessToken
	client := newIntegrationClient(t).WithAccessTokenProvider(func(context.Context) (string, error) {
		providerCalls.Add(1)
		return doublePrefixed, nil
	})

	_, _, err := postgrest.Collect(t.Context(), client, postgrest.From[practiceLog]("practice_logs"))
	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("HTTPStatus = %d, want 401 (the server rejects the malformed bearer value)", typedError.HTTPStatus)
	}
	if got := providerCalls.Load(); got != 4 {
		t.Errorf("provider calls = %d, want 4 (initial send plus three renewal re-asks)", got)
	}
}

// TestAccessTokenRenewalRecoversStaleToken proves renewal against the real
// stack: a provider that vends a garbage token first and the user's real token
// thereafter recovers within one call - the server's 401 triggers one re-ask and
// the re-send succeeds, returning the user's rows with the provider asked
// exactly twice.
func TestAccessTokenRenewalRecoversStaleToken(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)
	user := integrationsupport.SignUpUser(t, projectURL, apiKey)

	seedClient := newIntegrationClient(t).WithAccessTokenProvider(constantAccessToken(user.AccessToken))
	if _, err := postgrest.Execute(t.Context(), seedClient,
		postgrest.From[practiceLog]("practice_logs").Insert(practiceLog{UserID: user.ID, Piece: "Recovered Prelude", Minutes: 22})); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	var providerCalls atomic.Int64
	client := newIntegrationClient(t).WithAccessTokenProvider(func(context.Context) (string, error) {
		if providerCalls.Add(1) == 1 {
			return "not-a-valid-jwt", nil
		}
		return user.AccessToken, nil
	})

	rows, _, err := postgrest.Collect(t.Context(), client, postgrest.From[practiceLog]("practice_logs"))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := providerCalls.Load(); got != 2 {
		t.Errorf("provider calls = %d, want 2 (the server's 401 triggered one re-ask)", got)
	}
	if len(rows) != 1 {
		t.Fatalf("recovered client saw %d rows, want exactly 1 (the seeded row)", len(rows))
	}
	if rows[0].UserID != user.ID {
		t.Errorf("saw a row owned by %q, want the user's own %q", rows[0].UserID, user.ID)
	}
}
