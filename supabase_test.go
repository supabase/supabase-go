package supabase_test

import (
	"errors"
	"testing"

	supabase "github.com/supabase/supabase-go"
	"github.com/supabase/supabase-go/core"
)

func TestNewClientValidationPropagates(t *testing.T) {
	if _, err := supabase.NewClient("", "k"); !errors.Is(err, core.ErrMissingURL) {
		t.Fatalf("want ErrMissingURL, got %v", err)
	}
	if _, err := supabase.NewClient("https://project.supabase.co", ""); !errors.Is(err, core.ErrMissingKey) {
		t.Fatalf("want ErrMissingKey, got %v", err)
	}
}
