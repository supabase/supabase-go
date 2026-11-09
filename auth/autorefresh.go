package auth

import (
	"context"
	"errors"
	"time"

	"github.com/supabase/supabase-go/auth/internal/backoff"
	"github.com/supabase/supabase-go/auth/internal/session"
)

const (
	// autoRefreshLead is how long before the access token expires that
	// [Session.AutoRefresh] renews it.
	autoRefreshLead = 90 * time.Second

	// autoRefreshRetryWait is how long [Session.AutoRefresh] waits before
	// trying again when a refresh fails for a reason other than the server
	// rejecting the refresh token, such as a network error or an outage.
	autoRefreshRetryWait = 30 * time.Second
)

// AutoRefresh keeps the session fresh until ctx is done.
//
// It blocks, so run it on a goroutine of your own and cancel ctx to stop it:
//
//	group, groupContext := errgroup.WithContext(ctx)
//	group.Go(func() error { return session.AutoRefresh(groupContext) })
//
// It renews the access token ninety seconds before it expires. A token that
// lives less than ninety seconds is renewed halfway to its expiry instead.
//
// When a refresh fails:
//   - If the server rejected the refresh token, the session stays usable
//     until thirty seconds before the access token expires, then ends.
//   - Otherwise, once the retries [Session] describes are spent, AutoRefresh
//     tries again every thirty seconds, even after the access token has
//     expired, because the refresh token is still valid.
//
// AutoRefresh returns:
//   - nil when ctx is done or [Session.SignOut] has ended the session.
//   - [ErrSessionExpired], wrapping the server's rejection, when the session
//     ended because the server rejected its refresh token.
//   - the store's error, wrapped and joined with either of the above, when
//     its refresh ended the session and the store could not be cleared.
//
// Every caller shares the session's one in-flight refresh, so a second
// AutoRefresh on the same session joins the first rather than refreshing
// twice. A failure to save a renewed session to its store does not stop
// AutoRefresh and is not reported.
func (s *Session) AutoRefresh(ctx context.Context) error {
	if s.inner == nil {
		return nil
	}
	for {
		expiresAt, rejected := s.inner.Expiry()
		if expiresAt.IsZero() {
			return s.autoRefreshEnded()
		}
		wake := s.autoRefreshWake(expiresAt)
		if rejected {
			// The remembered rejection ends the session at the margin.
			wake = expiresAt.Add(-expiryMargin)
		}
		if !s.waitUntil(ctx, wake) {
			return nil
		}

		clearErr, err := s.inner.RefreshWithin(ctx, autoRefreshLead)
		var written *session.WriteError
		var ended *session.EndedError
		switch {
		case err == nil, errors.As(err, &written):
			continue
		case errors.As(err, &ended):
			return joinClearError(s.autoRefreshEnded(), clearErr)
		case ctx.Err() != nil:
			return nil
		}
		if _, rejected := s.inner.Expiry(); rejected {
			continue
		}
		if backoff.Sleep(ctx, autoRefreshRetryWait) != nil {
			return nil
		}
	}
}

// autoRefreshWake is the instant [Session.AutoRefresh] refreshes a token
// that expires at expiresAt: ninety seconds before, or halfway to expiry when
// less than ninety seconds remain.
func (s *Session) autoRefreshWake(expiresAt time.Time) time.Time {
	now := s.client.now()
	if wake := expiresAt.Add(-autoRefreshLead); wake.After(now) {
		return wake
	}
	return now.Add(expiresAt.Sub(now) / 2)
}

// autoRefreshEnded is what [Session.AutoRefresh] returns for the ended
// session: [ErrSessionExpired] wrapping the rejection when it expired, nil
// when it was signed out.
func (s *Session) autoRefreshEnded() error {
	err := sessionError(s.inner.Ended())
	if errors.Is(err, ErrSessionExpired) {
		return err
	}
	return nil
}

// waitUntil pauses until deadline, or until the session ends, and reports
// true, or reports false when ctx ends first.
func (s *Session) waitUntil(ctx context.Context, deadline time.Time) bool {
	timer := time.NewTimer(deadline.Sub(s.client.now()))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-s.inner.Done():
		return true
	case <-timer.C:
		return true
	}
}
