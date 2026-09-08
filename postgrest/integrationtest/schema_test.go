//go:build integration

package integrationtest

import (
	"errors"
	"testing"

	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/postgrest/internal/testkit"
)

// TestWithSchemaReadsFromSelectedSchema proves a schema-bound client reads the
// selected schema's table rather than the identically-named public one.
func TestWithSchemaReadsFromSelectedSchema(t *testing.T) {
	client := newIntegrationClient(t).WithSchema("personal")

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.From[seededInstrument]("instruments"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	testkit.AssertOKResponse(t, response)

	names := map[string]bool{}
	for _, row := range rows {
		names[row.Name] = true
	}
	if len(rows) != 2 || !names["lute"] || !names["harpsichord"] {
		t.Fatalf("personal.instruments = %v, want exactly lute and harpsichord", names)
	}
}

// TestDefaultClientReadsDefaultSchema proves deriving a schema-bound copy does
// not disturb the base client, which still reads the public schema's rows.
func TestDefaultClientReadsDefaultSchema(t *testing.T) {
	base := newIntegrationClient(t)
	_ = base.WithSchema("personal") // derive and discard

	rows, response, err := postgrest.Collect(
		t.Context(),
		base,
		postgrest.From[seededInstrument]("instruments"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	testkit.AssertOKResponse(t, response)
	if len(rows) != 3 {
		t.Fatalf("public.instruments row count = %d, want 3 (the derived copy leaked its schema?)", len(rows))
	}
}

// TestWithSchemaCallsFunctionInSelectedSchema proves a function call resolves in
// the selected schema: personal.count_instruments answers its two rows, while
// the same call against the default schema fails because the function is
// personal-only.
func TestWithSchemaCallsFunctionInSelectedSchema(t *testing.T) {
	count, response, err := postgrest.CollectRaw(
		t.Context(),
		newIntegrationClient(t).WithSchema("personal"),
		postgrest.RPC[int64]("count_instruments").Value().ReadOnly(),
	)
	if err != nil {
		t.Fatalf("CollectRaw: %v", err)
	}
	testkit.AssertOKResponse(t, response)
	if count != 2 {
		t.Fatalf("personal.count_instruments() = %d, want 2", count)
	}

	if _, _, err := postgrest.CollectRaw(
		t.Context(),
		newIntegrationClient(t),
		postgrest.RPC[int64]("count_instruments").Value().ReadOnly(),
	); err == nil {
		t.Fatal("count_instruments resolved in the default schema, want failure (it is personal-only)")
	}
}

// TestWithSchemaNotExposedFails proves selecting a schema absent from the
// exposed list fails with PostgREST's schema-not-found code rather than
// silently falling back to the default.
func TestWithSchemaNotExposedFails(t *testing.T) {
	_, _, err := postgrest.Collect(
		t.Context(),
		newIntegrationClient(t).WithSchema("restricted"),
		postgrest.From[seededInstrument]("instruments"),
	)

	var typedError *postgrest.Error
	if !errors.As(err, &typedError) {
		t.Fatalf("want *postgrest.Error, got %T: %v", err, err)
	}
	if typedError.Code != "PGRST106" {
		t.Errorf("Code = %q, want PGRST106 (schema not exposed to the API)", typedError.Code)
	}
}
