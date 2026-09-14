package supabase

import "testing"

// TestNewWiresHandles pins that New constructs every domain handle up front,
// the foundation of the context-free, error-free domain navigation contract.
func TestNewWiresHandles(t *testing.T) {
	client, err := New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if client.configuration == nil {
		t.Error("configuration was not wired")
	}
	if client.database == nil {
		t.Error("database handle was not wired")
	}
	if client.auth == nil {
		t.Error("auth handle was not wired")
	}
}
