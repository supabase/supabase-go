package configuration

import "net/http"

// transport is an http.RoundTripper that injects Supabase authentication and
// global headers into every outgoing request. It honours the RoundTripper
// contract: it never mutates the caller's request, operating on a clone instead.
type transport struct {
	base    http.RoundTripper
	apiKey  string
	headers http.Header
}

// wrapClient returns a copy of client whose transport injects the apikey, a
// default Authorization header and the supplied global headers. The input client
// is never mutated, so sharing http.DefaultClient remains safe.
func wrapClient(client *http.Client, apiKey string, headers http.Header) *http.Client {
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

// RoundTrip injects authentication and global headers, then delegates to the
// wrapped transport. Precedence: existing request headers win over global
// defaults; the apikey header is always the project key; Authorization defaults
// to the API key only when the caller has not already set it, so a per-request
// end-user token (added in a later block) takes precedence for row-level
// security.
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
	if clone.Header.Get("Authorization") == "" {
		clone.Header.Set("Authorization", "Bearer "+tr.apiKey)
	}

	return tr.base.RoundTrip(clone)
}
