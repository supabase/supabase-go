package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/internal/testkit"
	"github.com/supabase/supabase-go/auth/signout"
	"github.com/supabase/supabase-go/core/configuration"
)

// expiresSoon is a token lifetime inside the expiry margin, and
// expiredAlready one already past.
const (
	expiresSoon    = 10 * time.Second
	expiredAlready = -time.Minute
)

// scriptedError is an error answer a test scripts the server to send and then
// expects back as an *auth.Error.
type scriptedError struct {
	status  int
	code    string
	message string
}

// The error answers the session tests script.
var (
	refreshRejected = scriptedError{http.StatusBadRequest, "refresh_token_not_found", "Invalid Refresh Token: Refresh Token Not Found"}
	unavailable     = scriptedError{http.StatusServiceUnavailable, "unexpected_failure", "try later"}
	badJWT          = scriptedError{http.StatusForbidden, "bad_jwt", "invalid JWT"}
	sessionNotFound = scriptedError{http.StatusForbidden, "session_not_found", "Session from session_id claim in JWT does not exist"}
	logoutFailed    = scriptedError{http.StatusInternalServerError, "unexpected_failure", "Error logging out user"}
)

// answer returns the scripted answer that sends e as the server's versioned
// error body.
func (e scriptedError) answer() answer {
	return answer{status: e.status, body: fmt.Sprintf(`{"code":%q,"message":%q}`, e.code, e.message)}
}

// assertServerError asserts err carries want as an *auth.Error, with its
// status, code and message.
func assertServerError(t *testing.T, err error, want scriptedError) {
	t.Helper()
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Errorf("error = %v, want *auth.Error %+v", err, want)
		return
	}
	got := scriptedError{serverError.HTTPStatus, serverError.Code, serverError.Message}
	if got != want {
		t.Errorf("*auth.Error = %+v, want %+v", got, want)
	}
}

// assertSignedOut asserts session has ended as signed out, holding neither
// token nor user.
func assertSignedOut(t *testing.T, session *auth.Session) {
	t.Helper()
	if accessToken, err := session.AccessToken(t.Context()); accessToken != "" || !errors.Is(err, auth.ErrSignedOut) {
		t.Errorf("AccessToken() = (%q, %v), want ErrSignedOut", accessToken, err)
	}
	if session.RefreshToken() != "" || session.User() != nil {
		t.Errorf("the ended session holds (%q, %+v), want neither token nor user", session.RefreshToken(), session.User())
	}
}

// assertRefreshGrant asserts request is a refresh_token grant presenting
// refreshToken and no bearer.
func assertRefreshGrant(t *testing.T, request recordedRequest, refreshToken string) {
	t.Helper()
	wantBody := `{"refresh_token":"` + refreshToken + `"}`
	if request.method != http.MethodPost || request.query != "grant_type=refresh_token" || string(request.body) != wantBody || request.authorization != "" {
		t.Errorf("token request = %s ?%s %s with Authorization %q, want POST ?grant_type=refresh_token %s with none", request.method, request.query, request.body, request.authorization, wantBody)
	}
}

func TestSessionFromTokensValidatesOrRefreshes(t *testing.T) {
	server := newSessionServer(t)
	client := server.client(t)
	claims := testkit.DefaultClaims()
	claims["sub"] = sessionUserID

	t.Run("an unexpired token is verified by the user endpoint", func(t *testing.T) {
		expiresAt := time.Now().Add(time.Hour).Truncate(time.Second)
		claims["exp"] = expiresAt.Unix()
		accessToken := server.signer.Token(t, claims)
		result, err := client.SessionFromTokens(t.Context(), accessToken, "refresh-elsewhere")
		if err != nil {
			t.Fatalf("SessionFromTokens: %v", err)
		}
		if hits := server.requestsTo(userPath); len(hits) != 1 || hits[0].method != http.MethodGet || hits[0].authorization != "Bearer "+accessToken {
			t.Errorf("user requests = %d, want one GET carrying the adopted token", len(hits))
		}
		session := result.Session()
		if adopted, err := session.AccessToken(t.Context()); err != nil || adopted != accessToken {
			t.Errorf("AccessToken() = (%q, %v), want the adopted token", adopted, err)
		}
		if session.RefreshToken() != "refresh-elsewhere" {
			t.Errorf("RefreshToken() = %q, want the adopted one", session.RefreshToken())
		}
		if user := session.User(); user == nil || user.ID() != sessionUserID || user.Email() != "ada@example.com" {
			t.Errorf("User() = %+v, want the served user", user)
		}
		if hits := server.requestsTo(tokenPath); len(hits) != 0 {
			t.Errorf("token requests = %d, want none", len(hits))
		}
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		stored := decodeStored(t, store)
		if stored.AccessToken != accessToken || stored.RefreshToken != "refresh-elsewhere" || stored.ExpiresAt != expiresAt.Unix() || stored.User.ID != sessionUserID {
			t.Errorf("stored state = %+v, want the adopted tokens, the exp claim %d and the served user", stored, expiresAt.Unix())
		}
	})

	t.Run("an expired token is refreshed", func(t *testing.T) {
		claims["exp"] = time.Now().Add(expiredAlready).Unix()
		userRequests := len(server.requestsTo(userPath))
		result, err := client.SessionFromTokens(t.Context(), server.signer.Token(t, claims), "refresh-elsewhere")
		if err != nil {
			t.Fatalf("SessionFromTokens: %v", err)
		}
		hits := server.requestsTo(tokenPath)
		if len(hits) != 1 {
			t.Fatalf("token requests = %d, want one refresh of the adopted token", len(hits))
		}
		assertRefreshGrant(t, hits[0], "refresh-elsewhere")
		if after := len(server.requestsTo(userPath)); after != userRequests {
			t.Errorf("%d user requests sent, want none", after-userRequests)
		}
		assertHoldsMinted(t, server, result.Session(), 0)
	})

	t.Run("empty and malformed inputs fail before any request", func(t *testing.T) {
		before := len(server.requestPaths())
		if result, err := client.SessionFromTokens(t.Context(), "", "refresh"); result != nil || !errors.Is(err, auth.ErrMissingJWT) {
			t.Errorf("empty access token = (%v, %v), want ErrMissingJWT", result, err)
		}
		if result, err := client.SessionFromTokens(t.Context(), "a.b.c", ""); result != nil || !errors.Is(err, auth.ErrMissingRefreshToken) {
			t.Errorf("empty refresh token = (%v, %v), want ErrMissingRefreshToken", result, err)
		}
		if result, err := client.SessionFromTokens(t.Context(), "not-a-jwt", "refresh"); result != nil || !errors.Is(err, auth.ErrMalformedJWT) {
			t.Errorf("malformed access token = (%v, %v), want ErrMalformedJWT", result, err)
		}
		if after := len(server.requestPaths()); after != before {
			t.Errorf("%d requests sent, want none", after-before)
		}
	})
}

func TestAccessTokenRefreshesInsideMargin(t *testing.T) {
	cases := []struct {
		name      string
		lifetime  time.Duration
		wantIndex int
	}{
		{name: "twenty seconds left refreshes", lifetime: 20 * time.Second, wantIndex: 1},
		{name: "forty seconds left does not", lifetime: 40 * time.Second, wantIndex: 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newSessionServer(t)
			session := server.signIn(t, server.client(t), testCase.lifetime)
			accessToken, err := session.AccessToken(t.Context())
			if err != nil {
				t.Fatalf("AccessToken: %v", err)
			}
			if minted := server.mintedTokens(); len(minted) != testCase.wantIndex+1 || accessToken != minted[testCase.wantIndex] {
				t.Errorf("AccessToken() answered minted token %d of %d, want token %d of %d", slices.Index(minted, accessToken), len(minted), testCase.wantIndex, testCase.wantIndex+1)
			}
			assertHoldsMinted(t, server, session, testCase.wantIndex)
		})
	}
}

func TestRefreshIsSingleFlight(t *testing.T) {
	server := newSessionServer(t)
	session := server.signIn(t, server.client(t), expiresSoon)
	hold := make(chan struct{})
	server.script(tokenPath, answer{status: http.StatusOK, hold: hold}, answer{status: http.StatusOK})

	const readers = 50
	tokens := make([]string, readers)
	readErrors := make([]error, readers)
	var group sync.WaitGroup
	for i := range readers {
		group.Go(func() {
			tokens[i], readErrors[i] = session.AccessToken(t.Context())
		})
	}
	server.awaitRequests(t, tokenPath, 2)
	assertRefreshGrant(t, server.requestsTo(tokenPath)[1], "refresh-1")
	if session.RefreshToken() != "refresh-1" {
		t.Errorf("RefreshToken() while the refresh is in flight = %q, want refresh-1 until it commits", session.RefreshToken())
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if accessToken, err := session.AccessToken(cancelled); accessToken != "" || !errors.Is(err, context.Canceled) {
		t.Errorf("AccessToken(cancelled) = (%q, %v), want context.Canceled", accessToken, err)
	}

	close(hold)
	group.Wait()
	minted := server.mintedTokens()
	if len(minted) != 2 {
		t.Fatalf("minted %d tokens, want the sign-in and one refresh", len(minted))
	}
	for i := range readers {
		if readErrors[i] != nil || tokens[i] != minted[1] {
			t.Errorf("reader %d got (%q, %v), want the one refreshed token", i, tokens[i], readErrors[i])
		}
	}
	if hits := server.requestsTo(tokenPath); len(hits) != 2 {
		t.Errorf("token requests = %d, want the sign-in and one refresh", len(hits))
	}
	assertHoldsMinted(t, server, session, 1)
}

// waitObservingContext closes waiting the first time its Done channel is
// asked for, which a reader does only once it waits on a refresh another
// caller started.
type waitObservingContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func newWaitObservingContext(parent context.Context) *waitObservingContext {
	return &waitObservingContext{Context: parent, waiting: make(chan struct{})}
}

func (c *waitObservingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestCancelledRefreshFailsOnlyItsStarter(t *testing.T) {
	server := newSessionServer(t)
	session := server.signIn(t, server.client(t), expiredAlready)
	hold := make(chan struct{})
	defer close(hold)
	server.script(tokenPath, answer{status: http.StatusOK, hold: hold}, answer{status: http.StatusOK})

	starterContext, cancelStarter := context.WithCancel(t.Context())
	defer cancelStarter()
	var starterToken string
	var starterErr error
	var group sync.WaitGroup
	group.Go(func() { starterToken, starterErr = session.AccessToken(starterContext) })
	server.awaitRequests(t, tokenPath, 2)

	joinerContext := newWaitObservingContext(t.Context())
	var joinerToken string
	var joinerErr error
	group.Go(func() { joinerToken, joinerErr = session.AccessToken(joinerContext) })
	select {
	case <-joinerContext.waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("the second reader never waited on the refresh in flight")
	}
	cancelStarter()
	group.Wait()

	if starterToken != "" || !errors.Is(starterErr, context.Canceled) {
		t.Errorf("the starter's AccessToken = (%q, %v), want context.Canceled", starterToken, starterErr)
	}
	if minted := server.mintedTokens(); joinerErr != nil || len(minted) != 2 || joinerToken != minted[1] {
		t.Errorf("the joiner's AccessToken = (%q, %v) with %d tokens minted, want the token its own refresh obtained", joinerToken, joinerErr, len(minted))
	}
	requests := server.requestsTo(tokenPath)
	if len(requests) != 3 {
		t.Fatalf("token requests = %d, want the sign-in, the abandoned refresh and the joiner's own", len(requests))
	}
	assertRefreshGrant(t, requests[1], "refresh-1")
	assertRefreshGrant(t, requests[2], "refresh-1")
	assertHoldsMinted(t, server, session, 1)
}

// panickingTransport panics on every round trip while armed, and otherwise
// sends the request through the default transport.
type panickingTransport struct {
	armed atomic.Bool
}

func (p *panickingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if p.armed.Load() {
		panic("transport failure")
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestPanickingRefreshLeavesSessionUsable(t *testing.T) {
	server := newSessionServer(t)
	transport := &panickingTransport{}
	client := server.client(t, configuration.WithHTTPClient(&http.Client{Transport: transport}))
	session := server.signIn(t, client, expiredAlready)

	transport.armed.Store(true)
	func() {
		defer func() {
			if recovered := recover(); recovered != "transport failure" {
				t.Errorf("recovered %v, want the transport's panic passed on", recovered)
			}
		}()
		_, _ = session.AccessToken(t.Context())
	}()
	transport.armed.Store(false)
	if hits := server.requestsTo(tokenPath); len(hits) != 1 || session.RefreshToken() != "refresh-1" {
		t.Errorf("after the panic, %d token requests and RefreshToken() = %q, want the sign-in alone and refresh-1 kept", len(hits), session.RefreshToken())
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	accessToken, err := session.AccessToken(ctx)
	if err != nil {
		t.Fatalf("AccessToken() after the panic = %v, want a refreshed token", err)
	}
	if minted := server.mintedTokens(); len(minted) != 2 || accessToken != minted[1] {
		t.Errorf("AccessToken() after the panic answered minted token %d of %d, want the second, from a new refresh", slices.Index(minted, accessToken), len(minted))
	}
	assertHoldsMinted(t, server, session, 1)
}

func TestPersist(t *testing.T) {
	t.Run("the first write saves the current state", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1"}) {
			t.Errorf("the store saved %v, want the current state once", saved)
		}
		if stored := decodeStored(t, store); stored.AccessToken != server.mintedTokens()[0] || stored.User.ID != sessionUserID {
			t.Errorf("stored state = %+v, want the sign-in's access token and the served user", stored)
		}
		if store.deleteCount() != 0 {
			t.Errorf("store deletes = %d, want none", store.deleteCount())
		}
	})

	t.Run("a store whose first write fails is never written to", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		first := &recordingStore{}
		if err := session.Persist(t.Context(), first); err != nil {
			t.Fatalf("Persist(first): %v", err)
		}
		failing := &recordingStore{saveErr: errors.New("disk full")}
		if err := session.Persist(t.Context(), failing); !errors.Is(err, failing.saveErr) {
			t.Errorf("Persist(failing) = %v, want the store's error wrapped", err)
		}
		if saved := first.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1"}) {
			t.Errorf("after the failed Persist the first store saved %v, want its own first write alone", saved)
		}
		if result, err := session.Refresh(t.Context()); err != nil || result == nil {
			t.Fatalf("Refresh = (%v, %v), want the empty result", result, err)
		}
		if saved := failing.savedRefreshTokens(t); len(saved) != 0 {
			t.Errorf("the failing store saved %v, want nothing", saved)
		}
		if saved := first.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1", "refresh-2"}) {
			t.Errorf("the first store saved %v, want its first write then the refresh", saved)
		}
	})

	t.Run("a later Persist moves the writes to its own store", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		first, second := &recordingStore{}, &recordingStore{}
		if err := session.Persist(t.Context(), first); err != nil {
			t.Fatalf("Persist(first): %v", err)
		}
		if err := session.Persist(t.Context(), second); err != nil {
			t.Fatalf("Persist(second): %v", err)
		}
		if result, err := session.Refresh(t.Context()); err != nil || result == nil {
			t.Fatalf("Refresh = (%v, %v), want the empty result", result, err)
		}
		if saved := first.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1"}) {
			t.Errorf("the first store saved %v, want its first write alone, left as last written", saved)
		}
		if saved := second.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1", "refresh-2"}) {
			t.Errorf("the second store saved %v, want its first write then the refresh", saved)
		}
	})

	t.Run("a refresh committed during the first write reaches the store after it", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		entered, release := store.holdNextSave()
		var persistErr error
		var group sync.WaitGroup
		group.Go(func() { persistErr = session.Persist(t.Context(), store) })
		<-entered
		group.Go(func() {
			if _, err := session.Refresh(t.Context()); err != nil {
				t.Errorf("Refresh: %v", err)
			}
		})
		for deadline := time.Now().Add(5 * time.Second); session.RefreshToken() != "refresh-2"; {
			if time.Now().After(deadline) {
				t.Fatal("the refresh did not commit while the first write was held")
			}
			time.Sleep(time.Millisecond)
		}
		if saved := store.savedRefreshTokens(t); len(saved) != 0 {
			t.Errorf("with the first write held the store saved %v, want nothing yet", saved)
		}
		release()
		group.Wait()
		if persistErr != nil {
			t.Fatalf("Persist: %v", persistErr)
		}
		if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1", "refresh-2"}) {
			t.Errorf("the store saved %v, want the first write then the refresh", saved)
		}
	})

	t.Run("a nil store is refused", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		if err := session.Persist(t.Context(), nil); !errors.Is(err, auth.ErrMissingStore) {
			t.Errorf("Persist(nil) = %v, want ErrMissingStore", err)
		}
	})
}

func TestRefreshRotatesAndSaves(t *testing.T) {
	t.Run("both tokens rotate and the new state is saved", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		assertHoldsMinted(t, server, session, 0)

		if result, err := session.Refresh(t.Context()); err != nil || result == nil {
			t.Fatalf("Refresh = (%v, %v), want the empty result", result, err)
		}
		requests := server.requestsTo(tokenPath)
		if len(requests) != 2 {
			t.Fatalf("token requests = %d, want the sign-in and the refresh", len(requests))
		}
		assertRefreshGrant(t, requests[1], "refresh-1")
		assertHoldsMinted(t, server, session, 1)
		if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1", "refresh-2"}) {
			t.Fatalf("the store saved %v, want the first write then the refresh", saved)
		}
		stored := decodeStored(t, store)
		if stored.AccessToken != server.mintedTokens()[1] || stored.User.ID != sessionUserID {
			t.Errorf("stored state = %+v, want the refreshed access token and the served user", stored)
		}
		if remaining := time.Until(time.Unix(stored.ExpiresAt, 0)); remaining < 59*time.Minute || remaining > time.Hour {
			t.Errorf("stored expires_at is %v away, want the refreshed hour", remaining)
		}
	})

	t.Run("a failed write is reported after the tokens rotated", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		store.saveErr = errors.New("disk full")

		if result, err := session.Refresh(t.Context()); result != nil || !errors.Is(err, store.saveErr) {
			t.Errorf("Refresh with a failing store = (%v, %v), want the store's error wrapped", result, err)
		}
		assertHoldsMinted(t, server, session, 1)
		if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1"}) {
			t.Errorf("the store saved %v, want its first write alone", saved)
		}
	})
}

func TestStoreWritesFollowCommitOrder(t *testing.T) {
	server := newSessionServer(t)
	session := server.signIn(t, server.client(t), time.Hour)
	store := &recordingStore{}
	if err := session.Persist(t.Context(), store); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	entered, release := store.holdNextSave()
	var group sync.WaitGroup
	group.Go(func() {
		if _, err := session.Refresh(t.Context()); err != nil {
			t.Errorf("first Refresh: %v", err)
		}
	})
	<-entered
	if session.RefreshToken() != "refresh-2" {
		t.Errorf("RefreshToken() while the first refresh's write is held = %q, want refresh-2 already committed", session.RefreshToken())
	}

	// While the first refresh's write is held, a reader with time left
	// returns at once with the committed token.
	read := make(chan string, 1)
	go func() {
		accessToken, err := session.AccessToken(t.Context())
		if err != nil {
			t.Errorf("AccessToken: %v", err)
		}
		read <- accessToken
	}()
	select {
	case accessToken := <-read:
		if minted := server.mintedTokens(); len(minted) != 2 || accessToken != minted[1] {
			t.Errorf("the reader got minted token %d of %d, want the committed second", slices.Index(minted, accessToken), len(minted))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AccessToken waited on the held store write")
	}

	// A second refresh commits and queues its own write behind the first.
	group.Go(func() {
		if _, err := session.Refresh(t.Context()); err != nil {
			t.Errorf("second Refresh: %v", err)
		}
	})
	for deadline := time.Now().Add(5 * time.Second); session.RefreshToken() != "refresh-3"; {
		if time.Now().After(deadline) {
			t.Fatal("the second refresh did not commit while the first write was held")
		}
		time.Sleep(time.Millisecond)
	}
	if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1"}) {
		t.Errorf("with the first refresh's write held the store saved %v, want the first write alone", saved)
	}
	release()
	group.Wait()

	if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1", "refresh-2", "refresh-3"}) {
		t.Errorf("the store saved %v, want each state in commit order", saved)
	}
}

func TestRejectedRefreshPreservesUntilExpiry(t *testing.T) {
	t.Run("a valid token outlives the rejection", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), expiresSoon)
		server.script(tokenPath, refreshRejected.answer())

		accessToken, err := session.AccessToken(t.Context())
		if err != nil || accessToken != server.mintedTokens()[0] {
			t.Errorf("AccessToken() = (%q, %v), want the still-valid token and no error", accessToken, err)
		}
		if hits := server.requestsTo(tokenPath); len(hits) != 2 || session.RefreshToken() != "refresh-1" {
			t.Errorf("after the read, %d token requests and RefreshToken() = %q, want the read's rejected refresh and refresh-1 kept", len(hits), session.RefreshToken())
		}
		result, err := session.Refresh(t.Context())
		if result != nil {
			t.Errorf("Refresh() result = %v beside the error, want nil", result)
		}
		assertServerError(t, err, refreshRejected)
		if errors.Is(err, auth.ErrSessionExpired) {
			t.Error("Refresh reported ErrSessionExpired while the access token is valid")
		}
		if accessToken, err := session.AccessToken(t.Context()); err != nil || accessToken != server.mintedTokens()[0] {
			t.Errorf("AccessToken() after the rejection = (%q, %v), want the still-valid token", accessToken, err)
		}
		if hits := server.requestsTo(tokenPath); len(hits) != 3 {
			t.Errorf("token requests = %d, want the sign-in, the read's refresh and the explicit Refresh with no more after the rejection", len(hits))
		}
		if user := session.User(); user == nil || user.ID() != sessionUserID {
			t.Errorf("User() = %+v, want the served user kept", user)
		}
	})

	t.Run("an expired token ends the session", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), expiredAlready)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		server.script(tokenPath, refreshRejected.answer())

		accessToken, err := session.AccessToken(t.Context())
		if accessToken != "" || !errors.Is(err, auth.ErrSessionExpired) {
			t.Fatalf("AccessToken() = (%q, %v), want ErrSessionExpired", accessToken, err)
		}
		assertServerError(t, err, refreshRejected)
		if store.deleteCount() != 1 || store.stored() != nil {
			t.Errorf("store deletes = %d holding %s, want the store cleared", store.deleteCount(), store.stored())
		}
		if session.RefreshToken() != "" || session.User() != nil {
			t.Error("the ended session still holds credentials")
		}
		if result, err := session.Refresh(t.Context()); result != nil || !errors.Is(err, auth.ErrSessionExpired) {
			t.Errorf("Refresh() after expiry = (%v, %v), want ErrSessionExpired", result, err)
		}
		if hits := server.requestsTo(tokenPath); len(hits) != 2 {
			t.Errorf("token requests = %d, want the sign-in and the rejected refresh with none once the session ended", len(hits))
		}
	})
}

func TestTransientRefreshFailureKeepsSession(t *testing.T) {
	server := newSessionServer(t)
	session := server.signIn(t, server.client(t), expiredAlready)
	store := &recordingStore{}
	if err := session.Persist(t.Context(), store); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	server.script(tokenPath, unavailable.answer(), answer{status: http.StatusOK})

	accessToken, err := session.AccessToken(t.Context())
	if accessToken != "" {
		t.Errorf("AccessToken() = %q beside the error, want no token", accessToken)
	}
	assertServerError(t, err, unavailable)
	if errors.Is(err, auth.ErrSessionExpired) {
		t.Error("a transient failure reported ErrSessionExpired")
	}
	if saved := store.savedRefreshTokens(t); session.RefreshToken() != "refresh-1" || !slices.Equal(saved, []string{"refresh-1"}) || store.deleteCount() != 0 {
		t.Errorf("after the failure RefreshToken() = %q and the store saved %v with %d deletes, want both as they were", session.RefreshToken(), saved, store.deleteCount())
	}
	accessToken, err = session.AccessToken(t.Context())
	if minted := server.mintedTokens(); err != nil || len(minted) != 2 || accessToken != minted[1] {
		t.Errorf("AccessToken() on the next read = (%q, %v), want the refreshed token", accessToken, err)
	}
	assertHoldsMinted(t, server, session, 1)
	if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1", "refresh-2"}) || store.deleteCount() != 0 {
		t.Errorf("the store saved %v with %d deletes, want the refresh written and nothing cleared", saved, store.deleteCount())
	}
}

func TestSignOutGlobalRevokesFirst(t *testing.T) {
	t.Run("the session ends once the server has revoked", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		hold := make(chan struct{})
		server.script(logoutPath, answer{status: http.StatusNoContent, hold: hold})

		var signOutResult *auth.SignOutResult
		var signOutErr error
		var group sync.WaitGroup
		group.Go(func() { signOutResult, signOutErr = session.SignOut(t.Context()) })
		server.awaitRequests(t, logoutPath, 1)
		request := server.requestsTo(logoutPath)[0]
		if request.method != http.MethodPost || request.query != "scope=global" || request.authorization != "Bearer "+server.mintedTokens()[0] {
			t.Errorf("logout request = %s ?%s %q, want POST ?scope=global with the session's bearer", request.method, request.query, request.authorization)
		}
		assertHoldsMinted(t, server, session, 0)
		if store.deleteCount() != 0 {
			t.Errorf("store deletes while the revocation is in flight = %d, want none", store.deleteCount())
		}
		close(hold)
		group.Wait()
		if signOutErr != nil || signOutResult == nil {
			t.Fatalf("SignOut = (%v, %v), want the empty result", signOutResult, signOutErr)
		}
		if store.deleteCount() != 1 || store.stored() != nil {
			t.Errorf("store deletes = %d holding %s, want the store cleared", store.deleteCount(), store.stored())
		}
		assertSignedOut(t, session)
		if result, err := session.SignOut(t.Context()); result != nil || !errors.Is(err, auth.ErrSignedOut) {
			t.Errorf("a repeat SignOut = (%v, %v), want ErrSignedOut", result, err)
		}
		if hits := server.requestsTo(logoutPath); len(hits) != 1 {
			t.Errorf("logout requests = %d, want none after the session ended", len(hits))
		}
	})

	t.Run("a rejection leaves the session live for a retry", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		server.script(logoutPath, badJWT.answer(), answer{status: http.StatusNoContent})

		result, err := session.SignOut(t.Context())
		if result != nil {
			t.Errorf("SignOut() result = %v beside the error, want nil", result)
		}
		assertServerError(t, err, badJWT)
		assertHoldsMinted(t, server, session, 0)
		if store.deleteCount() != 0 {
			t.Errorf("store deletes = %d, want none", store.deleteCount())
		}
		if result, err := session.SignOut(t.Context()); err != nil || result == nil {
			t.Fatalf("the retry = (%v, %v), want the empty result", result, err)
		}
		if hits := server.requestsTo(logoutPath); len(hits) != 2 {
			t.Errorf("logout requests = %d, want the rejected one and the retry", len(hits))
		}
		if store.deleteCount() != 1 {
			t.Errorf("store deletes after the retry = %d, want 1", store.deleteCount())
		}
		assertSignedOut(t, session)
	})

	t.Run("session_not_found counts as the goal state reached", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		server.script(logoutPath, sessionNotFound.answer())

		result, err := session.SignOut(t.Context())
		if err != nil || result == nil {
			t.Fatalf("SignOut = (%v, %v), want the empty result", result, err)
		}
		if store.deleteCount() != 1 || store.stored() != nil {
			t.Errorf("store deletes = %d holding %s, want the store cleared", store.deleteCount(), store.stored())
		}
		assertSignedOut(t, session)
	})

	t.Run("a store that cannot be cleared is reported after the session ended", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{deleteErr: errors.New("disk gone")}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}

		result, err := session.SignOut(t.Context())
		var serverError *auth.Error
		if result != nil || !errors.Is(err, store.deleteErr) || errors.As(err, &serverError) {
			t.Errorf("SignOut = (%v, %v), want the store's error wrapped alone", result, err)
		}
		if hits := server.requestsTo(logoutPath); len(hits) != 1 {
			t.Errorf("logout requests = %d, want the revocation sent", len(hits))
		}
		assertSignedOut(t, session)
	})
}

func TestSignOutLocalTearsDownFirst(t *testing.T) {
	t.Run("the session ends before the server answers", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		hold := make(chan struct{})
		held := logoutFailed.answer()
		held.hold = hold
		server.script(logoutPath, held)

		var signOutResult *auth.SignOutResult
		var signOutErr error
		var group sync.WaitGroup
		group.Go(func() { signOutResult, signOutErr = session.SignOut(t.Context(), signout.WithLocalScope()) })
		server.awaitRequests(t, logoutPath, 1)
		if request := server.requestsTo(logoutPath)[0]; request.query != "scope=local" || request.authorization != "Bearer "+server.mintedTokens()[0] {
			t.Errorf("logout request = ?%s with Authorization %q, want ?scope=local with the captured sign-in token", request.query, request.authorization)
		}
		if hits := server.requestsTo(tokenPath); len(hits) != 1 {
			t.Errorf("token requests = %d, want the sign-in alone, an hour-long token needing no refresh", len(hits))
		}
		assertSignedOut(t, session)
		if store.deleteCount() != 1 {
			t.Errorf("store deletes while the revocation is in flight = %d, want 1", store.deleteCount())
		}
		close(hold)
		group.Wait()
		if signOutResult != nil {
			t.Errorf("SignOut() result = %v beside the error, want nil", signOutResult)
		}
		assertServerError(t, signOutErr, logoutFailed)
	})

	t.Run("session_not_found counts as the goal state reached", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		server.script(logoutPath, sessionNotFound.answer())

		if result, err := session.SignOut(t.Context(), signout.WithLocalScope()); err != nil || result == nil {
			t.Errorf("SignOut = (%v, %v), want the empty result", result, err)
		}
		assertSignedOut(t, session)
	})

	t.Run("a failed clear and a failed revocation are both reported", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{deleteErr: errors.New("disk gone")}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		server.script(logoutPath, logoutFailed.answer())

		result, err := session.SignOut(t.Context(), signout.WithLocalScope())
		if result != nil || !errors.Is(err, store.deleteErr) {
			t.Errorf("SignOut = (%v, %v), want the store's error among those reported", result, err)
		}
		assertServerError(t, err, logoutFailed)
		assertSignedOut(t, session)
	})
}

func TestSignOutLocalRefreshesAnExpiringTokenFirst(t *testing.T) {
	t.Run("a token inside the margin is refreshed, the result kept nowhere", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), expiresSoon)

		if result, err := session.SignOut(t.Context(), signout.WithLocalScope()); err != nil || result == nil {
			t.Fatalf("SignOut = (%v, %v), want the empty result", result, err)
		}
		if paths := server.requestPaths(); !slices.Equal(paths, []string{tokenPath, tokenPath, logoutPath}) {
			t.Fatalf("requests = %v, want the sign-in, one refresh and then the logout", paths)
		}
		assertRefreshGrant(t, server.requestsTo(tokenPath)[1], "refresh-1")
		minted := server.mintedTokens()
		if bearer := server.requestsTo(logoutPath)[0].authorization; bearer != "Bearer "+minted[1] {
			t.Errorf("logout bearer is token %d of %d minted, want the refreshed second", slices.Index(minted, strings.TrimPrefix(bearer, "Bearer ")), len(minted))
		}
		assertSignedOut(t, session)
	})

	t.Run("a failed refresh falls back to the captured token", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), expiresSoon)
		server.script(tokenPath, unavailable.answer())

		if result, err := session.SignOut(t.Context(), signout.WithLocalScope()); err != nil || result == nil {
			t.Fatalf("SignOut = (%v, %v), want the empty result", result, err)
		}
		if paths := server.requestPaths(); !slices.Equal(paths, []string{tokenPath, tokenPath, logoutPath}) {
			t.Fatalf("requests = %v, want the sign-in, the failed refresh and then the logout", paths)
		}
		if bearer := server.requestsTo(logoutPath)[0].authorization; bearer != "Bearer "+server.mintedTokens()[0] {
			t.Errorf("logout bearer = %q, want the captured sign-in token", bearer)
		}
		assertSignedOut(t, session)
	})

	t.Run("an expired token is still sent and the answer reported", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), expiredAlready)
		server.script(tokenPath, unavailable.answer())
		server.script(logoutPath, badJWT.answer())

		result, err := session.SignOut(t.Context(), signout.WithLocalScope())
		if result != nil {
			t.Errorf("SignOut() result = %v beside the error, want nil", result)
		}
		assertServerError(t, err, badJWT)
		if paths := server.requestPaths(); !slices.Equal(paths, []string{tokenPath, tokenPath, logoutPath}) {
			t.Fatalf("requests = %v, want the sign-in, the failed refresh and then the logout", paths)
		}
		if bearer := server.requestsTo(logoutPath)[0].authorization; bearer != "Bearer "+server.mintedTokens()[0] {
			t.Errorf("logout bearer = %q, want the captured sign-in token", bearer)
		}
		assertSignedOut(t, session)
	})
}

func TestSignOutOthersLeavesSessionLive(t *testing.T) {
	t.Run("the session stays live and its store untouched", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}

		if result, err := session.SignOut(t.Context(), signout.WithOthersScope()); err != nil || result == nil {
			t.Fatalf("SignOut = (%v, %v), want the empty result", result, err)
		}
		if request := server.requestsTo(logoutPath)[0]; request.query != "scope=others" || request.authorization != "Bearer "+server.mintedTokens()[0] {
			t.Errorf("logout request = ?%s with Authorization %q, want ?scope=others with the session's bearer", request.query, request.authorization)
		}
		assertHoldsMinted(t, server, session, 0)
		if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1"}) || store.deleteCount() != 0 {
			t.Errorf("the store saved %v with %d deletes, want its first write alone", saved, store.deleteCount())
		}
	})

	t.Run("session_not_found reports this session's own revocation", func(t *testing.T) {
		server := newSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		server.script(logoutPath, sessionNotFound.answer())

		result, err := session.SignOut(t.Context(), signout.WithOthersScope())
		if result != nil {
			t.Errorf("SignOut() result = %v beside the error, want nil", result)
		}
		assertServerError(t, err, sessionNotFound)
		assertHoldsMinted(t, server, session, 0)
	})
}

func TestSignOutDuringRefreshDiscardsRotation(t *testing.T) {
	server := newSessionServer(t)
	session := server.signIn(t, server.client(t), expiresSoon)
	hold := make(chan struct{})
	server.script(tokenPath, answer{status: http.StatusOK, hold: hold}, answer{status: http.StatusOK})

	var readToken string
	var readErr error
	var group sync.WaitGroup
	group.Go(func() { readToken, readErr = session.AccessToken(t.Context()) })
	server.awaitRequests(t, tokenPath, 2)

	if result, err := session.SignOut(t.Context(), signout.WithLocalScope()); err != nil || result == nil {
		t.Errorf("SignOut = (%v, %v), want the empty result", result, err)
	}
	close(hold)
	group.Wait()
	if readToken != "" || !errors.Is(readErr, auth.ErrSignedOut) {
		t.Errorf("the refreshing read = (%q, %v), want ErrSignedOut", readToken, readErr)
	}
	if minted := server.mintedTokens(); len(minted) != 3 {
		t.Errorf("minted %d tokens, want the sign-in, the sign-out's refresh and the rotation the read obtained", len(minted))
	}
	assertSignedOut(t, session)
}

func TestSessionFromStore(t *testing.T) {
	server := newSessionServer(t)
	client := server.client(t)

	t.Run("an unexpired session restores without a request", func(t *testing.T) {
		store := &recordingStore{}
		if err := server.signIn(t, client, time.Hour).Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		persisted := len(server.mintedTokens()) - 1
		before := len(server.requestPaths())
		result, err := server.client(t).SessionFromStore(t.Context(), store)
		if err != nil {
			t.Fatalf("SessionFromStore: %v", err)
		}
		restored := result.Session()
		assertHoldsMinted(t, server, restored, persisted)
		if after := len(server.requestPaths()); after != before {
			t.Errorf("%d requests sent, want none", after-before)
		}
		if result, err := restored.Refresh(t.Context()); err != nil || result == nil {
			t.Fatalf("Refresh = (%v, %v), want the empty result", result, err)
		}
		want := []string{mintedRefreshToken(persisted), mintedRefreshToken(persisted + 1)}
		if saved := store.savedRefreshTokens(t); !slices.Equal(saved, want) {
			t.Errorf("the store saved %v, want %v: the first write then the restored session's refresh", saved, want)
		}
	})

	t.Run("an expired session is refreshed first", func(t *testing.T) {
		store := &recordingStore{}
		if err := server.signIn(t, client, expiredAlready).Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		persisted := len(server.mintedTokens()) - 1
		before := len(server.requestsTo(tokenPath))
		result, err := server.client(t).SessionFromStore(t.Context(), store)
		if err != nil {
			t.Fatalf("SessionFromStore: %v", err)
		}
		requests := server.requestsTo(tokenPath)
		if len(requests) != before+1 {
			t.Fatalf("%d token requests sent, want one refresh", len(requests)-before)
		}
		assertRefreshGrant(t, requests[before], mintedRefreshToken(persisted))
		assertHoldsMinted(t, server, result.Session(), persisted+1)
		want := []string{mintedRefreshToken(persisted), mintedRefreshToken(persisted + 1)}
		if saved := store.savedRefreshTokens(t); !slices.Equal(saved, want) {
			t.Errorf("the store saved %v, want %v: the first write then the refreshed state", saved, want)
		}
	})

	t.Run("a refresh whose write fails restores the session regardless", func(t *testing.T) {
		store := &recordingStore{}
		if err := server.signIn(t, client, expiredAlready).Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		persisted := len(server.mintedTokens()) - 1
		store.saveErr = errors.New("disk full")

		result, err := server.client(t).SessionFromStore(t.Context(), store)
		if err != nil {
			t.Fatalf("SessionFromStore = %v, want the refreshed session despite the failed write", err)
		}
		assertHoldsMinted(t, server, result.Session(), persisted+1)
		if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{mintedRefreshToken(persisted)}) {
			t.Errorf("the store saved %v, want the expired state it held left in place", saved)
		}
	})

	t.Run("nothing stored, an empty or garbage document and a nil store", func(t *testing.T) {
		if result, err := client.SessionFromStore(t.Context(), &recordingStore{}); result != nil || !errors.Is(err, auth.ErrNoStoredSession) {
			t.Errorf("SessionFromStore(nothing stored) = (%v, %v), want ErrNoStoredSession", result, err)
		}
		var syntaxError *json.SyntaxError
		result, err := client.SessionFromStore(t.Context(), &recordingStore{state: []byte{}})
		if result != nil || !errors.As(err, &syntaxError) || !strings.Contains(err.Error(), "decoding stored session") {
			t.Errorf("SessionFromStore(empty document) = (%v, %v), want the JSON syntax error wrapped", result, err)
		}
		result, err = client.SessionFromStore(t.Context(), &recordingStore{state: []byte("garbage")})
		if result != nil || !errors.As(err, &syntaxError) || !strings.Contains(err.Error(), "decoding stored session") {
			t.Errorf("SessionFromStore(garbage) = (%v, %v), want the JSON syntax error wrapped", result, err)
		}
		if result, err := client.SessionFromStore(t.Context(), nil); result != nil || !errors.Is(err, auth.ErrMissingStore) {
			t.Errorf("SessionFromStore(nil) = (%v, %v), want ErrMissingStore", result, err)
		}
	})

	t.Run("a rejected refresh expires the session and clears the store", func(t *testing.T) {
		store := &recordingStore{}
		if err := server.signIn(t, client, expiredAlready).Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		server.script(tokenPath, refreshRejected.answer())

		result, err := server.client(t).SessionFromStore(t.Context(), store)
		if result != nil || !errors.Is(err, auth.ErrSessionExpired) {
			t.Fatalf("SessionFromStore = (%v, %v), want ErrSessionExpired", result, err)
		}
		assertServerError(t, err, refreshRejected)
		if store.deleteCount() != 1 || store.stored() != nil {
			t.Errorf("store deletes = %d holding %s, want the store cleared", store.deleteCount(), store.stored())
		}
	})
}

func TestZeroSessionIsInert(t *testing.T) {
	cases := []struct {
		name    string
		session func(t *testing.T) *auth.Session
	}{
		{
			name:    "the zero Session",
			session: func(*testing.T) *auth.Session { return &auth.Session{} },
		},
		{
			name: "a session SignOut ended",
			session: func(t *testing.T) *auth.Session {
				t.Helper()
				server := newSessionServer(t)
				session := server.signIn(t, server.client(t), time.Hour)
				if _, err := session.SignOut(t.Context()); err != nil {
					t.Fatalf("SignOut: %v", err)
				}
				return session
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			session := testCase.session(t)
			assertSignedOut(t, session)
			if result, err := session.Refresh(t.Context()); result != nil || !errors.Is(err, auth.ErrSignedOut) {
				t.Errorf("Refresh() = (%v, %v), want ErrSignedOut", result, err)
			}
			if result, err := session.SignOut(t.Context()); result != nil || !errors.Is(err, auth.ErrSignedOut) {
				t.Errorf("SignOut() = (%v, %v), want ErrSignedOut", result, err)
			}
			store := &recordingStore{}
			if err := session.Persist(t.Context(), store); !errors.Is(err, auth.ErrSignedOut) {
				t.Errorf("Persist() = %v, want ErrSignedOut", err)
			}
			if saved := store.savedRefreshTokens(t); len(saved) != 0 {
				t.Errorf("the store saved %v, want nothing", saved)
			}
		})
	}
}

func TestSessionCopySharesState(t *testing.T) {
	server := newSessionServer(t)
	session := server.signIn(t, server.client(t), time.Hour)
	copied := *session
	if result, err := session.Refresh(t.Context()); err != nil || result == nil {
		t.Fatalf("Refresh = (%v, %v), want the empty result", result, err)
	}
	assertHoldsMinted(t, server, &copied, 1)
	if result, err := copied.SignOut(t.Context()); err != nil || result == nil {
		t.Fatalf("SignOut through the copy = (%v, %v), want the empty result", result, err)
	}
	assertSignedOut(t, session)
}

func TestUserIsIndependentPerCall(t *testing.T) {
	server := newSessionServer(t)
	session := server.signIn(t, server.client(t), time.Hour)

	tags, ok := session.User().UserMetadata()["tags"].(map[string]any)
	if !ok {
		t.Fatalf("User() tags = %#v, want a map", session.User().UserMetadata()["tags"])
	}
	if tags["team"] != "platform" {
		t.Fatalf("User() serves team %v, want the served platform", tags["team"])
	}
	tags["team"] = "mutated"
	if team := session.User().UserMetadata()["tags"].(map[string]any)["team"]; team != "platform" {
		t.Errorf("a later User() sees team %q, want the served platform", team)
	}

	var group sync.WaitGroup
	for range 20 {
		group.Go(func() {
			if user := session.User(); user == nil || user.UserMetadata()["display_name"] != "Ada" {
				t.Errorf("User() = %+v, want the served user", user)
			}
		})
	}
	group.Go(func() {
		for range 20 {
			session.User().UserMetadata()["tags"].(map[string]any)["team"] = "mutated"
		}
	})
	group.Wait()
}
