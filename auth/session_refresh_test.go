package auth_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/internal/testkit"
)

// retryWaits are the waits before each retry of a refresh, before variation:
// the first and then each doubled to the ceiling of thirty seconds, the retry
// after the last of them being the last.
var retryWaits = []time.Duration{
	200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 1600 * time.Millisecond,
	3200 * time.Millisecond, 6400 * time.Millisecond, 12800 * time.Millisecond, 25600 * time.Millisecond,
	30 * time.Second,
}

// assertWaitedAbout asserts that later was sent wait after earlier, varied by
// up to a quarter either way.
func assertWaitedAbout(t *testing.T, earlier, later recordedRequest, wait time.Duration) {
	t.Helper()
	if gap := later.at.Sub(earlier.at); gap < wait*3/4 || gap >= wait*5/4 {
		t.Errorf("the retry waited %v, want %v varied by up to a quarter", gap, wait)
	}
}

func TestRefreshRetriesAFailureTheServerDidNotJudge(t *testing.T) {
	cases := []struct {
		name   string
		answer answer
	}{
		{name: "no answer", answer: answer{fail: true}},
		{name: "503", answer: unavailable.answer()},
		{name: "408", answer: requestTimeout.answer()},
		{name: "409 conflict", answer: conflict.answer()},
		{name: "429 over_request_rate_limit", answer: rateLimited.answer()},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				server := newInProcessSessionServer(t)
				session := server.signIn(t, server.client(t), expiredAlready)
				store := &recordingStore{}
				if err := session.Persist(t.Context(), store); err != nil {
					t.Fatalf("Persist: %v", err)
				}
				server.script(tokenPath, testCase.answer, answer{status: http.StatusOK})

				accessToken, err := session.AccessToken(t.Context())
				if minted := server.mintedTokens(); err != nil || len(minted) != 2 || accessToken != minted[1] {
					t.Fatalf("AccessToken() = (%q, %v) with %d tokens minted, want the token the retry obtained", accessToken, err, len(minted))
				}
				requests := server.requestsTo(tokenPath)
				if len(requests) != 3 {
					t.Fatalf("token requests = %d, want the sign-in, the failed attempt and the retry", len(requests))
				}
				assertRefreshGrant(t, requests[2], "refresh-1")
				assertWaitedAbout(t, requests[1], requests[2], retryWaits[0])
				assertHoldsMinted(t, server, session, 1)
				if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1", "refresh-2"}) || store.deleteCount() != 0 {
					t.Errorf("the store saved %v with %d deletes, want the refresh written and nothing cleared", saved, store.deleteCount())
				}
			})
		})
	}

	t.Run("a Retry-After longer than the backoff is waited in full", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), expiredAlready)
			delayed := unavailable.answer()
			delayed.retryAfter = "2"
			server.script(tokenPath, delayed, answer{status: http.StatusOK})

			if _, err := session.AccessToken(t.Context()); err != nil {
				t.Fatalf("AccessToken: %v", err)
			}
			requests := server.requestsTo(tokenPath)
			if len(requests) != 3 {
				t.Fatalf("token requests = %d, want the sign-in, the failed attempt and the retry", len(requests))
			}
			if gap := requests[2].at.Sub(requests[1].at); gap != 2*time.Second {
				t.Errorf("the retry waited %v, want the two seconds Retry-After asked for", gap)
			}
		})
	})
}

func TestRefreshStopsAfterTheLastWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), expiredAlready)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		server.script(tokenPath, unavailable.answer())

		start := time.Now()
		accessToken, err := session.AccessToken(t.Context())
		if accessToken != "" {
			t.Errorf("AccessToken() = %q beside the error, want no token", accessToken)
		}
		assertServerError(t, err, unavailable)
		if errors.Is(err, auth.ErrSessionExpired) {
			t.Error("an exhausted retry sequence reported ErrSessionExpired")
		}
		requests := server.requestsTo(tokenPath)
		if len(requests) != len(retryWaits)+2 {
			t.Fatalf("token requests = %d, want the sign-in and ten attempts", len(requests))
		}
		for i, wait := range retryWaits {
			assertWaitedAbout(t, requests[i+1], requests[i+2], wait)
		}
		if elapsed := time.Since(start); elapsed < 60750*time.Millisecond || elapsed >= 101250*time.Millisecond {
			t.Errorf("the sequence took %v, want 81 seconds of waits varied by up to a quarter", elapsed)
		}
		if saved := store.savedRefreshTokens(t); session.RefreshToken() != "refresh-1" || !slices.Equal(saved, []string{"refresh-1"}) || store.deleteCount() != 0 {
			t.Errorf("after the failure RefreshToken() = %q and the store saved %v with %d deletes, want both as they were", session.RefreshToken(), saved, store.deleteCount())
		}
		assertRecovers(t, server, session)
	})
}

func TestRetryAfterOfThirtySecondsEndsTheSequence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), expiredAlready)
		delayed := unavailable.answer()
		delayed.retryAfter = "31"
		server.script(tokenPath, delayed)

		_, err := session.AccessToken(t.Context())
		assertServerError(t, err, unavailable)
		requests := server.requestsTo(tokenPath)
		if len(requests) != 3 {
			t.Fatalf("token requests = %d, want the sign-in, the failed attempt and one retry", len(requests))
		}
		if gap := requests[2].at.Sub(requests[1].at); gap != 31*time.Second {
			t.Errorf("the retry waited %v, want the 31 seconds Retry-After asked for", gap)
		}
	})
}

func TestRefreshStopsAtTheCallersDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), expiredAlready)
		server.script(tokenPath, unavailable.answer())

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		accessToken, err := session.AccessToken(ctx)
		if accessToken != "" || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("AccessToken() = (%q, %v), want context.DeadlineExceeded", accessToken, err)
		}
		if requests := server.requestsTo(tokenPath); len(requests) != 4 {
			t.Errorf("token requests = %d, want the sign-in and the three attempts a second holds", len(requests))
		}
		if session.RefreshToken() != "refresh-1" {
			t.Errorf("RefreshToken() = %q, want refresh-1 kept", session.RefreshToken())
		}
		assertRecovers(t, server, session)
	})
}

func TestRefreshFailureWithoutAVerdictIsNotRetried(t *testing.T) {
	cases := []struct {
		name   string
		answer answer
		assert func(t *testing.T, err error)
	}{
		{
			name:   "a 400 with an unlisted code",
			answer: unlistedCode.answer(),
			assert: func(t *testing.T, err error) {
				t.Helper()
				assertServerError(t, err, unlistedCode)
			},
		},
		{
			name:   "a 200 that is not a token response",
			answer: answer{status: http.StatusOK, body: "<html>captive portal</html>"},
			assert: func(t *testing.T, err error) {
				t.Helper()
				if err == nil || !strings.Contains(err.Error(), "decoding token response") {
					t.Errorf("error = %v, want the decode failure", err)
				}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newSessionServer(t)
			session := server.signIn(t, server.client(t), expiredAlready)
			store := &recordingStore{}
			if err := session.Persist(t.Context(), store); err != nil {
				t.Fatalf("Persist: %v", err)
			}
			server.script(tokenPath, testCase.answer)

			accessToken, err := session.AccessToken(t.Context())
			if accessToken != "" {
				t.Errorf("AccessToken() = %q beside the error, want no token", accessToken)
			}
			testCase.assert(t, err)
			if errors.Is(err, auth.ErrSessionExpired) {
				t.Error("a failure without a verdict reported ErrSessionExpired")
			}
			if hits := server.requestsTo(tokenPath); len(hits) != 2 {
				t.Errorf("token requests = %d, want the sign-in and one attempt", len(hits))
			}
			_, err = session.AccessToken(t.Context())
			testCase.assert(t, err)
			if hits := server.requestsTo(tokenPath); len(hits) != 3 {
				t.Errorf("token requests = %d, want the next read to send its own attempt, the failure not being remembered", len(hits))
			}
			if saved := store.savedRefreshTokens(t); session.RefreshToken() != "refresh-1" || !slices.Equal(saved, []string{"refresh-1"}) || store.deleteCount() != 0 {
				t.Errorf("after the failure RefreshToken() = %q and the store saved %v with %d deletes, want both as they were", session.RefreshToken(), saved, store.deleteCount())
			}
			assertRecovers(t, server, session)
		})
	}
}

func TestRejectedRefreshEndsSessionAtMargin(t *testing.T) {
	t.Run("a rejection with more than thirty seconds left is remembered and acted on at the margin", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			store := &recordingStore{}
			if err := session.Persist(t.Context(), store); err != nil {
				t.Fatalf("Persist: %v", err)
			}
			server.script(tokenPath, refreshRejected.answer())

			result, err := session.Refresh(t.Context())
			if result != nil || errors.Is(err, auth.ErrSessionExpired) {
				t.Fatalf("Refresh() = (%v, %v), want the rejection with the session kept", result, err)
			}
			assertServerError(t, err, refreshRejected)
			result, err = session.Refresh(t.Context())
			if result != nil || errors.Is(err, auth.ErrSessionExpired) {
				t.Fatalf("a repeat Refresh() = (%v, %v), want the remembered rejection with the session kept", result, err)
			}
			assertServerError(t, err, refreshRejected)
			if hits := server.requestsTo(tokenPath); len(hits) != 2 {
				t.Errorf("token requests = %d, want the sign-in and the rejected refresh, the repeat sending nothing", len(hits))
			}
			assertHoldsMinted(t, server, session, 0)
			if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1"}) || store.deleteCount() != 0 {
				t.Errorf("the store saved %v with %d deletes, want its first write alone", saved, store.deleteCount())
			}

			time.Sleep(time.Hour - 31*time.Second)
			assertHoldsMinted(t, server, session, 0)

			time.Sleep(2 * time.Second)
			accessToken, err := session.AccessToken(t.Context())
			if accessToken != "" || !errors.Is(err, auth.ErrSessionExpired) {
				t.Fatalf("AccessToken() with 29 seconds left = (%q, %v), want ErrSessionExpired", accessToken, err)
			}
			assertServerError(t, err, refreshRejected)
			if hits := server.requestsTo(tokenPath); len(hits) != 2 {
				t.Errorf("token requests = %d, want none since the rejection", len(hits))
			}
			if store.deleteCount() != 1 || store.stored() != nil {
				t.Errorf("store deletes = %d holding %s, want the store cleared", store.deleteCount(), store.stored())
			}
			if session.RefreshToken() != "" || session.User() != nil {
				t.Error("the ended session still holds credentials")
			}
			if result, err := session.Refresh(t.Context()); result != nil || !errors.Is(err, auth.ErrSessionExpired) {
				t.Errorf("Refresh() after the session ended = (%v, %v), want ErrSessionExpired", result, err)
			}
		})
	})

	t.Run("a rejection with thirty seconds or less left ends the session", func(t *testing.T) {
		for _, rejection := range []scriptedError{refreshRejected, refreshAlreadyUsed, refreshNoSession, sessionExpired, userBanned, validationFailed} {
			t.Run(rejection.code, func(t *testing.T) {
				server := newSessionServer(t)
				session := server.signIn(t, server.client(t), expiresSoon)
				store := &recordingStore{}
				if err := session.Persist(t.Context(), store); err != nil {
					t.Fatalf("Persist: %v", err)
				}
				server.script(tokenPath, rejection.answer())

				accessToken, err := session.AccessToken(t.Context())
				if accessToken != "" || !errors.Is(err, auth.ErrSessionExpired) {
					t.Fatalf("AccessToken() = (%q, %v), want ErrSessionExpired", accessToken, err)
				}
				assertServerError(t, err, rejection)
				if store.deleteCount() != 1 || store.stored() != nil {
					t.Errorf("store deletes = %d holding %s, want the store cleared", store.deleteCount(), store.stored())
				}
				if session.RefreshToken() != "" || session.User() != nil {
					t.Error("the ended session still holds credentials")
				}
				if result, err := session.Refresh(t.Context()); result != nil || !errors.Is(err, auth.ErrSessionExpired) {
					t.Errorf("Refresh() after the session ended = (%v, %v), want ErrSessionExpired", result, err)
				}
				if hits := server.requestsTo(tokenPath); len(hits) != 2 {
					t.Errorf("token requests = %d, want the sign-in and the rejected refresh with none once the session ended", len(hits))
				}
			})
		}
	})

	t.Run("a store that cannot be cleared is reported with ErrSessionExpired", func(t *testing.T) {
		ending := func(t *testing.T) (*auth.Session, *recordingStore) {
			t.Helper()
			server := newSessionServer(t)
			session := server.signIn(t, server.client(t), expiresSoon)
			store := &recordingStore{deleteErr: errors.New("disk gone")}
			if err := session.Persist(t.Context(), store); err != nil {
				t.Fatalf("Persist: %v", err)
			}
			server.script(tokenPath, refreshRejected.answer())
			return session, store
		}

		t.Run("by AccessToken", func(t *testing.T) {
			session, store := ending(t)
			accessToken, err := session.AccessToken(t.Context())
			if accessToken != "" || !errors.Is(err, auth.ErrSessionExpired) || !reportsNotCleared(err, store) {
				t.Errorf("AccessToken() = (%q, %v), want ErrSessionExpired with ErrSessionNotCleared joined", accessToken, err)
			}
			assertServerError(t, err, refreshRejected)
		})

		t.Run("by Refresh", func(t *testing.T) {
			session, store := ending(t)
			result, err := session.Refresh(t.Context())
			if result != nil || !errors.Is(err, auth.ErrSessionExpired) || !reportsNotCleared(err, store) {
				t.Errorf("Refresh() = (%v, %v), want ErrSessionExpired with ErrSessionNotCleared joined", result, err)
			}
			assertServerError(t, err, refreshRejected)
		})
	})
}

func TestReadersJoinTheRetryingRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), expiredAlready)
		server.script(tokenPath, unavailable.answer(), answer{status: http.StatusOK})

		const readers = 5
		tokens := make([]string, readers)
		readErrors := make([]error, readers)
		var group sync.WaitGroup
		group.Go(func() { tokens[0], readErrors[0] = session.AccessToken(t.Context()) })
		synctest.Wait()
		if hits := server.requestsTo(tokenPath); len(hits) != 2 {
			t.Fatalf("token requests = %d, want the sign-in and the failed attempt, with the retry waiting", len(hits))
		}
		for i := 1; i < readers; i++ {
			group.Go(func() { tokens[i], readErrors[i] = session.AccessToken(t.Context()) })
		}
		synctest.Wait()
		if hits := server.requestsTo(tokenPath); len(hits) != 2 {
			t.Fatalf("token requests = %d once readers joined during the wait, want no attempt of their own", len(hits))
		}
		group.Wait()

		minted := server.mintedTokens()
		if len(minted) != 2 {
			t.Fatalf("minted %d tokens, want the sign-in and the one retry", len(minted))
		}
		for i := range readers {
			if readErrors[i] != nil || tokens[i] != minted[1] {
				t.Errorf("reader %d got (%q, %v), want the token the one retry obtained", i, tokens[i], readErrors[i])
			}
		}
		if hits := server.requestsTo(tokenPath); len(hits) != 3 {
			t.Errorf("token requests = %d, want the sign-in, the failed attempt and the one retry", len(hits))
		}
	})
}

func TestSessionFromTokensRetriesItsRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		client := server.client(t)
		claims := testkit.DefaultClaims()
		claims["sub"] = sessionUserID
		claims["exp"] = time.Now().Add(expiredAlready).Unix()
		server.script(tokenPath, unavailable.answer(), answer{status: http.StatusOK})

		result, err := client.SessionFromTokens(t.Context(), server.signer.Token(t, claims), "refresh-elsewhere")
		if err != nil {
			t.Fatalf("SessionFromTokens: %v", err)
		}
		requests := server.requestsTo(tokenPath)
		if len(requests) != 2 {
			t.Fatalf("token requests = %d, want the failed attempt and the retry", len(requests))
		}
		assertRefreshGrant(t, requests[1], "refresh-elsewhere")
		assertWaitedAbout(t, requests[0], requests[1], retryWaits[0])
		assertHoldsMinted(t, server, result.Session(), 0)
	})
}

func TestSignOutRetriesItsRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), expiresSoon)
		server.script(tokenPath, unavailable.answer(), answer{status: http.StatusOK})

		if result, err := session.SignOut(t.Context()); err != nil || result == nil {
			t.Fatalf("SignOut = (%v, %v), want the empty result", result, err)
		}
		if paths := server.requestPaths(); !slices.Equal(paths, []string{tokenPath, tokenPath, tokenPath, logoutPath}) {
			t.Fatalf("requests = %v, want the sign-in, the failed attempt, the retry and then the logout", paths)
		}
		minted := server.mintedTokens()
		if bearer := server.requestsTo(logoutPath)[0].authorization; bearer != "Bearer "+minted[1] {
			t.Errorf("logout bearer is token %d of %d minted, want the refreshed second", slices.Index(minted, strings.TrimPrefix(bearer, "Bearer ")), len(minted))
		}
		assertSignedOut(t, session)
	})
}
