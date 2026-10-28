package auth_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth"
)

func TestSignInWithPasswordRequestShape(t *testing.T) {
	cases := []struct {
		name        string
		credentials auth.PasswordCredentials
		want        map[string]any
	}{
		{
			name:        "an email sends email and password only",
			credentials: auth.PasswordCredentials{Email: "ada@example.com", Password: "pw"},
			want:        map[string]any{"email": "ada@example.com", "password": "pw"},
		},
		{
			name:        "a phone sends phone and password only",
			credentials: auth.PasswordCredentials{Phone: "15551234567", Password: "pw"},
			want:        map[string]any{"phone": "15551234567", "password": "pw"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newSessionServer(t)
			if _, err := server.client(t).SignInWithPassword(t.Context(), testCase.credentials); err != nil {
				t.Fatalf("SignInWithPassword: %v", err)
			}
			requests := server.requestsTo(tokenPath)
			if len(requests) != 1 {
				t.Fatalf("token requests = %d, want 1", len(requests))
			}
			request := requests[0]
			if request.method != http.MethodPost || request.query != "grant_type=password" {
				t.Errorf("request = %s %s?%s, want POST %s?grant_type=password", request.method, request.path, request.query, tokenPath)
			}
			if request.contentType != "application/json" || request.authorization != "" {
				t.Errorf("request carries Content-Type %q and Authorization %q, want application/json and no bearer", request.contentType, request.authorization)
			}
			var body map[string]any
			if err := json.Unmarshal(request.body, &body); err != nil {
				t.Fatalf("request body is not JSON: %v", err)
			}
			if !reflect.DeepEqual(body, testCase.want) {
				t.Errorf("request body = %v, want %v", body, testCase.want)
			}
		})
	}
}

func TestSignInWithPasswordParsesTheSession(t *testing.T) {
	server := newSessionServer(t)
	session := server.signIn(t, server.client(t), time.Hour)

	assertHoldsMinted(t, server, session, 0)
	user := session.User()
	if user == nil {
		t.Fatal("User() = nil, want the served user")
	}
	if user.Email() != "ada@example.com" || user.Audience() != "authenticated" || user.Role() != "authenticated" {
		t.Errorf("User() = (%q, %q, %q), want the served email, audience and role", user.Email(), user.Audience(), user.Role())
	}
	wantUserMetadata := map[string]any{"display_name": "Ada", "tags": map[string]any{"team": "platform"}}
	if metadata := user.UserMetadata(); !reflect.DeepEqual(metadata, wantUserMetadata) {
		t.Errorf("UserMetadata() = %v, want %v", metadata, wantUserMetadata)
	}
	if metadata := user.AppMetadata(); !reflect.DeepEqual(metadata, map[string]any{"provider": "email"}) {
		t.Errorf("AppMetadata() = %v, want the served provider alone", metadata)
	}
	if hits := server.requestsTo(tokenPath); len(hits) != 1 {
		t.Errorf("token requests = %d, want the sign-in alone, its hour-long token needing no refresh", len(hits))
	}
}

func TestSignInWithPasswordWeakPasswordAdvisory(t *testing.T) {
	t.Run("a response without the advisory gives nil", func(t *testing.T) {
		server := newSessionServer(t)
		result, err := server.client(t).SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: "ada@example.com", Password: "pw"})
		if err != nil {
			t.Fatalf("SignInWithPassword: %v", err)
		}
		if result.WeakPassword() != nil {
			t.Errorf("WeakPassword() = %+v, want nil", result.WeakPassword())
		}
		assertHoldsMinted(t, server, result.Session(), 0)
	})

	t.Run("a response carrying one surfaces it and keeps it out of the store", func(t *testing.T) {
		server := newSessionServer(t)
		var response map[string]any
		if err := json.Unmarshal(server.mint(time.Hour), &response); err != nil {
			t.Fatalf("decoding minted response: %v", err)
		}
		response["weak_password"] = map[string]any{"message": "Password should be at least 12 characters.", "reasons": []string{"length", "characters"}}
		advised, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("encoding advised response: %v", err)
		}
		server.script(tokenPath, answer{status: http.StatusOK, body: string(advised)})

		result, err := server.client(t).SignInWithPassword(t.Context(), auth.PasswordCredentials{Email: "ada@example.com", Password: "pw"})
		if err != nil {
			t.Fatalf("SignInWithPassword: %v", err)
		}
		weak := result.WeakPassword()
		if weak == nil {
			t.Fatal("WeakPassword() = nil, want the advisory")
		}
		if weak.Message() != "Password should be at least 12 characters." {
			t.Errorf("Message() = %q, want the server's message", weak.Message())
		}
		if reasons := weak.Reasons(); !reflect.DeepEqual(reasons, []string{"length", "characters"}) {
			t.Errorf("Reasons() = %v, want [length characters]", reasons)
		}
		assertHoldsMinted(t, server, result.Session(), 0)
		store := &recordingStore{}
		if err := result.Session().Persist(t.Context(), store); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		var stored map[string]any
		if err := json.Unmarshal(store.stored(), &stored); err != nil {
			t.Fatalf("stored state is not JSON: %v", err)
		}
		if fields := slices.Sorted(maps.Keys(stored)); !slices.Equal(fields, []string{"access_token", "expires_at", "refresh_token", "user"}) {
			t.Errorf("stored state carries %v, want the four session fields alone and no weak_password", fields)
		}
	})
}
