package supabase_test

import (
	"errors"
	"testing"

	"github.com/supabase/supabase-go"
	"github.com/supabase/supabase-go/core/configuration"
)

func TestNewValidationPropagates(t *testing.T) {
	if _, err := supabase.New("", "k"); !errors.Is(err, configuration.ErrMissingURL) {
		t.Fatalf("want ErrMissingURL, got %v", err)
	}
	if _, err := supabase.New("https://PROJECT_ID.supabase.co", ""); !errors.Is(err, configuration.ErrMissingKey) {
		t.Fatalf("want ErrMissingKey, got %v", err)
	}
}
