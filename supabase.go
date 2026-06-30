package supabase

import (
	"net/http"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/postgrest"
)

// Option configures a Client. It is an alias for core.Option, so the convenience
// options below and any option from the core module are interchangeable.
type Option = core.Option

// WithHTTPClient sets the HTTP client used for all requests. See
// core.WithHTTPClient for the precise semantics.
func WithHTTPClient(client *http.Client) Option {
	return core.WithHTTPClient(client)
}

// WithHeaders registers headers sent on every request. See core.WithHeaders.
func WithHeaders(headers map[string]string) Option {
	return core.WithHeaders(headers)
}

// Client is the composed Supabase client. It is safe for concurrent use by
// multiple goroutines. Construct it with NewClient; the zero value is not usable.
type Client struct {
	configuration *core.Configuration
	postgrest     *postgrest.Client
}

// NewClient constructs a Supabase client for the given project URL and API key.
// It returns an error if the URL or key is missing, or if the URL is not a valid
// absolute http(s) URL (see the core package's sentinel errors).
func NewClient(projectURL, apiKey string, options ...Option) (*Client, error) {
	configuration, err := core.NewConfiguration(projectURL, apiKey, options...)
	if err != nil {
		return nil, err
	}
	return &Client{
		configuration: configuration,
		postgrest:     postgrest.New(configuration),
	}, nil
}
