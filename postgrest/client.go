package postgrest

import (
	"net/http"
	"net/url"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest/internal/request"
)

// Client is the entry point for Database queries against PostgREST. Construct a
// standalone client with [New], or build one on a shared
// [configuration.Configuration] with [NewFromConfiguration], as the root supabase
// client does. A Client is safe for concurrent use by multiple goroutines.
type Client struct {
	httpClient configuration.HTTPClient
	baseURL    *url.URL
}

// New constructs a standalone PostgREST [Client] for the given project URL
// and API key.
//
// It returns the sentinel errors documented by [configuration.New] when
// projectURL or apiKey are unusable. See [configuration.WithHTTPClient] and
// [configuration.WithHeader] for the available options.
func New(projectURL, apiKey string, options ...configuration.Option) (*Client, error) {
	projectConfiguration, err := configuration.New(core.ModulePathPostgrest, projectURL, apiKey, options...)
	if err != nil {
		return nil, err
	}
	return NewFromConfiguration(projectConfiguration), nil
}

// NewFromConfiguration constructs a PostgREST [Client] from the shared
// [configuration.Configuration]. The PostgREST endpoints live under the project's
// /rest/v1 path, derived from [configuration.Configuration.BaseURL], and requests
// carry the authentication and global headers configured on
// [configuration.Configuration.HTTPClient].
func NewFromConfiguration(projectConfiguration *configuration.Configuration) *Client {
	return &Client{
		httpClient: projectConfiguration.HTTPClient(),
		baseURL:    projectConfiguration.BaseURL().JoinPath("rest", "v1"),
	}
}

// From begins a query against the given table or view.
// Chain a verb such as [QueryBuilder.Select], then pass the finished query
// to a generic read function such as [Collect].
func (c *Client) From(table string) QueryBuilder {
	return QueryBuilder{
		client:  c,
		request: request.New(http.MethodGet, table),
	}
}
