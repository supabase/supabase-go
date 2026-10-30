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

// ErrMissingJWT is reported by [Client.GetClaims], [Client.GetUser],
// [Client.SessionFromTokens] and [Admin.SignOut] when the supplied token is
// empty.
const ErrMissingJWT = authError("JWT is required")

// ErrMissingRefreshToken is reported by [Client.SessionFromTokens] when the
// supplied refresh token is empty, before any request is sent.
const ErrMissingRefreshToken = authError("refresh token is required")

// ErrMissingStore is reported by [Session.Persist] and
// [Client.SessionFromStore] when the given store is nil.
const ErrMissingStore = authError("session store is required")

// ErrNoStoredSession is reported by [Client.SessionFromStore] when the store
// holds nothing.
const ErrNoStoredSession = authError("no stored session")

// ErrSignedOut is reported by every operation on a [Session] that
// [Session.SignOut] has ended, and by every operation on the zero Session.
const ErrSignedOut = authError("session is signed out")

// ErrSessionExpired is reported by every operation on a [Session] whose
// access token expired after the server had refused to renew it, and by
// [Client.SessionFromStore] when the stored session can no longer be
// refreshed. It is always returned wrapping the server's rejection, an
// [*Error] carrying the code the server answered the refresh with.
const ErrSessionExpired = authError("session has expired")

// ErrMalformedJWT is reported by [Client.GetClaims] and
// [Client.SessionFromTokens] when the supplied token is not a three-part
// base64url JWT carrying a JSON header and JSON claims.
const ErrMalformedJWT = authError("JWT is malformed")

// ErrExpiredJWT is reported by [Client.GetClaims] when the token's exp claim is
// absent or in the past.
const ErrExpiredJWT = authError("JWT has expired")

// ErrInvalidSignature is reported by [Client.GetClaims] when the token's
// signature does not verify against the project's signing key.
const ErrInvalidSignature = authError("JWT signature is invalid")

// ErrInvalidUserID is reported by the [Admin] user operations when the given
// user id is not a UUID in its canonical hyphenated form - groups of 8, 4, 4,
// 4 and 12 hexadecimal digits, in either case - before any request is sent.
const ErrInvalidUserID = authError("user ID is not a canonical UUID")

// ErrInvalidFactorID is reported by the [Admin] MFA factor operations when the
// given factor id is not a UUID in its canonical hyphenated form, as
// [ErrInvalidUserID] describes for user ids, before any request is sent.
const ErrInvalidFactorID = authError("factor ID is not a canonical UUID")

// ErrMissingIdentifier is reported by the [Admin] custom provider operations
// that address one provider when the given identifier is empty, before any
// request is sent: an empty identifier would change the shape of the request
// path and reach a different route entirely.
const ErrMissingIdentifier = authError("provider identifier is required")

// Error is the typed failure returned when the Auth server answers a request
// with a non-2xx status, carrying the server's reported [error code] and message
// alongside the HTTP status. [Client.GetUser] returns it directly, and
// [Client.GetClaims] returns it when a token routes to server verification and
// the server rejects it.
//
// [error code]: https://supabase.com/docs/guides/auth/debugging/error-codes
type Error struct {
	responses.HTTPError
}

// Error renders the failure as "auth: <message> (code <code>, HTTP <status>)",
// omitting the code clause when the server supplied no code.
func (e *Error) Error() string {
	return "auth: " + e.HTTPError.Error()
}

// errorBody is the JSON shape of the Auth server's error responses: code and
// message under the API version every request states, or error and
// error_description for the rejections the server shapes as OAuth 2 errors.
type errorBody struct {
	Code             string `json:"code"`
	Message          string `json:"message"`
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

	code := parsed.Code
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
