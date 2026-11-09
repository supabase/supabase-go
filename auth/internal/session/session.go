// Package session holds one signed-in user's session state, the tokens, the
// expiry and the user document of a [State], and keeps it consistent under
// concurrent use: one refresh in flight at a time whose result every waiting
// caller shares unless the caller that started it gave up, a generation that
// fences a refresh overtaken by the session's end and store writes that reach
// the caller's store one at a time, in commit order. Listeners are told of
// each change in that order, once the store holds it. The network crosses
// the package boundary as the [RefreshFunc] the caller supplies and the
// verdict on its failures as the caller's [Options.Definitive], so no
// request is built and no error shaped here.
package session

import (
	"context"
	"errors"
	"math"
	"slices"
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

// RefreshFunc exchanges refreshToken for a renewed State. A [Session] calls
// it once per refresh in flight, from the goroutine of the caller that
// started the flight and with that caller's context, and shares its result
// with every caller waiting on the flight, so a RefreshFunc that retries
// shares its whole sequence.
type RefreshFunc func(ctx context.Context, refreshToken string) (State, error)

// Options configures a [Session]. Refresh, Definitive and Now are required,
// ExpiryMargin is the time left on an access token at or below which
// [Session.Token] refreshes first and a rejected session ends, and Store,
// when set, is the store the session persists to from the start without a
// first write.
type Options struct {
	Refresh      RefreshFunc
	Definitive   func(error) bool
	ExpiryMargin time.Duration
	Now          func() time.Time
	Store        Store
}

// Status is whether a [Session] is live and, once it is not, why it ended.
type Status string

const (
	// Live is a session whose tokens are in use.
	Live Status = "Live"

	// SignedOut is a session [Session.End] ended.
	SignedOut Status = "SignedOut"

	// Expired is a session that ended because its access token expired after
	// the server had refused to renew it.
	Expired Status = "Expired"
)

// Change is one change to a [Session] that its listeners are told of.
// Status is the status the session ended with for [Ended], the session's
// status as it stood when the listener was added for [Added] and [Live] for
// [Refreshed].
type Change struct {
	Kind   ChangeKind
	Status Status

	// sequence is the place in commit order of the store write the change
	// caused, which the store holds before a listener is told of it.
	sequence uint64
}

// terminal reports whether the change is the last a listener is told of: the
// session ended, or stood ended when the listener was added.
func (c Change) terminal() bool {
	return c.Status != Live
}

// ChangeKind is which change a [Change] reports.
type ChangeKind string

const (
	// Added is the first change every listener is told of: the session as it
	// stands when the listener is added.
	Added ChangeKind = "Added"

	// Refreshed is a refresh that committed renewed state.
	Refreshed ChangeKind = "Refreshed"

	// Ended is the session's end.
	Ended ChangeKind = "Ended"
)

// EndedError is the error every operation on an ended [Session] reports.
// Status is why the session ended and Cause the refresh failure that ended
// it, when one did: always for [Expired], and for [SignedOut] when the
// refresh a sign-out waited on was rejected.
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
	// store call. It is taken only by the locked method, so a panic never
	// leaves it held. completed is the place in commit order of the latest
	// write whose store call has returned, and storeDone is signaled, under
	// mutex, each time one returns.
	mutex         sync.Mutex
	state         State
	status        Status
	cause         error
	rejected      bool
	rejection     error
	generation    uint64
	inflight      *flight
	sequence      uint64
	completed     uint64
	storeDone     sync.Cond
	registrations []*registration

	// done is closed when the session ends.
	done chan struct{}

	// storeMutex is held across every store call, so writes reach the store
	// one at a time, and across [Session.Persist]'s first write to a new store.
	// Each holder releases it by defer, so a store that panics never leaves it
	// held. store is written with both storeMutex and mutex held, so either
	// serves to read it.
	storeMutex sync.Mutex
	store      Store
	written    uint64
}

// registration is one added listener: the changes queued for it, in commit
// order, whether a goroutine is delivering them and whether it has been
// removed. All of it is read and written with the session's mutex held.
type registration struct {
	listener func(Change)
	queue    []Change
	running  bool
	removed  bool
}

// flight is one in-flight refresh: done is closed once it has finished, and
// err and abandoned, read only after that, are its failure and whether it was
// abandoned, by its starter giving up or by a panic, in which case its waiters
// start a refresh of their own. ending is the status a definitive rejection
// ends the session with, [SignedOut] once any caller waiting on the refresh
// asked for it, and is read and written with the session's mutex held.
type flight struct {
	done      chan struct{}
	err       error
	abandoned bool
	ending    Status
}

// write is one store operation captured under the state lock: the document
// to save, or a clear when document is nil, and its place in commit order.
type write struct {
	sequence uint64
	document []byte
}

// New constructs a live Session over state.
func New(state State, options Options) *Session {
	session := &Session{
		refresh:      options.Refresh,
		definitive:   options.Definitive,
		expiryMargin: options.ExpiryMargin,
		now:          options.Now,
		state:        state,
		status:       Live,
		done:         make(chan struct{}),
		store:        options.Store,
	}
	session.storeDone.L = &session.mutex
	return session
}

// locked calls f with mutex held and releases it however f ends, a panic
// included.
func (s *Session) locked(f func()) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	f()
}

// State returns the current state, the zero State once the session has ended.
func (s *Session) State() State {
	var state State
	s.locked(func() { state = s.state })
	return state
}

// Expiry returns the instant the access token expires and whether the server
// has rejected the refresh token, the zero time once the session has ended.
func (s *Session) Expiry() (expiresAt time.Time, rejected bool) {
	s.locked(func() { expiresAt, rejected = s.state.expiresAt, s.rejected })
	return expiresAt, rejected
}

// Done returns a channel that is closed once the session has ended.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Ended returns the [*EndedError] once the session has ended, nil while it is
// live.
func (s *Session) Ended() error {
	var err error
	s.locked(func() { err = s.endedErrorLocked() })
	return err
}

// Token returns the access token when more than the expiry margin remains
// before it expires, and otherwise refreshes first, joining a refresh already
// in flight. A refresh the server rejects definitively ends the session as
// [Expired], as does a rejection remembered from an earlier refresh, without
// a request. A refresh that fails otherwise leaves the session as it was, and
// the access token is returned if it is unexpired and the failure otherwise.
// The store write a refresh or an ending causes is performed before Token
// returns. clearErr is the failure of the clear an ending caused, when this
// call performed it. A refresh that committed renewed tokens the store could
// not save returns a [*WriteError], the session holding the renewed tokens.
// Token returns an [*EndedError] once the session has ended and ctx's error
// when ctx ends while waiting on a refresh.
func (s *Session) Token(ctx context.Context) (token string, clearErr, err error) {
	token, clearErr, saveErr, err := s.tokenEnding(ctx, Expired)
	if err == nil && saveErr != nil {
		err = &WriteError{Err: saveErr}
	}
	return token, clearErr, s.endedOr(err)
}

// SignOutToken returns the access token for the request that signs the
// session out everywhere, as [Session.Token] does, except in what a
// definitive rejection does: the session ends as [SignedOut] rather than
// [Expired], and the rejection is returned as it is rather than as the
// [*EndedError] it caused. clearErr is the failure of the clear that ending
// caused, when this call performed it. saveErr, beside a token, is the
// failure of the store write a committed refresh caused, which the caller
// reports only if the session outlives the sign-out. A session that had
// already ended returns its [*EndedError].
func (s *Session) SignOutToken(ctx context.Context) (token string, clearErr, saveErr, err error) {
	return s.tokenEnding(ctx, SignedOut)
}

// tokenEnding is [Session.Token] with ending the status a definitive
// rejection ends the session with, that rejection returned as it is, and
// clearErr the failure of the clear this call performed for that ending.
// saveErr is the failure of the store write a committed refresh caused.
func (s *Session) tokenEnding(ctx context.Context, ending Status) (token string, clearErr, saveErr, err error) {
	var clearing write
	var endingNow, refresh bool
	s.locked(func() {
		switch {
		case s.status != Live:
			err = s.endedErrorLocked()
		case s.state.expiresAt.Sub(s.now()) > s.expiryMargin:
			token = s.state.accessToken
		case !s.rejected:
			refresh = true
		default:
			clearing, endingNow = s.endLocked(ending, s.rejection), true
			err = s.rejection
		}
	})
	if endingNow {
		clearErr = s.perform(ctx, clearing)
	}
	if !refresh {
		return token, clearErr, nil, err
	}

	writeErr, err := s.renew(ctx, ending, s.expiryMargin)
	if _, ok := errors.AsType[*EndedError](err); ok {
		return "", nil, nil, err
	}
	if err != nil && ctx.Err() != nil {
		return "", writeErr, nil, err
	}

	s.locked(func() {
		switch {
		case s.status != Live && (err == nil || !s.definitive(err)):
			// Committed and then ended, or failed transiently on a session
			// that has ended since.
			err = s.endedErrorLocked()
		case s.status != Live:
			// Rejected, and the session has ended: the rejection stands,
			// with the failure of the clear renew performed when the
			// rejection ended it.
			clearErr = writeErr
		case err == nil:
			// Committed: the renewed token, and the failure of its store
			// write for the caller that surfaces it.
			token, saveErr = s.state.accessToken, writeErr
		case s.now().Before(s.state.expiresAt):
			// Failed transiently, the access token still usable.
			token, err = s.state.accessToken, nil
		}
	})
	return token, clearErr, saveErr, err
}

// endedOr returns the [*EndedError] when the session has ended, and err
// otherwise, a nil err included.
func (s *Session) endedOr(err error) error {
	if err == nil {
		return nil
	}
	s.locked(func() {
		if s.status != Live {
			err = s.endedErrorLocked()
		}
	})
	return err
}

// Refresh renews the session now, joining a refresh already in flight. It
// returns the refresh failure as the [RefreshFunc] reported it, a
// [*WriteError] when the renewed state was committed but its store write
// failed and an [*EndedError] once the session has ended, including when this
// call's definitive rejection ended it. A definitive rejection is remembered:
// the session keeps its tokens while more than the expiry margin remains on
// the access token and ends as [Expired] once it does not, and a later
// Refresh returns the remembered rejection without a request. clearErr is the
// failure of the clear an ending caused, when this call performed it.
func (s *Session) Refresh(ctx context.Context) (clearErr, err error) {
	return s.RefreshWithin(ctx, always)
}

// always is the time left on an access token that no token has, so a refresh
// within it is a refresh whatever the token has left.
const always = time.Duration(math.MaxInt64)

// RefreshWithin renews the session as [Session.Refresh] does when within or
// less remains before the access token expires, or when a definitive
// rejection is remembered, and otherwise returns no error without a request.
func (s *Session) RefreshWithin(ctx context.Context, within time.Duration) (clearErr, err error) {
	writeErr, err := s.renew(ctx, Expired, within)
	switch {
	case err != nil:
		// A write beside a failure is the clear of the ending this call caused.
		return writeErr, s.endedOr(err)
	case writeErr != nil:
		return nil, &WriteError{Err: writeErr}
	}
	return nil, nil
}

// renew joins the in-flight refresh or starts one with the current refresh
// token, waits for it and reports its outcome: err is nil when renewed state
// was committed, the refresh failure otherwise and an [*EndedError] when the
// session had ended before the refresh ran or its result was discarded, and
// writeErr is the failure of a store write this call performed. While more
// than within remains before the access token expires and no rejection is
// remembered, nothing is sent and both are nil. A refresh whose starter's
// context ended before it succeeded is abandoned rather than shared, and its
// waiters start one of their own, as they do when a panic interrupts the
// refresh or the commit of its result before the panic carries on up the
// starter's stack. The caller that starts a refresh commits its result only
// if the session's generation is unchanged when it returns, and it alone
// writes the store, after releasing the waiters. A definitive rejection is
// remembered and returned as it is, and a remembered rejection is returned
// without a refresh. Either ends the session once the access token is inside
// the expiry margin, with status ending or [SignedOut] once any caller
// waiting on the refresh asked for it and the rejection as cause.
func (s *Session) renew(ctx context.Context, ending Status, within time.Duration) (writeErr, err error) {
	var started *flight
	var refreshToken string
	var generation uint64
	var pending write
	var hasPending, fresh bool
	for started == nil {
		var joined *flight
		s.locked(func() {
			switch {
			case s.status != Live:
				err = s.endedErrorLocked()
			case s.rejected:
				err = s.rejection
				pending, hasPending = s.rejectLocked(s.rejection, ending)
			case s.state.expiresAt.Sub(s.now()) > within:
				fresh = true
			case s.inflight != nil:
				joined = s.inflight
				if ending == SignedOut {
					joined.ending = SignedOut
				}
			default:
				started = &flight{done: make(chan struct{}), ending: ending}
				s.inflight = started
				refreshToken = s.state.refreshToken
				generation = s.generation
			}
		})
		if err != nil {
			if hasPending {
				writeErr = s.perform(ctx, pending)
			}
			return writeErr, err
		}
		if fresh {
			return nil, nil
		}
		if joined != nil {
			select {
			case <-joined.done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if !joined.abandoned {
				return nil, joined.err
			}
		}
	}

	settled := false
	defer func() {
		if settled {
			return
		}
		s.locked(func() {
			s.inflight = nil
			started.abandoned = true
		})
		close(started.done)
	}()

	renewed, refreshErr := s.refresh(ctx, refreshToken)

	s.locked(func() {
		s.inflight = nil
		switch {
		case s.generation != generation:
			started.err = s.endedErrorLocked()
		case refreshErr == nil:
			s.state = renewed
			s.rejected = false
			s.rejection = nil
			pending, hasPending = s.capture(renewed.encode()), true
			s.notifyLocked(Change{Kind: Refreshed, Status: Live, sequence: pending.sequence})
		case s.definitive(refreshErr):
			started.err = refreshErr
			pending, hasPending = s.rejectLocked(refreshErr, started.ending)
		case ctx.Err() != nil:
			started.abandoned = true
			started.err = refreshErr
		default:
			started.err = refreshErr
		}
		err = started.err
	})
	settled = true
	close(started.done)

	if hasPending {
		writeErr = s.perform(ctx, pending)
	}
	return writeErr, err
}

// Persist writes the current state to store and makes store the one every
// later write reaches. No other write reaches any store while the first write
// runs. A failed first write leaves the session persisting as before and
// returns the store's error verbatim, and an ended session returns an
// [*EndedError].
func (s *Session) Persist(ctx context.Context, store Store) error {
	s.storeMutex.Lock()
	defer s.storeMutex.Unlock()

	var first write
	var endedErr error
	s.locked(func() {
		if s.status != Live {
			endedErr = s.endedErrorLocked()
			return
		}
		first = s.capture(s.state.encode())
	})
	if endedErr != nil {
		return endedErr
	}

	err := store.Save(ctx, first.document)
	s.locked(func() {
		if err == nil {
			s.store = store
		}
		s.completedLocked(first.sequence)
	})
	if err != nil {
		return err
	}
	s.written = first.sequence
	return nil
}

// End ends the session as [SignedOut], discarding any refresh in flight, and
// clears its store. It returns the state the session held and the store's
// error verbatim when the clear fails, and an [*EndedError] when the session
// had already ended.
func (s *Session) End(ctx context.Context) (State, error) {
	var held State
	var clearing write
	var endedErr error
	s.locked(func() {
		if s.status != Live {
			endedErr = s.endedErrorLocked()
			return
		}
		held = s.state
		clearing = s.endLocked(SignedOut, nil)
	})
	if endedErr != nil {
		return State{}, endedErr
	}
	return held, s.perform(ctx, clearing)
}

// rejectLocked remembers rejection as the server's verdict on the refresh
// token and, once the access token is inside the expiry margin, ends the
// session with status and rejection as cause, returning the captured clear
// of the store and true. It is called with mutex held.
func (s *Session) rejectLocked(rejection error, status Status) (write, bool) {
	s.rejected = true
	s.rejection = rejection
	if s.state.expiresAt.Sub(s.now()) > s.expiryMargin {
		return write{}, false
	}
	return s.endLocked(status, rejection), true
}

// endLocked ends the session with status and cause, bumps the generation,
// drops the state from memory, tells the listeners and returns the captured
// clear of the store. It is called with mutex held.
func (s *Session) endLocked(status Status, cause error) write {
	s.status = status
	s.cause = cause
	s.state = State{}
	s.generation++
	close(s.done)
	clearing := s.capture(nil)
	s.notifyLocked(Change{Kind: Ended, Status: status, sequence: clearing.sequence})
	return clearing
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
	defer s.locked(func() { s.completedLocked(pending.sequence) })
	if pending.document == nil {
		return s.store.Delete(ctx)
	}
	return s.store.Save(ctx, pending.document)
}

// completedLocked records that the store call for the write at sequence in
// commit order has returned and wakes the deliveries waiting on it. It is
// called with mutex held.
func (s *Session) completedLocked(sequence uint64) {
	s.completed = max(s.completed, sequence)
	s.storeDone.Broadcast()
}

// AddListener adds listener to be told of every later change, the first
// being [Added], and returns the function that removes it. Each change is
// queued for the listener in the critical section that commits it and
// delivered by a goroutine the session starts while the queue is not empty,
// one change at a time, in order, with no lock held and once the store holds
// the state the change describes. The listener is removed on its own after a
// terminal change. Removing it discards the changes not yet delivered without
// waiting for a delivery in progress, and does nothing when repeated. A
// listener that panics ends its delivery goroutine with the panic.
func (s *Session) AddListener(listener func(Change)) (remove func()) {
	registered := &registration{listener: listener}
	s.locked(func() {
		s.registrations = append(s.registrations, registered)
		s.enqueueLocked(registered, Change{Kind: Added, Status: s.status, sequence: s.sequence})
	})
	return func() {
		s.locked(func() { s.removeLocked(registered) })
	}
}

// notifyLocked queues change for every listener. It is called with mutex
// held.
func (s *Session) notifyLocked(change Change) {
	for _, registered := range s.registrations {
		s.enqueueLocked(registered, change)
	}
}

// enqueueLocked queues change for registered and starts its delivery
// goroutine when none is running. It is called with mutex held.
func (s *Session) enqueueLocked(registered *registration, change Change) {
	registered.queue = append(registered.queue, change)
	if !registered.running {
		registered.running = true
		go s.deliver(registered)
	}
}

// removeLocked removes registered and discards its queue. It is called with
// mutex held.
func (s *Session) removeLocked(registered *registration) {
	registered.removed = true
	registered.queue = nil
	if index := slices.Index(s.registrations, registered); index >= 0 {
		s.registrations = slices.Delete(s.registrations, index, index+1)
	}
}

// deliver delivers registered's queued changes in order until the queue is
// empty or the listener is removed, removing it after a terminal change.
func (s *Session) deliver(registered *registration) {
	for {
		var next Change
		var empty bool
		s.locked(func() {
			if registered.removed || len(registered.queue) == 0 {
				registered.running = false
				registered.queue = nil
				empty = true
				return
			}
			next = registered.queue[0]
			registered.queue = registered.queue[1:]
		})
		if empty {
			return
		}
		s.awaitStore(next.sequence)
		registered.listener(next)
		if next.terminal() {
			s.locked(func() { s.removeLocked(registered) })
		}
	}
}

// awaitStore waits until the store call for the write at sequence in commit
// order, or a later one, has returned, or returns at once when the session
// persists to no store.
func (s *Session) awaitStore(sequence uint64) {
	s.locked(func() {
		for s.store != nil && s.completed < sequence {
			s.storeDone.Wait()
		}
	})
}
