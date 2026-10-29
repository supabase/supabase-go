package customprovider

import (
	"encoding/json"

	internal "github.com/supabase/supabase-go/auth/internal/customprovider"
)

// New starts a custom provider definition with the four inputs every
// provider needs: the "custom:"-prefixed identifier the server requires,
// the display name and the client credential pair registered with the
// provider. Choose the provider's type with [Builder.OIDC] or
// [Builder.OAuth2] - the result is what the admin creation call accepts.
func New(identifier, name, clientID, clientSecret string) Builder {
	return Builder{definition: internal.New(identifier, name, clientID, clientSecret)}
}

// Builder is a custom provider definition before its type is chosen. It is
// an immutable value: each method returns a new value, so builders fork
// freely and never alias.
type Builder struct {
	definition internal.Definition
}

// OIDC types the provider OpenID Connect, with the issuer the only endpoint
// it needs: the server fetches the issuer's discovery document at
// registration time and rejects the definition when that fails, so a
// misconfiguration surfaces to the administrator rather than to a user
// signing in.
func (b Builder) OIDC(issuer string) OIDCDefinition {
	return OIDCDefinition{definition: b.definition.WithOIDC(issuer)}
}

// OAuth2 types the provider plain OAuth 2.0, which discovers nothing, so all
// three endpoints are stated in the protocol's own order: where the user
// authorizes, where the code is exchanged for tokens and where the user's
// profile is read.
func (b Builder) OAuth2(authorizationURL, tokenURL, userinfoURL string) OAuth2Definition {
	return OAuth2Definition{definition: b.definition.WithOAuth2(authorizationURL, tokenURL, userinfoURL)}
}

// OIDCDefinition is an OIDC provider definition, ready for the admin
// creation call. Like the builder it is an immutable value: each method
// returns a new value.
type OIDCDefinition struct {
	definition internal.Definition
}

// Scopes names the scopes requested at authorization. The server prepends
// "openid" when it is missing, as OIDC requires it.
func (d OIDCDefinition) Scopes(scopes ...string) OIDCDefinition {
	return OIDCDefinition{definition: d.definition.WithScopes(scopes)}
}

// AcceptableClientIDs names additional client ids accepted in the provider's
// tokens, for providers that issue tokens to more than one registered client.
func (d OIDCDefinition) AcceptableClientIDs(ids ...string) OIDCDefinition {
	return OIDCDefinition{definition: d.definition.WithAcceptableClientIDs(ids)}
}

// CustomClaimsAllowlist names provider claim keys to copy verbatim into the
// user's custom claims.
func (d OIDCDefinition) CustomClaimsAllowlist(claims ...string) OIDCDefinition {
	return OIDCDefinition{definition: d.definition.WithCustomClaimsAllowlist(claims)}
}

// AttributeMapping maps provider claims to user attributes. The server
// rejects mappings onto protected system fields. The definition holds its own
// clone of the map.
func (d OIDCDefinition) AttributeMapping(mapping map[string]any) OIDCDefinition {
	return OIDCDefinition{definition: d.definition.WithAttributeMapping(mapping)}
}

// AuthorizationParams adds extra parameters to the authorization request. The
// server rejects reserved OAuth parameters such as redirect_uri or state. The
// definition holds its own clone of the map.
func (d OIDCDefinition) AuthorizationParams(params map[string]string) OIDCDefinition {
	return OIDCDefinition{definition: d.definition.WithAuthorizationParams(params)}
}

// WithoutPKCE disables PKCE for the provider's authorization flow, which the
// server otherwise enables.
func (d OIDCDefinition) WithoutPKCE() OIDCDefinition {
	return OIDCDefinition{definition: d.definition.WithoutPKCE()}
}

// StartDisabled registers the provider without accepting sign-ins, for
// configuring ahead of a launch; enable it later with [WithEnabled] through
// the admin update call.
func (d OIDCDefinition) StartDisabled() OIDCDefinition {
	return OIDCDefinition{definition: d.definition.WithStartDisabled()}
}

// EmailOptional allows sign-ins without an email claim, which the server
// otherwise requires.
func (d OIDCDefinition) EmailOptional() OIDCDefinition {
	return OIDCDefinition{definition: d.definition.WithEmailOptional()}
}

// DiscoveryURL states where the discovery document lives when it is not at
// the issuer's well-known location.
func (d OIDCDefinition) DiscoveryURL(discoveryURL string) OIDCDefinition {
	return OIDCDefinition{definition: d.definition.WithDiscoveryURL(discoveryURL)}
}

// SkipNonceCheck skips ID token nonce verification, for providers that do not
// echo the nonce correctly.
func (d OIDCDefinition) SkipNonceCheck() OIDCDefinition {
	return OIDCDefinition{definition: d.definition.WithSkipNonceCheck()}
}

// MarshalJSON renders the definition's creation request body.
func (d OIDCDefinition) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.definition.Payload())
}

func (OIDCDefinition) sealedDefinition() {}

// OAuth2Definition is an OAuth 2.0 provider definition, ready for the admin
// creation call. Like the builder it is an immutable value: each method
// returns a new value.
type OAuth2Definition struct {
	definition internal.Definition
}

// Scopes names the scopes requested at authorization.
func (d OAuth2Definition) Scopes(scopes ...string) OAuth2Definition {
	return OAuth2Definition{definition: d.definition.WithScopes(scopes)}
}

// AcceptableClientIDs names additional client ids accepted in the provider's
// tokens, for providers that issue tokens to more than one registered client.
func (d OAuth2Definition) AcceptableClientIDs(ids ...string) OAuth2Definition {
	return OAuth2Definition{definition: d.definition.WithAcceptableClientIDs(ids)}
}

// CustomClaimsAllowlist names provider claim keys to copy verbatim into the
// user's custom claims.
func (d OAuth2Definition) CustomClaimsAllowlist(claims ...string) OAuth2Definition {
	return OAuth2Definition{definition: d.definition.WithCustomClaimsAllowlist(claims)}
}

// AttributeMapping maps provider claims to user attributes. The server
// rejects mappings onto protected system fields. The definition holds its own
// clone of the map.
func (d OAuth2Definition) AttributeMapping(mapping map[string]any) OAuth2Definition {
	return OAuth2Definition{definition: d.definition.WithAttributeMapping(mapping)}
}

// AuthorizationParams adds extra parameters to the authorization request. The
// server rejects reserved OAuth parameters such as redirect_uri or state. The
// definition holds its own clone of the map.
func (d OAuth2Definition) AuthorizationParams(params map[string]string) OAuth2Definition {
	return OAuth2Definition{definition: d.definition.WithAuthorizationParams(params)}
}

// WithoutPKCE disables PKCE for the provider's authorization flow, which the
// server otherwise enables.
func (d OAuth2Definition) WithoutPKCE() OAuth2Definition {
	return OAuth2Definition{definition: d.definition.WithoutPKCE()}
}

// StartDisabled registers the provider without accepting sign-ins, for
// configuring ahead of a launch; enable it later with [WithEnabled] through
// the admin update call.
func (d OAuth2Definition) StartDisabled() OAuth2Definition {
	return OAuth2Definition{definition: d.definition.WithStartDisabled()}
}

// EmailOptional allows sign-ins without an email claim, which the server
// otherwise requires.
func (d OAuth2Definition) EmailOptional() OAuth2Definition {
	return OAuth2Definition{definition: d.definition.WithEmailOptional()}
}

// JWKSURI states where the provider publishes its signing keys, for
// validating provider-issued JWTs.
func (d OAuth2Definition) JWKSURI(jwksURI string) OAuth2Definition {
	return OAuth2Definition{definition: d.definition.WithJWKSURI(jwksURI)}
}

// MarshalJSON renders the definition's creation request body.
func (d OAuth2Definition) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.definition.Payload())
}

func (OAuth2Definition) sealedDefinition() {}

// Definition is a custom provider definition with its type chosen, built
// from [New]. The two definition types satisfy it and nothing outside this
// package can. Its JSON form is the creation request body.
type Definition interface {
	json.Marshaler
	sealedDefinition()
}
