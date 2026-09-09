package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
)

// Client verifies end-user JWTs and fetches user profiles for one Supabase
// project. Construct a standalone client with [New], or build one on a shared
// [configuration.Configuration] with [NewFromConfiguration], as the root
// supabase client does. A Client caches the project's signing keys internally,
// so construct one Client per project and reuse it rather than constructing per
// request. A Client is safe for concurrent use by multiple goroutines.
//
// A Client authenticates with the project API key alone and holds no
// signing-key secrets. It never starts background work and owns no resources
// that need releasing.
type Client struct {
	httpClient configuration.HTTPClient
	baseURL    *url.URL
	keyCache   *jwkSetCache
	now        func() time.Time
}

// New constructs a standalone Auth [Client] for the given project URL and API
// key.
//
// It returns the sentinel errors documented by [configuration.New] when
// projectURL or apiKey are unusable. See [configuration.WithHTTPClient] and
// [configuration.WithHeader] for the available options.
func New(projectURL, apiKey string, options ...configuration.Option) (*Client, error) {
	projectConfiguration, err := configuration.New(core.ModulePathAuth, projectURL, apiKey, options...)
	if err != nil {
		return nil, err
	}
	return NewFromConfiguration(projectConfiguration), nil
}

// NewFromConfiguration constructs an Auth [Client] from the shared
// [configuration.Configuration]. The Auth endpoints live under the project's
// /auth/v1 path, derived from [configuration.Configuration.BaseURL], and
// requests carry the authentication and global headers configured on
// [configuration.Configuration.HTTPClient].
func NewFromConfiguration(projectConfiguration *configuration.Configuration) *Client {
	baseURL := projectConfiguration.BaseURL().JoinPath("auth", "v1")
	return &Client{
		httpClient: projectConfiguration.HTTPClient(),
		baseURL:    baseURL,
		keyCache: &jwkSetCache{
			httpClient: projectConfiguration.HTTPClient(),
			endpoint:   baseURL.JoinPath(".well-known", "jwks.json"),
		},
		now: time.Now,
	}
}

// GetClaims verifies jwt and returns its claims. A token signed with one of the
// project's asymmetric signing keys (ES256, RS256 or EdDSA) is verified locally
// against the project's published key set, fetched from the
// /.well-known/jwks.json discovery endpoint and cached for ten minutes; a token
// the key set cannot verify locally - one signed with the legacy shared secret,
// or carrying an unrecognized key id - is verified by the Auth server instead,
// exactly as [Client.GetUser] verifies. Claims are returned only after one of
// those verifications succeeds.
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
func (c *Client) GetClaims(ctx context.Context, jwt string) (*Claims, error) {
	if jwt == "" {
		return nil, ErrMissingJWT
	}

	token, err := decodeToken(jwt)
	if err != nil {
		return nil, err
	}
	claims, err := parseClaims(token.claimsBytes)
	if err != nil {
		return nil, err
	}
	if claims.expiresAt.IsZero() || !claims.expiresAt.After(c.now()) {
		return nil, ErrExpiredJWT
	}

	// A token the local key set cannot verify - the legacy shared secret (an HS*
	// or absent alg) or one without a key id - is verified by the Auth server,
	// which trusts the returned claims only when it answers 200.
	if !asymmetricAlgorithms[token.header.Algorithm] || token.header.KeyID == "" {
		if _, err := c.GetUser(ctx, jwt); err != nil {
			return nil, err
		}
		return claims, nil
	}

	key, found, err := c.keyCache.key(ctx, token.header.KeyID, c.now())
	if err != nil {
		return nil, err
	}
	// A key id absent from the published set - a token from outside the project,
	// or a key the server knows but the discovery endpoint has not yet
	// advertised - defers to server verification rather than failing locally.
	if !found {
		if _, err := c.GetUser(ctx, jwt); err != nil {
			return nil, err
		}
		return claims, nil
	}

	if err := verifySignature(token, key); err != nil {
		return nil, err
	}
	return claims, nil
}

// GetUser fetches the profile of the user jwt authenticates from the Auth
// server, which verifies the token as part of serving the request. Every call
// is a server round trip: prefer [Client.GetClaims] on request paths that only
// need verified claims. An empty jwt returns [ErrMissingJWT], and a token the
// server rejects surfaces as [*Error] with the server's response.
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

	user, err := parseUser(body)
	if err != nil {
		return nil, fmt.Errorf("auth: decoding user: %w", err)
	}
	return user, nil
}
