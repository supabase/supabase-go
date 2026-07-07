package postgrest

import (
	"testing"

	"github.com/supabase/supabase-go/core"
)

func TestNewWiresConfiguration(t *testing.T) {
	configuration, err := core.NewConfiguration("https://project.supabase.co", "anon-key")
	if err != nil {
		t.Fatalf("NewConfiguration: %v", err)
	}

	client := New(configuration)
	if client.httpClient == nil {
		t.Error("httpClient was not wired")
	}
	if got, want := client.baseURL.String(), "https://project.supabase.co/rest/v1"; got != want {
		t.Errorf("baseURL = %q, want %q", got, want)
	}
}
