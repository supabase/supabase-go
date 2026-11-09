package auth

import (
	"log/slog"
	"runtime/debug"

	"github.com/supabase/supabase-go/auth/internal/session"
)

// SessionEvent is one change to a [Session], delivered to the listeners
// [Session.AddEventListener] added. It names the change and, for a session
// that ended, why. The session itself is not carried: a listener reads the
// state the event describes from the Session, which holds it by the time
// the listener runs. A SessionEvent is a value, so a listener's copy is its
// own.
type SessionEvent struct {
	name          SessionEventName
	signOutReason SignOutReason
}

// Name returns which change the event reports.
func (e SessionEvent) Name() SessionEventName { return e.name }

// SignOutReason returns why the session ended, for an event named
// [SessionEventSignedOut], and "" for any other.
func (e SessionEvent) SignOutReason() SignOutReason { return e.signOutReason }

// SessionEventName names a change to a [Session].
type SessionEventName string

const (
	// SessionEventInitialSession is the first event every listener receives:
	// the session as it stands when the listener is added, live or ended. It
	// is the only event a listener added to an ended session receives.
	SessionEventInitialSession SessionEventName = "InitialSession"

	// SessionEventTokenRefreshed reports a refresh that rotated both tokens.
	SessionEventTokenRefreshed SessionEventName = "TokenRefreshed"

	// SessionEventSignedOut reports that the session ended, with
	// [SessionEvent.SignOutReason] saying why. It is the last event a
	// listener receives.
	SessionEventSignedOut SessionEventName = "SignedOut"
)

// SignOutReason is why a session ended, carried by an event named
// [SessionEventSignedOut].
type SignOutReason string

const (
	// SignOutReasonUserInitiated is a session [Session.SignOut] ended.
	SignOutReasonUserInitiated SignOutReason = "UserInitiated"

	// SignOutReasonSessionExpired is a session that ended because the server
	// had refused to renew it and thirty seconds or less of its access token
	// remained.
	SignOutReasonSessionExpired SignOutReason = "SessionExpired"
)

// AddEventListener adds listener to receive the session's events and
// returns the function that removes it. A nil listener adds nothing, as does
// the zero Session.
//
// Events arrive in this order:
//   - [SessionEventInitialSession] first.
//   - Then every change the session commits after AddEventListener
//     returns, as its own event, in the order the changes were committed,
//     none dropped and none delivered twice.
//
// An event is delivered once the session's store, when it has one, holds
// the state the event describes. There is no event for the sign-in that
// created the session: the sign-in call's return is that signal.
//
// How listeners run:
//   - The session queues each event for each listener and carries on at
//     once, so no listener runs on the goroutine that changed the session.
//   - While a listener has events queued, a goroutine the session starts
//     calls it once per event, in order, with no lock held, and exits once
//     the queue is empty.
//   - A listener may call any Session method, including [Session.Refresh]
//     and the function that removes it.
//   - A slow listener delays only its own deliveries.
//   - A listener that panics is recovered and its panic logged through the
//     configured [slog.Logger] at [slog.LevelError]. Delivery continues with
//     the next event.
//
// Removing a listener discards its undelivered events without waiting for a
// delivery in progress. It is safe from inside the listener and does nothing
// when repeated.
//
// A listener is removed on its own after [SessionEventSignedOut], or after
// [SessionEventInitialSession] on a session that had already ended. A
// listener with no event queued holds no goroutine, so a Session dropped
// with listeners added leaves nothing running.
func (s *Session) AddEventListener(listener func(SessionEvent)) (remove func()) {
	if listener == nil || s.inner == nil {
		return func() {}
	}
	logger := s.client.logger
	return s.inner.AddListener(func(change session.Change) {
		event := toSessionEvent(change)
		defer func() {
			if recovered := recover(); recovered != nil {
				logListenerPanic(logger, event, recovered)
			}
		}()
		listener(event)
	})
}

// toSessionEvent translates a change the session machinery reports into the
// [SessionEvent] a listener receives.
func toSessionEvent(change session.Change) SessionEvent {
	switch change.Kind {
	case session.Refreshed:
		return SessionEvent{name: SessionEventTokenRefreshed}
	case session.Ended:
		return SessionEvent{name: SessionEventSignedOut, signOutReason: signOutReason(change.Status)}
	}
	return SessionEvent{name: SessionEventInitialSession}
}

// signOutReason translates the status a session ended with into the
// [SignOutReason] its [SessionEventSignedOut] carries.
func signOutReason(status session.Status) SignOutReason {
	if status == session.Expired {
		return SignOutReasonSessionExpired
	}
	return SignOutReasonUserInitiated
}

// logListenerPanic logs recovered, the panic a listener raised on receiving
// event, through logger, with the stack of the goroutine that recovered it.
func logListenerPanic(logger *slog.Logger, event SessionEvent, recovered any) {
	logger.Error("auth: session listener panicked",
		slog.String("event", string(event.Name())),
		slog.Any("panic", recovered),
		slog.String("stack", string(debug.Stack())),
	)
}
