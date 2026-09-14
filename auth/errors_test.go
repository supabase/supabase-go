package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/core/responses"
)

func TestErrorMessage(t *testing.T) {
	withCode := &auth.Error{HTTPError: responses.HTTPError{HTTPStatus: 401, Code: "bad_jwt", Message: "invalid token"}}
	if got, want := withCode.Error(), "auth: invalid token (code bad_jwt, HTTP 401)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	withoutCode := &auth.Error{HTTPError: responses.HTTPError{HTTPStatus: 500, Message: "boom"}}
	if got, want := withoutCode.Error(), "auth: boom (HTTP 500)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestErrorShapesFromServer drives the Auth server's error-response shapes
// through GetUser and asserts how each parses into [*auth.Error].
func TestErrorShapesFromServer(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantCode    string
		wantMessage string
	}{
		{"recent shape", `{"error_code":"bad_jwt","msg":"invalid token"}`, "bad_jwt", "invalid token"},
		{"legacy shape", `{"error":"invalid_grant","error_description":"bad grant"}`, "invalid_grant", "bad grant"},
		{"non-json preserved", `<html>502</html>`, "", "<html>502</html>"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mock := newMockAuthServer(t)
			mock.userStatus = 400
			mock.userBody = testCase.body
			client := mock.client(t)

			_, err := client.GetUser(context.Background(), "any-token")
			var serverError *auth.Error
			if !errors.As(err, &serverError) {
				t.Fatalf("GetUser error = %v, want *auth.Error", err)
			}
			if serverError.Code != testCase.wantCode {
				t.Errorf("Code = %q, want %q", serverError.Code, testCase.wantCode)
			}
			if serverError.Message != testCase.wantMessage {
				t.Errorf("Message = %q, want %q", serverError.Message, testCase.wantMessage)
			}
			if serverError.HTTPStatus != 400 {
				t.Errorf("HTTPStatus = %d, want 400", serverError.HTTPStatus)
			}
		})
	}
}

func TestSentinelsAreDistinct(t *testing.T) {
	all := []error{auth.ErrMissingJWT, auth.ErrMalformedJWT, auth.ErrExpiredJWT, auth.ErrInvalidSignature}
	for i := range all {
		for j := range all {
			if i != j && errors.Is(all[i], all[j]) {
				t.Errorf("%v and %v compare equal, want distinct", all[i], all[j])
			}
		}
	}
}
