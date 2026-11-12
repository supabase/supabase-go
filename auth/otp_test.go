package auth_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/messaging"
	"github.com/supabase/supabase-go/auth/otp"
)

func TestSignInWithOTPRequestShape(t *testing.T) {
	metadata := map[string]any{"display_name": "Ada"}
	cases := []struct {
		name      string
		send      func(ctx context.Context, client *auth.Client) (*auth.SignInWithOTPResult, error)
		wantQuery string
		wantBody  map[string]any
	}{
		{
			name: "an email states that an account may be created",
			send: func(ctx context.Context, client *auth.Client) (*auth.SignInWithOTPResult, error) {
				return client.SignInWithOTPByEmail(ctx, "ada@example.com")
			},
			wantBody: map[string]any{"email": "ada@example.com", "create_user": true},
		},
		{
			name: "a send without user creation states create_user false",
			send: func(ctx context.Context, client *auth.Client) (*auth.SignInWithOTPResult, error) {
				return client.SignInWithOTPByEmail(ctx, "ada@example.com", otp.WithoutUserCreation())
			},
			wantBody: map[string]any{"email": "ada@example.com", "create_user": false},
		},
		{
			name: "user metadata travels as data",
			send: func(ctx context.Context, client *auth.Client) (*auth.SignInWithOTPResult, error) {
				return client.SignInWithOTPByEmail(ctx, "ada@example.com", otp.WithUserMetadata(metadata))
			},
			wantBody: map[string]any{"email": "ada@example.com", "create_user": true, "data": map[string]any{"display_name": "Ada"}},
		},
		{
			name: "a redirect lands on the query string, not in the body",
			send: func(ctx context.Context, client *auth.Client) (*auth.SignInWithOTPResult, error) {
				return client.SignInWithOTPByEmail(ctx, "ada@example.com", otp.WithRedirectTo("https://app.example.com/welcome"))
			},
			wantQuery: "redirect_to=" + url.QueryEscape("https://app.example.com/welcome"),
			wantBody:  map[string]any{"email": "ada@example.com", "create_user": true},
		},
		{
			name: "a phone states the sms channel when none is chosen",
			send: func(ctx context.Context, client *auth.Client) (*auth.SignInWithOTPResult, error) {
				return client.SignInWithOTPByPhone(ctx, "15551234567")
			},
			wantBody: map[string]any{"phone": "15551234567", "create_user": true, "channel": "sms"},
		},
		{
			name: "a phone states the channel chosen",
			send: func(ctx context.Context, client *auth.Client) (*auth.SignInWithOTPResult, error) {
				return client.SignInWithOTPByPhone(ctx, "15551234567", otp.WithChannel(messaging.ChannelWhatsApp), otp.WithoutUserCreation(), otp.WithUserMetadata(metadata))
			},
			wantBody: map[string]any{"phone": "15551234567", "create_user": false, "channel": "whatsapp", "data": map[string]any{"display_name": "Ada"}},
		},
		{
			name: "of two channels the later wins",
			send: func(ctx context.Context, client *auth.Client) (*auth.SignInWithOTPResult, error) {
				return client.SignInWithOTPByPhone(ctx, "15551234567", otp.WithChannel(messaging.ChannelWhatsApp), otp.WithChannel(messaging.ChannelSMS))
			},
			wantBody: map[string]any{"phone": "15551234567", "create_user": true, "channel": "sms"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newSessionServer(t)
			if _, err := testCase.send(t.Context(), server.client(t)); err != nil {
				t.Fatalf("send: %v", err)
			}
			assertRequestShape(t, server, http.MethodPost, otpPath, testCase.wantQuery, "", testCase.wantBody)
		})
	}
}

func TestSignInWithOTPAnswers(t *testing.T) {
	t.Run("an email send answers an empty result", func(t *testing.T) {
		server := newSessionServer(t)
		result, err := server.client(t).SignInWithOTPByEmail(t.Context(), "ada@example.com")
		if err != nil || result == nil {
			t.Errorf("SignInWithOTPByEmail = (%v, %v), want the empty result", result, err)
		}
	})

	t.Run("a phone send answers an empty result", func(t *testing.T) {
		server := newSessionServer(t)
		server.script(otpPath, answer{status: http.StatusOK, body: `{"message_id":"test-otp"}`})
		result, err := server.client(t).SignInWithOTPByPhone(t.Context(), "15551234567")
		if err != nil || result == nil {
			t.Errorf("SignInWithOTPByPhone = (%v, %v), want the empty result", result, err)
		}
	})

	t.Run("an address with no account is the server's error without user creation", func(t *testing.T) {
		server := newSessionServer(t)
		server.script(otpPath, otpDisabled.answer())
		result, err := server.client(t).SignInWithOTPByEmail(t.Context(), "nobody@example.com", otp.WithoutUserCreation())
		if result != nil {
			t.Errorf("result = %+v, want nil beside an error", result)
		}
		assertServerError(t, err, otpDisabled)
	})
}

func TestVerifyOTPRequestShape(t *testing.T) {
	cases := []struct {
		name     string
		verify   func(ctx context.Context, client *auth.Client) (*auth.VerifyOTPResult, error)
		wantBody map[string]any
	}{
		{
			name: "a code sent to an email",
			verify: func(ctx context.Context, client *auth.Client) (*auth.VerifyOTPResult, error) {
				return client.VerifyOTPByEmail(ctx, "ada@example.com", "123456", otp.TypeEmail)
			},
			wantBody: map[string]any{"email": "ada@example.com", "token": "123456", "type": "email"},
		},
		{
			name: "a code sent to a phone",
			verify: func(ctx context.Context, client *auth.Client) (*auth.VerifyOTPResult, error) {
				return client.VerifyOTPByPhone(ctx, "15551234567", "123456", otp.TypeSMS)
			},
			wantBody: map[string]any{"phone": "15551234567", "token": "123456", "type": "sms"},
		},
		{
			name: "a token hash on its own",
			verify: func(ctx context.Context, client *auth.Client) (*auth.VerifyOTPResult, error) {
				return client.VerifyOTPByTokenHash(ctx, "hashed-token", otp.TypeRecovery)
			},
			wantBody: map[string]any{"token_hash": "hashed-token", "type": "recovery"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newSessionServer(t)
			if _, err := testCase.verify(t.Context(), server.client(t)); err != nil {
				t.Fatalf("verify: %v", err)
			}
			assertRequestShape(t, server, http.MethodPost, verifyPath, "", "", testCase.wantBody)
		})
	}
}

func TestVerifyOTPAnswers(t *testing.T) {
	t.Run("a redeemed code signs its user in", func(t *testing.T) {
		server := newSessionServer(t)
		result, err := server.client(t).VerifyOTPByEmail(t.Context(), "ada@example.com", "123456", otp.TypeEmail)
		if err != nil {
			t.Fatalf("VerifyOTPByEmail: %v", err)
		}
		if result.Session() == nil {
			t.Fatal("Session() = nil, want the session the server answered with")
		}
		assertHoldsMinted(t, server, result.Session(), 0)
	})

	t.Run("a confirmation accepted without a sign-in carries no session", func(t *testing.T) {
		server := newSessionServer(t)
		server.script(verifyPath, answer{status: http.StatusOK, body: `{"msg":"Confirmation link accepted. Please proceed to confirm link sent to the other email","code":"200"}`})
		result, err := server.client(t).VerifyOTPByTokenHash(t.Context(), "hash", otp.TypeEmailChange)
		if err != nil {
			t.Fatalf("VerifyOTPByTokenHash: %v", err)
		}
		if result.Session() != nil {
			t.Errorf("Session() = %+v, want nil for the first of two confirmations", result.Session())
		}
	})

	t.Run("a wrong or expired code is the server's error", func(t *testing.T) {
		server := newSessionServer(t)
		server.script(verifyPath, otpExpired.answer())
		result, err := server.client(t).VerifyOTPByEmail(t.Context(), "ada@example.com", "123456", otp.TypeEmail)
		if result != nil {
			t.Errorf("result = %+v, want nil beside an error", result)
		}
		assertServerError(t, err, otpExpired)
	})
}
