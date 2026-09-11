package integrationtest

import (
	"context"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/integration-testing/testkit"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/supabase"
)

// practiceLog maps a row of the public.practice_logs table, whose Row Level
// Security policies admit a user to only their own rows. The id is a
// server-defaulted UUID, so omitzero leaves it out of an insert.
type practiceLog struct {
	ID      string `json:"id,omitzero"`
	UserID  string `json:"user_id"`
	Piece   string `json:"piece"`
	Minutes int    `json:"minutes"`
}

// TestVerifiedTokenScopesRLSQuery is the block's readiness signal in composed
// form: a backend verifies an inbound end-user token with the Auth client, then
// issues a database call as that user, and Row Level Security confines the
// result to the user's own rows.
func TestVerifiedTokenScopesRLSQuery(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)

	// 1. Verify the token the way backend middleware would.
	authClient, err := auth.New(projectURL, apiKey)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	claims, _, _, err := authClient.GetClaims(t.Context(), user.AccessToken)
	if err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	if claims.Subject() != user.ID {
		t.Fatalf("verified subject = %q, want %q", claims.Subject(), user.ID)
	}

	// 2. Act as the verified user against the database with the same token.
	client, err := supabase.New(projectURL, apiKey)
	if err != nil {
		t.Fatalf("supabase.New: %v", err)
	}
	userDatabase := client.Database().WithAccessTokenProvider(
		func(context.Context) (string, error) { return user.AccessToken, nil },
	)

	if _, err := postgrest.Execute(
		t.Context(), userDatabase,
		postgrest.From[practiceLog]("practice_logs").Insert(
			practiceLog{UserID: user.ID, Piece: "Verified Etude", Minutes: 25},
		),
	); err != nil {
		t.Fatalf("insert as verified user: %v", err)
	}

	rows, _, err := postgrest.Collect(t.Context(), userDatabase, postgrest.From[practiceLog]("practice_logs"))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("verified user saw %d rows, want exactly 1 (only their own)", len(rows))
	}
	if rows[0].UserID != user.ID {
		t.Errorf("row owned by %q, want the verified user %q", rows[0].UserID, user.ID)
	}
}
