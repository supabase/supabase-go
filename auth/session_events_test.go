package auth_test

import (
	"bytes"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/signout"
	"github.com/supabase/supabase-go/core/configuration"
)

// eventRecorder records the events a listener receives, in order.
type eventRecorder struct {
	mutex  sync.Mutex
	events []auth.SessionEvent
}

// record is the listener that records.
func (r *eventRecorder) record(event auth.SessionEvent) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.events = append(r.events, event)
}

// recorded returns the events recorded so far, in order.
func (r *eventRecorder) recorded() []auth.SessionEvent {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return slices.Clone(r.events)
}

// names returns the names of the events recorded so far, in order.
func (r *eventRecorder) names() []auth.SessionEventName {
	names := []auth.SessionEventName{}
	for _, event := range r.recorded() {
		names = append(names, event.Name())
	}
	return names
}

// recordEvents adds a recorder to session as a listener, removing it when
// the test ends, and returns it.
func recordEvents(t *testing.T, session *auth.Session) *eventRecorder {
	t.Helper()
	recorder := &eventRecorder{}
	t.Cleanup(session.AddEventListener(recorder.record))
	return recorder
}

// assertEvents asserts that, once every delivery in flight has finished,
// recorder has received exactly the events named, in order.
func assertEvents(t *testing.T, recorder *eventRecorder, want ...auth.SessionEventName) {
	t.Helper()
	synctest.Wait()
	if got := recorder.names(); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

// assertSignedOutEvent asserts the last event recorder received is
// SessionEventSignedOut carrying reason.
func assertSignedOutEvent(t *testing.T, recorder *eventRecorder, reason auth.SignOutReason) {
	t.Helper()
	synctest.Wait()
	events := recorder.recorded()
	if len(events) == 0 {
		t.Fatal("no event delivered, want SessionEventSignedOut")
	}
	last := events[len(events)-1]
	if last.Name() != auth.SessionEventSignedOut || last.SignOutReason() != reason {
		t.Errorf("last event = %s with reason %q, want %s with reason %q", last.Name(), last.SignOutReason(), auth.SessionEventSignedOut, reason)
	}
	for _, event := range events[:len(events)-1] {
		if event.SignOutReason() != "" {
			t.Errorf("event %s carries reason %q, want none", event.Name(), event.SignOutReason())
		}
	}
}

// synchronizedBuffer is a bytes.Buffer safe for concurrent writes, the sink of
// a test's logger.
type synchronizedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

func TestAddEventListenerDeliversTheInitialSessionFirst(t *testing.T) {
	t.Run("on a live session", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			recorder := recordEvents(t, session)
			assertEvents(t, recorder, auth.SessionEventInitialSession)
			if events := recorder.recorded(); events[0].SignOutReason() != "" {
				t.Errorf("the initial event carries reason %q, want none", events[0].SignOutReason())
			}

			if _, err := session.Refresh(t.Context()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed)
		})
	})

	t.Run("on an ended session, alone", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			if _, err := session.SignOut(t.Context()); err != nil {
				t.Fatalf("SignOut: %v", err)
			}
			recorder := recordEvents(t, session)
			assertEvents(t, recorder, auth.SessionEventInitialSession)
			if events := recorder.recorded(); events[0].SignOutReason() != "" {
				t.Errorf("the initial event carries reason %q, want none", events[0].SignOutReason())
			}
		})
	})

	t.Run("not on the zero Session", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var session auth.Session
			recorder := recordEvents(t, &session)
			remove := session.AddEventListener(recorder.record)
			remove()
			remove()
			assertEvents(t, recorder)
		})
	})

	t.Run("a nil listener adds nothing", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			remove := session.AddEventListener(nil)
			remove()
			if _, err := session.Refresh(t.Context()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			synctest.Wait()
		})
	})
}

func TestEventsAreDeliveredOnceTheStoreHoldsTheState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		recorder := recordEvents(t, session)
		assertEvents(t, recorder, auth.SessionEventInitialSession)

		entered, release := store.holdNextSave()
		var refreshErr error
		var group sync.WaitGroup
		group.Go(func() { _, refreshErr = session.Refresh(t.Context()) })
		<-entered
		synctest.Wait()
		accessToken, err := session.AccessToken(t.Context())
		if err != nil {
			// The refresh held in Save must finish for the bubble to end.
			release()
			t.Fatalf("AccessToken while the store write is held: %v", err)
		}
		if accessToken != server.mintedTokens()[1] {
			t.Error("the session does not hold the refreshed token while its store write is held")
		}
		assertEvents(t, recorder, auth.SessionEventInitialSession)

		release()
		group.Wait()
		if refreshErr != nil {
			t.Fatalf("Refresh: %v", refreshErr)
		}
		assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed)
		if saved := store.savedRefreshTokens(t); !slices.Equal(saved, []string{"refresh-1", "refresh-2"}) {
			t.Errorf("the store saved %v, want the refresh written before its event was delivered", saved)
		}
	})
}

func TestSignedOutEventCarriesTheReason(t *testing.T) {
	cases := []struct {
		name     string
		lifetime time.Duration
		script   []answer
		act      func(t *testing.T, session *auth.Session)
		reason   auth.SignOutReason
	}{
		{
			name:     "a bare SignOut",
			lifetime: time.Hour,
			act: func(t *testing.T, session *auth.Session) {
				t.Helper()
				if _, err := session.SignOut(t.Context()); err != nil {
					t.Fatalf("SignOut: %v", err)
				}
			},
			reason: auth.SignOutReasonUserInitiated,
		},
		{
			name:     "SignOut with WithLocalScope",
			lifetime: time.Hour,
			act: func(t *testing.T, session *auth.Session) {
				t.Helper()
				if _, err := session.SignOut(t.Context(), signout.WithLocalScope()); err != nil {
					t.Fatalf("SignOut: %v", err)
				}
			},
			reason: auth.SignOutReasonUserInitiated,
		},
		{
			name:     "a bare SignOut whose refresh the server rejects",
			lifetime: expiresSoon,
			script:   []answer{refreshRejected.answer()},
			act: func(t *testing.T, session *auth.Session) {
				t.Helper()
				_, err := session.SignOut(t.Context())
				assertServerError(t, err, refreshRejected)
			},
			reason: auth.SignOutReasonUserInitiated,
		},
		{
			name:     "SignOut with WithOthersScope whose refresh the server rejects",
			lifetime: expiresSoon,
			script:   []answer{refreshRejected.answer()},
			act: func(t *testing.T, session *auth.Session) {
				t.Helper()
				if _, err := session.SignOut(t.Context(), signout.WithOthersScope()); !errors.Is(err, auth.ErrSessionExpired) {
					t.Fatalf("SignOut = %v, want ErrSessionExpired", err)
				}
			},
			reason: auth.SignOutReasonSessionExpired,
		},
		{
			name:     "a read whose refresh the server rejects",
			lifetime: expiresSoon,
			script:   []answer{refreshRejected.answer()},
			act: func(t *testing.T, session *auth.Session) {
				t.Helper()
				if _, err := session.AccessToken(t.Context()); !errors.Is(err, auth.ErrSessionExpired) {
					t.Fatalf("AccessToken = %v, want ErrSessionExpired", err)
				}
			},
			reason: auth.SignOutReasonSessionExpired,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				server := newInProcessSessionServer(t)
				session := server.signIn(t, server.client(t), testCase.lifetime)
				if testCase.script != nil {
					server.script(tokenPath, testCase.script...)
				}
				recorder := recordEvents(t, session)
				assertEvents(t, recorder, auth.SessionEventInitialSession)

				testCase.act(t, session)
				assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventSignedOut)
				assertSignedOutEvent(t, recorder, testCase.reason)

				if _, err := session.Refresh(t.Context()); err == nil {
					t.Error("Refresh after the session ended succeeded")
				}
				assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventSignedOut)
			})
		})
	}

	t.Run("SignOut with WithOthersScope emits nothing", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			recorder := recordEvents(t, session)
			if _, err := session.SignOut(t.Context(), signout.WithOthersScope()); err != nil {
				t.Fatalf("SignOut: %v", err)
			}
			assertEvents(t, recorder, auth.SessionEventInitialSession)
			assertHoldsMinted(t, server, session, 0)
		})
	})
}

func TestListenerMayCallTheSession(t *testing.T) {
	t.Run("a Refresh from inside a delivery", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			recorder := &eventRecorder{}
			var once sync.Once
			t.Cleanup(session.AddEventListener(func(event auth.SessionEvent) {
				recorder.record(event)
				if event.Name() == auth.SessionEventTokenRefreshed {
					once.Do(func() {
						if _, err := session.Refresh(t.Context()); err != nil {
							t.Errorf("Refresh from inside the listener: %v", err)
						}
					})
				}
			}))

			if _, err := session.Refresh(t.Context()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed, auth.SessionEventTokenRefreshed)
			if hits := server.requestsTo(tokenPath); len(hits) != 3 {
				t.Errorf("token requests = %d, want the sign-in, the refresh and the one the listener made", len(hits))
			}
			assertHoldsMinted(t, server, session, 2)
		})
	})

	t.Run("a panic is recovered, logged and followed by the next event", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			logged := &synchronizedBuffer{}
			client := server.client(t, configuration.WithLogger(slog.New(slog.NewTextHandler(logged, nil))))
			session := server.signIn(t, client, time.Hour)
			recorder := &eventRecorder{}
			t.Cleanup(session.AddEventListener(func(event auth.SessionEvent) {
				recorder.record(event)
				if len(recorder.recorded()) == 1 {
					panic("listener failure")
				}
			}))
			assertEvents(t, recorder, auth.SessionEventInitialSession)
			if output := logged.String(); !strings.Contains(output, "level=ERROR") || !strings.Contains(output, "session listener panicked") || !strings.Contains(output, "listener failure") || !strings.Contains(output, "event=InitialSession") {
				t.Errorf("logged %q, want the panic logged at ERROR with its event and value", output)
			}

			if _, err := session.Refresh(t.Context()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed)
		})
	})
}

func TestRemovingAListenerStopsDelivery(t *testing.T) {
	t.Run("after the call nothing more is delivered, and repeating it does nothing", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			recorder := &eventRecorder{}
			remove := session.AddEventListener(recorder.record)
			assertEvents(t, recorder, auth.SessionEventInitialSession)

			remove()
			if _, err := session.Refresh(t.Context()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			assertEvents(t, recorder, auth.SessionEventInitialSession)
			remove()
			if _, err := session.SignOut(t.Context()); err != nil {
				t.Fatalf("SignOut: %v", err)
			}
			assertEvents(t, recorder, auth.SessionEventInitialSession)
		})
	})

	t.Run("from inside the listener", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			server := newInProcessSessionServer(t)
			session := server.signIn(t, server.client(t), time.Hour)
			recorder := &eventRecorder{}
			registered := make(chan func(), 1)
			remove := session.AddEventListener(func(event auth.SessionEvent) {
				recorder.record(event)
				(<-registered)()
			})
			registered <- remove
			assertEvents(t, recorder, auth.SessionEventInitialSession)

			if _, err := session.Refresh(t.Context()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			assertEvents(t, recorder, auth.SessionEventInitialSession)
		})
	})
}

func TestSlowListenerDelaysOnlyItself(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		slow := &eventRecorder{}
		proceed := make(chan struct{})
		t.Cleanup(session.AddEventListener(func(event auth.SessionEvent) {
			<-proceed
			slow.record(event)
		}))
		prompt := recordEvents(t, session)

		for range 2 {
			if _, err := session.Refresh(t.Context()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
		}
		assertEvents(t, prompt, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed, auth.SessionEventTokenRefreshed)
		assertEvents(t, slow)
		assertHoldsMinted(t, server, session, 2)

		for range 3 {
			proceed <- struct{}{}
		}
		assertEvents(t, slow, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed, auth.SessionEventTokenRefreshed)
	})
}

func TestDroppedSessionLeavesNothingRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		recorder := &eventRecorder{}
		_ = session.AddEventListener(recorder.record)
		if _, err := session.Refresh(t.Context()); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventTokenRefreshed)
	})
}

func TestEventsEndAfterTheSessionDoes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		recorder := recordEvents(t, session)
		if _, err := session.SignOut(t.Context(), signout.WithLocalScope()); err != nil {
			t.Fatalf("SignOut: %v", err)
		}
		assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventSignedOut)
		assertSignedOutEvent(t, recorder, auth.SignOutReasonUserInitiated)
		if store.deleteCount() != 1 {
			t.Errorf("store deletes = %d, want the clear performed before the event was delivered", store.deleteCount())
		}
		if _, err := session.SignOut(t.Context(), signout.WithLocalScope()); err != nil {
			t.Fatalf("a repeat SignOut: %v", err)
		}
		if _, err := session.Refresh(t.Context()); !errors.Is(err, auth.ErrSignedOut) {
			t.Errorf("Refresh after the session ended = %v, want ErrSignedOut", err)
		}
		assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventSignedOut)
		if paths := server.requestPaths(); !slices.Equal(paths, []string{tokenPath, logoutPath}) {
			t.Errorf("requests = %v, want the sign-in and one logout", paths)
		}
	})
}
