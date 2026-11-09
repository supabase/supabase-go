package auth_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/supabase/supabase-go/auth"
)

// autoRefreshLead is how long before its access token expires that
// AutoRefresh renews a session.
const autoRefreshLead = 90 * time.Second

// startAutoRefresh runs AutoRefresh on a goroutine of its own and returns
// the function that cancels its context and reports what it returned. The
// context is cancelled when the test ends in any case.
func startAutoRefresh(t *testing.T, session *auth.Session) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	returned := make(chan error, 1)
	go func() { returned <- session.AutoRefresh(ctx) }()
	return func() error {
		cancel()
		return <-returned
	}
}

// assertStopped asserts that AutoRefresh, stopped through stop, returned nil.
func assertStopped(t *testing.T, stop func() error) {
	t.Helper()
	if err := stop(); err != nil {
		t.Errorf("AutoRefresh returned %v, want nil", err)
	}
}

// assertTokenRequests asserts count requests have reached the token
// endpoint, the sign-in included, and returns them.
func assertTokenRequests(t *testing.T, server *sessionServer, count int, want string) []recordedRequest {
	t.Helper()
	requests := server.requestsTo(tokenPath)
	if len(requests) != count {
		t.Fatalf("token requests = %d, want %d: %s", len(requests), count, want)
	}
	return requests
}

func TestAutoRefreshRenewsAheadOfExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		start := time.Now()
		session := server.signIn(t, server.client(t), time.Hour)
		recorder := recordEvents(t, session)
		stop := startAutoRefresh(t, session)

		time.Sleep(time.Hour - autoRefreshLead - time.Second)
		synctest.Wait()
		assertTokenRequests(t, server, 1, "the sign-in alone before the lead")
		assertEvents(t, recorder, auth.SessionEventInitialSession)

		time.Sleep(time.Second)
		synctest.Wait()
		requests := assertTokenRequests(t, server, 2, "the sign-in and the refresh at the lead")
		assertRefreshGrant(t, requests[1], "refresh-1")
		if at := requests[1].at.Sub(start); at != time.Hour-autoRefreshLead {
			t.Errorf("the refresh was sent %v after sign-in, want ninety seconds before expiry", at)
		}
		assertHoldsMinted(t, server, session, 1)
		assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed)

		time.Sleep(time.Hour - autoRefreshLead)
		synctest.Wait()
		requests = assertTokenRequests(t, server, 3, "a second refresh at the renewed token's lead")
		assertRefreshGrant(t, requests[2], "refresh-2")
		if gap := requests[2].at.Sub(requests[1].at); gap != time.Hour-autoRefreshLead {
			t.Errorf("the second refresh followed the first by %v, want ninety seconds before the renewed token's expiry", gap)
		}
		assertHoldsMinted(t, server, session, 2)
		assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed, auth.SessionEventTokenRefreshed)
		assertStopped(t, stop)
	})
}

func TestAutoRefreshHalvesAShortLifetime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		server.lifetime = time.Minute
		start := time.Now()
		session := server.signIn(t, server.client(t), time.Minute)
		stop := startAutoRefresh(t, session)

		time.Sleep(time.Minute)
		synctest.Wait()
		requests := assertTokenRequests(t, server, 3, "the sign-in and a refresh halfway through each lifetime")
		if first, second := requests[1].at.Sub(start), requests[2].at.Sub(start); first != 30*time.Second || second != time.Minute {
			t.Errorf("the refreshes were sent %v and %v after sign-in, want 30s and 1m0s", first, second)
		}
		assertHoldsMinted(t, server, session, 2)
		assertStopped(t, stop)
	})
}

func TestAutoRefreshSurvivesTransientFailures(t *testing.T) {
	t.Run("the refresh retries inside the loop's one flight", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			recorder := recordEvents(t, session)
			delayed := unavailable.answer()
			delayed.retryAfter = "2"
			server.script(tokenPath, unavailable.answer(), unavailable.answer(), delayed, unavailable.answer(), unavailable.answer(), answer{status: http.StatusOK})
			stop := startAutoRefresh(t, session)

			time.Sleep(time.Hour - autoRefreshLead + 10*time.Second)
			synctest.Wait()
			requests := assertTokenRequests(t, server, 7, "the sign-in, five failed attempts and the retry that succeeded")
			assertWaitedAbout(t, requests[1], requests[2], retryWaits[0])
			assertWaitedAbout(t, requests[2], requests[3], retryWaits[1])
			if gap := requests[4].at.Sub(requests[3].at); gap != 2*time.Second {
				t.Errorf("the retry after the Retry-After answer waited %v, want the two seconds asked for", gap)
			}
			assertWaitedAbout(t, requests[4], requests[5], retryWaits[3])
			assertWaitedAbout(t, requests[5], requests[6], retryWaits[4])
			assertHoldsMinted(t, server, session, 1)
			assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed)
			assertStopped(t, stop)
		})
	})

	t.Run("a sequence that fails is followed by another thirty seconds later, through expiry", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			recorder := recordEvents(t, session)
			server.script(tokenPath, unavailable.answer())
			stop := startAutoRefresh(t, session)

			time.Sleep(time.Hour - autoRefreshLead + 50*time.Second)
			synctest.Wait()
			if accessToken, err := session.AccessToken(t.Context()); err != nil || accessToken != server.mintedTokens()[0] {
				t.Errorf("AccessToken() with forty seconds left during the outage = (%q, %v), want the token held", accessToken, err)
			}

			// The first sequence ends within 101.25 seconds of the lead and the
			// next starts 30 seconds later, so at 140 seconds past the lead the
			// second sequence is in flight, and it cannot end before the token
			// expires.
			time.Sleep(90 * time.Second)
			var readToken string
			var readErr error
			var group sync.WaitGroup
			group.Go(func() { readToken, readErr = session.AccessToken(t.Context()) })
			synctest.Wait()

			time.Sleep(100 * time.Second)
			group.Wait()
			if readToken != "" {
				t.Errorf("AccessToken() after expiry during the outage = %q beside the error, want no token", readToken)
			}
			assertServerError(t, readErr, unavailable)
			if errors.Is(readErr, auth.ErrSessionExpired) {
				t.Error("a failed sequence reported ErrSessionExpired")
			}
			requests := server.requestsTo(tokenPath)
			if len(requests) < len(retryWaits)+3 {
				t.Fatalf("token requests = %d, want the sign-in, a full sequence of ten and the start of the next", len(requests))
			}
			for i, wait := range retryWaits {
				assertWaitedAbout(t, requests[i+1], requests[i+2], wait)
			}
			assertWaitedAbout(t, requests[len(retryWaits)+1], requests[len(retryWaits)+2], 30*time.Second)
			assertEvents(t, recorder, auth.SessionEventInitialSession)

			server.script(tokenPath, answer{status: http.StatusOK})
			time.Sleep(40 * time.Second)
			synctest.Wait()
			assertHoldsMinted(t, server, session, 1)
			assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed)
			assertStopped(t, stop)
		})
	})
}

func TestAutoRefreshEndsOnRejectionAtMargin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		recorder := recordEvents(t, session)
		server.script(tokenPath, refreshRejected.answer())
		stop := startAutoRefresh(t, session)

		time.Sleep(time.Hour - autoRefreshLead)
		synctest.Wait()
		assertTokenRequests(t, server, 2, "the sign-in and the rejected refresh")
		assertHoldsMinted(t, server, session, 0)
		assertEvents(t, recorder, auth.SessionEventInitialSession)

		time.Sleep(autoRefreshLead - 30*time.Second)
		synctest.Wait()
		err := stop()
		if !errors.Is(err, auth.ErrSessionExpired) {
			t.Fatalf("AutoRefresh returned %v, want ErrSessionExpired", err)
		}
		assertServerError(t, err, refreshRejected)
		assertTokenRequests(t, server, 2, "none since the rejection")
		assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventSignedOut)
		assertSignedOutEvent(t, recorder, auth.SignOutReasonSessionExpired)
		if store.deleteCount() != 1 || store.stored() != nil {
			t.Errorf("store deletes = %d holding %s, want the store cleared", store.deleteCount(), store.stored())
		}
		if accessToken, err := session.AccessToken(t.Context()); accessToken != "" || !errors.Is(err, auth.ErrSessionExpired) {
			t.Errorf("AccessToken() = (%q, %v), want ErrSessionExpired", accessToken, err)
		}
	})
}

func TestAutoRefreshReportsAStoreThatCannotBeCleared(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{deleteErr: errors.New("disk gone")}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		server.script(tokenPath, refreshRejected.answer())
		stop := startAutoRefresh(t, session)

		time.Sleep(time.Hour - autoRefreshLead)
		synctest.Wait()
		time.Sleep(autoRefreshLead - 30*time.Second)
		synctest.Wait()
		err := stop()
		if !errors.Is(err, auth.ErrSessionExpired) || !errors.Is(err, store.deleteErr) {
			t.Errorf("AutoRefresh returned %v, want ErrSessionExpired with the store's error joined", err)
		}
		assertServerError(t, err, refreshRejected)
	})
}

func TestAutoRefreshReportsAStoreThatCannotBeSaved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		store.saveErr = errors.New("disk full")
		stop := startAutoRefresh(t, session)

		time.Sleep(time.Hour - autoRefreshLead)
		synctest.Wait()
		assertNotSaved(t, server, session, store, stop(), 1)

		// A later call resumes, refreshing at the renewed token's lead.
		stop = startAutoRefresh(t, session)
		time.Sleep(time.Hour - autoRefreshLead)
		synctest.Wait()
		assertTokenRequests(t, server, 3, "the sign-in and a refresh by each call")
		assertHoldsMinted(t, server, session, 2)
		assertStopped(t, stop)
	})
}

func TestAutoRefreshStopsCleanly(t *testing.T) {
	t.Run("cancelled while it sleeps", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			recorder := recordEvents(t, session)
			stop := startAutoRefresh(t, session)

			time.Sleep(100 * time.Second)
			assertStopped(t, stop)
			assertTokenRequests(t, server, 1, "the sign-in alone")
			assertHoldsMinted(t, server, session, 0)
			assertEvents(t, recorder, auth.SessionEventInitialSession)
		})
	})

	t.Run("cancelled while its request is held open", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			recorder := recordEvents(t, session)
			hold := make(chan struct{})
			defer close(hold)
			server.script(tokenPath, answer{status: http.StatusOK, hold: hold}, answer{status: http.StatusOK})
			stop := startAutoRefresh(t, session)

			time.Sleep(time.Hour - autoRefreshLead)
			synctest.Wait()
			assertTokenRequests(t, server, 2, "the sign-in and the refresh held open")
			assertStopped(t, stop)
			assertTokenRequests(t, server, 2, "no request after the cancellation")
			assertHoldsMinted(t, server, session, 0)
			assertEvents(t, recorder, auth.SessionEventInitialSession)
		})
	})

	t.Run("signed out while it sleeps", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			recorder := recordEvents(t, session)
			stop := startAutoRefresh(t, session)

			time.Sleep(100 * time.Second)
			if _, err := session.SignOut(t.Context()); err != nil {
				t.Fatalf("SignOut: %v", err)
			}
			synctest.Wait()
			assertStopped(t, stop)
			assertSignedOut(t, session)
			assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventSignedOut)
			assertSignedOutEvent(t, recorder, auth.SignOutReasonUserInitiated)
			if paths := server.requestPaths(); !slices.Equal(paths, []string{tokenPath, logoutPath}) {
				t.Errorf("requests = %v, want the sign-in and the logout", paths)
			}
		})
	})
}

func TestAutoRefreshTwiceJoins(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		recorder := recordEvents(t, session)
		first := startAutoRefresh(t, session)
		second := startAutoRefresh(t, session)

		time.Sleep(time.Hour - autoRefreshLead)
		synctest.Wait()
		assertTokenRequests(t, server, 2, "the sign-in and one refresh for both loops")
		assertHoldsMinted(t, server, session, 1)
		assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed)
		assertStopped(t, first)
		assertStopped(t, second)
	})
}

func TestReadersJoinTheLoopsRefresh(t *testing.T) {
	t.Run("while its request is held open", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			hold := make(chan struct{})
			server.script(tokenPath, answer{status: http.StatusOK, hold: hold}, answer{status: http.StatusOK})
			stop := startAutoRefresh(t, session)

			time.Sleep(time.Hour - autoRefreshLead)
			synctest.Wait()
			assertTokenRequests(t, server, 2, "the sign-in and the refresh held open")

			time.Sleep(61 * time.Second)
			const readers = 20
			tokens := make([]string, readers)
			readErrors := make([]error, readers)
			var group sync.WaitGroup
			for i := range readers {
				group.Go(func() { tokens[i], readErrors[i] = session.AccessToken(t.Context()) })
			}
			synctest.Wait()
			assertTokenRequests(t, server, 2, "no request of the readers' own")

			close(hold)
			group.Wait()
			minted := server.mintedTokens()
			if len(minted) != 2 {
				t.Fatalf("minted %d tokens, want the sign-in and the loop's one refresh", len(minted))
			}
			for i := range readers {
				if readErrors[i] != nil || tokens[i] != minted[1] {
					t.Errorf("reader %d got (%q, %v), want the token the loop's refresh obtained", i, tokens[i], readErrors[i])
				}
			}
			assertTokenRequests(t, server, 2, "the sign-in and the loop's one refresh")
			assertStopped(t, stop)
		})
	})

	t.Run("while its refresh waits to retry", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			delayed := unavailable.answer()
			delayed.retryAfter = "120"
			server.script(tokenPath, delayed, answer{status: http.StatusOK})
			stop := startAutoRefresh(t, session)

			time.Sleep(time.Hour - autoRefreshLead)
			synctest.Wait()
			assertTokenRequests(t, server, 2, "the sign-in and the attempt that failed")

			time.Sleep(61 * time.Second)
			const readers = 4
			tokens := make([]string, readers)
			readErrors := make([]error, readers)
			var group sync.WaitGroup
			for i := range readers {
				group.Go(func() { tokens[i], readErrors[i] = session.AccessToken(t.Context()) })
			}
			impatient, cancelImpatient := context.WithTimeout(t.Context(), time.Second)
			defer cancelImpatient()
			var impatientToken string
			var impatientErr error
			group.Go(func() { impatientToken, impatientErr = session.AccessToken(impatient) })
			synctest.Wait()
			assertTokenRequests(t, server, 2, "no request of the readers' own")

			time.Sleep(2 * time.Second)
			synctest.Wait()
			if impatientToken != "" || !errors.Is(impatientErr, context.DeadlineExceeded) {
				t.Errorf("the impatient reader got (%q, %v), want context.DeadlineExceeded while the loop's refresh carries on", impatientToken, impatientErr)
			}

			time.Sleep(60 * time.Second)
			group.Wait()
			minted := server.mintedTokens()
			if len(minted) != 2 {
				t.Fatalf("minted %d tokens, want the sign-in and the one retry", len(minted))
			}
			for i := range readers {
				if readErrors[i] != nil || tokens[i] != minted[1] {
					t.Errorf("reader %d got (%q, %v), want the token the loop's retry obtained", i, tokens[i], readErrors[i])
				}
			}
			requests := assertTokenRequests(t, server, 3, "the sign-in, the failed attempt and the one retry")
			if gap := requests[2].at.Sub(requests[1].at); gap != 120*time.Second {
				t.Errorf("the retry waited %v, want the 120 seconds Retry-After asked for", gap)
			}
			assertStopped(t, stop)
		})
	})
}

func TestSignOutDuringAutoRefreshRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		recorder := recordEvents(t, session)
		hold := make(chan struct{})
		server.script(tokenPath, answer{status: http.StatusOK, hold: hold}, answer{status: http.StatusOK})
		stop := startAutoRefresh(t, session)

		time.Sleep(time.Hour - autoRefreshLead)
		synctest.Wait()
		assertTokenRequests(t, server, 2, "the sign-in and the refresh held open")

		if _, err := session.SignOut(t.Context()); err != nil {
			t.Fatalf("SignOut: %v", err)
		}
		assertSignedOut(t, session)
		close(hold)
		assertStopped(t, stop)
		if minted := server.mintedTokens(); len(minted) != 2 {
			t.Errorf("minted %d tokens, want the sign-in and the refresh the sign-out overtook", len(minted))
		}
		assertSignedOut(t, session)
		assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventSignedOut)
		assertSignedOutEvent(t, recorder, auth.SignOutReasonUserInitiated)
		if paths := server.requestPaths(); !slices.Equal(paths, []string{tokenPath, tokenPath, logoutPath}) {
			t.Errorf("requests = %v, want the sign-in, the overtaken refresh and the logout", paths)
		}
	})
}
