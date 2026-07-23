package postgrest

import (
	"testing"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
)

// TestNewFromConfigurationWiresConfiguration pins the constructor's wiring:
// the shared HTTP client is carried over and the PostgREST base URL is the
// project URL joined with /rest/v1.
func TestNewFromConfigurationWiresConfiguration(t *testing.T) {
	projectConfiguration, err := configuration.New(core.ModulePathPostgrest, "https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}

	client := NewFromConfiguration(projectConfiguration)
	if client.httpClient == nil {
		t.Error("httpClient was not wired")
	}
	if got, want := client.baseURL.String(), "https://PROJECT_ID.supabase.co/rest/v1"; got != want {
		t.Errorf("baseURL = %q, want %q", got, want)
	}
}
