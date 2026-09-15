package supabase

import (
	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
)

// Client is the composed Supabase client. It is safe for concurrent use by
// multiple goroutines. Construct it with [New]. The zero value is not
// usable.
type Client struct {
	configuration *configuration.Configuration
	database      *postgrest.Client
	auth          *auth.Client
}

// New constructs a Supabase client for the given project URL and API key.
//
// Returned sentinel errors relate to misconfiguration:
//   - [configuration.ErrMissingURL] when projectURL is empty.
//   - [configuration.ErrMissingKey] when apiKey is empty.
//   - [configuration.ErrInvalidURL] when projectURL is not an absolute http or
//     https URL.
//
// See [configuration.WithHTTPClient], [configuration.WithHeader],
// [configuration.WithRetry] and [configuration.WithLogger] for the available
// options.
func New(projectURL, apiKey string, options ...configuration.Option) (*Client, error) {
	projectConfiguration, err := configuration.New(core.ModulePathRoot, projectURL, apiKey, options...)
	if err != nil {
		return nil, err
	}
	return &Client{
		configuration: projectConfiguration,
		database:      postgrest.NewFromConfiguration(projectConfiguration),
		auth:          auth.NewFromConfiguration(projectConfiguration),
	}, nil
}

// Database returns the composed Database client, through which queries built
// with [postgrest.From] execute:
//
//	rows, response, err := postgrest.Collect(
//	    ctx,
//	    supabase.Database(),
//	    postgrest.From[Instrument]("instruments").Select("id, name"))
func (c *Client) Database() *postgrest.Client {
	return c.database
}

// Auth returns the composed Auth client, which verifies end-user access
// tokens and fetches the profiles they authenticate:
//
//	claims, _, _, err := supabase.Auth().GetClaims(ctx, token)
func (c *Client) Auth() *auth.Client {
	return c.auth
}
