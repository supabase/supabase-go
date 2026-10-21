package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/supabase/supabase-go/auth"
)

func TestGenerateLinkRequestShape(t *testing.T) {
	cases := []struct {
		name    string
		request auth.LinkRequest
		want    map[string]any
	}{
		{name: "signup carries password, metadata and redirect", request: auth.NewLinkRequest("ada@example.com").RedirectTo("https://app.example.com/welcome").Signup("correct horse battery staple").UserMetadata(map[string]any{"plan": "pro"}), want: map[string]any{"type": "signup", "email": "ada@example.com", "password": "correct horse battery staple", "data": map[string]any{"plan": "pro"}, "redirect_to": "https://app.example.com/welcome"}},
		{name: "invite needs the email alone", request: auth.NewLinkRequest("ada@example.com").Invite(), want: map[string]any{"type": "invite", "email": "ada@example.com"}},
		{name: "magiclink accepts metadata for the user it may create", request: auth.NewLinkRequest("ada@example.com").MagicLink().UserMetadata(map[string]any{"plan": "pro"}), want: map[string]any{"type": "magiclink", "email": "ada@example.com", "data": map[string]any{"plan": "pro"}}},
		{name: "recovery needs the email alone", request: auth.NewLinkRequest("ada@example.com").Recovery(), want: map[string]any{"type": "recovery", "email": "ada@example.com"}},
		{name: "email change to the current address carries the new email", request: auth.NewLinkRequest("ada@example.com").EmailChangeToCurrent("ada@new.example.com"), want: map[string]any{"type": "email_change_current", "email": "ada@example.com", "new_email": "ada@new.example.com"}},
		{name: "email change to the new address carries it too", request: auth.NewLinkRequest("ada@example.com").EmailChangeToNew("ada@new.example.com"), want: map[string]any{"type": "email_change_new", "email": "ada@example.com", "new_email": "ada@new.example.com"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			admin := recorder.admin(t)

			if _, err := admin.GenerateLink(context.Background(), testCase.request); err != nil {
				t.Fatalf("GenerateLink: %v", err)
			}
			if len(recorder.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(recorder.requests))
			}
			request := recorder.requests[0]
			if request.method != http.MethodPost || request.path != "/auth/v1/admin/generate_link" {
				t.Errorf("request = %s %s, want POST of the generate_link path", request.method, request.path)
			}
			var sent map[string]any
			if err := json.Unmarshal(request.body, &sent); err != nil {
				t.Fatalf("request body %q: %v", request.body, err)
			}
			if !reflect.DeepEqual(sent, testCase.want) {
				t.Errorf("body = %v, want %v", sent, testCase.want)
			}
		})
	}
}

// TestLinkRequestForksDoNotAlias proves the builder is a value: a shared base
// forks into two independent requests, and metadata is cloned when set, so a
// caller's later map writes never reach a request already built.
func TestLinkRequestForksDoNotAlias(t *testing.T) {
	recorder := newAdminServer(t)
	admin := recorder.admin(t)

	base := auth.NewLinkRequest("ada@example.com").RedirectTo("https://app.example.com/welcome")
	metadata := map[string]any{"plan": "pro"}
	signup := base.Signup("correct horse battery staple").UserMetadata(metadata)
	metadata["plan"] = "mutated after the request was built"
	invite := base.Invite()

	for _, request := range []auth.LinkRequest{signup, invite} {
		if _, err := admin.GenerateLink(context.Background(), request); err != nil {
			t.Fatalf("GenerateLink: %v", err)
		}
	}
	if len(recorder.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(recorder.requests))
	}

	var signupSent, inviteSent map[string]any
	if err := json.Unmarshal(recorder.requests[0].body, &signupSent); err != nil {
		t.Fatalf("signup body: %v", err)
	}
	if err := json.Unmarshal(recorder.requests[1].body, &inviteSent); err != nil {
		t.Fatalf("invite body: %v", err)
	}
	wantSignup := map[string]any{"type": "signup", "email": "ada@example.com", "password": "correct horse battery staple", "data": map[string]any{"plan": "pro"}, "redirect_to": "https://app.example.com/welcome"}
	if !reflect.DeepEqual(signupSent, wantSignup) {
		t.Errorf("signup body = %v, want %v - the cloned metadata must predate the mutation", signupSent, wantSignup)
	}
	wantInvite := map[string]any{"type": "invite", "email": "ada@example.com", "redirect_to": "https://app.example.com/welcome"}
	if !reflect.DeepEqual(inviteSent, wantInvite) {
		t.Errorf("invite body = %v, want %v - the fork must not inherit signup's refinements", inviteSent, wantInvite)
	}
}

func TestGenerateLinkResultParsing(t *testing.T) {
	recorder := newAdminServer(t)
	recorder.body = `{
		"action_link": "http://stack.local/verify?token=hashed-token-value&type=signup&redirect_to=https://app.example.com/welcome",
		"email_otp": "123456",
		"hashed_token": "hashed-token-value",
		"redirect_to": "https://app.example.com/welcome",
		"verification_type": "signup",
		"id": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
		"email": "ada@example.com",
		"aud": "authenticated"
	}`
	admin := recorder.admin(t)

	result, err := admin.GenerateLink(context.Background(), auth.NewLinkRequest("ada@example.com").Signup("correct horse battery staple"))
	if err != nil {
		t.Fatalf("GenerateLink: %v", err)
	}
	if result.ActionLink() != "http://stack.local/verify?token=hashed-token-value&type=signup&redirect_to=https://app.example.com/welcome" {
		t.Errorf("ActionLink = %q, want the wire's action_link", result.ActionLink())
	}
	if result.EmailOTP() != "123456" {
		t.Errorf("EmailOTP = %q, want 123456", result.EmailOTP())
	}
	if result.HashedToken() != "hashed-token-value" {
		t.Errorf("HashedToken = %q, want the wire's hashed_token", result.HashedToken())
	}
	if result.RedirectTo() != "https://app.example.com/welcome" {
		t.Errorf("RedirectTo = %q, want the wire's redirect_to", result.RedirectTo())
	}
	if result.VerificationType() != "signup" {
		t.Errorf("VerificationType = %q, want signup", result.VerificationType())
	}
	user := result.User()
	if user.ID() != "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d" || user.Email() != "ada@example.com" {
		t.Errorf("User = (%q, %q), want the flattened user's id and email", user.ID(), user.Email())
	}
}
