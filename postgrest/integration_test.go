//go:build integration

package postgrest_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/supabase/supabase-go/configuration"
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
	projectConfiguration, err := configuration.New(projectURL, apiKey)
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	return postgrest.New(projectConfiguration)
}

type seededInstrument struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestIntegrationSelectAllColumns(t *testing.T) {
	client := newIntegrationClient(t)

	var rows []seededInstrument
	response, err := client.From("instruments").Select("").Execute(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Execute: %v", err)
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

func TestIntegrationSelectColumnSubset(t *testing.T) {
	client := newIntegrationClient(t)

	var rows []struct {
		Name string `json:"name"`
	}
	if _, err := client.From("instruments").Select("name").Execute(context.Background(), &rows); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rows) != 3 || rows[0].Name == "" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestIntegrationMissingRelationReturnsTypedError(t *testing.T) {
	client := newIntegrationClient(t)

	var rows []seededInstrument
	_, err := client.From("does_not_exist").Select("").Execute(context.Background(), &rows)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code == "" || typedError.HTTPStatus == 0 {
		t.Errorf("typedError = %+v; want populated Code and HTTPStatus", typedError)
	}
}
