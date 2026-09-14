package auth

import (
	"encoding/json"

	"github.com/supabase/supabase-go/core/responses"
)

// authError is a string-backed error type. Its values can be declared as
// compile-time constants, are matched with [errors.Is] and cannot be minted
// outside this package.
type authError string

func (e authError) Error() string { return "auth: " + string(e) }

// ErrMissingJWT is reported by [Client.GetClaims] and [Client.GetUser] when the
// supplied token is empty.
const ErrMissingJWT = authError("JWT is required")

// ErrMalformedJWT is reported by [Client.GetClaims] when the supplied token is
// not a three-part base64url JWT carrying a JSON header and JSON claims.
const ErrMalformedJWT = authError("JWT is malformed")

// ErrExpiredJWT is reported by [Client.GetClaims] when the token's exp claim is
// absent or in the past.
const ErrExpiredJWT = authError("JWT has expired")

// ErrInvalidSignature is reported by [Client.GetClaims] when the token's
// signature does not verify against the project's signing key.
const ErrInvalidSignature = authError("JWT signature is invalid")

// Error is the typed failure returned when the Auth server answers a request
// with a non-2xx status, carrying the server's reported code and message
// alongside the HTTP status. [Client.GetUser] returns it directly, and
// [Client.GetClaims] returns it when a token routes to server verification and
// the server rejects it.
type Error struct {
	responses.HTTPError
}

// Error renders the failure as "auth: <message> (code <code>, HTTP <status>)",
// omitting the code clause when the server supplied no code.
func (e *Error) Error() string {
	return "auth: " + e.HTTPError.Error()
}

// errorBody is the JSON shape the Auth server uses for error responses. Recent
// servers send error_code and msg; older ones send error and
// error_description. Both are parsed so the richer field wins.
type errorBody struct {
	ErrorCode        string `json:"error_code"`
	Message          string `json:"msg"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// newError builds an [*Error] from a non-2xx Auth response. A body that is not
// the documented error JSON (for example, HTML from an intermediary) is
// preserved raw in Message so no diagnostic information is lost.
func newError(httpStatus int, responseBody []byte) *Error {
	var parsed errorBody
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return &Error{responses.HTTPError{
			HTTPStatus: httpStatus,
			Message:    string(responseBody),
		}}
	}

	code := parsed.ErrorCode
	if code == "" {
		code = parsed.Error
	}
	message := parsed.Message
	if message == "" {
		message = parsed.ErrorDescription
	}
	if message == "" {
		message = string(responseBody)
	}
	return &Error{responses.HTTPError{
		HTTPStatus: httpStatus,
		Code:       code,
		Message:    message,
	}}
}
