package asuser

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/signout"
	"github.com/supabase/supabase-go/integration-testing/testkit"
)

// memoryStore is an auth.SessionStore holding one session's state in memory.
type memoryStore struct {
	mutex sync.Mutex
	state []byte
}

func (s *memoryStore) Load(_ context.Context) ([]byte, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.state, nil
}

func (s *memoryStore) Save(_ context.Context, state []byte) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.state = state
	return nil
}

func (s *memoryStore) Delete(_ context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.state = nil
	return nil
}

// signIn signs the signed-up user in through the SDK and returns the session.
func signIn(t *testing.T, client *auth.Client, user testkit.User) *auth.Session {
	t.Helper()
	result, err := client.SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: user.Email, Password: testkit.Password})
	if err != nil {
		t.Fatalf("SignInWithPassword: %v", err)
	}
	return result.Session()
}

// assertHonored asserts the server honors the session accessToken belongs to:
// GetUser answers with the user whose id is userID.
func assertHonored(t *testing.T, client *auth.Client, accessToken, userID string) {
	t.Helper()
	result, err := client.GetUser(t.Context(), accessToken)
	if err != nil || result.User().ID() != userID {
		t.Errorf("GetUser with the session's token = (%v, %v), want the user %s", result, err, userID)
	}
}

// assertRevoked asserts the server has revoked the session accessToken
// belongs to: GetUser answers 403 session_not_found.
func assertRevoked(t *testing.T, client *auth.Client, accessToken string) {
	t.Helper()
	result, err := client.GetUser(t.Context(), accessToken)
	var serverError *auth.Error
	if result != nil || !errors.As(err, &serverError) || serverError.HTTPStatus != http.StatusForbidden || serverError.Code != "session_not_found" {
		t.Errorf("GetUser with the revoked session's token = (%v, %v), want *auth.Error with HTTP 403 and code session_not_found", result, err)
	}
}

// TestSignInWithPassword signs a real user in and proves the session carries
// the served profile and a new session's tokens, the access token verifying
// locally. A wrong password is answered with invalid_credentials.
func TestSignInWithPassword(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	client := newAuthClient(t)

	t.Run("the session carries the user and a token that verifies", func(t *testing.T) {
		user := testkit.SignUpUser(t, projectURL, apiKey)
		result, err := client.SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: user.Email, Password: testkit.Password})
		if err != nil {
			t.Fatalf("SignInWithPassword: %v", err)
		}
		if result.WeakPassword() != nil {
			t.Errorf("WeakPassword() = %+v, want nil for a password the project accepts", result.WeakPassword())
		}
		session := result.Session()
		if profile := session.User(); profile.ID() != user.ID || profile.Email() != user.Email {
			t.Errorf("User() = (%q, %q), want (%q, %q)", profile.ID(), profile.Email(), user.ID, user.Email)
		}
		refreshToken := session.RefreshToken()
		if refreshToken == "" || refreshToken == user.RefreshToken {
			t.Errorf("RefreshToken() = %q, want the new session's own", refreshToken)
		}
		accessToken, err := session.AccessToken(t.Context())
		if err != nil {
			t.Fatalf("AccessToken: %v", err)
		}
		if session.RefreshToken() != refreshToken {
			t.Error("AccessToken refreshed a token the sign-in had just issued")
		}
		verified, err := client.GetClaims(t.Context(), accessToken)
		if err != nil {
			t.Fatalf("GetClaims: %v", err)
		}
		if claims := verified.Claims(); claims.Subject() != user.ID || claims.Email() != user.Email || claims.Role() != "authenticated" {
			t.Errorf("claims = (sub %q, email %q, role %q), want (%q, %q, authenticated)", claims.Subject(), claims.Email(), claims.Role(), user.ID, user.Email)
		}
	})

	t.Run("a wrong password is answered with invalid_credentials", func(t *testing.T) {
		user := testkit.SignUpUser(t, projectURL, apiKey)
		result, err := client.SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: user.Email, Password: "not-the-password"})
		var serverError *auth.Error
		if result != nil || !errors.As(err, &serverError) || serverError.HTTPStatus != http.StatusBadRequest || serverError.Code != "invalid_credentials" {
			t.Errorf("a wrong password = (%v, %v), want *auth.Error with HTTP 400 and code invalid_credentials", result, err)
		}
	})
}

// TestSessionFromTokens adopts a signed-up user's tokens, refreshes them within
// the same server session and proves the server still honors the pair it
// rotated away from, as the parent of the active token.
func TestSessionFromTokens(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)
	client := newAuthClient(t)

	result, err := client.SessionFromTokens(t.Context(), user.AccessToken, user.RefreshToken)
	if err != nil {
		t.Fatalf("SessionFromTokens: %v", err)
	}
	session := result.Session()
	if profile := session.User(); profile.ID() != user.ID || profile.Email() != user.Email {
		t.Errorf("User() = (%q, %q), want (%q, %q)", profile.ID(), profile.Email(), user.ID, user.Email)
	}
	if session.RefreshToken() != user.RefreshToken {
		t.Errorf("RefreshToken() = %q, want the adopted %q", session.RefreshToken(), user.RefreshToken)
	}
	if accessToken, err := session.AccessToken(t.Context()); err != nil || accessToken != user.AccessToken {
		t.Errorf("AccessToken() = (%q, %v), want the adopted token, unexpired and so not refreshed", accessToken, err)
	}
	adoptedClaims, err := client.GetClaims(t.Context(), user.AccessToken)
	if err != nil {
		t.Fatalf("GetClaims of the adopted token: %v", err)
	}

	if refreshed, err := session.Refresh(t.Context()); err != nil || refreshed == nil {
		t.Fatalf("Refresh = (%v, %v), want the empty result", refreshed, err)
	}
	rotatedAccessToken, err := session.AccessToken(t.Context())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if rotatedAccessToken == user.AccessToken || session.RefreshToken() == user.RefreshToken {
		t.Error("Refresh left a token in place")
	}
	rotatedClaims, err := client.GetClaims(t.Context(), rotatedAccessToken)
	if err != nil {
		t.Fatalf("GetClaims of the rotated token: %v", err)
	}
	if rotated, adopted := rotatedClaims.Claims(), adoptedClaims.Claims(); rotated.Subject() != user.ID || rotated.SessionID() != adopted.SessionID() {
		t.Errorf("the rotated token names (sub %q, session %q), want (%q, %q): the adopted session renewed", rotated.Subject(), rotated.SessionID(), user.ID, adopted.SessionID())
	}

	// The pair the refresh rotated away from is still adopted: the server
	// answers the parent of its active refresh token with that active token.
	adoptedAgain, err := client.SessionFromTokens(t.Context(), user.AccessToken, user.RefreshToken)
	if err != nil {
		t.Fatalf("SessionFromTokens with the rotated-away pair: %v", err)
	}
	again := adoptedAgain.Session()
	if refreshed, err := again.Refresh(t.Context()); err != nil || refreshed == nil {
		t.Fatalf("Refresh of the rotated-away pair = (%v, %v), want the server to honor the parent token", refreshed, err)
	}
	if again.RefreshToken() != session.RefreshToken() {
		t.Errorf("the rotated-away pair refreshed to %q, want the active %q", again.RefreshToken(), session.RefreshToken())
	}
}

// TestSignOutRevokes proves a bare SignOut revokes on the server and ends the
// session, and that WithOthersScope from a second session revokes the first
// while leaving its own tokens working.
func TestSignOutRevokes(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	client := newAuthClient(t)

	t.Run("a bare SignOut revokes on the server and ends the session", func(t *testing.T) {
		user := testkit.SignUpUser(t, projectURL, apiKey)
		session := signIn(t, client, user)
		accessToken, err := session.AccessToken(t.Context())
		if err != nil {
			t.Fatalf("AccessToken: %v", err)
		}
		assertHonored(t, client, accessToken, user.ID)

		if result, err := session.SignOut(t.Context()); err != nil || result == nil {
			t.Fatalf("SignOut = (%v, %v), want the empty result", result, err)
		}
		assertRevoked(t, client, accessToken)
		if accessToken, err := session.AccessToken(t.Context()); accessToken != "" || !errors.Is(err, auth.ErrSignedOut) {
			t.Errorf("AccessToken() after SignOut = (%q, %v), want ErrSignedOut", accessToken, err)
		}
		if session.RefreshToken() != "" || session.User() != nil {
			t.Error("the ended session still holds credentials")
		}
	})

	t.Run("WithOthersScope revokes the first session and spares its own", func(t *testing.T) {
		user := testkit.SignUpUser(t, projectURL, apiKey)
		first, second := signIn(t, client, user), signIn(t, client, user)
		firstAccessToken, err := first.AccessToken(t.Context())
		if err != nil {
			t.Fatalf("AccessToken of the first session: %v", err)
		}
		secondAccessToken, err := second.AccessToken(t.Context())
		if err != nil {
			t.Fatalf("AccessToken of the second session: %v", err)
		}
		firstRefreshToken, secondRefreshToken := first.RefreshToken(), second.RefreshToken()

		if result, err := second.SignOut(t.Context(), signout.WithOthersScope()); err != nil || result == nil {
			t.Fatalf("SignOut with WithOthersScope = (%v, %v), want the empty result", result, err)
		}
		assertRevoked(t, client, firstAccessToken)
		assertHonored(t, client, secondAccessToken, user.ID)
		if accessToken, err := second.AccessToken(t.Context()); err != nil || accessToken != secondAccessToken {
			t.Errorf("the second session's AccessToken() = (%q, %v), want its own token unchanged", accessToken, err)
		}

		// The first Session learns of the revocation from its next refresh,
		// rejected for good, while its unexpired tokens stay in place.
		refreshed, err := first.Refresh(t.Context())
		var serverError *auth.Error
		if refreshed != nil || !errors.As(err, &serverError) || serverError.HTTPStatus != http.StatusBadRequest || errors.Is(err, auth.ErrSessionExpired) {
			t.Errorf("Refresh of the first session = (%v, %v), want *auth.Error with HTTP 400 and the session kept", refreshed, err)
		}
		if accessToken, err := first.AccessToken(t.Context()); err != nil || accessToken != firstAccessToken || first.RefreshToken() != firstRefreshToken {
			t.Errorf("the first session after the rejection reads (%q, %v) with refresh token %q, want its unexpired tokens kept", accessToken, err, first.RefreshToken())
		}

		if refreshed, err := second.Refresh(t.Context()); err != nil || refreshed == nil {
			t.Errorf("Refresh of the second session = (%v, %v), want its own session spared", refreshed, err)
		}
		if second.RefreshToken() == secondRefreshToken {
			t.Error("the second session's Refresh rotated nothing")
		}
	})
}

// TestSessionFromStoreRoundTrip persists a session, restores it in a fresh
// client, finds the restored session's refresh written back to the store and
// finds the store cleared once the session has signed out.
func TestSessionFromStoreRoundTrip(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)
	store := &memoryStore{}

	session := signIn(t, newAuthClient(t), user)
	accessToken, err := session.AccessToken(t.Context())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if err := session.Persist(t.Context(), store); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if document, err := store.Load(t.Context()); err != nil || len(document) == 0 {
		t.Fatalf("the store after Persist holds %d bytes (%v), want the session", len(document), err)
	}

	fromStore, err := newAuthClient(t).SessionFromStore(t.Context(), store)
	if err != nil {
		t.Fatalf("SessionFromStore: %v", err)
	}
	restored := fromStore.Session()
	if restored.User().ID() != user.ID || restored.RefreshToken() != session.RefreshToken() {
		t.Errorf("the restored session holds (%q, %q), want the persisted user and refresh token", restored.User().ID(), restored.RefreshToken())
	}
	if restoredAccessToken, err := restored.AccessToken(t.Context()); err != nil || restoredAccessToken != accessToken {
		t.Errorf("the restored AccessToken() = (%q, %v), want the persisted token", restoredAccessToken, err)
	}

	if result, err := restored.Refresh(t.Context()); err != nil || result == nil {
		t.Fatalf("Refresh of the restored session = (%v, %v), want the empty result", result, err)
	}
	reloaded, err := newAuthClient(t).SessionFromStore(t.Context(), store)
	if err != nil {
		t.Fatalf("SessionFromStore after the refresh: %v", err)
	}
	if stored := reloaded.Session().RefreshToken(); stored != restored.RefreshToken() || stored == session.RefreshToken() {
		t.Errorf("the store holds refresh token %q, want the rotated %q", stored, restored.RefreshToken())
	}

	if result, err := restored.SignOut(t.Context()); err != nil || result == nil {
		t.Fatalf("SignOut = (%v, %v), want the empty result", result, err)
	}
	if result, err := newAuthClient(t).SessionFromStore(t.Context(), store); result != nil || !errors.Is(err, auth.ErrNoStoredSession) {
		t.Errorf("SessionFromStore after SignOut = (%v, %v), want ErrNoStoredSession", result, err)
	}
}

// TestConcurrentReadersShareOneRefresh refreshes one session from twenty
// goroutines at once and proves they share refreshes rather than race them:
// every call succeeds, with no 409 from the server's concurrency guard, and
// the session ends up holding rotated tokens that verify.
func TestConcurrentReadersShareOneRefresh(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)
	client := newAuthClient(t)
	session := signIn(t, client, user)
	accessTokenBefore, err := session.AccessToken(t.Context())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	refreshTokenBefore := session.RefreshToken()

	const readers = 20
	results := make([]*auth.RefreshResult, readers)
	refreshErrors := make([]error, readers)
	var group sync.WaitGroup
	for i := range readers {
		group.Go(func() {
			results[i], refreshErrors[i] = session.Refresh(t.Context())
		})
	}
	group.Wait()
	for i := range readers {
		if refreshErrors[i] != nil || results[i] == nil {
			t.Errorf("reader %d: Refresh = (%v, %v), want every reader to share a refresh", i, results[i], refreshErrors[i])
		}
	}
	accessToken, err := session.AccessToken(t.Context())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if accessToken == accessTokenBefore || session.RefreshToken() == refreshTokenBefore {
		t.Error("the shared refresh rotated nothing")
	}
	if verified, err := client.GetClaims(t.Context(), accessToken); err != nil || verified.Claims().Subject() != user.ID {
		t.Errorf("GetClaims of the rotated token = (%v, %v), want the user's verified claims", verified, err)
	}
}
