// Package configuration provides the shared project configuration and
// functional options that other Supabase domain modules rely upon.
package configuration

import (
	"context"
	"errors"
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
	// absolute HTTP or HTTPS base URL. The error never repeats the URL. When
	// the URL fails to parse, the error wraps the cause, which can quote the
	// malformed part of the URL, such as a port.
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
// transport such as otelhttp.NewTransport for tracing. The client's
// CheckRedirect applies only to the redirects [Configuration.HTTPClient] may
// follow. A nil client is ignored.
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
// the build, otherwise this function panics:
//   - [core.ModulePathRoot]
//   - [core.ModulePathPostgrest]
//   - [core.ModulePathAuth]
//
// Returns one of these sentinel errors, when inputs are unusable:
//   - [ErrMissingURL] when rawURL is empty.
//   - [ErrMissingKey] when apiKey is empty.
//   - [ErrInvalidURL] when rawURL is not an absolute http or https URL.
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
		// A *url.Error repeats the URL it failed to parse, so only its cause is wrapped.
		if parseError, ok := errors.AsType[*url.Error](err); ok {
			err = parseError.Err
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	if (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, fmt.Errorf("%w: must be an absolute http(s) URL", ErrInvalidURL)
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
	// Do sends request and returns the response, whatever the status code, or
	// an error. A response returned with a nil error has an open body, which
	// the caller closes.
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
//
// It answers a redirect according to where the redirect leads:
//   - within the scheme and host of the request it was sent, port included, it
//     follows the redirect as the CheckRedirect of the client supplied through
//     [WithHTTPClient] decides, or up to ten in a row when there is none
//   - anywhere else, it never follows the redirect, whatever that
//     CheckRedirect decides, and Do returns the redirect response itself
//
// When Do fails, it returns a *url.Error, as [http.Client.Do] does, whose URL
// holds only the scheme, host and path of the request's URL, so the error's
// text leaves out the query string and any user information. Its Op and Err
// are those that [http.Client.Do] returned, so [errors.Is], [errors.As] and
// its Timeout method answer as they would for that error.
func (c *Configuration) HTTPClient() HTTPClient {
	return redactingClient{c.httpClient}
}

// redactingClient is the [HTTPClient] a [Configuration] hands out. It holds
// the client it sends through unexported, so a caller can neither reach nor
// replace that client's transport.
type redactingClient struct {
	client *http.Client
}

// Do sends request through the client. When that fails with a *url.Error, Do
// returns a copy whose URL keeps only the scheme, host and path.
func (c redactingClient) Do(request *http.Request) (*http.Response, error) {
	// #nosec G704 -- request is outgoing, built by the caller of Do. gosec takes
	// it for one a server received and is passing on to a URL its sender chose.
	response, err := c.client.Do(request)
	if urlError, ok := errors.AsType[*url.Error](err); ok {
		err = &url.Error{Op: urlError.Op, URL: schemeHostAndPath(urlError.URL), Err: urlError.Err}
	}
	return response, err
}

// schemeHostAndPath returns the scheme, host and path of rawURL, and nothing
// when rawURL does not parse.
func schemeHostAndPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	kept := url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path, RawPath: parsed.RawPath}
	return kept.String()
}

// BaseURL returns a copy of the project base URL. Callers may mutate the result
// freely (for example by setting a field on it) without affecting the [Configuration].
func (c *Configuration) BaseURL() *url.URL {
	clone := *c.baseURL
	return &clone
}

// Logger returns the [slog.Logger] that receives the SDK's log emissions: the
// one [WithLogger] set, or one that discards everything.
func (c *Configuration) Logger() *slog.Logger {
	return c.logger
}
