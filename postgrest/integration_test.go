//go:build integration

package postgrest_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
)

// newIntegrationClient wires a client at the local Supabase stack started by
// scripts/integration-test.sh, failing the test when the required environment
// variables are absent.
func newIntegrationClient(t *testing.T) *postgrest.Client {
	t.Helper()
	projectURL := os.Getenv("SUPABASE_URL")
	apiKey := os.Getenv("SUPABASE_PUBLISHABLE_KEY")
	if projectURL == "" || apiKey == "" {
		t.Fatal("SUPABASE_URL and SUPABASE_PUBLISHABLE_KEY must be set for integration tests - run scripts/integration-test.sh")
	}
	projectConfiguration, err := configuration.New(core.ModulePathPostgrest, projectURL, apiKey)
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	return postgrest.NewFromConfiguration(projectConfiguration)
}

type seededInstrument struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// TestIntegrationSelectAllColumns proves the read path against real
// PostgREST: seeded rows decode, the status is 200 and a request without a
// count preference reports an unknown total as -1, confirming live the
// Content-Range behavior the unit tests synthesize.
func TestIntegrationSelectAllColumns(t *testing.T) {
	client := newIntegrationClient(t)

	rows, response, err := postgrest.Collect[seededInstrument](
		context.Background(),
		client.
			From("instruments").
			Select(""),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
	}
	// No count was requested, so PostgREST reports an unknown total ("0-2/*").
	if response.Count != -1 {
		t.Errorf("Count = %d, want -1 (no count requested)", response.Count)
	}
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3 (seed drifted?)", len(rows))
	}
	names := map[string]bool{}
	for _, row := range rows {
		names[row.Name] = true
	}
	for _, want := range []string{"violin", "viola", "cello"} {
		if !names[want] {
			t.Errorf("seeded row %q missing from result set %v", want, names)
		}
	}
}

// TestIntegrationSelectColumnSubset proves the cleaned select list is
// accepted by the real server and that a narrower row type decodes the
// projection.
func TestIntegrationSelectColumnSubset(t *testing.T) {
	client := newIntegrationClient(t)

	type nameOnly struct {
		Name string `json:"name"`
	}

	rows, _, err := postgrest.Collect[nameOnly](
		context.Background(),
		client.
			From("instruments").
			Select("name"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3 (seed drifted?)", len(rows))
	}
	for index, row := range rows {
		if row.Name == "" {
			t.Errorf("rows[%d].Name is empty", index)
		}
	}
}

// TestIntegrationMissingRelationReturnsTypedError proves error parsing
// against a real PostgREST error response. The stack is version-pinned by
// the harness, so the exact protocol shape (PGRST205, HTTP 404) is asserted
// deliberately: a failure here on a pin bump is upstream drift worth
// reviewing.
func TestIntegrationMissingRelationReturnsTypedError(t *testing.T) {
	client := newIntegrationClient(t)

	_, _, err := postgrest.Collect[seededInstrument](
		context.Background(),
		client.
			From("does_not_exist").
			Select(""),
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "PGRST205" {
		t.Errorf("Code = %q, want PGRST205 (unknown relation)", typedError.Code)
	}
	if typedError.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %d, want 404", typedError.HTTPStatus)
	}
}
