package postgrest

import (
	"net/url"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
)

// Client executes Database queries against PostgREST, owning the pooled HTTP
// client and project base URL that every request shares. Build queries with
// [From] and execute them with a generic read function such as [Collect].
// Construct a standalone client with [New], or build one on a shared
// [configuration.Configuration] with [NewFromConfiguration], as the root supabase
// client does. A Client is safe for concurrent use by multiple goroutines.
//
// When automatic retries are enabled - the default, controlled by
// [configuration.WithRetry] and overridable per read with [WithRetry] - the
// client retries GET and HEAD requests that fail with HTTP 503, HTTP 520 or
// a transport error. A construction-time client
// timeout ([configuration.WithHTTPClient]) applies to each attempt
// separately and a timed-out attempt retries like any other transport
// failure, so bound a whole read, backoff included, through ctx.
// It waits 1s, 2s then 4s
// between attempts, or the whole seconds of a Retry-After response header
// when one parses, and gives up after three retries, surfacing the final
// failure. Each retried attempt carries an X-Retry-Count header. Requests
// are never retried once ctx is done, and requests with any other HTTP
// method are never retried.
type Client struct {
	httpClient configuration.HTTPClient
	baseURL    *url.URL
	retry      bool
}

// New constructs a standalone PostgREST [Client] for the given project URL
// and API key.
//
// It returns the sentinel errors documented by [configuration.New] when
// projectURL or apiKey are unusable. See [configuration.WithHTTPClient],
// [configuration.WithHeader] and [configuration.WithRetry] for the available
// options.
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
		retry:      projectConfiguration.Retry(),
	}
}
