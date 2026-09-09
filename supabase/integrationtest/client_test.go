package integrationtest

import (
	"net/http"
	"testing"

	"github.com/supabase/supabase-go/integrationsupport"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/supabase"
)

// TestRootClientSelect selects seeded rows through the root client against
// the local Supabase stack started by scripts/integration-test.sh, covering
// the composition of a new client, Database, From, Select and Collect.
func TestRootClientSelect(t *testing.T) {
	projectURL, apiKey := integrationsupport.Credentials(t)

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
