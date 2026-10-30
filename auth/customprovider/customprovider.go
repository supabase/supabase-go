// Package customprovider carries the vocabulary the SDK's custom provider
// administration takes: the definition a creation call registers, the
// fields an update call changes and the type filter a listing call narrows
// by, expressed as a builder and functional options.
package customprovider

import (
	"encoding/json"
	"maps"
)

// Parameters is the resolved set of changes for one update call. The zero
// value holds none and marshals to the empty JSON object, which the server
// answers without changing anything.
type Parameters struct {
	request updateRequest
}

// Option sets one field on an update call: an option carried is an
// instruction, an option absent leaves the stored value alone. Options are
// applied in the order they are supplied. An option for the other type's
// fields - [WithIssuer] on an OAuth 2.0 provider, [WithTokenURL] on an OIDC
// one - is ignored by the server, which still answers with success.
type Option func(*Parameters)

// Resolve applies options to an empty Parameters and returns the result.
func Resolve(options ...Option) Parameters {
	var parameters Parameters
	for _, option := range options {
		option(&parameters)
	}
	return parameters
}

// MarshalJSON renders the update request body: exactly the fields the
// options set.
func (p Parameters) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.request)
}

// updateRequest is the update request body: only what the options set is
// sent. Deviating booleans are pointers because the field's absence, not
// falseness, is what leaves the stored value alone.
type updateRequest struct {
	Name                  string            `json:"name,omitzero"`
	ClientID              string            `json:"client_id,omitzero"`
	ClientSecret          string            `json:"client_secret,omitzero"`
	Scopes                []string          `json:"scopes,omitzero"`
	AcceptableClientIDs   []string          `json:"acceptable_client_ids,omitzero"`
	CustomClaimsAllowlist []string          `json:"custom_claims_allowlist,omitzero"`
	AttributeMapping      map[string]any    `json:"attribute_mapping,omitzero"`
	AuthorizationParams   map[string]string `json:"authorization_params,omitzero"`
	PKCEEnabled           *bool             `json:"pkce_enabled,omitzero"`
	Enabled               *bool             `json:"enabled,omitzero"`
	EmailOptional         *bool             `json:"email_optional,omitzero"`
	Issuer                string            `json:"issuer,omitzero"`
	DiscoveryURL          string            `json:"discovery_url,omitzero"`
	SkipNonceCheck        *bool             `json:"skip_nonce_check,omitzero"`
	AuthorizationURL      string            `json:"authorization_url,omitzero"`
	TokenURL              string            `json:"token_url,omitzero"`
	UserinfoURL           string            `json:"userinfo_url,omitzero"`
	JWKSURI               string            `json:"jwks_uri,omitzero"`
}

// WithName renames the provider.
func WithName(name string) Option {
	return func(parameters *Parameters) {
		parameters.request.Name = name
	}
}

// WithClientID replaces the OAuth client id registered with the provider.
func WithClientID(clientID string) Option {
	return func(parameters *Parameters) {
		parameters.request.ClientID = clientID
	}
}

// WithClientSecret rotates the provider's client secret. The server encrypts
// it at rest and never serves it back.
func WithClientSecret(clientSecret string) Option {
	return func(parameters *Parameters) {
		parameters.request.ClientSecret = clientSecret
	}
}

// WithScopes replaces the scopes requested at authorization. A call with no
// arguments clears the list, and the server keeps "openid" present on OIDC
// providers either way.
func WithScopes(scopes ...string) Option {
	return func(parameters *Parameters) {
		parameters.request.Scopes = append([]string{}, scopes...)
	}
}

// WithAcceptableClientIDs replaces the additional client ids accepted in the
// provider's tokens. A call with no arguments clears the list.
func WithAcceptableClientIDs(ids ...string) Option {
	return func(parameters *Parameters) {
		parameters.request.AcceptableClientIDs = append([]string{}, ids...)
	}
}

// WithCustomClaimsAllowlist replaces the provider claim keys copied verbatim
// into the user's custom claims. A call with no arguments clears the list.
func WithCustomClaimsAllowlist(claims ...string) Option {
	return func(parameters *Parameters) {
		parameters.request.CustomClaimsAllowlist = append([]string{}, claims...)
	}
}

// WithAttributeMapping replaces the mapping of provider claims to user
// attributes. A nil or empty map clears the stored mapping. The request
// holds its own clone of the map.
func WithAttributeMapping(mapping map[string]any) Option {
	return func(parameters *Parameters) {
		parameters.request.AttributeMapping = clonedOrEmpty(mapping)
	}
}

// WithAuthorizationParams replaces the extra parameters the authorization
// request carries. A nil or empty map clears the stored parameters. The
// request holds its own clone of the map.
func WithAuthorizationParams(params map[string]string) Option {
	return func(parameters *Parameters) {
		parameters.request.AuthorizationParams = clonedOrEmpty(params)
	}
}

// WithPKCEEnabled turns PKCE on or off for the provider's authorization
// flow.
func WithPKCEEnabled(enabled bool) Option {
	return func(parameters *Parameters) {
		parameters.request.PKCEEnabled = &enabled
	}
}

// WithEnabled turns sign-ins through the provider on or off, the kill switch
// that keeps the configuration while the provider is distrusted.
func WithEnabled(enabled bool) Option {
	return func(parameters *Parameters) {
		parameters.request.Enabled = &enabled
	}
}

// WithEmailOptional sets whether users may sign in through the provider
// without an email claim.
func WithEmailOptional(optional bool) Option {
	return func(parameters *Parameters) {
		parameters.request.EmailOptional = &optional
	}
}

// WithIssuer moves an OIDC provider to a new issuer URL. The server fetches
// and validates the new issuer's discovery document before storing it.
func WithIssuer(issuer string) Option {
	return func(parameters *Parameters) {
		parameters.request.Issuer = issuer
	}
}

// WithDiscoveryURL states where an OIDC provider's discovery document lives
// when it is not at the issuer's well-known location. The server fetches and
// validates the document before storing the URL.
func WithDiscoveryURL(discoveryURL string) Option {
	return func(parameters *Parameters) {
		parameters.request.DiscoveryURL = discoveryURL
	}
}

// WithSkipNonceCheck sets whether an OIDC provider skips ID token nonce
// verification.
func WithSkipNonceCheck(skip bool) Option {
	return func(parameters *Parameters) {
		parameters.request.SkipNonceCheck = &skip
	}
}

// WithAuthorizationURL replaces an OAuth 2.0 provider's authorization
// endpoint.
func WithAuthorizationURL(authorizationURL string) Option {
	return func(parameters *Parameters) {
		parameters.request.AuthorizationURL = authorizationURL
	}
}

// WithTokenURL replaces an OAuth 2.0 provider's token endpoint.
func WithTokenURL(tokenURL string) Option {
	return func(parameters *Parameters) {
		parameters.request.TokenURL = tokenURL
	}
}

// WithUserinfoURL replaces an OAuth 2.0 provider's userinfo endpoint.
func WithUserinfoURL(userinfoURL string) Option {
	return func(parameters *Parameters) {
		parameters.request.UserinfoURL = userinfoURL
	}
}

// WithJWKSURI replaces where an OAuth 2.0 provider publishes its signing
// keys.
func WithJWKSURI(jwksURI string) Option {
	return func(parameters *Parameters) {
		parameters.request.JWKSURI = jwksURI
	}
}

// clonedOrEmpty clones a caller's map, normalizing nil to an empty map so an
// option carried always sends its field: on the wire an empty object clears
// where an absent key keeps.
func clonedOrEmpty[V any](source map[string]V) map[string]V {
	cloned := make(map[string]V, len(source))
	maps.Copy(cloned, source)
	return cloned
}

// ListParameters is the resolved set of listing parameters for one call. The
// zero value holds none.
type ListParameters struct {
	providerType string
}

// ListOption narrows which providers a listing call answers with. Options
// are applied in the order they are supplied, the later wins.
type ListOption func(*ListParameters)

// ResolveList applies options to an empty ListParameters and returns the
// result.
func ResolveList(options ...ListOption) ListParameters {
	var parameters ListParameters
	for _, option := range options {
		option(&parameters)
	}
	return parameters
}

// WithOIDCOnly narrows the listing to OIDC providers.
func WithOIDCOnly() ListOption {
	return func(parameters *ListParameters) {
		parameters.providerType = "oidc"
	}
}

// WithOAuth2Only narrows the listing to OAuth 2.0 providers.
func WithOAuth2Only() ListOption {
	return func(parameters *ListParameters) {
		parameters.providerType = "oauth2"
	}
}

// ProviderType returns the chosen type filter, empty when no option
// narrowed the listing.
func (p ListParameters) ProviderType() string { return p.providerType }
