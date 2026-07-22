package supabase

import "testing"

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
}
