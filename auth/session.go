package auth

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"time"

	"github.com/supabase/supabase-go/auth/internal/backoff"
	"github.com/supabase/supabase-go/auth/internal/scopes"
	"github.com/supabase/supabase-go/auth/internal/session"
	"github.com/supabase/supabase-go/auth/internal/token"
	"github.com/supabase/supabase-go/auth/signout"
	"github.com/supabase/supabase-go/core/configuration"
)

// expiryMargin is the time left on an access token at or below which
// [Session.AccessToken] refreshes it before answering, and a session whose
// refresh the server has rejected ends.
const expiryMargin = 30 * time.Second

// Session is one signed-in user's live session with the Auth server: the
// access token the user's requests carry, the refresh token that renews it
// and the user's profile as the server last served it. Its state changes
// only through its own methods.
//
// Hold one Session per signed-in user and share it freely:
//   - It is safe for concurrent use by multiple goroutines.
//   - Every read reflects the latest state.
//   - A copy of a Session is the same session.
//   - It holds no resource that needs releasing and starts no long-lived
//     goroutine, so one that is no longer wanted is simply dropped.
//
// The access token is renewed on demand by [Session.AccessToken] and
// [Session.Refresh], and ahead of expiry by [Session.AutoRefresh]. A
// listener added with [Session.AddEventListener] receives each change as a
// [SessionEvent].
//
// At most one refresh is in flight at a time, and concurrent callers share
// its result. A caller's context bounds only its own wait, so a caller that
// gives up fails no other. What follows a failed refresh depends on the
// answer:
//   - A 400 whose code is refresh_token_not_found, refresh_token_already_used,
//     session_not_found, session_expired, user_banned or validation_failed
//     rejects the refresh token for good. No later refresh of the session
//     sends it again, and the session ends once thirty seconds or less of its
//     access token remain.
//   - No answer, or a 5xx, 408, 409 or 429 answer, is retried with the same
//     token on the schedule below.
//   - Any other failure leaves the session as it was, with no retry.
//
// The retry schedule:
//   - The first retry waits 200 milliseconds and each later one twice as
//     long, to at most thirty seconds.
//   - Every wait varies by up to a quarter either way, and is lengthened to
//     a Retry-After header's wait when that is longer.
//   - The retry after the first wait of thirty seconds is the last.
//   - The caller's context ending ends the retries with its error.
//
// A session also ends when [Session.SignOut] ends it. An ended session holds
// no credentials:
//   - Every operation returns [ErrSignedOut], or [ErrSessionExpired] when the
//     session ended by expiring.
//   - [Session.RefreshToken] returns "" and [Session.User] returns nil.
//
// The zero Session answers as a signed-out one.
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
	// except after a refresh started by [Session.AccessToken],
	// [Session.AutoRefresh], [Session.SignOut] or [Client.SessionFromStore],
	// when the failure reaches no caller.
	Save(ctx context.Context, state []byte) error

	// Delete clears the state the store holds. A failed Delete is returned by
	// [Session.SignOut], and reaches no caller when the session ends by
	// expiring.
	Delete(ctx context.Context) error
}

// AccessToken returns the current access token, refreshing the session first
// when thirty seconds or less of the token remain, as [Session] describes. A
// refresh the server rejects ends the session, and [ErrSessionExpired] is
// returned wrapping the [*Error]. Any other failed refresh returns the token
// while it is unexpired, and the failure once it has expired: a transport
// error or an [*Error]. A context that ends during the refresh returns its
// error. The method value session.AccessToken is a
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
// rotating both tokens and writing the renewed state to the session's store,
// and retrying as [Session] describes. A refresh the server rejects is
// returned as the [*Error] while more than thirty seconds of the access
// token remain, the session keeping its tokens, and as [ErrSessionExpired]
// wrapping the [*Error] once they do not, the session having ended. A later
// Refresh after a rejection sends nothing and answers the same way. Any
// other failed refresh is returned as a transport error, an [*Error] or the
// context's error when the retries were cut short. A store that cannot be
// written is reported as the wrapped error, after the session already holds
// the new tokens.
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
// ends only once revocation succeeded, any other answer leaving the session
// live and returning the error. An answer of session_not_found is such an
// error: this session was already revoked and no other was, so the call
// ended nothing, and [signout.WithLocalScope] then ends the session. With
// [signout.WithLocalScope] the session ends and its store is cleared first,
// and the server then revokes this session alone, its answer reported after
// the session has ended. With [signout.WithOthersScope] the server revokes
// every other session and this one stays live. An answer of user_not_found
// counts as success under every scope, and session_not_found under
// [signout.WithLocalScope] alone.
//
// An access token with thirty seconds or less left is refreshed first. Under
// [signout.WithLocalScope] the refresh is for the revocation alone, one
// attempt whose result is kept nowhere: an answer of refresh_token_not_found,
// session_not_found or session_expired counts as success, the session being
// gone already, and any other failure sends the token as it is while
// unexpired. Under the other scopes the refresh is the session's own, as
// [Session] describes, retries included, and one the server rejects ends the
// session without a request to revoke anything: as signed out for a bare
// call, which returns the server's rejection, and as expired under
// [signout.WithOthersScope], which returns [ErrSessionExpired] wrapping it.
// Any other failed refresh sends the token as it is while unexpired, and is
// otherwise returned with nothing sent.
//
// A store that cannot be cleared is reported as the wrapped error, alongside
// a failed revocation under [signout.WithLocalScope] or the rejection that
// ended the session under a bare call. On a session that has already ended,
// a bare call or [signout.WithLocalScope] returns success without a request,
// and [signout.WithOthersScope] returns [ErrSignedOut] or
// [ErrSessionExpired].
func (s *Session) SignOut(ctx context.Context, options ...signout.Option) (*SignOutResult, error) {
	scope := signout.Resolve(options...).Scope()
	if s.inner == nil {
		if scope == scopes.Others {
			return nil, ErrSignedOut
		}
		return &SignOutResult{}, nil
	}
	switch scope {
	case scopes.Local:
		return s.signOutLocally(ctx)
	case scopes.Others:
		return s.revokeOthers(ctx)
	}
	return s.signOutGlobally(ctx)
}

// signOutGlobally asks the server to revoke every session the user holds and
// ends this one once it has. A session that had already ended counts as
// signed out, and a refresh the server rejects ends it as signed out and
// reports the rejection, with a clear that failed joined.
func (s *Session) signOutGlobally(ctx context.Context) (*SignOutResult, error) {
	bearer, clearErr, err := s.inner.SignOutToken(ctx)
	if err != nil {
		if _, ok := errors.AsType[*session.EndedError](err); ok {
			return &SignOutResult{}, nil
		}
		if clearErr != nil {
			err = errors.Join(err, storeClearError(clearErr))
		}
		return nil, err
	}
	err = logout(ctx, s.client.httpClient, s.client.baseURL, bearer, scopes.Global)
	if err != nil && !nothingLeftToRevoke(err, scopes.Global) {
		return nil, err
	}
	if _, err := s.inner.End(ctx); err != nil {
		if _, ok := errors.AsType[*session.EndedError](err); ok {
			return &SignOutResult{}, nil
		}
		return nil, storeClearError(err)
	}
	return &SignOutResult{}, nil
}

// revokeOthers asks the server to revoke every session the user holds but
// this one, which the request leaves as it is. The access token is obtained
// as for any request, so a refresh the server rejects ends the session as
// expired before anything is sent.
func (s *Session) revokeOthers(ctx context.Context) (*SignOutResult, error) {
	bearer, err := s.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	err = logout(ctx, s.client.httpClient, s.client.baseURL, bearer, scopes.Others)
	if err != nil && !nothingLeftToRevoke(err, scopes.Others) {
		return nil, err
	}
	return &SignOutResult{}, nil
}

// signOutLocally ends the session before the server is called, then revokes
// the ended session. A failed revocation and a failed clear are reported
// together, and a session that had already ended counts as signed out.
func (s *Session) signOutLocally(ctx context.Context) (*SignOutResult, error) {
	held, clearErr := s.inner.End(ctx)
	if _, ok := errors.AsType[*session.EndedError](clearErr); ok {
		return &SignOutResult{}, nil
	}
	revokeErr := s.revokeHeld(ctx, held)
	if clearErr != nil {
		clearErr = storeClearError(clearErr)
	}
	if err := errors.Join(revokeErr, clearErr); err != nil {
		return nil, err
	}
	return &SignOutResult{}, nil
}

// revokeHeld revokes the session that held describes on the server, under
// the local scope. Its access token is sent while outside the expiry margin,
// and is otherwise refreshed once for this request alone, the result kept
// nowhere. When that refresh fails, an answer of refresh_token_not_found,
// session_not_found or session_expired reports success with nothing sent,
// the session being gone already, any other failure sends the held token
// while it is unexpired, and is otherwise reported with nothing sent.
func (s *Session) revokeHeld(ctx context.Context, held session.State) error {
	bearer := held.AccessToken()
	if held.ExpiresAt().Sub(s.client.now()) <= expiryMargin {
		renewed, err := s.client.refreshGrant(ctx, held.RefreshToken())
		switch {
		case err == nil:
			bearer = renewed.AccessToken()
		case sessionGone(err):
			return nil
		case !s.client.now().Before(held.ExpiresAt()):
			return err
		}
	}
	err := logout(ctx, s.client.httpClient, s.client.baseURL, bearer, scopes.Local)
	if nothingLeftToRevoke(err, scopes.Local) {
		return nil
	}
	return err
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
		if _, ok := errors.AsType[*session.EndedError](err); ok {
			return sessionError(err)
		}
		return fmt.Errorf("auth: writing session store: %w", err)
	}
	return nil
}

// SessionFromTokens adopts a session from an access token and refresh token
// obtained elsewhere, such as a session another process signed in. An
// unexpired access token is verified by the server, which also supplies the
// user's profile. An expired one is refreshed, retrying as [Session]
// describes, rotating both tokens. An empty accessToken returns
// [ErrMissingJWT], one that is not a JWT returns [ErrMalformedJWT], an empty
// refreshToken returns [ErrMissingRefreshToken] and a rejected token surfaces
// as [*Error].
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
		user, userErr := send(ctx, c.httpClient, http.MethodGet, c.baseURL.JoinPath("user"), nil, accessToken)
		if userErr != nil {
			return nil, userErr
		}
		state, err = session.NewState(accessToken, refreshToken, expiresAt, user)
		if err != nil {
			return nil, fmt.Errorf("auth: decoding user: %w", err)
		}
	} else {
		state, err = c.refreshWithRetries(ctx, refreshToken)
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
// first, retrying as [Session] describes. It returns [ErrNoStoredSession]
// when the store holds nothing and [ErrMissingStore] for a nil store. When
// the server rejects that refresh, the store is cleared and
// [ErrSessionExpired] is returned wrapping the rejection, and any other
// refresh failure, a transport error, an [*Error] or the context's error
// when the retries were cut short, is returned with the store left as it
// was. A document the store holds that Persist did not write is reported as
// the wrapped decode error.
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
		Refresh:      c.refreshWithRetries,
		Definitive:   rejectedRefresh,
		ExpiryMargin: expiryMargin,
		Now:          c.now,
		Store:        store,
	})}
}

// refreshWithRetries exchanges refreshToken for a renewed session state,
// repeating the refresh grant through the schedule [Session] describes while
// the server fails to judge the token, and ending the sequence with ctx's
// error when ctx ends during a wait. It is the refresh a [Session] renews
// through and the one [Client.SessionFromTokens] runs on an expired access
// token.
func (c *Client) refreshWithRetries(ctx context.Context, refreshToken string) (session.State, error) {
	reached := false
	for attempt := 0; ; attempt++ {
		state, err := c.refreshGrant(ctx, refreshToken)
		if err == nil {
			return state, nil
		}
		retryAfter, retryable := retryableRefresh(err)
		if !retryable || reached {
			return session.State{}, err
		}
		var wait time.Duration
		wait, reached = backoff.Wait(attempt, retryAfter, rand.Float64())
		if err := backoff.Sleep(ctx, wait); err != nil {
			return session.State{}, err
		}
	}
}

// refreshGrant exchanges refreshToken for a renewed session state through one
// request to the token endpoint's refresh_token grant.
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

// rejectedRefresh reports whether a refresh failure is the server's rejection
// of the refresh token, which it repeats for the same token: a 400 answer
// whose code is one of the six it refuses a token with.
func rejectedRefresh(err error) bool {
	var serverError *Error
	if !errors.As(err, &serverError) || serverError.HTTPStatus != http.StatusBadRequest {
		return false
	}
	switch serverError.Code {
	case "refresh_token_not_found", "refresh_token_already_used", "session_not_found", "session_expired", "user_banned", "validation_failed":
		return true
	}
	return false
}

// retryableRefresh reports whether a refresh failure is one the server did
// not judge the refresh token on, which a repeat may cure: a request that got
// no answer, or a 5xx, 408, 409 or 429 answer. With it comes the wait the
// answer's Retry-After header asked for, zero when it stated none.
func retryableRefresh(err error) (retryAfter time.Duration, ok bool) {
	if _, noAnswer := errors.AsType[*transportError](err); noAnswer {
		return 0, true
	}
	var serverError *Error
	if !errors.As(err, &serverError) {
		return 0, false
	}
	switch status := serverError.HTTPStatus; {
	case status >= 500, status == http.StatusRequestTimeout, status == http.StatusConflict, status == http.StatusTooManyRequests:
		return serverError.retryAfter, true
	}
	return 0, false
}

// nothingLeftToRevoke reports whether a revocation's failure says the server
// holds nothing left to revoke for scope: the user no longer exists, or, for
// the local scope, this session no longer exists. Under the other scopes a
// missing session says nothing of the sessions the call was to revoke.
func nothingLeftToRevoke(err error, scope string) bool {
	var serverError *Error
	if !errors.As(err, &serverError) {
		return false
	}
	switch serverError.Code {
	case "user_not_found":
		return true
	case "session_not_found":
		return scope == scopes.Local
	}
	return false
}

// sessionGone reports whether a refresh failure says the session can never
// be refreshed again: its refresh token or the session itself was not found,
// or the session expired, timed out or was replaced by a newer sign-in.
func sessionGone(err error) bool {
	var serverError *Error
	if !errors.As(err, &serverError) {
		return false
	}
	switch serverError.Code {
	case "refresh_token_not_found", "session_not_found", "session_expired":
		return true
	}
	return false
}

// sessionError translates the session machinery's errors into this package's:
// an ended session is [ErrSignedOut] or [ErrSessionExpired] wrapping the
// server's rejection, a store write that failed after its state was
// committed is wrapped and every other error is returned as it is.
func sessionError(err error) error {
	if ended, ok := errors.AsType[*session.EndedError](err); ok {
		if ended.Status == session.Expired {
			return fmt.Errorf("%w: %w", ErrSessionExpired, ended.Cause)
		}
		return ErrSignedOut
	}
	if write, ok := errors.AsType[*session.WriteError](err); ok {
		return fmt.Errorf("auth: writing session store: %w", write.Err)
	}
	return err
}

// storeClearError wraps the error of a store that could not be cleared when
// the session ended.
func storeClearError(err error) error {
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
