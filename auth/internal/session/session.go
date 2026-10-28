// Package session holds one signed-in user's session state, the tokens, the
// expiry and the user document of a [State], and keeps it consistent under
// concurrent use: one refresh in flight at a time whose result every waiting
// caller shares unless the caller that started it gave up, a generation that
// fences a refresh overtaken by the session's end and store writes that reach
// the caller's store one at a time, in commit order. The network crosses the
// package boundary as the [RefreshFunc] the caller supplies and the verdict on
// its failures as the caller's [Options.Definitive], so no request is built
// and no error shaped here.
package session

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Store receives a session's state. A Session calls a Store from one
// goroutine at a time.
type Store interface {
	// Save replaces the state the store holds with state.
	Save(ctx context.Context, state []byte) error

	// Delete clears the state the store holds.
	Delete(ctx context.Context) error
}

// RefreshFunc exchanges refreshToken for a renewed State.
type RefreshFunc func(ctx context.Context, refreshToken string) (State, error)

// Options configures a [Session]. Refresh, Definitive and Now are required,
// ExpiryMargin is the time left on an access token below which [Session.Token]
// refreshes first, and Store, when set, is the store the session persists to
// from the start without a first write.
type Options struct {
	Refresh      RefreshFunc
	Definitive   func(error) bool
	ExpiryMargin time.Duration
	Now          func() time.Time
	Store        Store
}

// Status is whether a [Session] is live and, once it is not, why it ended.
type Status int

const (
	// Live is a session whose tokens are in use.
	Live Status = iota
	// SignedOut is a session [Session.End] ended.
	SignedOut
	// Expired is a session that ended because its access token expired after
	// the server had refused to renew it.
	Expired
)

// EndedError is the error every operation on an ended [Session] reports.
// Status is why the session ended and Cause, for [Expired], the refresh
// failure the server answered with.
type EndedError struct {
	Status Status
	Cause  error
}

func (e *EndedError) Error() string {
	if e.Status == Expired {
		return "session has expired: " + e.Cause.Error()
	}
	return "session is signed out"
}

// Unwrap returns the refresh failure that expired the session, nil otherwise.
func (e *EndedError) Unwrap() error { return e.Cause }

// WriteError reports a store write that failed after the state it carried had
// already been committed to the session.
type WriteError struct {
	Err error
}

func (e *WriteError) Error() string { return "writing session store: " + e.Err.Error() }

// Unwrap returns the store's own error.
func (e *WriteError) Unwrap() error { return e.Err }

// Session is one signed-in user's session. It is safe for concurrent use by
// multiple goroutines. Construct with [New].
type Session struct {
	refresh      RefreshFunc
	definitive   func(error) bool
	expiryMargin time.Duration
	now          func() time.Time

	// mutex guards the state below and is never held across a refresh or a
	// store call.
	mutex      sync.Mutex
	state      State
	status     Status
	cause      error
	rejected   bool
	rejection  error
	generation uint64
	inflight   *flight
	sequence   uint64

	// storeMutex is held across every store call, so writes reach the store
	// one at a time, and across [Session.Persist]'s first write to a new store.
	storeMutex sync.Mutex
	store      Store
	written    uint64
}

// flight is one in-flight refresh: done is closed once it has finished, and
// err and abandoned, read only after that, are its failure and whether its
// starter gave up on it, in which case its waiters start a refresh of their
// own.
type flight struct {
	done      chan struct{}
	err       error
	abandoned bool
}

// write is one store operation captured under the state lock: the document
// to save, or a clear when document is nil, and its place in commit order.
type write struct {
	sequence uint64
	document []byte
}

// New constructs a live Session over state.
func New(state State, options Options) *Session {
	return &Session{
		refresh:      options.Refresh,
		definitive:   options.Definitive,
		expiryMargin: options.ExpiryMargin,
		now:          options.Now,
		state:        state,
		store:        options.Store,
	}
}

// State returns the current state, the zero State once the session has ended.
func (s *Session) State() State {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.state
}

// Token returns the access token when more than the expiry margin remains
// before it expires, and otherwise refreshes first, joining a refresh already
// in flight. A refresh the server rejects definitively marks the session
// rejected: the access token is returned while it is unexpired and the session
// ends as [Expired] once it is not. A refresh that fails transiently leaves the
// session as it was, and the access token is returned if it is unexpired and
// the failure otherwise. The store write a refresh or an ending causes is
// performed before Token returns, and its failure is returned to no caller.
// Token returns an [*EndedError] once the session has ended and ctx's error
// when ctx ends while waiting on a refresh.
func (s *Session) Token(ctx context.Context) (string, error) {
	s.mutex.Lock()
	if s.status != Live {
		err := s.endedErrorLocked()
		s.mutex.Unlock()
		return "", err
	}
	now := s.now()
	if s.state.expiresAt.Sub(now) > s.expiryMargin {
		token := s.state.accessToken
		s.mutex.Unlock()
		return token, nil
	}
	if s.rejected {
		if now.Before(s.state.expiresAt) {
			token := s.state.accessToken
			s.mutex.Unlock()
			return token, nil
		}
		clearing := s.endLocked(Expired, s.rejection)
		err := s.endedErrorLocked()
		s.mutex.Unlock()
		_ = s.perform(ctx, clearing)
		return "", err
	}
	s.mutex.Unlock()

	_, err := s.renew(ctx)
	var ended *EndedError
	if errors.As(err, &ended) {
		return "", err
	}
	if err != nil && ctx.Err() != nil {
		return "", err
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.status != Live {
		return "", s.endedErrorLocked()
	}
	if err != nil && !s.now().Before(s.state.expiresAt) {
		return "", err
	}
	return s.state.accessToken, nil
}

// Refresh renews the session now, joining a refresh already in flight. It
// returns the refresh failure as the [RefreshFunc] reported it, a
// [*WriteError] when the renewed state was committed but its store write
// failed and an [*EndedError] once the session has ended, including when this
// call's definitive rejection ended it. The failure of the clear an ending
// causes is returned to no caller.
func (s *Session) Refresh(ctx context.Context) error {
	writeErr, err := s.renew(ctx)
	switch {
	case err != nil:
		return err
	case writeErr != nil:
		return &WriteError{Err: writeErr}
	}
	return nil
}

// renew joins the in-flight refresh or starts one with the current refresh
// token, waits for it and reports its outcome: err is nil when renewed state
// was committed, the refresh failure otherwise and an [*EndedError] when the
// session has ended, and writeErr is the failure of a store write this call
// performed. A refresh whose starter's context ended before it succeeded is
// abandoned rather than shared, and its waiters start one of their own. The
// caller that starts a refresh commits its result only if the session's
// generation is unchanged when it returns, and it alone writes the store,
// after releasing the waiters.
func (s *Session) renew(ctx context.Context) (writeErr, err error) {
	s.mutex.Lock()
	for s.status == Live && s.inflight != nil {
		joined := s.inflight
		s.mutex.Unlock()
		select {
		case <-joined.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if !joined.abandoned {
			return nil, joined.err
		}
		s.mutex.Lock()
	}
	if s.status != Live {
		endedErr := s.endedErrorLocked()
		s.mutex.Unlock()
		return nil, endedErr
	}
	started := &flight{done: make(chan struct{})}
	s.inflight = started
	refreshToken := s.state.refreshToken
	generation := s.generation
	s.mutex.Unlock()

	renewed, refreshErr := s.refreshAbandoningOnPanic(ctx, started, refreshToken)

	s.mutex.Lock()
	s.inflight = nil
	var pending write
	var hasPending bool
	switch {
	case s.generation != generation:
		started.err = s.endedErrorLocked()
	case refreshErr == nil:
		s.state = renewed
		s.rejected = false
		s.rejection = nil
		pending, hasPending = s.capture(renewed.encode()), true
	case s.definitive(refreshErr):
		s.rejected = true
		s.rejection = refreshErr
		started.err = refreshErr
		if !s.now().Before(s.state.expiresAt) {
			pending, hasPending = s.endLocked(Expired, refreshErr), true
			started.err = s.endedErrorLocked()
		}
	case ctx.Err() != nil:
		started.abandoned = true
		started.err = refreshErr
	default:
		started.err = refreshErr
	}
	err = started.err
	s.mutex.Unlock()
	close(started.done)

	if hasPending {
		writeErr = s.perform(ctx, pending)
	}
	return writeErr, err
}

// refreshAbandoningOnPanic calls the refresh function for started and, should
// it panic, abandons started, releasing its waiters to start their own
// refresh, before the panic carries on up the starter's stack.
func (s *Session) refreshAbandoningOnPanic(ctx context.Context, started *flight, refreshToken string) (State, error) {
	returned := false
	defer func() {
		if returned {
			return
		}
		s.mutex.Lock()
		s.inflight = nil
		started.abandoned = true
		s.mutex.Unlock()
		close(started.done)
	}()
	renewed, err := s.refresh(ctx, refreshToken)
	returned = true
	return renewed, err
}

// Persist writes the current state to store and makes store the one every
// later write reaches. No other write reaches any store while the first write
// runs. A failed first write leaves the session persisting as before and
// returns the store's error verbatim, and an ended session returns an
// [*EndedError].
func (s *Session) Persist(ctx context.Context, store Store) error {
	s.storeMutex.Lock()
	defer s.storeMutex.Unlock()

	s.mutex.Lock()
	if s.status != Live {
		err := s.endedErrorLocked()
		s.mutex.Unlock()
		return err
	}
	first := s.capture(s.state.encode())
	s.mutex.Unlock()

	if err := store.Save(ctx, first.document); err != nil {
		return err
	}
	s.store = store
	s.written = first.sequence
	return nil
}

// End ends the session as [SignedOut], discarding any refresh in flight, and
// clears its store. It returns the state the session held and the store's
// error verbatim when the clear fails, and an [*EndedError] when the session
// had already ended.
func (s *Session) End(ctx context.Context) (State, error) {
	s.mutex.Lock()
	if s.status != Live {
		err := s.endedErrorLocked()
		s.mutex.Unlock()
		return State{}, err
	}
	held := s.state
	clearing := s.endLocked(SignedOut, nil)
	s.mutex.Unlock()
	return held, s.perform(ctx, clearing)
}

// endLocked ends the session with status and cause, bumps the generation,
// drops the state from memory and returns the captured clear of the store. It
// is called with mutex held.
func (s *Session) endLocked(status Status, cause error) write {
	s.status = status
	s.cause = cause
	s.state = State{}
	s.generation++
	return s.capture(nil)
}

// endedErrorLocked builds the [*EndedError] for the session's status, nil
// while it is live. It is called with mutex held.
func (s *Session) endedErrorLocked() error {
	if s.status == Live {
		return nil
	}
	return &EndedError{Status: s.status, Cause: s.cause}
}

// capture allocates the next place in commit order to a store write of
// document, a clear when document is nil. It is called with mutex held.
func (s *Session) capture(document []byte) write {
	s.sequence++
	return write{sequence: s.sequence, document: document}
}

// perform carries a captured write to the store, holding storeMutex across
// the call, and skips it when a later write has already reached the store. It
// returns the store's error verbatim and is called with mutex released.
func (s *Session) perform(ctx context.Context, pending write) error {
	s.storeMutex.Lock()
	defer s.storeMutex.Unlock()
	if s.store == nil || pending.sequence <= s.written {
		return nil
	}
	s.written = pending.sequence
	if pending.document == nil {
		return s.store.Delete(ctx)
	}
	return s.store.Save(ctx, pending.document)
}
