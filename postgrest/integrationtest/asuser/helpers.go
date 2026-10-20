// Package asuser exercises the postgrest module against the live local
// Supabase stack that scripts/integration-test.sh starts, as a signed-in user:
// every client here holds the publishable key, and the secret key is absent
// from the package's environment.
package asuser

import (
	"testing"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/integration-testing/testkit"
	"github.com/supabase/supabase-go/postgrest"
)

// newIntegrationClient wires a client at the local Supabase stack started by
// scripts/integration-test.sh, failing the test when the required environment
// variables are absent. It forwards the optional configuration options so
// tests can exercise construction-time settings against the real stack.
func newIntegrationClient(t *testing.T, options ...configuration.Option) *postgrest.Client {
	t.Helper()
	projectURL, apiKey := testkit.Credentials(t)
	projectConfiguration, err := configuration.New(core.ModulePathPostgrest, projectURL, apiKey, options...)
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	return postgrest.NewFromConfiguration(projectConfiguration)
}
