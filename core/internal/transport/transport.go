// Package transport provides the authenticating HTTP pipeline that other
// Supabase domain modules are built on.
package transport

import "net/http"

// transport is an http.RoundTripper that injects Supabase authentication and
// global headers into every outgoing request. It honors the RoundTripper
// contract: it never mutates the caller's request, operating on a clone instead.
type transport struct {
	base    http.RoundTripper
	apiKey  string
	headers http.Header
}

// WrapClient returns a copy of client whose transport injects the project API key
// header and the supplied global headers. The input client is never mutated, so
// sharing [http.DefaultClient] remains safe.
func WrapClient(client *http.Client, apiKey string, headers http.Header) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}

	clone := *client
	clone.Transport = &transport{
		base:    base,
		apiKey:  apiKey,
		headers: headers.Clone(),
	}
	return &clone
}

// RoundTrip implements [http.RoundTripper].
// It injects the project API key header and any global headers, then
// delegates to the wrapped transport. Precedence: existing request headers win
// over global defaults, and the apikey header is always the project key. The
// transport never sets Authorization: that header carries an end-user's JWT,
// supplied by the application when acting for a signed-in user.
func (tr *transport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())

	for key, values := range tr.headers {
		if clone.Header.Get(key) != "" {
			continue
		}
		for _, value := range values {
			clone.Header.Add(key, value)
		}
	}

	clone.Header.Set("apikey", tr.apiKey)

	return tr.base.RoundTrip(clone)
}
