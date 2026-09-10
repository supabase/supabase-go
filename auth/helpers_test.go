package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/internal/testkit"
)

// mockAuthServer stands in for the Auth server, serving the JWK Set discovery
// endpoint and the user endpoint. Its counters and configurable responses let a
// test assert which path a call took.
type mockAuthServer struct {
	server *httptest.Server

	keys          []map[string]any
	jwkSetFetches atomic.Int32
	jwkSetStatus  int
	userHits      atomic.Int32
	userStatus    int
	userBody      string
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
	mux.HandleFunc("/auth/v1/.well-known/jwks.json", func(writer http.ResponseWriter, _ *http.Request) {
		mock.jwkSetFetches.Add(1)
		if mock.jwkSetStatus != http.StatusOK {
			writer.WriteHeader(mock.jwkSetStatus)
			_, _ = writer.Write([]byte(`{"msg":"discovery unavailable"}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"keys": mock.keys})
	})
	mux.HandleFunc("/auth/v1/user", func(writer http.ResponseWriter, _ *http.Request) {
		mock.userHits.Add(1)
		writer.WriteHeader(mock.userStatus)
		_, _ = writer.Write([]byte(mock.userBody))
	})
	mock.server = httptest.NewServer(mux)
	t.Cleanup(mock.server.Close)
	return mock
}

// client builds an auth client wired to the mock server.
func (m *mockAuthServer) client(t *testing.T) *auth.Client {
	t.Helper()
	client, err := auth.New(m.server.URL, "test-anon-key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}
