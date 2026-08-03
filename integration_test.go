//go:build integration

package supabase_test

import (
	"net/http"
	"os"
	"testing"

	"github.com/supabase/supabase-go"
	"github.com/supabase/supabase-go/postgrest"
)

// TestIntegrationRootClientSelect selects seeded rows through the root
// client against the local Supabase stack started by
// scripts/integration-test.sh, covering the composition of a new client,
// From, Select and Collect.
func TestIntegrationRootClientSelect(t *testing.T) {
	projectURL := os.Getenv("SUPABASE_URL")
	apiKey := os.Getenv("SUPABASE_PUBLISHABLE_KEY")
	if projectURL == "" || apiKey == "" {
		t.Fatal("SUPABASE_URL and SUPABASE_PUBLISHABLE_KEY must be set for integration tests - run scripts/integration-test.sh")
	}

	client, err := supabase.New(projectURL, apiKey)
	if err != nil {
		t.Fatalf("supabase.New: %v", err)
	}

	type instrument struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	rows, response, err := postgrest.Collect[instrument](
		t.Context(),
		client.
			From("instruments").
			Select("id, name"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
	}
	if len(rows) != 3 {
		t.Errorf("row count = %d, want 3 (seed drifted?)", len(rows))
	}
}
