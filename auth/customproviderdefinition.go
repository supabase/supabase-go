package auth

import (
	"github.com/supabase/supabase-go/auth/internal/customprovider"
)

// NewCustomProvider starts a custom provider definition with the four inputs
// every provider needs: the "custom:"-prefixed identifier the server requires,
// the display name and the client credential pair registered with the
// provider. Choose the provider's type with [CustomProviderBuilder.OIDC] or
// [CustomProviderBuilder.OAuth2] - the result is what
// [Admin.CreateCustomProvider] accepts.
func NewCustomProvider(identifier, name, clientID, clientSecret string) CustomProviderBuilder {
	return CustomProviderBuilder{definition: customprovider.New(identifier, name, clientID, clientSecret)}
}

// CustomProviderBuilder is a custom provider definition before its type is
// chosen. It is an immutable value: each method returns a new value, so
// builders fork freely and never alias.
type CustomProviderBuilder struct {
	definition customprovider.Definition
}

// OIDC types the provider OpenID Connect, with the issuer the only endpoint
// it needs: the server fetches the issuer's discovery document at
// registration time and rejects the definition when that fails, so a
// misconfiguration surfaces to the administrator rather than to a user
// signing in.
func (b CustomProviderBuilder) OIDC(issuer string) OIDCProviderDefinition {
	return OIDCProviderDefinition{definition: b.definition.WithOIDC(issuer)}
}

// OAuth2 types the provider plain OAuth 2.0, which discovers nothing, so all
// three endpoints are stated in the protocol's own order: where the user
// authorizes, where the code is exchanged for tokens and where the user's
// profile is read.
func (b CustomProviderBuilder) OAuth2(authorizationURL, tokenURL, userinfoURL string) OAuth2ProviderDefinition {
	return OAuth2ProviderDefinition{definition: b.definition.WithOAuth2(authorizationURL, tokenURL, userinfoURL)}
}

// OIDCProviderDefinition is an OIDC provider definition, ready for
// [Admin.CreateCustomProvider]. Like the builder it is an immutable value:
// each method returns a new value.
type OIDCProviderDefinition struct {
	definition customprovider.Definition
}

// Scopes names the scopes requested at authorization. The server prepends
// "openid" when it is missing, as OIDC requires it.
func (d OIDCProviderDefinition) Scopes(scopes ...string) OIDCProviderDefinition {
	return OIDCProviderDefinition{definition: d.definition.WithScopes(scopes)}
}

// AcceptableClientIDs names additional client ids accepted in the provider's
// tokens, for providers that issue tokens to more than one registered client.
func (d OIDCProviderDefinition) AcceptableClientIDs(ids ...string) OIDCProviderDefinition {
	return OIDCProviderDefinition{definition: d.definition.WithAcceptableClientIDs(ids)}
}

// CustomClaimsAllowlist names provider claim keys to copy verbatim into the
// user's custom claims.
func (d OIDCProviderDefinition) CustomClaimsAllowlist(claims ...string) OIDCProviderDefinition {
	return OIDCProviderDefinition{definition: d.definition.WithCustomClaimsAllowlist(claims)}
}

// AttributeMapping maps provider claims to user attributes. The server
// rejects mappings onto protected system fields. The definition holds its own
// clone of the map.
func (d OIDCProviderDefinition) AttributeMapping(mapping map[string]any) OIDCProviderDefinition {
	return OIDCProviderDefinition{definition: d.definition.WithAttributeMapping(mapping)}
}

// AuthorizationParams adds extra parameters to the authorization request. The
// server rejects reserved OAuth parameters such as redirect_uri or state. The
// definition holds its own clone of the map.
func (d OIDCProviderDefinition) AuthorizationParams(params map[string]string) OIDCProviderDefinition {
	return OIDCProviderDefinition{definition: d.definition.WithAuthorizationParams(params)}
}

// WithoutPKCE disables PKCE for the provider's authorization flow, which the
// server otherwise enables.
func (d OIDCProviderDefinition) WithoutPKCE() OIDCProviderDefinition {
	return OIDCProviderDefinition{definition: d.definition.WithoutPKCE()}
}

// StartDisabled registers the provider without accepting sign-ins, for
// configuring ahead of a launch; enable it later with [Admin.UpdateCustomProvider]
// and [WithEnabled].
func (d OIDCProviderDefinition) StartDisabled() OIDCProviderDefinition {
	return OIDCProviderDefinition{definition: d.definition.WithStartDisabled()}
}

// EmailOptional allows sign-ins without an email claim, which the server
// otherwise requires.
func (d OIDCProviderDefinition) EmailOptional() OIDCProviderDefinition {
	return OIDCProviderDefinition{definition: d.definition.WithEmailOptional()}
}

// DiscoveryURL states where the discovery document lives when it is not at
// the issuer's well-known location.
func (d OIDCProviderDefinition) DiscoveryURL(discoveryURL string) OIDCProviderDefinition {
	return OIDCProviderDefinition{definition: d.definition.WithDiscoveryURL(discoveryURL)}
}

// SkipNonceCheck skips ID token nonce verification, for providers that do not
// echo the nonce correctly.
func (d OIDCProviderDefinition) SkipNonceCheck() OIDCProviderDefinition {
	return OIDCProviderDefinition{definition: d.definition.WithSkipNonceCheck()}
}

func (d OIDCProviderDefinition) providerDefinition() customprovider.Definition { return d.definition }

// OAuth2ProviderDefinition is an OAuth 2.0 provider definition, ready for
// [Admin.CreateCustomProvider]. Like the builder it is an immutable value:
// each method returns a new value.
type OAuth2ProviderDefinition struct {
	definition customprovider.Definition
}

// Scopes names the scopes requested at authorization.
func (d OAuth2ProviderDefinition) Scopes(scopes ...string) OAuth2ProviderDefinition {
	return OAuth2ProviderDefinition{definition: d.definition.WithScopes(scopes)}
}

// AcceptableClientIDs names additional client ids accepted in the provider's
// tokens, for providers that issue tokens to more than one registered client.
func (d OAuth2ProviderDefinition) AcceptableClientIDs(ids ...string) OAuth2ProviderDefinition {
	return OAuth2ProviderDefinition{definition: d.definition.WithAcceptableClientIDs(ids)}
}

// CustomClaimsAllowlist names provider claim keys to copy verbatim into the
// user's custom claims.
func (d OAuth2ProviderDefinition) CustomClaimsAllowlist(claims ...string) OAuth2ProviderDefinition {
	return OAuth2ProviderDefinition{definition: d.definition.WithCustomClaimsAllowlist(claims)}
}

// AttributeMapping maps provider claims to user attributes. The server
// rejects mappings onto protected system fields. The definition holds its own
// clone of the map.
func (d OAuth2ProviderDefinition) AttributeMapping(mapping map[string]any) OAuth2ProviderDefinition {
	return OAuth2ProviderDefinition{definition: d.definition.WithAttributeMapping(mapping)}
}

// AuthorizationParams adds extra parameters to the authorization request. The
// server rejects reserved OAuth parameters such as redirect_uri or state. The
// definition holds its own clone of the map.
func (d OAuth2ProviderDefinition) AuthorizationParams(params map[string]string) OAuth2ProviderDefinition {
	return OAuth2ProviderDefinition{definition: d.definition.WithAuthorizationParams(params)}
}

// WithoutPKCE disables PKCE for the provider's authorization flow, which the
// server otherwise enables.
func (d OAuth2ProviderDefinition) WithoutPKCE() OAuth2ProviderDefinition {
	return OAuth2ProviderDefinition{definition: d.definition.WithoutPKCE()}
}

// StartDisabled registers the provider without accepting sign-ins, for
// configuring ahead of a launch; enable it later with [Admin.UpdateCustomProvider]
// and [WithEnabled].
func (d OAuth2ProviderDefinition) StartDisabled() OAuth2ProviderDefinition {
	return OAuth2ProviderDefinition{definition: d.definition.WithStartDisabled()}
}

// EmailOptional allows sign-ins without an email claim, which the server
// otherwise requires.
func (d OAuth2ProviderDefinition) EmailOptional() OAuth2ProviderDefinition {
	return OAuth2ProviderDefinition{definition: d.definition.WithEmailOptional()}
}

// JWKSURI states where the provider publishes its signing keys, for
// validating provider-issued JWTs.
func (d OAuth2ProviderDefinition) JWKSURI(jwksURI string) OAuth2ProviderDefinition {
	return OAuth2ProviderDefinition{definition: d.definition.WithJWKSURI(jwksURI)}
}

func (d OAuth2ProviderDefinition) providerDefinition() customprovider.Definition {
	return d.definition
}

// CustomProviderDefinition is a custom provider definition with its type
// chosen, built from [NewCustomProvider]. The two definition types satisfy it
// and nothing outside this package can.
type CustomProviderDefinition interface {
	providerDefinition() customprovider.Definition
}
