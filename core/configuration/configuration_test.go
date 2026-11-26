package configuration_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
)

func TestNewValidation(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		key       string
		wantError error
	}{
		{name: "missing url", url: "", key: "k", wantError: configuration.ErrMissingURL},
		{name: "missing key", url: "https://PROJECT_ID.supabase.co", key: "", wantError: configuration.ErrMissingKey},
		{name: "relative url", url: "/no/scheme", key: "k", wantError: configuration.ErrInvalidURL},
		{name: "non-http scheme", url: "ftp://PROJECT_ID.supabase.co", key: "k", wantError: configuration.ErrInvalidURL},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := configuration.New(core.ModulePathRoot, testCase.url, testCase.key)
			if !errors.Is(err, testCase.wantError) {
				t.Fatalf("configuration.New error = %v, want errors.Is %v", err, testCase.wantError)
			}
		})
	}
}

// TestNewSwappedURLAndKey pins the error New returns when a caller passes
// the project URL and the secret key the wrong way round. New rejects the
// key it received as the project URL with an error whose text never quotes
// the key, so a program that logs the error keeps the key out of its log.
// That holds in both of the ways New can reject it:
//   - the key fails the check for an absolute http or https URL
//   - the key fails to parse, as it does when it ends in the newline that a
//     key read from a file can keep
func TestNewSwappedURLAndKey(t *testing.T) {
	const key = "sb_secret_..."
	tests := []struct {
		name   string
		rawURL string
	}{
		{name: "key", rawURL: key},
		{name: "key with a trailing newline", rawURL: key + "\n"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := configuration.New(core.ModulePathRoot, testCase.rawURL, "https://PROJECT_ID.supabase.co")
			if !errors.Is(err, configuration.ErrInvalidURL) {
				t.Fatalf("configuration.New error = %v, want errors.Is %v", err, configuration.ErrInvalidURL)
			}
			if strings.Contains(err.Error(), key) {
				t.Errorf("configuration.New error = %q, want it free of the key", err)
			}
		})
	}
}

func TestNewValid(t *testing.T) {
	projectConfiguration, err := configuration.New(core.ModulePathRoot, "https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		t.Fatalf("configuration.New returned error: %v", err)
	}
	if projectConfiguration.HTTPClient() == nil {
		t.Fatal("HTTPClient() returned nil")
	}
	if got, want := projectConfiguration.BaseURL().String(), "https://PROJECT_ID.supabase.co"; got != want {
		t.Fatalf("BaseURL() = %q, want %q", got, want)
	}
}

func TestConfiguration_BaseURLReturnsCopy(t *testing.T) {
	projectConfiguration, err := configuration.New(core.ModulePathRoot, "https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}

	// Mutating the returned URL must not reach back into the Configuration.
	returned := projectConfiguration.BaseURL()
	returned.Path = "/mutated"

	if got := projectConfiguration.BaseURL().Path; got != "" {
		t.Errorf("BaseURL mutation leaked into the Configuration: Path = %q, want empty", got)
	}
}
