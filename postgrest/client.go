package postgrest

import (
	"net/url"

	"github.com/supabase/supabase-go/core"
)

// Client is the entry point for Database queries against PostgREST. It is built
// from the shared [core.Configuration], either directly (when importing this
// module on its own) or by the root supabase client. A Client is safe for
// concurrent use by multiple goroutines.
type Client struct {
	httpClient core.HTTPClient
	baseURL    *url.URL
}

// New constructs a PostgREST [Client] from the shared [core.Configuration]. The
// PostgREST endpoints live under the project's /rest/v1 path, derived from
// [core.Configuration.BaseURL], and requests carry the authentication and global
// headers configured on [core.Configuration.HTTPClient].
func New(configuration *core.Configuration) *Client {
	return &Client{
		httpClient: configuration.HTTPClient(),
		baseURL:    configuration.BaseURL().JoinPath("rest", "v1"),
	}
}
