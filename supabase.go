package supabase

import (
	"github.com/supabase/supabase-go/configuration"
	"github.com/supabase/supabase-go/postgrest"
)

// Client is the composed Supabase client. It is safe for concurrent use by
// multiple goroutines. Construct it with [NewClient]. The zero value is not
// usable.
type Client struct {
	configuration *configuration.Configuration
	database      *postgrest.Client
}

// NewClient constructs a Supabase client for the given project URL and API key.
//
// Validation is delegated to [configuration.New], so NewClient returns its
// sentinel errors unchanged, each matchable with [errors.Is]:
//   - [configuration.ErrMissingURL] when projectURL is empty.
//   - [configuration.ErrMissingKey] when apiKey is empty.
//   - [configuration.ErrInvalidURL] when projectURL is not an absolute http or
//     https URL.
//
// See [configuration.WithHTTPClient] and [configuration.WithHeader] for the
// available options.
func NewClient(projectURL, apiKey string, options ...configuration.Option) (*Client, error) {
	projectConfiguration, err := configuration.New(projectURL, apiKey, options...)
	if err != nil {
		return nil, err
	}
	return &Client{
		configuration: projectConfiguration,
		database:      postgrest.New(projectConfiguration),
	}, nil
}

// From begins a Database query against the given table or view, returning an
// immutable [postgrest.QueryBuilder]. Chain a verb such as
// [postgrest.QueryBuilder.Select] and terminate with
// [postgrest.FilterBuilder.Execute]:
//
//	var rows []Instrument
//	response, err := supabase.From("instruments").Select("id, name").Execute(ctx, &rows)
func (c *Client) From(table string) postgrest.QueryBuilder {
	return c.database.From(table)
}
