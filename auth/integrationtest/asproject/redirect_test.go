package asproject

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/integration-testing/testkit"
)

// forgedUserID is the id of the user intermediaryServer makes up.
const forgedUserID = "0d6f7e3a-5b1c-4f2e-9a8d-7c6b5a4f3e2d"

// receivedRequest is what intermediaryServer recorded of a request: its
// apikey header and its body.
type receivedRequest struct {
	apiKey string
	body   string
}

// intermediaryServer is a server that is not the project's. It records every
// request it receives and answers each as the Auth admin API answers a
// request to create a user, with a user it makes up: forgedUserID, carrying
// the email address the request asked for.
type intermediaryServer struct {
	server   *httptest.Server
	mutex    sync.Mutex
	requests []receivedRequest
}

// newIntermediaryServer starts an intermediaryServer that closes when the test
// ends.
func newIntermediaryServer(t *testing.T) *intermediaryServer {
	t.Helper()
	intermediary := &intermediaryServer{}
	intermediary.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("reading the request body: %v", err)
		}
		intermediary.record(receivedRequest{apiKey: request.Header.Get("apikey"), body: string(body)})

		var attributes struct {
			Email string `json:"email"`
		}
		if err := json.Unmarshal(body, &attributes); err != nil {
			t.Errorf("decoding the request body: %v", err)
		}
		forged := map[string]string{"id": forgedUserID, "aud": "authenticated", "role": "authenticated", "email": attributes.Email}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(forged); err != nil {
			t.Errorf("writing the response body: %v", err)
		}
	}))
	t.Cleanup(intermediary.server.Close)
	return intermediary
}

// record records request.
func (s *intermediaryServer) record(request receivedRequest) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.requests = append(s.requests, request)
}

// received returns every request the server has recorded, in order.
func (s *intermediaryServer) received() []receivedRequest {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return slices.Clone(s.requests)
}

// TestRedirectToAnotherServer pins a security defect. When the project's host
// answers an admin call holding the secret key with a permanent redirect to a
// server that is not the project's, the call follows it. A host retired in
// favor of a mistyped or lapsed domain answers this way. Each outcome asserted
// here is one the SDK must not produce:
//   - that server receives the secret key and the new user's password
//   - the call returns the user that server made up as one the project created
//   - the key that server received opens the project's admin API
func TestRedirectToAnotherServer(t *testing.T) {
	projectURL, secretKey := testkit.SecretCredentials(t)
	intermediary := newIntermediaryServer(t)
	target := intermediary.server.URL + "/auth/v1/admin/users"
	projectHost := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target, http.StatusPermanentRedirect)
	}))
	t.Cleanup(projectHost.Close)
	client, err := auth.New(projectHost.URL, secretKey)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}

	email := testkit.UniqueEmail(t)
	result, err := client.Admin().CreateUser(t.Context(), auth.UserAttributes{Email: email, Password: testkit.Password, EmailConfirm: true})
	// Wrongly, the call succeeds with a user the project never created.
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created := result.User(); created.ID() != forgedUserID || created.Email() != email {
		t.Errorf("created user = (%q, %q), want the one the intermediary made up, (%q, %q)", created.ID(), created.Email(), forgedUserID, email)
	}

	// Wrongly, the other server received the secret key and the password.
	received := intermediary.received()
	if len(received) != 1 {
		t.Fatalf("intermediary received %d requests, want 1", len(received))
	}
	if received[0].apiKey != secretKey {
		t.Error("intermediary received an apikey other than the secret key")
	}
	if !strings.Contains(received[0].body, `"password":"`+testkit.Password+`"`) {
		t.Errorf("intermediary received body %s, want it to carry the new user's password", received[0].body)
	}

	// The project itself holds no user with the id CreateUser returned, so the
	// call reported a user that exists nowhere but in the other server's answer.
	fetched, err := newAdmin(t).GetUser(t.Context(), forgedUserID)
	assertServerError(t, fetched, err, http.StatusNotFound, "user_not_found")

	// The key the other server received opens the project's admin API, so
	// whoever holds that server holds the project.
	stolen, err := auth.New(projectURL, received[0].apiKey)
	if err != nil {
		t.Fatalf("auth.New with the key the intermediary received: %v", err)
	}
	if _, err := stolen.Admin().ListUsers(t.Context()); err != nil {
		t.Errorf("ListUsers with the key the intermediary received: %v, want the project's users", err)
	}
}
