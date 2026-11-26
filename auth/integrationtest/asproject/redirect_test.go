package asproject

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
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

// TestRedirectToAnotherServer proves that an admin call holding the secret key
// keeps to the project's host. When that host answers the call with a
// permanent redirect to a server that is not the project's, the call does not
// follow it. A host retired in favor of a mistyped or lapsed domain answers
// this way. The outcomes asserted here:
//   - the call fails with the redirect's status, so it returns no user that
//     server made up
//   - that server receives no request, so neither the secret key nor the new
//     user's password reaches it
func TestRedirectToAnotherServer(t *testing.T) {
	_, secretKey := testkit.SecretCredentials(t)
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
	// The call fails with the redirect's status, returning no user.
	assertServerError(t, result, err, http.StatusPermanentRedirect, "")

	// The other server received no request, so it holds neither the secret key
	// nor the password.
	received := intermediary.received()
	if len(received) != 0 {
		t.Errorf("intermediary received %d requests, want none", len(received))
	}
}
