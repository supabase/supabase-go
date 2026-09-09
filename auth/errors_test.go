package auth

import (
	"errors"
	"testing"
)

func TestErrorMessage(t *testing.T) {
	withCode := &Error{HTTPStatus: 401, Code: "bad_jwt", Message: "invalid token"}
	if got, want := withCode.Error(), "auth: invalid token (code bad_jwt, HTTP 401)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	withoutCode := &Error{HTTPStatus: 500, Message: "boom"}
	if got, want := withoutCode.Error(), "auth: boom (HTTP 500)"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestNewError(t *testing.T) {
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
			err := newError(400, []byte(testCase.body))
			if err.Code != testCase.wantCode {
				t.Errorf("Code = %q, want %q", err.Code, testCase.wantCode)
			}
			if err.Message != testCase.wantMessage {
				t.Errorf("Message = %q, want %q", err.Message, testCase.wantMessage)
			}
			if err.HTTPStatus != 400 {
				t.Errorf("HTTPStatus = %d, want 400", err.HTTPStatus)
			}
		})
	}
}

func TestSentinelsAreDistinct(t *testing.T) {
	all := []error{ErrMissingJWT, ErrMalformedJWT, ErrExpiredJWT, ErrInvalidSignature}
	for i := range all {
		for j := range all {
			if i != j && errors.Is(all[i], all[j]) {
				t.Errorf("%v and %v compare equal, want distinct", all[i], all[j])
			}
		}
	}
}
