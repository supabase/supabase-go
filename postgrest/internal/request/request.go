// Package request holds the immutable HTTP request model shared by the
// postgrest builders. Every method on a Request either reads state or
// returns a new independent Request; no method mutates its receiver.
package request

import (
	"context"
	"net/http"
	"net/url"
	"slices"
)

// parameter is one key/value pair of a request's query string.
type parameter struct {
	key   string
	value string
}

// Request describes a single PostgREST HTTP request. The zero value is not
// useful so you must construct instances with New. A Request is immutable:
// With* methods return a new independent value and the receiver is never
// changed, so Requests may be freely copied, forked and shared between goroutines.
type Request struct {
	method     string
	path       string
	parameters []parameter
}

// New returns a Request for the given HTTP method and relation path (a table
// or view name). The path is not escaped here, so it must be escaped on query
// assembly.
func New(method, path string) Request {
	return Request{method: method, path: path}
}

// Path returns the relation path the Request targets. It is not escaped, so
// it must be escaped on query assembly.
func (r Request) Path() string {
	return r.path
}

// WithParameter returns a new Request with the given query-string pair appended.
// Keys may repeat, and insertion order is preserved: a query string is an ordered
// multimap. This is important because PostgREST assigns meaning to repeated keys
// (age=gte.18&age=lte.65 filters one column twice, combined with AND).
func (r Request) WithParameter(key, value string) Request {
	clone := r
	clone.parameters = append(slices.Clone(r.parameters), parameter{key: key, value: value})
	return clone
}

// HTTPRequest assembles the Request into an *http.Request against the given
// base URL, carrying ctx. The base is not mutated. The Accept header is set
// for JSON. Authentication headers are not injected here (they are injected later
// by the configured transport).
func (r Request) HTTPRequest(ctx context.Context, base *url.URL) (*http.Request, error) {
	target := base.JoinPath(r.path)

	queryValues := url.Values{}
	for _, pair := range r.parameters {
		queryValues.Add(pair.key, pair.value)
	}
	target.RawQuery = queryValues.Encode()

	httpRequest, err := http.NewRequestWithContext(ctx, r.method, target.String(), nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Accept", "application/json")
	return httpRequest, nil
}
