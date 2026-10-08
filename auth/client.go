package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/supabase/supabase-go/auth/internal/cache"
	"github.com/supabase/supabase-go/auth/internal/key"
	"github.com/supabase/supabase-go/auth/internal/profile"
	"github.com/supabase/supabase-go/auth/internal/token"
	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
)

// Client verifies end-user JWTs and fetches user profiles for one Supabase
// project, and a Client constructed with a secret API key also administers
// the project's users through [Client.Admin]. Construct a standalone client
// with [New], or build one on a shared
// [configuration.Configuration] with [NewFromConfiguration]. A Client caches
// the project's signing keys internally, so construct one Client per project
// and reuse it rather than constructing per request. A Client is safe for
// concurrent use by multiple goroutines.
//
// A Client authenticates with the project API key alone and holds no
// signing-key secrets. It never starts background work and owns no resources
// that need releasing.
//
// A Client sends each request once. [configuration.WithRetry] has no effect
// on it.
type Client struct {
	httpClient configuration.HTTPClient
	baseURL    *url.URL
	keys       *cache.Cache
	now        func() time.Time
}

// New constructs a standalone Auth [Client] for the given project URL and API
// key. The apiKey may be any Supabase project API key, publishable or secret.
//
// It returns the sentinel errors documented by [configuration.New] when
// projectURL or apiKey are unusable. See [configuration.WithHTTPClient],
// [configuration.WithHeader] and [configuration.WithLogger] for the available
// options.
func New(projectURL, apiKey string, options ...configuration.Option) (*Client, error) {
	projectConfiguration, err := configuration.New(core.ModulePathAuth, projectURL, apiKey, options...)
	if err != nil {
		return nil, err
	}
	return NewFromConfiguration(projectConfiguration), nil
}

// NewFromConfiguration constructs an Auth [Client] from the shared
// [configuration.Configuration].
func NewFromConfiguration(projectConfiguration *configuration.Configuration) *Client {
	httpClient := projectConfiguration.HTTPClient()
	baseURL := projectConfiguration.BaseURL().JoinPath("auth", "v1")
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		keys:       cache.NewCache(jwkSetFetch(httpClient, baseURL.JoinPath(".well-known", "jwks.json"))),
		now:        time.Now,
	}
}

// jwkSetFetch builds the [cache.FetchFunc] the key cache pulls the project's
// JWK Set through: one GET of the discovery endpoint, with every failure
// shaped by this package's error model so the cache propagates consumer-ready
// errors verbatim.
func jwkSetFetch(httpClient configuration.HTTPClient, endpoint *url.URL) cache.FetchFunc {
	return func(ctx context.Context) ([]key.Key, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("auth: building JWK Set request: %w", err)
		}

		response, err := httpClient.Do(request)
		if err != nil {
			return nil, fmt.Errorf("auth: fetching JWK Set: %w", err)
		}
		defer func() { _ = response.Body.Close() }()

		body, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, fmt.Errorf("auth: reading JWK Set: %w", err)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, newError(response.StatusCode, body)
		}

		keys, err := key.ParseSet(body)
		if err != nil {
			return nil, fmt.Errorf("auth: parsing JWK Set: %w", err)
		}
		return keys, nil
	}
}

// GetClaims verifies jwt and returns its claims together with the token's
// decoded header and raw signature bytes. A token signed with one of the
// project's asymmetric signing keys (ES256, RS256 or EdDSA) is verified locally
// against the project's published key set, fetched from the
// /.well-known/jwks.json discovery endpoint and cached for ten minutes; a token
// the key set cannot verify locally - one signed with the legacy shared secret,
// or carrying an unrecognized key id - is verified by the Auth server instead,
// exactly as [Client.GetUser] verifies. Claims are returned only after one of
// those verifications succeeds, with the [JWTHeader] and signature describing
// the token envelope on both routes. The signature is a copy the caller may
// retain and modify freely.
//
// Verification establishes authenticity - the token was signed for this
// project and has not expired - not authorization. Policy over claim values
// such as audience, issuer or role stays with the caller, decided on the
// verified [Claims] through its accessors, because those values vary
// legitimately per deployment: the audience is configurable and third-party
// auth providers change the issuer, so no fixed check here fits every project.
//
// Local verification does not consult the token's session, so a locally
// verified token passes until it expires, even after the user signs out. Where
// a decision must reflect the session still being live, use [Client.GetUser].
//
// Returned sentinel errors:
//   - [ErrMissingJWT] when jwt is empty.
//   - [ErrMalformedJWT] when jwt is not a three-part base64url JWT carrying a
//     JSON header and JSON claims.
//   - [ErrExpiredJWT] when the exp claim is absent or in the past.
//   - [ErrInvalidSignature] when local signature verification fails.
//
// A server-verified rejection surfaces as [*Error] with the Auth server's
// response, and a JWK Set discovery failure is returned wrapped.
func (c *Client) GetClaims(ctx context.Context, jwt string) (*Claims, JWTHeader, []byte, error) {
	if jwt == "" {
		return nil, JWTHeader{}, nil, ErrMissingJWT
	}

	decoded, err := token.Decode(jwt)
	if err != nil {
		return nil, JWTHeader{}, nil, ErrMalformedJWT
	}
	claims, err := decoded.Claims()
	if err != nil {
		return nil, JWTHeader{}, nil, ErrMalformedJWT
	}
	if claims.ExpiresAt().IsZero() || !claims.ExpiresAt().After(c.now()) {
		return nil, JWTHeader{}, nil, ErrExpiredJWT
	}
	header := JWTHeader{inner: decoded.Header()}

	// A token the local key set cannot verify - the legacy shared secret (an HS*
	// or absent alg) or one without a key id - is verified by the Auth server,
	// which trusts the returned claims only when it answers 200.
	if !key.Supported(header.Algorithm()) || header.KeyID() == "" {
		if _, err := c.GetUser(ctx, jwt); err != nil {
			return nil, JWTHeader{}, nil, err
		}
		return &Claims{inner: claims}, header, decoded.Signature(), nil
	}

	signingKey, found, err := c.keys.Key(ctx, header.KeyID(), c.now())
	if err != nil {
		return nil, JWTHeader{}, nil, err
	}
	// A key id absent from the published set - a token from outside the project,
	// or a key the server knows but the discovery endpoint has not yet
	// advertised - defers to server verification rather than failing locally.
	if !found {
		if _, err := c.GetUser(ctx, jwt); err != nil {
			return nil, JWTHeader{}, nil, err
		}
		return &Claims{inner: claims}, header, decoded.Signature(), nil
	}

	if err := signingKey.Verify(decoded.SigningInput(), decoded.Signature()); err != nil {
		return nil, JWTHeader{}, nil, ErrInvalidSignature
	}
	return &Claims{inner: claims}, header, decoded.Signature(), nil
}

// GetUser fetches the profile of the user jwt authenticates from the Auth
// server, which verifies the token as part of serving the request. Every call
// is a server round trip: prefer [Client.GetClaims] on request paths that only
// need verified claims. An empty jwt returns [ErrMissingJWT], and a token the
// server rejects surfaces as [*Error] with the server's response.
//
// The server also rejects an unexpired token whose session no longer exists,
// after sign-out for example, or whose user no longer exists or is banned.
func (c *Client) GetUser(ctx context.Context, jwt string) (*User, error) {
	if jwt == "" {
		return nil, ErrMissingJWT
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL.JoinPath("user").String(), nil)
	if err != nil {
		return nil, fmt.Errorf("auth: building request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+jwt)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("auth: executing request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("auth: reading response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, newError(response.StatusCode, body)
	}

	user, err := profile.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("auth: decoding user: %w", err)
	}
	return &User{inner: user}, nil
}
