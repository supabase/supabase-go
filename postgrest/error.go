package postgrest

import (
	"encoding/json"
	"fmt"
)

// postgrestError is a string-backed error type whose values can be declared as
// compile-time constants, following the same immutable-sentinel pattern as the
// configuration package: the compiler rejects any attempt to reassign them.
type postgrestError string

func (e postgrestError) Error() string { return "postgrest: " + string(e) }

// ErrMissingTable is reported by [FilterBuilder.Execute] when the builder was
// created with an empty table name. Match it with [errors.Is].
const ErrMissingTable = postgrestError("table name is required")

// Error is the typed failure returned when PostgREST answers a query with a
// non-2xx status. Match it with [errors.As]:
//
//	var postgrestError *postgrest.Error
//	if errors.As(err, &postgrestError) {
//		// branch on postgrestError.Code, not on message text
//	}
//
// Field usefulness typically runs Hint (the database's suggested fix, when it
// knows one), then Code (a stable PostgREST or Postgres code such as "42P01" -
// branch on this), then Details, then Message.
type Error struct {
	// HTTPStatus is the HTTP status code of the PostgREST response.
	HTTPStatus int

	// Code is the stable PostgREST (for example "PGRST116") or Postgres (for
	// example "42P01") error code. Programmatic handling should branch on this.
	Code string

	// Message is the human-readable summary of the failure.
	Message string

	// Details carries extra context from the database, often the offending
	// value, key or row. Empty when the server supplied none.
	Details string

	// Hint is actionable guidance from the database when available, often the
	// literal fix. Empty when the server supplied none.
	Hint string

	// cause is the underlying error when this Error wraps one; see Unwrap.
	cause error
}

// Error renders the failure as "postgrest: <message> (code <code>, HTTP
// <status>)", omitting the code clause when the server supplied no code.
func (e *Error) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("postgrest: %s (HTTP %d)", e.Message, e.HTTPStatus)
	}
	return fmt.Sprintf("postgrest: %s (code %s, HTTP %d)", e.Message, e.Code, e.HTTPStatus)
}

// Unwrap returns the underlying cause when this Error wraps one, and nil
// otherwise, so [errors.Is] and [errors.As] can traverse the chain. Errors
// parsed from a PostgREST response body have no underlying cause.
func (e *Error) Unwrap() error {
	return e.cause
}

// errorBody is the JSON shape PostgREST uses for error responses.
type errorBody struct {
	Message string `json:"message"`
	Code    string `json:"code"`
	Details string `json:"details"`
	Hint    string `json:"hint"`
}

// newError builds an *Error from a non-2xx response. A body that is not the
// documented PostgREST error JSON (for example, HTML from an intermediary) is
// preserved raw in Message so no diagnostic information is lost.
func newError(httpStatus int, responseBody []byte) *Error {
	var parsed errorBody
	if err := json.Unmarshal(responseBody, &parsed); err != nil || parsed.Message == "" {
		return &Error{
			HTTPStatus: httpStatus,
			Message:    string(responseBody),
		}
	}
	return &Error{
		HTTPStatus: httpStatus,
		Code:       parsed.Code,
		Message:    parsed.Message,
		Details:    parsed.Details,
		Hint:       parsed.Hint,
	}
}
