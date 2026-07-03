package postgrest

import (
	"net/url"

	configuration "github.com/supabase/supabase-go/configuration"
)

// Client is the entry point for Database queries against PostgREST. It is built
// from the shared [configuration.Configuration], either directly (when importing this
// module on its own) or by the root supabase client. A Client is safe for
// concurrent use by multiple goroutines.
type Client struct {
	httpClient configuration.HTTPClient
	baseURL    *url.URL
}

// New constructs a PostgREST [Client] from the shared [configuration.Configuration]. The
// PostgREST endpoints live under the project's /rest/v1 path, derived from
// [configuration.Configuration.BaseURL], and requests carry the authentication and global
// headers configured on [configuration.Configuration.HTTPClient].
func New(projectConfiguration *configuration.Configuration) *Client {
	return &Client{
		httpClient: projectConfiguration.HTTPClient(),
		baseURL:    projectConfiguration.BaseURL().JoinPath("rest", "v1"),
	}
}
