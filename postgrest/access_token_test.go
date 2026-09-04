package postgrest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
)

// testAPIKey is the project key every test client is constructed with. The
// access-token tests assert it keeps traveling on the apikey header, never
// displaced by a resolved user token.
const testAPIKey = "TEST_API_KEY"

// headerRecorder collects a clone of each request's headers. httptest serves
// each request in its own goroutine, so record and read are mutex-guarded;
// reading after the served calls have returned is safe by that same lock.
type headerRecorder struct {
	mutex   sync.Mutex
	headers []http.Header
}

func (r *headerRecorder) record(header http.Header) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.headers = append(r.headers, header)
}

func (r *headerRecorder) all() []http.Header {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return slices.Clone(r.headers)
}

// only returns the single recorded request's headers, failing when the count
// is not exactly one.
func (r *headerRecorder) only(t *testing.T) http.Header {
	t.Helper()
	all := r.all()
	if len(all) != 1 {
		t.Fatalf("recorded %d requests, want exactly 1", len(all))
	}
	return all[0]
}

// newRecordingServer starts a test server that records each request's headers
// and answers every request with an empty JSON array.
func newRecordingServer(t *testing.T) (*httptest.Server, *headerRecorder) {
	t.Helper()
	recorder := &headerRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorder.record(request.Header.Clone())
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("[]"))
	}))
	t.Cleanup(server.Close)
	return server, recorder
}

// authorizationEcho decodes the one-row body the echo server returns, carrying
// back the Authorization header the request arrived with.
type authorizationEcho struct {
	Authorization string `json:"authorization"`
}

// newEchoAuthorizationServer starts a test server answering each request with a
// one-row body echoing that request's Authorization header, so a concurrent
// caller verifies its own token round-tripped without shared assertion state.
func newEchoAuthorizationServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload, err := json.Marshal([]authorizationEcho{{Authorization: request.Header.Get("Authorization")}})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(payload)
	}))
	t.Cleanup(server.Close)
	return server
}

// newClient builds a postgrest client at serverURL with the project key.
func newClient(t *testing.T, serverURL string, options ...configuration.Option) *postgrest.Client {
	t.Helper()
	client, err := postgrest.New(serverURL, testAPIKey, options...)
	if err != nil {
		t.Fatalf("postgrest.New: %v", err)
	}
	return client
}

// constantToken is the documented attachment for a token already in hand: a
// provider resolving to the given token.
func constantToken(token string) configuration.AccessTokenProvider {
	return func(context.Context) (string, error) { return token, nil }
}

// TestClientWithAccessTokenProviderSendsBearer proves a derived client sends
// the resolved token as a Bearer credential while the project key stays on the
// apikey header and the SDK's X-Client-Info is untouched.
func TestClientWithAccessTokenProviderSendsBearer(t *testing.T) {
	server, recorder := newRecordingServer(t)
	client := newClient(t, server.URL).WithAccessTokenProvider(constantToken("token-a"))

	if _, _, err := postgrest.Collect(t.Context(), client, postgrest.From[map[string]any]("instruments")); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	header := recorder.only(t)
	if got := header.Get("Authorization"); got != "Bearer token-a" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer token-a")
	}
	if got := header.Get("apikey"); got != testAPIKey {
		t.Errorf("apikey = %q, want the project key %q (a token never displaces it)", got, testAPIKey)
	}
	if header.Get("X-Client-Info") == "" {
		t.Error("X-Client-Info was not set by the SDK")
	}
}

// TestClientWithAccessTokenProviderLeavesBaseUnchanged proves copy-on-derive:
// deriving never touches the receiver, and chained derivation sends the latest
// token.
func TestClientWithAccessTokenProviderLeavesBaseUnchanged(t *testing.T) {
	t.Run("base client carries no Authorization after deriving", func(t *testing.T) {
		server, recorder := newRecordingServer(t)
		base := newClient(t, server.URL)

		_ = base.WithAccessTokenProvider(constantToken("token-a")) // derive and discard

		if _, _, err := postgrest.Collect(t.Context(), base, postgrest.From[map[string]any]("instruments")); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if got := recorder.only(t).Get("Authorization"); got != "" {
			t.Errorf("base client sent Authorization %q, want none", got)
		}
	})

	t.Run("chained derivation sends the latest token", func(t *testing.T) {
		server, recorder := newRecordingServer(t)
		client := newClient(t, server.URL).
			WithAccessTokenProvider(constantToken("token-a")).
			WithAccessTokenProvider(constantToken("token-b"))

		if _, _, err := postgrest.Collect(t.Context(), client, postgrest.From[map[string]any]("instruments")); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if got := recorder.only(t).Get("Authorization"); got != "Bearer token-b" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer token-b")
		}
	})
}

// TestWithAccessTokenProviderOptionScopesOneCall proves the per-read option
// sets the header for its call alone: the same client's next call without it
// carries none.
func TestWithAccessTokenProviderOptionScopesOneCall(t *testing.T) {
	server, recorder := newRecordingServer(t)
	client := newClient(t, server.URL)

	if _, _, err := postgrest.Collect(
		t.Context(), client, postgrest.From[map[string]any]("instruments"),
		postgrest.WithAccessTokenProvider(constantToken("scoped-token")),
	); err != nil {
		t.Fatalf("Collect with option: %v", err)
	}
	if _, _, err := postgrest.Collect(
		t.Context(), client, postgrest.From[map[string]any]("instruments"),
	); err != nil {
		t.Fatalf("Collect without option: %v", err)
	}

	all := recorder.all()
	if len(all) != 2 {
		t.Fatalf("requests = %d, want 2", len(all))
	}
	if got := all[0].Get("Authorization"); got != "Bearer scoped-token" {
		t.Errorf("first Authorization = %q, want %q", got, "Bearer scoped-token")
	}
	if got := all[1].Get("Authorization"); got != "" {
		t.Errorf("second Authorization = %q, want none (the option was scoped to the first call)", got)
	}
}

// TestWithAccessTokenProviderOptionReplacesClientProvider proves the option's
// provider replaces the client's for that call - the client's is never invoked -
// and that a later option replaces an earlier one.
func TestWithAccessTokenProviderOptionReplacesClientProvider(t *testing.T) {
	t.Run("option replaces the client provider without invoking it", func(t *testing.T) {
		server, recorder := newRecordingServer(t)

		var clientProviderCalls atomic.Int64
		clientProvider := func(context.Context) (string, error) {
			clientProviderCalls.Add(1)
			return "client-token", nil
		}
		client := newClient(t, server.URL).WithAccessTokenProvider(clientProvider)

		if _, _, err := postgrest.Collect(
			t.Context(), client, postgrest.From[map[string]any]("instruments"),
			postgrest.WithAccessTokenProvider(constantToken("option-token")),
		); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if got := recorder.only(t).Get("Authorization"); got != "Bearer option-token" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer option-token")
		}
		if calls := clientProviderCalls.Load(); calls != 0 {
			t.Errorf("client provider invoked %d times, want 0 (the option replaces it)", calls)
		}
	})

	t.Run("later option replaces the earlier", func(t *testing.T) {
		server, recorder := newRecordingServer(t)
		client := newClient(t, server.URL)

		if _, _, err := postgrest.Collect(
			t.Context(), client, postgrest.From[map[string]any]("instruments"),
			postgrest.WithAccessTokenProvider(constantToken("first")),
			postgrest.WithAccessTokenProvider(constantToken("second")),
		); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if got := recorder.only(t).Get("Authorization"); got != "Bearer second" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer second")
		}
	})
}

// TestWithAccessTokenProviderFailsClosed proves a nil provider or an empty
// resolution fails the call with ErrMissingAccessToken before any I/O, through
// either layer, and that an explicit per-read attachment never falls back to
// the client's provider.
func TestWithAccessTokenProviderFailsClosed(t *testing.T) {
	testCases := []struct {
		name string
		call func(t *testing.T, client *postgrest.Client) error
	}{
		{
			name: "nil provider at the client",
			call: func(t *testing.T, client *postgrest.Client) error {
				t.Helper()
				_, _, err := postgrest.Collect(
					t.Context(), client.WithAccessTokenProvider(nil),
					postgrest.From[map[string]any]("instruments"),
				)
				return err
			},
		},
		{
			name: "nil provider as an option over a valid client provider",
			call: func(t *testing.T, client *postgrest.Client) error {
				t.Helper()
				_, _, err := postgrest.Collect(
					t.Context(), client.WithAccessTokenProvider(constantToken("client-token")),
					postgrest.From[map[string]any]("instruments"),
					postgrest.WithAccessTokenProvider(nil),
				)
				return err
			},
		},
		{
			name: "empty resolution via Collect",
			call: func(t *testing.T, client *postgrest.Client) error {
				t.Helper()
				_, _, err := postgrest.Collect(
					t.Context(), client.WithAccessTokenProvider(constantToken("")),
					postgrest.From[map[string]any]("instruments"),
				)
				return err
			},
		},
		{
			name: "empty resolution via Execute",
			call: func(t *testing.T, client *postgrest.Client) error {
				t.Helper()
				_, err := postgrest.Execute(
					t.Context(), client.WithAccessTokenProvider(constantToken("")),
					postgrest.From[map[string]any]("instruments").Insert(map[string]any{"name": "viola"}),
				)
				return err
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server, recorder := newRecordingServer(t)
			client := newClient(t, server.URL)

			err := testCase.call(t, client)
			if !errors.Is(err, postgrest.ErrMissingAccessToken) {
				t.Fatalf("errors.Is(err, ErrMissingAccessToken) = false, want true: %v", err)
			}
			if got := len(recorder.all()); got != 0 {
				t.Errorf("requests = %d, want 0 (fail closed before any I/O)", got)
			}
		})
	}
}

// TestWithAccessTokenProviderErrorPropagates proves a provider's own error
// fails the call, wrapped and matchable, with no request sent.
func TestWithAccessTokenProviderErrorPropagates(t *testing.T) {
	server, recorder := newRecordingServer(t)
	sentinel := errors.New("token vend failed")
	client := newClient(t, server.URL).WithAccessTokenProvider(
		func(context.Context) (string, error) { return "", sentinel },
	)

	_, _, err := postgrest.Collect(t.Context(), client, postgrest.From[map[string]any]("instruments"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(err, sentinel) = false, want true: %v", err)
	}
	if !strings.Contains(err.Error(), "postgrest: resolving access token") {
		t.Errorf("error = %q, want it to mention resolving the access token", err.Error())
	}
	if got := len(recorder.all()); got != 0 {
		t.Errorf("requests = %d, want 0", got)
	}
}

// TestAccessTokenProviderReceivesCallContext proves the provider observes the
// exact context passed to the read, so deadlines and cancellation flow into
// token resolution.
func TestAccessTokenProviderReceivesCallContext(t *testing.T) {
	server, _ := newRecordingServer(t)

	type contextKey string
	const marker contextKey = "marker"

	var observed any
	client := newClient(t, server.URL).WithAccessTokenProvider(
		func(ctx context.Context) (string, error) {
			observed = ctx.Value(marker)
			return "token", nil
		},
	)

	ctx := context.WithValue(t.Context(), marker, "present")
	if _, _, err := postgrest.Collect(ctx, client, postgrest.From[map[string]any]("instruments")); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if observed != "present" {
		t.Errorf("provider saw context value %v, want %q", observed, "present")
	}
}

// TestWithAccessTokenProviderAppliesToExecute proves the header rides the
// mutation path too: an Execute of an Insert through a derived client carries
// the resolved token.
func TestWithAccessTokenProviderAppliesToExecute(t *testing.T) {
	server, recorder := newRecordingServer(t)
	client := newClient(t, server.URL).WithAccessTokenProvider(constantToken("write-token"))

	if _, err := postgrest.Execute(
		t.Context(), client,
		postgrest.From[map[string]any]("instruments").Insert(map[string]any{"name": "viola"}),
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := recorder.only(t).Get("Authorization"); got != "Bearer write-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer write-token")
	}
}

// TestWithAccessTokenProviderGlobalHeaderPrecedence proves a resolved token
// wins over a construction-time global Authorization default, while a base
// client still sends that default.
func TestWithAccessTokenProviderGlobalHeaderPrecedence(t *testing.T) {
	t.Run("base client sends the configured global default", func(t *testing.T) {
		server, recorder := newRecordingServer(t)
		client := newClient(t, server.URL, configuration.WithHeader("Authorization", "Bearer global-default"))

		if _, _, err := postgrest.Collect(t.Context(), client, postgrest.From[map[string]any]("instruments")); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if got := recorder.only(t).Get("Authorization"); got != "Bearer global-default" {
			t.Errorf("Authorization = %q, want the global default %q", got, "Bearer global-default")
		}
	})

	t.Run("resolved token wins over the global default", func(t *testing.T) {
		server, recorder := newRecordingServer(t)
		client := newClient(t, server.URL, configuration.WithHeader("Authorization", "Bearer global-default")).
			WithAccessTokenProvider(constantToken("user-token"))

		if _, _, err := postgrest.Collect(t.Context(), client, postgrest.From[map[string]any]("instruments")); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if got := recorder.only(t).Get("Authorization"); got != "Bearer user-token" {
			t.Errorf("Authorization = %q, want the resolved token %q", got, "Bearer user-token")
		}
	})
}

// TestWithAccessTokenProviderConcurrentDerivedClients is the race-aware
// immutability proof: from one base client, many goroutines derive unique
// per-user clients and assert their own token round-trips, while others use the
// base client concurrently and assert no Authorization is ever attached. Run
// under -race, any shared-state mutation fails the test.
func TestWithAccessTokenProviderConcurrentDerivedClients(t *testing.T) {
	server := newEchoAuthorizationServer(t)
	base := newClient(t, server.URL)
	ctx := t.Context()

	const workers = 8
	var waitGroup sync.WaitGroup

	for index := range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			token := fmt.Sprintf("token-%d", index)
			userClient := base.WithAccessTokenProvider(constantToken(token))
			rows, _, err := postgrest.Collect(ctx, userClient, postgrest.From[authorizationEcho]("instruments"))
			if err != nil {
				t.Errorf("worker %d Collect: %v", index, err)
				return
			}
			if len(rows) != 1 {
				t.Errorf("worker %d rows = %d, want 1", index, len(rows))
				return
			}
			if got, want := rows[0].Authorization, "Bearer "+token; got != want {
				t.Errorf("worker %d Authorization = %q, want %q", index, got, want)
			}
		}()
	}

	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			rows, _, err := postgrest.Collect(ctx, base, postgrest.From[authorizationEcho]("instruments"))
			if err != nil {
				t.Errorf("base client Collect: %v", err)
				return
			}
			if len(rows) != 1 {
				t.Errorf("base client rows = %d, want 1", len(rows))
				return
			}
			if rows[0].Authorization != "" {
				t.Errorf("base client sent Authorization %q, want none", rows[0].Authorization)
			}
		}()
	}

	waitGroup.Wait()
}
