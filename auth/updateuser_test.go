package auth_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/messaging"
	"github.com/supabase/supabase-go/auth/signout"
)

// updatedUserDocument is sessionUserDocument with a plan added to its user
// metadata: the profile the server answers an update with.
func updatedUserDocument(t *testing.T) string {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal([]byte(sessionUserDocument), &document); err != nil {
		t.Fatalf("decoding the user document: %v", err)
	}
	metadata, ok := document["user_metadata"].(map[string]any)
	if !ok {
		t.Fatal("the user document carries no user_metadata object")
	}
	metadata["plan"] = "pro"
	updated, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encoding the updated user document: %v", err)
	}
	return string(updated)
}

// assertUpdatedUser asserts user is the served profile with the plan the
// update added.
func assertUpdatedUser(t *testing.T, user *auth.User) {
	t.Helper()
	if user == nil {
		t.Fatal("user = nil, want the updated profile")
	}
	if user.ID() != sessionUserID || user.UserMetadata()["plan"] != "pro" {
		t.Errorf("user = (%q, %v), want the served id with the plan the update added", user.ID(), user.UserMetadata())
	}
}

func TestUpdateUserRequestShape(t *testing.T) {
	cases := []struct {
		name      string
		update    auth.UserUpdate
		wantQuery string
		wantBody  map[string]any
	}{
		{
			name:     "the zero update sends an empty object",
			update:   auth.UserUpdate{},
			wantBody: map[string]any{},
		},
		{
			name:     "user metadata travels as data",
			update:   auth.UserUpdate{UserMetadata: map[string]any{"plan": "pro"}},
			wantBody: map[string]any{"data": map[string]any{"plan": "pro"}},
		},
		{
			name:      "a new email with a redirect on the query string",
			update:    auth.UserUpdate{Email: "ada@example.org", RedirectTo: "https://app.example.com/account"},
			wantQuery: "redirect_to=" + url.QueryEscape("https://app.example.com/account"),
			wantBody:  map[string]any{"email": "ada@example.org"},
		},
		{
			name:     "a new phone states the sms channel when none is chosen",
			update:   auth.UserUpdate{Phone: "15551234567"},
			wantBody: map[string]any{"phone": "15551234567", "channel": "sms"},
		},
		{
			name:     "a new phone states the channel chosen",
			update:   auth.UserUpdate{Phone: "15551234567", Channel: messaging.ChannelWhatsApp},
			wantBody: map[string]any{"phone": "15551234567", "channel": "whatsapp"},
		},
		{
			name:     "a new password with what the project requires beside it",
			update:   auth.UserUpdate{Password: "new", CurrentPassword: "old", Nonce: "654321"},
			wantBody: map[string]any{"password": "new", "current_password": "old", "nonce": "654321"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newSessionServer(t)
			if _, err := server.client(t).UpdateUser(t.Context(), "user-jwt", testCase.update); err != nil {
				t.Fatalf("UpdateUser: %v", err)
			}
			assertRequestShape(t, server, http.MethodPut, userPath, testCase.wantQuery, "user-jwt", testCase.wantBody)
		})
	}
}

func TestClientUpdateUser(t *testing.T) {
	t.Run("answers the profile as the server now holds it", func(t *testing.T) {
		server := newSessionServer(t)
		server.script(userPath, answer{status: http.StatusOK, body: updatedUserDocument(t)})
		result, err := server.client(t).UpdateUser(t.Context(), "user-jwt", auth.UserUpdate{UserMetadata: map[string]any{"plan": "pro"}})
		if err != nil {
			t.Fatalf("UpdateUser: %v", err)
		}
		assertUpdatedUser(t, result.User())
	})

	t.Run("requires a token before any request", func(t *testing.T) {
		server := newSessionServer(t)
		result, err := server.client(t).UpdateUser(t.Context(), "", auth.UserUpdate{})
		if result != nil || !errors.Is(err, auth.ErrMissingJWT) {
			t.Errorf("UpdateUser = (%v, %v), want ErrMissingJWT", result, err)
		}
		if hits := server.requestsTo(userPath); len(hits) != 0 {
			t.Errorf("user requests = %d, want none", len(hits))
		}
	})

	t.Run("a refused change is the server's error", func(t *testing.T) {
		for _, refusal := range []scriptedError{reauthentication, samePassword} {
			server := newSessionServer(t)
			server.script(userPath, refusal.answer())
			result, err := server.client(t).UpdateUser(t.Context(), "user-jwt", auth.UserUpdate{Password: "new"})
			if result != nil {
				t.Errorf("result = %+v, want nil beside an error", result)
			}
			assertServerError(t, err, refusal)
		}
	})
}

func TestSessionUpdateUserReplacesTheUser(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		recorder := recordEvents(t, session)
		server.script(userPath, answer{status: http.StatusOK, body: updatedUserDocument(t)})

		result, err := session.UpdateUser(t.Context(), auth.UserUpdate{UserMetadata: map[string]any{"plan": "pro"}})
		if err != nil {
			t.Fatalf("UpdateUser: %v", err)
		}
		assertUpdatedUser(t, result.User())
		assertUpdatedUser(t, session.User())
		assertRequestShape(t, server, http.MethodPut, userPath, "", server.mintedTokens()[0], map[string]any{"data": map[string]any{"plan": "pro"}})
		if session.RefreshToken() != mintedRefreshToken(0) {
			t.Errorf("RefreshToken() = %q, want the tokens left as they were", session.RefreshToken())
		}

		assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventUserUpdated)
		if saves := store.savedStates(); len(saves) != 2 {
			t.Fatalf("the store saved %d states, want Persist's and the update's", len(saves))
		}
		var stored struct {
			User struct {
				UserMetadata map[string]any `json:"user_metadata"`
			} `json:"user"`
		}
		if err := json.Unmarshal(store.stored(), &stored); err != nil {
			t.Fatalf("stored state is not JSON: %v", err)
		}
		if stored.User.UserMetadata["plan"] != "pro" {
			t.Errorf("the store holds metadata %v, want the updated profile", stored.User.UserMetadata)
		}
	})
}

func TestSessionUpdateUserAfterTheSessionEndedChangesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := newInProcessSessionServer(t)
		session := server.signIn(t, server.client(t), time.Hour)
		store := &recordingStore{}
		if err := session.Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		recorder := recordEvents(t, session)
		hold := make(chan struct{})
		server.script(userPath, answer{status: http.StatusOK, body: updatedUserDocument(t), hold: hold})

		var result *auth.UpdateUserResult
		var updateErr error
		var group sync.WaitGroup
		group.Go(func() {
			result, updateErr = session.UpdateUser(t.Context(), auth.UserUpdate{UserMetadata: map[string]any{"plan": "pro"}})
		})
		server.awaitRequests(t, userPath, 1)

		if _, err := session.SignOut(t.Context(), signout.WithLocalScope()); err != nil {
			t.Fatalf("SignOut: %v", err)
		}
		close(hold)
		group.Wait()

		if updateErr != nil {
			t.Fatalf("UpdateUser: %v", updateErr)
		}
		assertUpdatedUser(t, result.User())
		assertSignedOut(t, session)
		assertEvents(t, recorder, auth.SessionEventInitialSession, auth.SessionEventSignedOut)
		if saves, deletes := store.savedStates(), store.deleteCount(); len(saves) != 1 || deletes != 1 || store.stored() != nil {
			t.Errorf("the store saw %d saves and %d deletes and holds %s, want Persist's save, the sign-out's delete and nothing after", len(saves), deletes, store.stored())
		}
	})
}

func TestSessionUpdateUserReportsAStoreThatCannotSave(t *testing.T) {
	server := newSessionServer(t)
	session := server.signIn(t, server.client(t), time.Hour)
	store := &recordingStore{}
	if err := session.Persist(t.Context(), store); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	store.saveErr = errors.New("disk full")
	server.script(userPath, answer{status: http.StatusOK, body: updatedUserDocument(t)})

	result, err := session.UpdateUser(t.Context(), auth.UserUpdate{UserMetadata: map[string]any{"plan": "pro"}})
	if result != nil || !errors.Is(err, auth.ErrSessionNotSaved) || !errors.Is(err, store.saveErr) {
		t.Fatalf("UpdateUser = (%v, %v), want ErrSessionNotSaved wrapping the store's error", result, err)
	}
	assertUpdatedUser(t, session.User())
	store.saveErr = nil
	if err := session.Persist(t.Context(), store); err != nil {
		t.Fatalf("Persist once the store recovered: %v", err)
	}
	var stored struct {
		User struct {
			UserMetadata map[string]any `json:"user_metadata"`
		} `json:"user"`
	}
	if err := json.Unmarshal(store.stored(), &stored); err != nil {
		t.Fatalf("stored state is not JSON: %v", err)
	}
	if stored.User.UserMetadata["plan"] != "pro" {
		t.Errorf("the store holds metadata %v after Persist, want the updated profile", stored.User.UserMetadata)
	}
}
