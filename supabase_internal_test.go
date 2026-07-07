package supabase

import "testing"

func TestNewClientWiresHandles(t *testing.T) {
	client, err := NewClient("https://project.supabase.co", "anon-key")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.configuration == nil {
		t.Error("configuration was not wired")
	}
	if client.database == nil {
		t.Error("database handle was not wired")
	}
}
