package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/messaging"
	"github.com/supabase/supabase-go/auth/signup"
)

func TestSignUpRequestShape(t *testing.T) {
	metadata := map[string]any{"display_name": "Ada"}
	cases := []struct {
		name      string
		signUp    func(ctx context.Context, client *auth.Client) (*auth.SignUpResult, error)
		wantQuery string
		wantBody  map[string]any
	}{
		{
			name: "an email sends email and password only",
			signUp: func(ctx context.Context, client *auth.Client) (*auth.SignUpResult, error) {
				return client.SignUpByEmail(ctx, "ada@example.com", "pw")
			},
			wantBody: map[string]any{"email": "ada@example.com", "password": "pw"},
		},
		{
			name: "user metadata travels as data",
			signUp: func(ctx context.Context, client *auth.Client) (*auth.SignUpResult, error) {
				return client.SignUpByEmail(ctx, "ada@example.com", "pw", signup.WithUserMetadata(metadata))
			},
			wantBody: map[string]any{"email": "ada@example.com", "password": "pw", "data": map[string]any{"display_name": "Ada"}},
		},
		{
			name: "a redirect lands on the query string, not in the body",
			signUp: func(ctx context.Context, client *auth.Client) (*auth.SignUpResult, error) {
				return client.SignUpByEmail(ctx, "ada@example.com", "pw", signup.WithRedirectTo("https://app.example.com/welcome"))
			},
			wantQuery: "redirect_to=" + url.QueryEscape("https://app.example.com/welcome"),
			wantBody:  map[string]any{"email": "ada@example.com", "password": "pw"},
		},
		{
			name: "of two redirects the later wins",
			signUp: func(ctx context.Context, client *auth.Client) (*auth.SignUpResult, error) {
				return client.SignUpByEmail(ctx, "ada@example.com", "pw", signup.WithRedirectTo("https://app.example.com/start"), signup.WithRedirectTo("https://app.example.com/welcome"))
			},
			wantQuery: "redirect_to=" + url.QueryEscape("https://app.example.com/welcome"),
			wantBody:  map[string]any{"email": "ada@example.com", "password": "pw"},
		},
		{
			name: "a phone states the sms channel when none is chosen",
			signUp: func(ctx context.Context, client *auth.Client) (*auth.SignUpResult, error) {
				return client.SignUpByPhone(ctx, "15551234567", "pw")
			},
			wantBody: map[string]any{"phone": "15551234567", "password": "pw", "channel": "sms"},
		},
		{
			name: "a phone states the channel chosen",
			signUp: func(ctx context.Context, client *auth.Client) (*auth.SignUpResult, error) {
				return client.SignUpByPhone(ctx, "15551234567", "pw", signup.WithChannel(messaging.ChannelWhatsApp), signup.WithUserMetadata(metadata))
			},
			wantBody: map[string]any{"phone": "15551234567", "password": "pw", "channel": "whatsapp", "data": map[string]any{"display_name": "Ada"}},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newSessionServer(t)
			if _, err := testCase.signUp(t.Context(), server.client(t)); err != nil {
				t.Fatalf("sign-up: %v", err)
			}
			assertRequestShape(t, server, http.MethodPost, signupPath, testCase.wantQuery, "", testCase.wantBody)
		})
	}
}

func TestSignUpRequiresEmailOrPhone(t *testing.T) {
	server := newSessionServer(t)
	client := server.client(t)
	byEmail, emailErr := client.SignUpByEmail(t.Context(), "", "pw")
	if byEmail != nil || !errors.Is(emailErr, auth.ErrMissingEmailOrPhone) {
		t.Errorf("SignUpByEmail with no address = (%v, %v), want ErrMissingEmailOrPhone", byEmail, emailErr)
	}
	byPhone, phoneErr := client.SignUpByPhone(t.Context(), "", "pw")
	if byPhone != nil || !errors.Is(phoneErr, auth.ErrMissingEmailOrPhone) {
		t.Errorf("SignUpByPhone with no number = (%v, %v), want ErrMissingEmailOrPhone", byPhone, phoneErr)
	}
	if hits := server.requestsTo(signupPath); len(hits) != 0 {
		t.Errorf("signup requests = %d, want none", len(hits))
	}
}

func TestSignUpParsesTheAnswer(t *testing.T) {
	t.Run("a confirmed account carries its session", func(t *testing.T) {
		server := newSessionServer(t)
		result, err := server.client(t).SignUpByEmail(t.Context(), "ada@example.com", "pw")
		if err != nil {
			t.Fatalf("SignUpByEmail: %v", err)
		}
		if result.Session() == nil {
			t.Fatal("Session() = nil, want the session the server answered with")
		}
		assertHoldsMinted(t, server, result.Session(), 0)
		if user := result.User(); user == nil || user.ID() != sessionUserID {
			t.Errorf("User() = %+v, want the served user", user)
		}
	})

	t.Run("a pending confirmation carries the user and no session", func(t *testing.T) {
		server := newSessionServer(t)
		server.script(signupPath, answer{status: http.StatusOK, body: sessionUserDocument})
		result, err := server.client(t).SignUpByEmail(t.Context(), "ada@example.com", "pw")
		if err != nil {
			t.Fatalf("SignUpByEmail: %v", err)
		}
		if result.Session() != nil {
			t.Errorf("Session() = %+v, want nil while the confirmation is pending", result.Session())
		}
		if user := result.User(); user == nil || user.ID() != sessionUserID || user.Email() != "ada@example.com" {
			t.Errorf("User() = %+v, want the served user", user)
		}
		if hits := server.requestsTo(tokenPath); len(hits) != 0 {
			t.Errorf("token requests = %d, want none: a pending confirmation is not a sign-in", len(hits))
		}
	})

	t.Run("a rejection is the server's error", func(t *testing.T) {
		server := newSessionServer(t)
		server.script(signupPath, weakPassword.answer())
		result, err := server.client(t).SignUpByEmail(t.Context(), "ada@example.com", "pw")
		if result != nil {
			t.Errorf("result = %+v, want nil beside an error", result)
		}
		assertServerError(t, err, weakPassword)
	})
}
