package supabase

import (
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
}

// New constructs a Supabase client for the given project URL and API key.
//
// Returned sentinel errors relate to misconfiguration:
//   - [configuration.ErrMissingURL] when projectURL is empty.
//   - [configuration.ErrMissingKey] when apiKey is empty.
//   - [configuration.ErrInvalidURL] when projectURL is not an absolute http or
//     https URL.
//
// See [configuration.WithHTTPClient] and [configuration.WithHeader] for the
// available options.
func New(projectURL, apiKey string, options ...configuration.Option) (*Client, error) {
	projectConfiguration, err := configuration.New(core.ModulePathRoot, projectURL, apiKey, options...)
	if err != nil {
		return nil, err
	}
	return &Client{
		configuration: projectConfiguration,
		database:      postgrest.NewFromConfiguration(projectConfiguration),
	}, nil
}

// From begins a Database query against the given table or view.
// Chain a verb such as [postgrest.QueryBuilder.Select] and terminate with
// a generic terminal such as [postgrest.Collect]:
//
//	rows, response, err := postgrest.Collect[Instrument](ctx, supabase.From("instruments").Select("id, name"))
func (c *Client) From(table string) postgrest.QueryBuilder {
	return c.database.From(table)
}
