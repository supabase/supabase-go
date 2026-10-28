package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/supabase/supabase-go/auth/internal/session"
	"github.com/supabase/supabase-go/auth/internal/token"
	"github.com/supabase/supabase-go/auth/signout"
	"github.com/supabase/supabase-go/core/configuration"
)

// expiryMargin is the time left on an access token below which
// [Session.AccessToken] refreshes it before answering.
const expiryMargin = 30 * time.Second

// Session is one signed-in user's live session with the Auth server: the
// access token the user's requests carry, the refresh token that renews it
// and the user's profile as the server last served it. Its state changes
// only through its own methods. Hold one Session per signed-in user and
// share it freely: a Session is safe for concurrent use by multiple
// goroutines, every read reflects the latest state and a copy of a Session
// is the same session. A Session holds no resource that needs releasing and
// starts no long-lived goroutine, so one that is no longer wanted is simply
// dropped.
//
// The access token is renewed on demand, by [Session.AccessToken] and
// [Session.Refresh], with at most one refresh in flight at a time whose
// result concurrent callers share. A caller's context bounds its own wait
// alone, so a caller that gives up fails no other. A refresh the server
// answers with a 4xx status other than 429 rejects the refresh token for
// good, and the session ends once its access token expires. Any other
// failure, in transport or a 5xx or 429 answer, leaves the session as it
// was.
//
// A session also ends when [Session.SignOut] ends it. An ended session holds
// no credentials: every operation returns [ErrSignedOut], or
// [ErrSessionExpired] when it ended by expiring, while [Session.RefreshToken]
// returns "" and [Session.User] nil. The zero Session answers as a
// signed-out one.
//
// A Session lives in memory until [Session.Persist] hands it a
// [SessionStore], which it then writes on every change of its tokens or user
// and clears when the session ends.
type Session struct {
	client *Client
	inner  *session.Session
}

// SessionStore keeps one session's state between runs of a program. The
// state is an opaque UTF-8 JSON document and a credential: treat it as a
// secret. A store holds one session's state, since two sessions persisting
// to one store overwrite each other. A session calls its store from one
// goroutine at a time, in the order its state changed, and each method
// receives the context of the call that caused it. A store's methods must
// not call methods of the session it persists.
type SessionStore interface {
	// Load returns the state the store holds, or nil when it holds nothing.
	// An empty slice is stored state, not its absence. The slice is read,
	// never modified or kept. [Client.SessionFromStore] returns an error from
	// Load wrapped.
	Load(ctx context.Context) ([]byte, error)

	// Save replaces the state the store holds with state, a slice the store
	// may keep. A failed Save is returned by the call that caused the write,
	// except after a refresh started by [Session.AccessToken] or
	// [Client.SessionFromStore], when the failure reaches no caller.
	Save(ctx context.Context, state []byte) error

	// Delete clears the state the store holds. A failed Delete is returned by
	// [Session.SignOut], and reaches no caller when the session ends by
	// expiring.
	Delete(ctx context.Context) error
}

// AccessToken returns the current access token, refreshing the session first
// when fewer than thirty seconds of the token remain and returning the token
// while it is unexpired even when that refresh fails. Once the token has
// expired, a failed refresh is returned as a transport error, an [*Error] or,
// when the rejection ended the session, [ErrSessionExpired] wrapping the
// [*Error]. The method value session.AccessToken is a
// [configuration.AccessTokenProvider]. A refresh this call starts is written
// to the session's store before it returns.
func (s *Session) AccessToken(ctx context.Context) (string, error) {
	if s.inner == nil {
		return "", ErrSignedOut
	}
	accessToken, err := s.inner.Token(ctx)
	if err != nil {
		return "", sessionError(err)
	}
	return accessToken, nil
}

// RefreshToken returns the current refresh token. Hand it to another
// process together with [Session.AccessToken]'s result and
// [Client.SessionFromTokens] adopts the session there. The server rotates
// it on every refresh, so a copy goes stale as soon as any holder refreshes.
func (s *Session) RefreshToken() string {
	if s.inner == nil {
		return ""
	}
	return s.inner.State().RefreshToken()
}

// User returns the user's profile as the server last served it: at sign-in,
// adoption or the latest refresh. Each call returns a value of its own,
// nested metadata included, which the caller may retain and modify freely.
// It is never nil while the session is live. For a profile the server has
// just verified, pass [Session.AccessToken]'s result to [Client.GetUser].
func (s *Session) User() *User {
	if s.inner == nil {
		return nil
	}
	document := s.inner.State().User()
	if document == nil {
		return nil
	}
	user, err := parseUser(document)
	if err != nil {
		// A document only reaches the state once it has decoded, so this
		// branch is unreachable.
		return nil
	}
	return user
}

// Refresh renews the session now, however long its access token has left,
// rotating both tokens and writing the renewed state to the session's store.
// A failed refresh is returned as a transport error, an [*Error] or, when the
// rejection ended the session because its access token had already expired,
// [ErrSessionExpired] wrapping the [*Error]. A store that cannot be written
// is reported as the wrapped error, after the session already holds the new
// tokens.
func (s *Session) Refresh(ctx context.Context) (*RefreshResult, error) {
	if s.inner == nil {
		return nil, ErrSignedOut
	}
	if err := s.inner.Refresh(ctx); err != nil {
		return nil, sessionError(err)
	}
	return &RefreshResult{}, nil
}

// RefreshResult is the answer to [Session.Refresh]. It carries nothing: the
// renewed tokens are read from the [Session].
type RefreshResult struct{}

// SignOut ends the session and revokes it on the server. A bare call revokes
// every session the user holds: the server is asked first and the session
// ends only once revocation succeeded, a failure leaving the session live
// and returning the error. With [signout.WithLocalScope] the session ends
// and its store is cleared first, and the server then revokes this session
// alone, its answer reported after the session has ended. An access token
// inside its expiry margin is refreshed once for that revocation, the result
// kept nowhere. With [signout.WithOthersScope] the server revokes every
// other session and this one stays live. An answer of session_not_found or
// user_not_found counts as success, except under [signout.WithOthersScope],
// where it reports this session's own revocation. A store that cannot be
// cleared is reported as the wrapped error, alongside a failed revocation
// under [signout.WithLocalScope].
func (s *Session) SignOut(ctx context.Context, options ...signout.Option) (*SignOutResult, error) {
	if s.inner == nil {
		return nil, ErrSignedOut
	}
	scope := signout.Resolve(options...).Scope()
	if scope == "local" {
		return s.signOutLocally(ctx)
	}

	bearer, err := s.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	err = logout(ctx, s.client.httpClient, s.client.baseURL, bearer, scope)
	if scope == "others" {
		if err != nil {
			return nil, err
		}
		return &SignOutResult{}, nil
	}
	if err != nil && !goalStateReached(err) {
		return nil, err
	}
	if _, err := s.inner.End(ctx); err != nil {
		return nil, storeClearError(err)
	}
	return &SignOutResult{}, nil
}

// signOutLocally ends the session before the server is called, then revokes
// the ended session with the freshest bearer in hand: the captured access
// token, refreshed once when it is inside the expiry margin, with the result
// kept nowhere. A failed revocation and a failed clear are reported together.
func (s *Session) signOutLocally(ctx context.Context) (*SignOutResult, error) {
	held, clearErr := s.inner.End(ctx)
	var ended *session.EndedError
	if errors.As(clearErr, &ended) {
		return nil, sessionError(clearErr)
	}
	bearer := held.AccessToken()
	if held.ExpiresAt().Sub(s.client.now()) < expiryMargin {
		if renewed, err := s.client.refreshGrant(ctx, held.RefreshToken()); err == nil {
			bearer = renewed.AccessToken()
		}
	}
	revokeErr := logout(ctx, s.client.httpClient, s.client.baseURL, bearer, "local")
	if goalStateReached(revokeErr) {
		revokeErr = nil
	}
	if clearErr != nil {
		clearErr = storeClearError(clearErr)
	}
	if err := errors.Join(revokeErr, clearErr); err != nil {
		return nil, err
	}
	return &SignOutResult{}, nil
}

// Persist writes the session to store at once and on every later change of
// its tokens or user, and clears the store when the session ends, so
// [Client.SessionFromStore] can restore it in a later run. If that first
// write fails, the error is returned wrapped and the session persists as
// before. A later Persist moves persistence to its own store and leaves the
// earlier one as last written, no other write reaching either store while
// its first write runs. Persist returns [ErrMissingStore] for a nil store.
func (s *Session) Persist(ctx context.Context, store SessionStore) error {
	if store == nil {
		return ErrMissingStore
	}
	if s.inner == nil {
		return ErrSignedOut
	}
	if err := s.inner.Persist(ctx, store); err != nil {
		var ended *session.EndedError
		if errors.As(err, &ended) {
			return sessionError(err)
		}
		return fmt.Errorf("auth: writing session store: %w", err)
	}
	return nil
}

// SessionFromTokens adopts a session from an access token and refresh token
// obtained elsewhere, such as a session another process signed in. An
// unexpired access token is verified by the server, which also supplies the
// user's profile. An expired one is refreshed, rotating both tokens. An
// empty accessToken returns [ErrMissingJWT], one that is not a JWT returns
// [ErrMalformedJWT], an empty refreshToken returns [ErrMissingRefreshToken]
// and a rejected token surfaces as [*Error].
func (c *Client) SessionFromTokens(ctx context.Context, accessToken, refreshToken string) (*SessionFromTokensResult, error) {
	if accessToken == "" {
		return nil, ErrMissingJWT
	}
	if refreshToken == "" {
		return nil, ErrMissingRefreshToken
	}
	decoded, err := token.Decode(accessToken)
	if err != nil {
		return nil, ErrMalformedJWT
	}
	claims, err := decoded.Claims()
	if err != nil {
		return nil, ErrMalformedJWT
	}

	var state session.State
	if expiresAt := claims.ExpiresAt(); expiresAt.After(c.now()) {
		user, err := send(ctx, c.httpClient, http.MethodGet, c.baseURL.JoinPath("user"), nil, accessToken)
		if err != nil {
			return nil, err
		}
		state, err = session.NewState(accessToken, refreshToken, expiresAt, user)
		if err != nil {
			return nil, fmt.Errorf("auth: decoding user: %w", err)
		}
	} else {
		state, err = c.refreshGrant(ctx, refreshToken)
		if err != nil {
			return nil, err
		}
	}
	return &SessionFromTokensResult{session: c.newSession(state, nil)}, nil
}

// SessionFromTokensResult is the answer to [Client.SessionFromTokens].
type SessionFromTokensResult struct {
	session *Session
}

// Session returns the adopted session.
func (r *SessionFromTokensResult) Session() *Session { return r.session }

// SessionFromStore restores the session a previous run persisted to store
// with [Session.Persist], and keeps persisting to it. An unexpired access
// token is adopted without a request, and an expired one is refreshed
// first. It returns [ErrNoStoredSession] when the store holds nothing and
// [ErrMissingStore] for a nil store. When the server rejects that refresh,
// the store is cleared and [ErrSessionExpired] is returned wrapping the
// rejection, and any other refresh failure, a transport error or an
// [*Error], is returned with the store left as it was. A document the store
// holds that Persist did not write is reported as the wrapped decode error.
func (c *Client) SessionFromStore(ctx context.Context, store SessionStore) (*SessionFromStoreResult, error) {
	if store == nil {
		return nil, ErrMissingStore
	}
	document, err := store.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: reading session store: %w", err)
	}
	if document == nil {
		return nil, ErrNoStoredSession
	}
	state, err := session.Parse(document)
	if err != nil {
		return nil, fmt.Errorf("auth: decoding stored session: %w", err)
	}
	restored := c.newSession(state, store)
	if !state.ExpiresAt().After(c.now()) {
		var write *session.WriteError
		if err := restored.inner.Refresh(ctx); err != nil && !errors.As(err, &write) {
			return nil, sessionError(err)
		}
	}
	return &SessionFromStoreResult{session: restored}, nil
}

// SessionFromStoreResult is the answer to [Client.SessionFromStore].
type SessionFromStoreResult struct {
	session *Session
}

// Session returns the restored session.
func (r *SessionFromStoreResult) Session() *Session { return r.session }

// newSession wraps state in a live [Session] that refreshes through this
// client and persists to store, when one is given, from the start.
func (c *Client) newSession(state session.State, store SessionStore) *Session {
	return &Session{client: c, inner: session.New(state, session.Options{
		Refresh:      c.refreshGrant,
		Definitive:   definitiveRejection,
		ExpiryMargin: expiryMargin,
		Now:          c.now,
		Store:        store,
	})}
}

// refreshGrant exchanges refreshToken for a renewed session state through the
// token endpoint's refresh_token grant.
func (c *Client) refreshGrant(ctx context.Context, refreshToken string) (session.State, error) {
	endpoint := c.baseURL.JoinPath("token")
	endpoint.RawQuery = url.Values{"grant_type": {"refresh_token"}}.Encode()
	grant := struct {
		RefreshToken string `json:"refresh_token"`
	}{refreshToken}
	body, err := send(ctx, c.httpClient, http.MethodPost, endpoint, grant, "")
	if err != nil {
		return session.State{}, err
	}
	return parseTokenResponse(body)
}

// parseTokenResponse decodes the token endpoint's response into a session
// state.
func parseTokenResponse(body []byte) (session.State, error) {
	state, err := session.Parse(body)
	if err != nil {
		return session.State{}, fmt.Errorf("auth: decoding token response: %w", err)
	}
	return state, nil
}

// definitiveRejection reports whether a refresh failure is one the server
// repeats for the same refresh token, a 4xx answer other than 429, as
// opposed to a transport failure, a 5xx answer or a 429, which a later
// attempt may cure.
func definitiveRejection(err error) bool {
	var serverError *Error
	if !errors.As(err, &serverError) {
		return false
	}
	status := serverError.HTTPStatus
	return status >= 400 && status < 500 && status != http.StatusTooManyRequests
}

// goalStateReached reports whether a sign-out failure says the server was
// already in the state the sign-out exists to reach: the session, or the
// user, no longer exists.
func goalStateReached(err error) bool {
	var serverError *Error
	if !errors.As(err, &serverError) {
		return false
	}
	return serverError.Code == "session_not_found" || serverError.Code == "user_not_found"
}

// sessionError translates the session machinery's errors into this package's:
// an ended session is [ErrSignedOut] or [ErrSessionExpired] wrapping the
// server's rejection, a store write that failed after its state was
// committed is wrapped and every other error is returned as it is.
func sessionError(err error) error {
	var ended *session.EndedError
	if errors.As(err, &ended) {
		if ended.Status == session.Expired {
			return fmt.Errorf("%w: %w", ErrSessionExpired, ended.Cause)
		}
		return ErrSignedOut
	}
	var write *session.WriteError
	if errors.As(err, &write) {
		return fmt.Errorf("auth: writing session store: %w", write.Err)
	}
	return err
}

// storeClearError translates the error of a session's end: an already ended
// session is [ErrSignedOut] and a store that could not be cleared is wrapped.
func storeClearError(err error) error {
	var ended *session.EndedError
	if errors.As(err, &ended) {
		return sessionError(err)
	}
	return fmt.Errorf("auth: clearing session store: %w", err)
}

// logout sends the revocation request for the session bearer belongs to,
// stating scope on the wire, and returns the server's rejection as [*Error].
func logout(ctx context.Context, httpClient configuration.HTTPClient, baseURL *url.URL, bearer, scope string) error {
	endpoint := baseURL.JoinPath("logout")
	endpoint.RawQuery = url.Values{"scope": {scope}}.Encode()
	_, err := send(ctx, httpClient, http.MethodPost, endpoint, nil, bearer)
	return err
}
