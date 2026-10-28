package auth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/internal/testkit"
	"github.com/supabase/supabase-go/core/configuration"
)

// mockAuthServer stands in for the Auth server, serving the JWK Set discovery
// endpoint and the user endpoint. Its counters and configurable responses let a
// test assert which path a call took, and each endpoint records the API
// version header its latest request carried.
type mockAuthServer struct {
	server *httptest.Server

	keys             []map[string]any
	jwkSetFetches    atomic.Int32
	jwkSetStatus     int
	jwkSetAPIVersion string
	userHits         atomic.Int32
	userStatus       int
	userBody         string
	userAPIVersion   string
}

// newMockAuthServer starts a server publishing the signers' public keys at the
// discovery endpoint and answering the user endpoint with 200 and an empty
// user object by default.
func newMockAuthServer(t *testing.T, signers ...testkit.Signer) *mockAuthServer {
	t.Helper()
	mock := &mockAuthServer{jwkSetStatus: http.StatusOK, userStatus: http.StatusOK, userBody: "{}"}
	for _, signer := range signers {
		mock.keys = append(mock.keys, signer.PublicJWK())
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/v1/.well-known/jwks.json", func(writer http.ResponseWriter, request *http.Request) {
		mock.jwkSetFetches.Add(1)
		mock.jwkSetAPIVersion = request.Header.Get("X-Supabase-Api-Version")
		if mock.jwkSetStatus != http.StatusOK {
			writer.WriteHeader(mock.jwkSetStatus)
			_, _ = writer.Write([]byte(`{"code":"unexpected_failure","message":"discovery unavailable"}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"keys": mock.keys})
	})
	mux.HandleFunc("/auth/v1/user", func(writer http.ResponseWriter, request *http.Request) {
		mock.userHits.Add(1)
		mock.userAPIVersion = request.Header.Get("X-Supabase-Api-Version")
		writer.WriteHeader(mock.userStatus)
		_, _ = writer.Write([]byte(mock.userBody))
	})
	mock.server = httptest.NewServer(mux)
	t.Cleanup(mock.server.Close)
	return mock
}

// client builds an auth client wired to the mock server, configured by
// options.
func (m *mockAuthServer) client(t *testing.T, options ...configuration.Option) *auth.Client {
	t.Helper()
	client, err := auth.New(m.server.URL, "test-anon-key", options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

// The paths of the session endpoints a sessionServer serves.
const (
	tokenPath  = "/auth/v1/token"
	userPath   = "/auth/v1/user"
	logoutPath = "/auth/v1/logout"
)

// sessionUserID is the id of the one user a sessionServer signs in, the sub
// of every token it mints and the id in sessionUserDocument.
const sessionUserID = "33333333-3333-3333-3333-333333333333"

// sessionUserDocument is the user resource a sessionServer serves, carrying
// nested metadata so a test can prove a profile's independence per call.
const sessionUserDocument = `{"id":"` + sessionUserID + `","aud":"authenticated","role":"authenticated","email":"ada@example.com","user_metadata":{"display_name":"Ada","tags":{"team":"platform"}},"app_metadata":{"provider":"email"}}`

// answer is one scripted response from a sessionServer. A 200 answer to the
// token endpoint with no body mints a token response, lifetime overriding
// the server's default access token lifetime when set, and a 200 answer to
// the user endpoint with no body serves sessionUserDocument. When hold is
// set, the response waits until it is closed, the request having been
// recorded already.
type answer struct {
	status   int
	body     string
	lifetime time.Duration
	hold     <-chan struct{}
}

// sessionServer stands in for the Auth server's session endpoints - the
// token grants, the user resource and logout - minting signed tokens and
// recording every request. Each endpoint answers from a script of answers
// consumed in order, the last repeating.
type sessionServer struct {
	server   *httptest.Server
	t        *testing.T
	signer   testkit.Signer
	lifetime time.Duration

	mutex    sync.Mutex
	requests []recordedRequest
	minted   []string
	scripts  map[string][]answer
}

// newSessionServer starts a server whose tokens live an hour, answering the
// token and user endpoints with 200 and logout with 204 until a test replaces
// their scripts.
func newSessionServer(t *testing.T) *sessionServer {
	t.Helper()
	server := &sessionServer{
		t:        t,
		signer:   testkit.NewES256(t, "session-key"),
		lifetime: time.Hour,
		scripts: map[string][]answer{
			tokenPath:  {{status: http.StatusOK}},
			userPath:   {{status: http.StatusOK}},
			logoutPath: {{status: http.StatusNoContent}},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/v1/.well-known/jwks.json", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(testkit.JWKSetDocument(t, server.signer))
	})
	mux.HandleFunc("/", server.handle)
	server.server = httptest.NewServer(mux)
	t.Cleanup(server.server.Close)
	return server
}

// handle records the request, takes the next scripted answer for its path,
// waits on the answer's hold and writes it.
func (s *sessionServer) handle(writer http.ResponseWriter, request *http.Request) {
	requestBody, err := io.ReadAll(request.Body)
	if err != nil {
		s.t.Errorf("reading request body: %v", err)
	}
	s.mutex.Lock()
	s.requests = append(s.requests, recordedRequest{
		method:        request.Method,
		path:          request.URL.Path,
		query:         request.URL.RawQuery,
		contentType:   request.Header.Get("Content-Type"),
		apiKey:        request.Header.Get("apikey"),
		apiVersion:    request.Header.Get("X-Supabase-Api-Version"),
		authorization: request.Header.Get("Authorization"),
		body:          requestBody,
	})
	script := s.scripts[request.URL.Path]
	var next answer
	if len(script) > 0 {
		next = script[0]
		if len(script) > 1 {
			s.scripts[request.URL.Path] = script[1:]
		}
	}
	s.mutex.Unlock()
	if next.status == 0 {
		writer.WriteHeader(http.StatusNotFound)
		return
	}
	if next.hold != nil {
		<-next.hold
	}

	body := []byte(next.body)
	switch {
	case request.URL.Path == tokenPath && next.status == http.StatusOK && next.body == "":
		lifetime := next.lifetime
		if lifetime == 0 {
			lifetime = s.lifetime
		}
		body = s.mint(lifetime)
	case request.URL.Path == userPath && next.status == http.StatusOK && next.body == "":
		body = []byte(sessionUserDocument)
	}
	writer.WriteHeader(next.status)
	_, _ = writer.Write(body)
}

// mint builds a token response whose access token, signed by the server's
// key, expires lifetime from now, with the refresh token mintedRefreshToken
// names for its place among the responses minted.
func (s *sessionServer) mint(lifetime time.Duration) []byte {
	expiresAt := time.Now().Add(lifetime)
	claims := testkit.DefaultClaims()
	claims["sub"] = sessionUserID
	claims["exp"] = expiresAt.Unix()
	accessToken := s.signer.Token(s.t, claims)

	s.mutex.Lock()
	s.minted = append(s.minted, accessToken)
	refreshToken := mintedRefreshToken(len(s.minted) - 1)
	s.mutex.Unlock()

	response, err := json.Marshal(map[string]any{
		"access_token":  accessToken,
		"token_type":    "bearer",
		"expires_in":    int(lifetime / time.Second),
		"expires_at":    expiresAt.Unix(),
		"refresh_token": refreshToken,
		"user":          json.RawMessage(sessionUserDocument),
	})
	if err != nil {
		s.t.Errorf("marshaling token response: %v", err)
	}
	return response
}

// script replaces the answers for path, consumed in order with the last
// repeating.
func (s *sessionServer) script(path string, answers ...answer) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.scripts[path] = answers
}

// mintedTokens returns every access token the server has minted, in order.
func (s *sessionServer) mintedTokens() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return slices.Clone(s.minted)
}

// mintedRefreshToken returns the refresh token of the token response a
// sessionServer minted at index: refresh-1 for the first.
func mintedRefreshToken(index int) string {
	return fmt.Sprintf("refresh-%d", index+1)
}

// assertHoldsMinted asserts session is live and holds the token response the
// server minted at index: that access token, the refresh token minted with it
// and the served user. Reading an access token inside the expiry margin
// refreshes it first, so the token held must have longer left.
func assertHoldsMinted(t *testing.T, server *sessionServer, session *auth.Session, index int) {
	t.Helper()
	minted := server.mintedTokens()
	if index >= len(minted) {
		t.Fatalf("%d tokens minted, want at least %d", len(minted), index+1)
	}
	if accessToken, err := session.AccessToken(t.Context()); err != nil || accessToken != minted[index] {
		t.Errorf("AccessToken() = (minted token %d, %v), want minted token %d", slices.Index(minted, accessToken), err, index)
	}
	if want := mintedRefreshToken(index); session.RefreshToken() != want {
		t.Errorf("RefreshToken() = %q, want %q", session.RefreshToken(), want)
	}
	if user := session.User(); user == nil || user.ID() != sessionUserID {
		t.Errorf("User() = %+v, want the served user", user)
	}
}

// requestPaths returns the path of every recorded request, in order.
func (s *sessionServer) requestPaths() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	paths := make([]string, 0, len(s.requests))
	for _, request := range s.requests {
		paths = append(paths, request.path)
	}
	return paths
}

// requestsTo returns the requests recorded for path, in order.
func (s *sessionServer) requestsTo(path string) []recordedRequest {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var matching []recordedRequest
	for _, request := range s.requests {
		if request.path == path {
			matching = append(matching, request)
		}
	}
	return matching
}

// awaitRequests waits until count requests to path have been recorded,
// failing the test after five seconds.
func (s *sessionServer) awaitRequests(t *testing.T, path string, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(s.requestsTo(path)) < count {
		if time.Now().After(deadline) {
			t.Fatalf("%d requests to %s recorded, want %d", len(s.requestsTo(path)), path, count)
		}
		time.Sleep(time.Millisecond)
	}
}

// client builds an auth client wired to the server.
func (s *sessionServer) client(t *testing.T, options ...configuration.Option) *auth.Client {
	t.Helper()
	client, err := auth.New(s.server.URL, "test-publishable-key", options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

// signIn signs the server's user in through client with a token that lives
// lifetime, later refreshes minting tokens that live the server's default.
func (s *sessionServer) signIn(t *testing.T, client *auth.Client, lifetime time.Duration) *auth.Session {
	t.Helper()
	s.script(tokenPath, answer{status: http.StatusOK, lifetime: lifetime}, answer{status: http.StatusOK})
	result, err := client.SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: "ada@example.com", Password: "correct horse battery staple"})
	if err != nil {
		t.Fatalf("SignInWithPassword: %v", err)
	}
	return result.Session()
}

// recordingStore is an auth.SessionStore in memory that records every write
// and can fail or hold a Save on demand.
type recordingStore struct {
	mutex     sync.Mutex
	state     []byte
	saves     [][]byte
	deletes   int
	saveErr   error
	deleteErr error
	hold      chan struct{}
	entered   chan struct{}
}

// holdNextSave makes the next Save block until the returned release function
// is called, and returns a channel closed once that Save has started.
func (s *recordingStore) holdNextSave() (entered <-chan struct{}, release func()) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.hold = make(chan struct{})
	s.entered = make(chan struct{})
	hold := s.hold
	return s.entered, func() { close(hold) }
}

func (s *recordingStore) Load(_ context.Context) ([]byte, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return slices.Clone(s.state), nil
}

func (s *recordingStore) Save(_ context.Context, state []byte) error {
	s.mutex.Lock()
	hold, entered := s.hold, s.entered
	s.hold, s.entered = nil, nil
	s.mutex.Unlock()
	if hold != nil {
		close(entered)
		<-hold
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.state = slices.Clone(state)
	s.saves = append(s.saves, slices.Clone(state))
	return nil
}

func (s *recordingStore) Delete(_ context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.state = nil
	s.deletes++
	return nil
}

// savedRefreshTokens returns the refresh token of each state the store saved,
// in order.
func (s *recordingStore) savedRefreshTokens(t *testing.T) []string {
	t.Helper()
	s.mutex.Lock()
	saves := slices.Clone(s.saves)
	s.mutex.Unlock()
	refreshTokens := make([]string, 0, len(saves))
	for _, save := range saves {
		refreshTokens = append(refreshTokens, decodeDocument(t, save).RefreshToken)
	}
	return refreshTokens
}

// deleteCount returns how many Deletes succeeded.
func (s *recordingStore) deleteCount() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.deletes
}

// stored returns the state the store holds.
func (s *recordingStore) stored() []byte {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return slices.Clone(s.state)
}

// storedDocument is the part of a stored session's JSON the tests read.
type storedDocument struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	User         struct {
		ID string `json:"id"`
	} `json:"user"`
}

// decodeDocument decodes a stored session document, failing the test when it
// is not JSON.
func decodeDocument(t *testing.T, document []byte) storedDocument {
	t.Helper()
	var decoded storedDocument
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("stored state is not JSON: %v", err)
	}
	return decoded
}

// decodeStored decodes the session state store holds, failing the test when
// it is not JSON.
func decodeStored(t *testing.T, store *recordingStore) storedDocument {
	t.Helper()
	return decodeDocument(t, store.stored())
}
