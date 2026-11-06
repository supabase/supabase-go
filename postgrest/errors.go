package postgrest

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/supabase/supabase-go/core/responses"
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

// ErrNoRows is wrapped by the [*Error] that answers [CollectSingle] when the
// query matched no row.
const ErrNoRows = postgrestError("query matched no row")

// ErrTooManyRows is reported by [CollectSingleMaybe] when a read matched more
// than one row, and wrapped by the [*Error] that answers [CollectSingle], or
// a write through [CollectSingleMaybe], when the query matched more than one.
const ErrTooManyRows = postgrestError("query matched more than one row")

// Error is the typed failure returned when PostgREST answers a query with a
// non-2xx status.
//
// Field usefulness typically runs Hint (the database's suggested fix, when it
// knows one), then Code (a stable [PostgREST error code] or
// [Postgres error code] such as "42P01" - branch on this), then Details, then
// Message.
//
// [PostgREST error code]: https://docs.postgrest.org/en/stable/references/errors.html
// [Postgres error code]: https://www.postgresql.org/docs/current/errcodes-appendix.html
type Error struct {
	responses.HTTPError

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
	return "postgrest: " + e.HTTPError.Error()
}

// Unwrap returns the underlying cause when this Error wraps one, and nil
// otherwise, so [errors.Is] and [errors.As] can traverse the chain. The Error
// that answers a singular request with HTTP status 406 and code "PGRST116"
// wraps [ErrNoRows] or [ErrTooManyRows], and every other Error has no
// underlying cause.
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
		return &Error{HTTPError: responses.HTTPError{
			HTTPStatus: httpStatus,
			Message:    string(responseBody),
		}}
	}
	return &Error{
		HTTPError: responses.HTTPError{
			HTTPStatus: httpStatus,
			Code:       parsed.Code,
			Message:    parsed.Message,
		},
		Details: parsed.Details,
		Hint:    parsed.Hint,
	}
}

// singularityCode is the PostgREST error code for a singular request whose
// result held no row or more than one.
const singularityCode = "PGRST116"

// noRows matches the details of the server's singularity error for a result
// that held no row, "0 rows" as whole words, in either wording PostgREST has
// used.
var noRows = regexp.MustCompile(`\b0 rows\b`)

// classifySingular gives a 406 PGRST116 answer to a singular request the
// sentinel it stands for as its cause: [ErrNoRows] when its details report no
// row, and [ErrTooManyRows] otherwise. Any other error is returned as it is.
func classifySingular(err error) error {
	serverError, ok := errors.AsType[*Error](err)
	if !ok || serverError.HTTPStatus != http.StatusNotAcceptable || serverError.Code != singularityCode {
		return err
	}
	if noRows.MatchString(serverError.Details) {
		serverError.cause = ErrNoRows
	} else {
		serverError.cause = ErrTooManyRows
	}
	return err
}
