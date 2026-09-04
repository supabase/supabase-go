package postgrest

import (
	"encoding/json"
	"fmt"
)

// postgrestError is a string-backed error type. Its values can be declared as
// compile-time constants, are matched with [errors.Is] and cannot be minted
// outside this package.
type postgrestError string

func (e postgrestError) Error() string { return "postgrest: " + string(e) }

// ErrMissingTable is reported by the executing read function, such as
// [Collect], when the builder was created with an empty table name.
const ErrMissingTable = postgrestError("table name is required")

// ErrMissingClient is reported by the executing read function, such as
// [Collect], when the supplied client is nil.
const ErrMissingClient = postgrestError("client is required")

// ErrMissingFunction is reported by an executing function, such as [Execute] or
// [CollectRaw], when an [RPC] or [RPCVoid] call was created with an empty
// function name.
const ErrMissingFunction = postgrestError("function name is required")

// ErrMissingAccessToken is reported by an executing function, such as
// [Collect] or [Execute], when the provider attached by
// [Client.WithAccessTokenProvider] or the [WithAccessTokenProvider] option
// is nil or resolves to an empty token, at the call's first resolution or
// on a renewal re-ask after the server rejected the sent token.
const ErrMissingAccessToken = postgrestError("access token is required")

// ErrTooManyRows is reported by [CollectSingleMaybe] when the query matched
// more than one row.
const ErrTooManyRows = postgrestError("query matched more than one row")

// Error is the typed failure returned when PostgREST answers a query with a
// non-2xx status.
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
	// value, key, or row. Empty when the server supplied none.
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
