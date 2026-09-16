package integrationtest

import (
	"net/http"
	"testing"

	"github.com/supabase/supabase-go/integration-testing/testkit"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/supabase"
)

// TestRootClientSelect selects seeded rows through the root client against
// the local Supabase stack started by scripts/integration-test.sh, covering
// the composition of a new client, Database, From, Select and Collect.
func TestRootClientSelect(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)

	client, err := supabase.New(projectURL, apiKey)
	if err != nil {
		t.Fatalf("supabase.New: %v", err)
	}

	type instrument struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	rows, response, err := postgrest.Collect(
		t.Context(),
		client.Database(),
		postgrest.
			From[instrument]("instruments").
			Select("id, name"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
	}
	if response.Count != -1 {
		t.Errorf("Count = %d, want -1 (no count requested)", response.Count)
	}
	if len(rows) != 3 {
		t.Errorf("row count = %d, want 3 (seed drifted?)", len(rows))
	}
}

// TestRootClientGetUser fetches the signed-up user's profile through the
// composed Auth client, proving the auth handle New wires derives its base
// URL, key and transport from the root configuration.
func TestRootClientGetUser(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)

	client, err := supabase.New(projectURL, apiKey)
	if err != nil {
		t.Fatalf("supabase.New: %v", err)
	}

	profile, err := client.Auth().GetUser(t.Context(), user.AccessToken)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if profile.ID() != user.ID {
		t.Errorf("ID = %q, want %q", profile.ID(), user.ID)
	}
	if profile.Email() != user.Email {
		t.Errorf("Email = %q, want %q", profile.Email(), user.Email)
	}
}
