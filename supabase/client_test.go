package supabase_test

import (
	"errors"
	"testing"

	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/supabase"
)

// TestNewValidationPropagates pins that New surfaces the configuration
// package's sentinels unchanged, so callers match them with errors.Is as the
// doc comment promises.
func TestNewValidationPropagates(t *testing.T) {
	if _, err := supabase.New("", "k"); !errors.Is(err, configuration.ErrMissingURL) {
		t.Fatalf("want ErrMissingURL, got %v", err)
	}
	if _, err := supabase.New("https://PROJECT_ID.supabase.co", ""); !errors.Is(err, configuration.ErrMissingKey) {
		t.Fatalf("want ErrMissingKey, got %v", err)
	}
}

// TestAuthAccessorReturnsStableHandle pins that Auth hands back the one
// handle New constructed, identical across calls, keeping domain navigation
// context-free and error-free.
func TestAuthAccessorReturnsStableHandle(t *testing.T) {
	client, err := supabase.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	first := client.Auth()
	if first == nil {
		t.Fatal("Auth returned nil")
	}
	if second := client.Auth(); second != first {
		t.Error("Auth returned a different handle on the second call")
	}
}
