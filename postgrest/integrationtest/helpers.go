//go:build integration

package integrationtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
)

// integrationCredentials returns the local stack's project URL and publishable
// key from the environment scripts/integration-test.sh exports, failing the
// test when either is absent.
func integrationCredentials(t *testing.T) (projectURL, apiKey string) {
	t.Helper()
	projectURL = os.Getenv("SUPABASE_URL")
	apiKey = os.Getenv("SUPABASE_PUBLISHABLE_KEY")
	if projectURL == "" || apiKey == "" {
		t.Fatal("SUPABASE_URL and SUPABASE_PUBLISHABLE_KEY must be set for integration tests - run scripts/integration-test.sh")
	}
	return projectURL, apiKey
}

// newIntegrationClient wires a client at the local Supabase stack started by
// scripts/integration-test.sh, failing the test when the required environment
// variables are absent. It forwards the optional configuration options so
// tests can exercise construction-time settings against the real stack.
func newIntegrationClient(t *testing.T, options ...configuration.Option) *postgrest.Client {
	t.Helper()
	projectURL, apiKey := integrationCredentials(t)
	projectConfiguration, err := configuration.New(core.ModulePathPostgrest, projectURL, apiKey, options...)
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}
	return postgrest.NewFromConfiguration(projectConfiguration)
}

// testUser is one signed-up end user of the local stack's auth service,
// carrying the credentials the Row Level Security tests act with.
type testUser struct {
	ID          string
	AccessToken string
}

// signUpUser registers a fresh user with the local stack's auth service and
// returns its id and access token. The local stack auto-confirms new email
// accounts, so the response carries a usable session immediately. The helper is
// deliberately reusable: later auth-area tests build their fixtures on it.
func signUpUser(t *testing.T, projectURL, apiKey string) testUser {
	t.Helper()
	safeName := strings.ReplaceAll(t.Name(), "/", "-")
	email := fmt.Sprintf("user-%d-%s@example.com", time.Now().UnixNano(), safeName)
	payload, err := json.Marshal(map[string]string{"email": email, "password": "integration-password"})
	if err != nil {
		t.Fatalf("marshaling signup payload: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, projectURL+"/auth/v1/signup", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("building signup request: %v", err)
	}
	request.Header.Set("apikey", apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("signing up %s: %v", email, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("signup for %s: HTTP %d: %s", email, response.StatusCode, body)
	}
	var session struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatalf("decoding signup response: %v", err)
	}
	if session.AccessToken == "" || session.User.ID == "" {
		t.Fatalf("signup for %s returned no session - is the local stack's auth service enabled with auto-confirm?", email)
	}
	return testUser{ID: session.User.ID, AccessToken: session.AccessToken}
}
