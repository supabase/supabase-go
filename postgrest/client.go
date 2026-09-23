package postgrest

import (
	"context"
	"fmt"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest/internal/http"
)

// Client executes Database queries against PostgREST, owning the pooled HTTP
// client and project base URL that every request shares. Build queries with
// [From] and execute them with a generic read function such as [Collect].
// Construct a standalone client with [New], or build one on a shared
// [configuration.Configuration] with [NewFromConfiguration]. A Client is safe
// for concurrent use by multiple goroutines.
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
//
// A Client authenticates with the project API key alone.
// [Client.WithAccessTokenProvider] derives a copy that also acts for one
// signed-in end user. When the server answers such a copy's request with
// HTTP 401, the client asks its provider for a token again and re-sends
// immediately with the result - once per 401, with no backoff, for any
// HTTP method and whether or not automatic retries are enabled, since the
// server rejects an invalid token before executing the statement. Renewal
// re-sends and transient retries spend from the same cap above, so one
// call sends at most four requests.
type Client struct {
	httpClient http.Client
}

// New constructs a standalone PostgREST [Client] for the given project URL
// and API key.
//
// The apiKey may be any Supabase project API key. Pass the publishable key
// for access governed by Row Level Security, attaching end-user tokens via
// [Client.WithAccessTokenProvider] so queries run as that user, or the
// secret key for privileged access that bypasses Row Level Security. See
// https://supabase.com/docs/guides/getting-started/api-keys for the key types.
//
// It returns the sentinel errors documented by [configuration.New] when
// projectURL or apiKey are unusable. See [configuration.WithHTTPClient],
// [configuration.WithHeader], [configuration.WithRetry] and
// [configuration.WithLogger] for the available options.
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
	return &Client{httpClient: http.New(
		projectConfiguration.HTTPClient(),
		*projectConfiguration.BaseURL().JoinPath("rest", "v1"),
		projectConfiguration.Retry(),
	)}
}

// WithAccessTokenProvider returns a copy of this client that executes every
// request as a signed-in end user, resolving the user's access token through
// provider and sending it as the credentials of the Bearer authentication
// scheme on the Authorization header, so the database applies that user's
// Row Level Security policies. The receiver is unchanged and the copy shares
// its HTTP connection pool, so one base client serves many users through
// independent derived copies, each safe for concurrent use by multiple
// goroutines. The project API key continues to travel on the apikey header.
// A resolved token the server rejects is renewed and re-sent as documented
// on [Client].
//
// A token already in hand - an inbound request's bearer token, say - attaches
// as a constant provider:
//
//	userClient := client.WithAccessTokenProvider(
//	    func(context.Context) (string, error) { return accessToken, nil })
//
// The [WithAccessTokenProvider] option replaces this client's provider for a
// single call. A nil provider is attached as one that fails the call with
// [ErrMissingAccessToken].
func (c *Client) WithAccessTokenProvider(provider configuration.AccessTokenProvider) *Client {
	return &Client{httpClient: c.httpClient.WithTokenResolver(tokenResolver(provider))}
}

// tokenResolver adapts provider into the resolver an exchange calls: a
// resolved token returns verbatim, a provider error is wrapped as a
// token-resolution failure and a nil provider or empty token fails with
// [ErrMissingAccessToken]. Both attachment sites use it, so every attached
// provider carries the same failure semantics.
func tokenResolver(provider configuration.AccessTokenProvider) http.TokenResolver {
	return func(ctx context.Context) (string, error) {
		if provider == nil {
			return "", ErrMissingAccessToken
		}
		token, err := provider(ctx)
		if err != nil {
			return "", fmt.Errorf("postgrest: resolving access token: %w", err)
		}
		if token == "" {
			return "", ErrMissingAccessToken
		}
		return token, nil
	}
}

// WithSchema returns a copy of this client that targets the named database
// schema in place of the server's default, naming it on the profile header
// PostgREST reads for each request's method: Accept-Profile on GET and HEAD,
// Content-Profile otherwise, so table reads, writes and function calls alike
// run against schema. The receiver is unchanged and the copy shares its HTTP
// connection pool, so one base client serves several schemas through
// independent derived copies, each safe for concurrent use by multiple
// goroutines. The schema is sent verbatim and never validated client-side:
// the server accepts only a schema [exposed to the API], failing the call with
// an [*Error] carrying code "PGRST106" otherwise. An empty schema restores the
// default, sending no profile header.
//
// [exposed to the API]: https://supabase.com/docs/guides/api/using-custom-schemas
func (c *Client) WithSchema(schema string) *Client {
	return &Client{httpClient: c.httpClient.WithSchema(schema)}
}
