package supabase

import (
	"net/http"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/postgrest"
)

// Option configures a [Client]. It is an alias for [core.Option], so the
// convenience options below and any option from the [core] package are
// interchangeable.
type Option = core.Option

// WithHTTPClient sets the HTTP client used for all requests. See
// [core.WithHTTPClient] for the precise semantics.
func WithHTTPClient(client *http.Client) Option {
	return core.WithHTTPClient(client)
}

// WithHeaders registers headers sent on every request. See [core.WithHeaders].
func WithHeaders(headers map[string]string) Option {
	return core.WithHeaders(headers)
}

// Client is the composed Supabase client. It is safe for concurrent use by
// multiple goroutines. Construct it with [NewClient]. The zero value is not
// usable.
type Client struct {
	configuration *core.Configuration
	database      *postgrest.Client
}

// NewClient constructs a Supabase client for the given project URL and API key.
//
// Validation is delegated to [core.NewConfiguration], so NewClient returns its
// sentinel errors unchanged, each matchable with [errors.Is]:
//   - [core.ErrMissingURL] when projectURL is empty.
//   - [core.ErrMissingKey] when apiKey is empty.
//   - [core.ErrInvalidURL] when projectURL is not an absolute http or https URL.
//
// See [WithHTTPClient] and [WithHeaders] for the available options.
func NewClient(projectURL, apiKey string, options ...Option) (*Client, error) {
	configuration, err := core.NewConfiguration(projectURL, apiKey, options...)
	if err != nil {
		return nil, err
	}
	return &Client{
		configuration: configuration,
		database:      postgrest.New(configuration),
	}, nil
}

// Database returns the client for the Supabase Database (PostgREST) surface. The
// returned [postgrest.Client] is safe for concurrent use by multiple goroutines.
func (c *Client) Database() *postgrest.Client {
	return c.database
}
