// Package integrationsupport provides the fixtures the SDK modules'
// integration tests share: the local Supabase stack's credentials and
// end-user signup. It exists so each integrationtest module states these
// once-per-repository concerns exactly once.
package integrationsupport

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
)

// Credentials returns the local stack's project URL and publishable key from
// the environment scripts/integration-test.sh exports, failing the test when
// either is absent.
func Credentials(t *testing.T) (projectURL, apiKey string) {
	t.Helper()
	projectURL = os.Getenv("SUPABASE_URL")
	apiKey = os.Getenv("SUPABASE_PUBLISHABLE_KEY")
	if projectURL == "" || apiKey == "" {
		t.Fatal("SUPABASE_URL and SUPABASE_PUBLISHABLE_KEY must be set for integration tests - run scripts/integration-test.sh")
	}
	return projectURL, apiKey
}

// User is one signed-up end user of the local stack's auth service, carrying
// the credentials tests act with and the fields they assert on.
type User struct {
	ID          string
	Email       string
	AccessToken string
}

// SignUpUser registers a fresh user with the local stack's auth service and
// returns its id, email and access token. The local stack auto-confirms new
// email accounts, so the response carries a usable session immediately.
func SignUpUser(t *testing.T, projectURL, apiKey string) User {
	t.Helper()
	safeName := strings.ReplaceAll(t.Name(), "/", "-")
	// The Auth server normalizes email addresses to lower case, so generate a
	// lower-case address (the test name supplying safeName carries capitals) to
	// keep the recorded fixture equal to what the server stores and returns.
	email := strings.ToLower(fmt.Sprintf("user-%d-%s@example.com", time.Now().UnixNano(), safeName))
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
	return User{ID: session.User.ID, Email: email, AccessToken: session.AccessToken}
}
