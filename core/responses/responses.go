// Package responses provides shared error types.
package responses

import "fmt"

// HTTPError represents the components of an error which relate directly to
// the HTTP response returned by the server as a result of a single request
// sent by this SDK, when that response had a non-2xx status.
type HTTPError struct {
	// HTTPStatus is the HTTP status code of the response, which will never be
	// in the 2xx range.
	HTTPStatus int

	// Code is the stable error code returned by the service if one was returned,
	// otherwise will be empty.
	// Programmatic handling should branch on this.
	//
	// Examples of values found here include:
	//   - PostgREST: "PGRST116"
	//   - Postgres: "42P01"
	//   - Auth: "bad_jwt"
	Code string

	// Message is the human-readable summary of the failure.
	Message string
}

// Error renders the failure as "<message> (code <code>, HTTP <status>)",
// omitting the code clause when the server supplied no code. Module error
// types embedding HTTPError shadow this method to prepend their package
// prefix.
func (e HTTPError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("%s (HTTP %d)", e.Message, e.HTTPStatus)
	}
	return fmt.Sprintf("%s (code %s, HTTP %d)", e.Message, e.Code, e.HTTPStatus)
}
