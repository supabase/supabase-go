// Package configuration provides the shared project configuration and
// functional options that other Supabase domain modules rely upon.
package configuration

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/internal/transport"
)

// configurationError is a string-backed error type. Its values can be declared
// as compile-time constants, are matched with [errors.Is] and cannot be minted
// outside this package.
type configurationError string

func (e configurationError) Error() string { return "configuration: " + string(e) }

// Sentinel errors returned by [New].
const (
	// ErrMissingURL is returned when an empty project URL is supplied.
	ErrMissingURL = configurationError("project URL is required")

	// ErrMissingKey is returned when an empty API key is supplied.
	ErrMissingKey = configurationError("API key is required")

	// ErrInvalidURL is returned when the project URL cannot be used as an
	// absolute HTTP or HTTPS base URL. The underlying parse error, when there is
	// one, is wrapped and recoverable with [errors.Unwrap].
	ErrInvalidURL = configurationError("project URL is invalid")
)

// Configuration holds the resolved settings shared across the SDK: the project
// base URL, the API key, the HTTP client whose transport injects
// authentication and global headers on every request, and the logger that
// receives that transport's log emissions.
//
// A Configuration is created with [New] and is safe for concurrent
// use by multiple goroutines once constructed. Its zero value is not usable, so
// always build it through [New].
type Configuration struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
	headers    http.Header
	retry      bool
	logger     *slog.Logger
}

// Option configures a [Configuration]. Options are applied by [New]
// in the order they are supplied. This is the SDK's single functional-option
// type, shared by the root supabase package and every domain module.
type Option func(*Configuration)

// WithHTTPClient sets the HTTP client used for all requests. The SDK never
// mutates the supplied client: it is cloned and its transport is wrapped, so the
// caller's client (including the shared [http.DefaultClient]) is left untouched.
// Use this to control timeouts or proxies, or to inject an instrumented
// transport such as otelhttp.NewTransport for tracing. A nil client is ignored.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Configuration) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithRetry sets whether the SDK's domain clients retry failed requests
// automatically. Retries are enabled by default. Only requests that are safe
// to repeat are ever retried, and each domain client documents the exact
// contract it enforces: which requests qualify, on which failures and with
// what backoff.
func WithRetry(enabled bool) Option {
	return func(c *Configuration) {
		c.retry = enabled
	}
}

// WithHeader registers a single header sent on every request. Call it once per
// header to build up a set, and a later WithHeader for the same key replaces an
// earlier one. Per-request headers take precedence over these defaults. The
// reserved apikey header is always set from the API key and cannot be overridden
// here.
func WithHeader(key, value string) Option {
	return func(c *Configuration) {
		c.headers.Set(key, value)
	}
}

// WithLogger sets the [slog.Logger] that receives the SDK's log emissions.
// Without one the SDK is silent: every emission is discarded, and failures
// reach the caller as returned errors regardless. The SDK emits once per
// request round trip at [slog.LevelDebug], carrying the request method,
// host, path, outcome (response status or transport error) and duration.
// Bodies, query strings and header values are never logged. A nil logger is
// ignored.
func WithLogger(logger *slog.Logger) Option {
	return func(c *Configuration) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// New validates rawURL and apiKey, applies the supplied options in order, and
// returns a ready-to-use [Configuration]. It identifies itself to Supabase
// services according to entryModulePath.
//
// The apiKey may be any Supabase project API key, publishable or secret.
//
// entryModulePath must be one of the following, with its module linked into
// the build, otherwise this function will panic:
//   - [core.ModulePathRoot]
//   - [core.ModulePathPostgrest]
//   - [core.ModulePathAuth]
//
// Returns one of these sentinel errors, when inputs are unusable:
//   - [ErrMissingURL] when rawURL is empty.
//   - [ErrMissingKey] when apiKey is empty.
//   - [ErrInvalidURL] when rawURL is not an absolute http or https URL. The
//     underlying parse failure, when there is one, is wrapped.
//
// See [WithHTTPClient], [WithHeader], [WithRetry] and [WithLogger] for the
// available options.
func New(entryModulePath core.ModulePath, rawURL, apiKey string, options ...Option) (*Configuration, error) {
	if rawURL == "" {
		return nil, ErrMissingURL
	}
	if apiKey == "" {
		return nil, ErrMissingKey
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	if (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, fmt.Errorf("%w: must be an absolute http(s) URL, got %q", ErrInvalidURL, rawURL)
	}

	configuration := &Configuration{
		baseURL:    parsedURL,
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
		headers:    make(http.Header),
		retry:      true,
		logger:     slog.New(slog.DiscardHandler),
	}
	for _, option := range options {
		option(configuration)
	}

	configuration.httpClient = transport.WrapClient(entryModulePath, configuration.httpClient, configuration.apiKey, configuration.headers, configuration.logger)
	return configuration, nil
}

// HTTPClient performs an HTTP request. The standard library's [*http.Client] satisfies it.
type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

// AccessTokenProvider returns the end-user access token (a JWT) a request
// sends as the credentials of the Bearer authentication scheme on the
// Authorization header. It receives the executing call's context. A call
// resolves its token before the first request is sent, reuses it across
// transient retries and asks again only after the server rejects the sent
// token, so return the best token currently available - blocking on an
// in-flight refresh rather than returning one known to be stale. It must
// be safe for concurrent use by multiple goroutines. Return the token
// without the "Bearer " prefix: it is sent verbatim, never validated,
// inspected or compared, and the server verifies it. A returned error
// fails the call, wrapped, and a returned empty token likewise fails the
// call: a resolution failure never lowers a call to the project key's own
// role.
type AccessTokenProvider func(ctx context.Context) (string, error)

// Retry reports whether automatic retries are enabled for this project's
// clients.
func (c *Configuration) Retry() bool {
	return c.retry
}

// HTTPClient returns the [HTTPClient] configured for this project. Its transport
// injects the API key header and any configured global headers. The result is
// safe for concurrent use by multiple goroutines.
func (c *Configuration) HTTPClient() HTTPClient {
	return c.httpClient
}

// BaseURL returns a copy of the project base URL. Callers may mutate the result
// freely (for example by setting a field on it) without affecting the [Configuration].
func (c *Configuration) BaseURL() *url.URL {
	clone := *c.baseURL
	return &clone
}
