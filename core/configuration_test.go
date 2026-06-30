package core_test

import (
	"errors"
	"testing"

	"github.com/supabase/supabase-go/core"
)

func TestNewConfigurationValidation(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		key       string
		wantError error
	}{
		{name: "missing url", url: "", key: "k", wantError: core.ErrMissingURL},
		{name: "missing key", url: "https://project.supabase.co", key: "", wantError: core.ErrMissingKey},
		{name: "relative url", url: "/no/scheme", key: "k", wantError: core.ErrInvalidURL},
		{name: "non-http scheme", url: "ftp://project.supabase.co", key: "k", wantError: core.ErrInvalidURL},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := core.NewConfiguration(testCase.url, testCase.key)
			if !errors.Is(err, testCase.wantError) {
				t.Fatalf("NewConfiguration error = %v, want errors.Is %v", err, testCase.wantError)
			}
		})
	}
}

func TestNewConfigurationValid(t *testing.T) {
	configuration, err := core.NewConfiguration("https://project.supabase.co", "anon-key")
	if err != nil {
		t.Fatalf("NewConfiguration returned error: %v", err)
	}
	if configuration.Client() == nil {
		t.Fatal("Client() returned nil")
	}
	if got, want := configuration.BaseURL().String(), "https://project.supabase.co"; got != want {
		t.Fatalf("BaseURL() = %q, want %q", got, want)
	}
}
