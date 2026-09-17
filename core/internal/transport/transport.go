// Package transport provides the authenticating HTTP pipeline that other
// Supabase domain modules are built on.
package transport

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/internal/telemetry"
)

// transport is an http.RoundTripper that injects Supabase authentication and
// global headers into every outgoing request. It honors the RoundTripper
// contract: it never mutates the caller's request, operating on a clone instead.
type transport struct {
	base                         http.RoundTripper
	apiKey                       string
	headers                      http.Header
	clientInformationHeaderValue string
	logger                       *slog.Logger
}

// WrapClient returns a copy of client whose transport injects the project API
// key header, the supplied global headers and the X-Client-Info header that
// identifies entryModulePath, and writes one debug log emission per round
// trip through logger. The input client is never mutated, so sharing
// [http.DefaultClient] remains safe.
//
// entryModulePath must be one of the following, with its module linked into
// the build, otherwise this function panics:
//   - [core.ModulePathRoot]
//   - [core.ModulePathPostgrest]
//   - [core.ModulePathAuth]
func WrapClient(entryModulePath core.ModulePath, client *http.Client, apiKey string, headers http.Header, logger *slog.Logger) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}

	clone := *client
	clone.Transport = &transport{
		base:                         base,
		apiKey:                       apiKey,
		headers:                      headers.Clone(),
		clientInformationHeaderValue: telemetry.ClientInformationHeaderValue(entryModulePath),
		logger:                       logger,
	}
	return &clone
}

// RoundTrip implements [http.RoundTripper].
// It injects the project API key header and any global headers, then
// delegates to the wrapped transport. Precedence: existing request headers win
// over global defaults, and the apikey header is always the project key. The
// transport never sets Authorization: that header carries an end-user's JWT,
// supplied by the application when acting for a signed-in user. Each round
// trip writes one debug log emission through the configured logger.
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
	clone.Header.Set("X-Client-Info", tr.clientInformationHeaderValue)

	start := time.Now()
	response, err := tr.base.RoundTrip(clone)
	tr.logRoundTrip(clone, response, err, time.Since(start))
	return response, err
}

// logRoundTrip writes one debug log emission for the round trip, carrying the
// request method, host, path, outcome and duration. Bodies, query strings and
// header values never appear in the emission. It writes nothing when the
// logger does not enable the debug level.
func (tr *transport) logRoundTrip(request *http.Request, response *http.Response, err error, duration time.Duration) {
	ctx := request.Context()
	if !tr.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}
	attributes := []slog.Attr{
		slog.String("method", request.Method),
		slog.String("host", request.URL.Host),
		slog.String("path", request.URL.Path),
		slog.Duration("duration", duration),
	}
	if err != nil {
		tr.logger.LogAttrs(ctx, slog.LevelDebug, "supabase: request failed", append(attributes, slog.String("error", err.Error()))...)
		return
	}
	tr.logger.LogAttrs(ctx, slog.LevelDebug, "supabase: request completed", append(attributes, slog.Int("status", response.StatusCode))...)
}
