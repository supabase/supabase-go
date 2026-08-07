// Package request holds the immutable HTTP request model shared by the
// postgrest builders. Every method on a Request either reads state or
// returns a new independent Request; no method mutates its receiver.
package request

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// parameter is one key/value pair of a request's query string.
type parameter struct {
	key   string
	value string
}

// Request describes a single PostgREST HTTP request. The zero value is not
// useful, so you must construct instances with [New]. A Request is immutable:
// With* methods return a new independent value and the receiver is never
// changed, so Requests may be freely copied, forked, and shared between goroutines.
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
// Keys may repeat, and insertion order is preserved: PostgREST assigns meaning to
// both, for example age=gte.18&age=lte.65 ANDs two filters on the same column.
func (r Request) WithParameter(key, value string) Request {
	clone := r
	clone.parameters = append(slices.Clone(r.parameters), parameter{key: key, value: value})
	return clone
}

// WithParameterReplacing returns a new Request carrying the given query-string
// pair, with every existing pair for key removed first, so the key appears
// exactly once. Use it for keys PostgREST treats as singletons, such as limit.
func (r Request) WithParameterReplacing(key, value string) Request {
	clone := r
	retained := make([]parameter, 0, len(r.parameters)+1)
	for _, pair := range r.parameters {
		if pair.key != key {
			retained = append(retained, pair)
		}
	}
	clone.parameters = append(retained, parameter{key: key, value: value})
	return clone
}

// WithParameterJoining returns a new Request carrying one pair for key, whose
// value is every existing pair's value for key followed by the given value,
// comma-joined in their original order, positioned as a freshly appended
// pair. Use it for keys PostgREST reads as one comma-separated list, such as
// order.
func (r Request) WithParameterJoining(key, value string) Request {
	clone := r
	retained := make([]parameter, 0, len(r.parameters)+1)
	joined := ""
	for _, pair := range r.parameters {
		if pair.key == key {
			joined += pair.value + ","
			continue
		}
		retained = append(retained, pair)
	}
	clone.parameters = append(retained, parameter{key: key, value: joined + value})
	return clone
}

// WithParameterValueAppended returns a new Request whose last pair for key
// carries its value with addition appended verbatim. When key is absent the
// Request is returned unchanged.
func (r Request) WithParameterValueAppended(key, addition string) Request {
	clone := r
	clone.parameters = slices.Clone(r.parameters)
	for index := len(clone.parameters) - 1; index >= 0; index-- {
		if clone.parameters[index].key == key {
			clone.parameters[index].value += addition
			break
		}
	}
	return clone
}

// HTTPRequest assembles the Request into an *http.Request against the given
// base URL, carrying ctx. The base is not mutated. The Accept header is set
// for JSON. Authentication headers are not injected here.
func (r Request) HTTPRequest(ctx context.Context, base *url.URL) (*http.Request, error) {
	target := base.JoinPath(r.path)
	target.RawQuery = rawQuery(r.parameters)
	httpRequest, err := http.NewRequestWithContext(ctx, r.method, target.String(), nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Accept", "application/json")
	return httpRequest, nil
}

// rawQuery renders parameters as the request's query-string text: pairs in
// insertion order, joined by &, each key and value escaped independently by
// escapeQueryComponent. Rendering no parameters yields the empty string, so
// the assembled URL carries no ?.
func rawQuery(parameters []parameter) string {
	var text strings.Builder
	for index, pair := range parameters {
		if index > 0 {
			text.WriteByte('&')
		}
		text.WriteString(escapeQueryComponent(pair.key))
		text.WriteByte('=')
		text.WriteString(escapeQueryComponent(pair.value))
	}
	return text.String()
}

// upperHexDigits indexes the digits of percent-escapes.
const upperHexDigits = "0123456789ABCDEF"

// escapeQueryComponent percent-encodes s for use as one key or value of the
// query string. It operates on octets: any byte sequence, valid UTF-8 or
// not, renders unambiguously and percent-decodes back to the exact input
// bytes. Octets the pair grammar reads as structure (& = + % #), octets
// RFC 3986 bars from a query (spaces, double quotes, controls, non-ASCII)
// and the historical pair separator ; are escaped; the remaining query
// characters, commas and parentheses included, pass through literally.
func escapeQueryComponent(s string) string {
	var escaped strings.Builder
	for _, octet := range []byte(s) {
		if queryOctetSafe(octet) {
			escaped.WriteByte(octet)
			continue
		}
		escaped.WriteByte('%')
		escaped.WriteByte(upperHexDigits[octet>>4])
		escaped.WriteByte(upperHexDigits[octet&0x0F])
	}
	return escaped.String()
}

// queryOctetSafe reports whether octet may appear literally in a query
// component key or value.
func queryOctetSafe(octet byte) bool {
	if 'a' <= octet && octet <= 'z' || 'A' <= octet && octet <= 'Z' || '0' <= octet && octet <= '9' {
		return true
	}
	switch octet {
	case '-', '.', '_', '~', // RFC 3986 unreserved marks
		'!', '$', '\'', '(', ')', '*', ',', // sub-delimiters that are data within a pair
		':', '@', '/', '?': // the query production's extra characters
		return true
	}
	return false
}
