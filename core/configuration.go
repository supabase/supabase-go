package core

import (
	"fmt"
	"net/http"
	"net/url"
)

// coreError is a string-backed error type whose values can be declared
// as compile-time constants. The sentinels below are therefore immutable: unlike
// package-level error variables (which any importing package can reassign), these
// cannot be replaced - the compiler rejects any attempt. The type is unexported,
// so consumers can match the sentinels with errors.Is but cannot mint new values
// of it.
type coreError string

func (e coreError) Error() string { return "core: " + string(e) }

// Sentinel errors returned by [NewConfiguration]. Match them with [errors.Is].
const (
	// ErrMissingURL is returned when an empty project URL is supplied.
	ErrMissingURL = coreError("project URL is required")

	// ErrMissingKey is returned when an empty API key is supplied.
	ErrMissingKey = coreError("API key is required")

	// ErrInvalidURL is returned when the project URL cannot be used as an
	// absolute HTTP or HTTPS base URL. The underlying parse error, when there is
	// one, is wrapped and recoverable with [errors.Unwrap].
	ErrInvalidURL = coreError("project URL is invalid")
)

// Configuration holds the resolved settings shared across the SDK: the project
// base URL, the API key and the HTTP client whose transport injects
// authentication and global headers on every request.
//
// A Configuration is created with [NewConfiguration] and is safe for concurrent
// use by multiple goroutines once constructed. Its zero value is not usable, so
// always build it through [NewConfiguration].
type Configuration struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
	headers    http.Header
}

// Option configures a [Configuration]. Options are applied by [NewConfiguration]
// in the order they are supplied. This is the SDK's single functional-option
// type, shared by the root supabase package and every domain module.
type Option func(*Configuration)

// WithHTTPClient sets the HTTP client used for all requests. The SDK never
// mutates the supplied client: it is cloned and its transport is wrapped, so the
// caller's client (including the shared [http.DefaultClient]) is left untouched.
// Use this to control timeouts or proxies, or to inject an instrumented
// transport such as otelhttp.NewTransport for tracing. A nil client is ignored.
//
// Implements client.request_configuration.custom_http_client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Configuration) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithHeaders registers headers sent on every request. Per-request headers take
// precedence over these defaults. The reserved apikey header is always set from
// the API key and cannot be overridden here.
//
// Implements client.request_configuration.global_headers.
func WithHeaders(headers map[string]string) Option {
	return func(c *Configuration) {
		for key, value := range headers {
			c.headers.Set(key, value)
		}
	}
}

// NewConfiguration validates rawURL and apiKey, applies the supplied options in
// order, and returns a ready-to-use [Configuration].
//
// It returns one of these sentinel errors, each matchable with [errors.Is], when
// its inputs are unusable:
//   - [ErrMissingURL] when rawURL is empty.
//   - [ErrMissingKey] when apiKey is empty.
//   - [ErrInvalidURL] when rawURL is not an absolute http or https URL. The
//     underlying parse failure, when there is one, is wrapped.
//
// See [WithHTTPClient] and [WithHeaders] for the available options.
func NewConfiguration(rawURL, apiKey string, options ...Option) (*Configuration, error) {
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
	}
	for _, option := range options {
		option(configuration)
	}

	configuration.httpClient = wrapClient(configuration.httpClient, configuration.apiKey, configuration.headers)
	return configuration, nil
}

// HTTPClient performs an HTTP request. The standard library's [*http.Client] satisfies it.
type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

// HTTPClient returns the [HTTPClient] configured for this project. Its transport
// injects the API key header, a default Bearer Authorization header and any
// configured global headers. The result is safe for concurrent use by multiple
// goroutines.
func (c *Configuration) HTTPClient() HTTPClient {
	return c.httpClient
}

// BaseURL returns a copy of the project base URL. Callers may mutate the result
// freely (for example by setting a field on it) without affecting the [Configuration].
func (c *Configuration) BaseURL() *url.URL {
	clone := *c.baseURL
	return &clone
}
