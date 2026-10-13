package auth

import (
	"fmt"
	"time"

	"github.com/supabase/supabase-go/auth/internal/customprovider"
)

// CustomProvider is one custom OAuth 2.0 or OIDC sign-in provider, as the
// Auth server holds it now. The server never serves the client secret, so no
// accessor carries it. A CustomProvider is immutable and safe for concurrent
// use by multiple goroutines. Timestamp accessors return the zero [time.Time]
// when the server supplied no value, and accessors for the other type's
// fields return zero values.
type CustomProvider struct {
	inner customprovider.CustomProvider
}

// ID returns the provider's unique ID, the UUID the server generated for it.
func (p CustomProvider) ID() string { return p.inner.ID() }

// Type returns the provider's type: "oauth2" or "oidc", with any type a newer
// server adds passing through verbatim.
func (p CustomProvider) Type() string { return p.inner.Type() }

// Identifier returns the provider's identifier, the "custom:"-prefixed handle
// the admin calls address it by and sign-ins name it with.
func (p CustomProvider) Identifier() string { return p.inner.Identifier() }

// Name returns the provider's display name.
func (p CustomProvider) Name() string { return p.inner.Name() }

// ClientID returns the OAuth client id registered with the provider.
func (p CustomProvider) ClientID() string { return p.inner.ClientID() }

// AcceptableClientIDs returns the additional client ids accepted in the
// provider's tokens, or nil when there are none. The result is a copy the
// caller may retain and modify freely.
func (p CustomProvider) AcceptableClientIDs() []string { return p.inner.AcceptableClientIDs() }

// Scopes returns the scopes requested at authorization, or nil when there are
// none. The result is a copy the caller may retain and modify freely.
func (p CustomProvider) Scopes() []string { return p.inner.Scopes() }

// PKCEEnabled reports whether the provider's authorization flow uses PKCE.
func (p CustomProvider) PKCEEnabled() bool { return p.inner.PKCEEnabled() }

// AttributeMapping returns the mapping of provider claims to user attributes,
// or nil when there is none. The result is a copy the caller may retain, with
// a deliberate shallowness: nested values are shared references.
func (p CustomProvider) AttributeMapping() map[string]any { return p.inner.AttributeMapping() }

// CustomClaimsAllowlist returns the provider claim keys copied verbatim into
// the user's custom claims, or nil when there are none. The result is a copy
// the caller may retain and modify freely.
func (p CustomProvider) CustomClaimsAllowlist() []string { return p.inner.CustomClaimsAllowlist() }

// AuthorizationParams returns the extra parameters the authorization request
// carries, or nil when there are none. The result is a copy the caller may
// retain and modify freely.
func (p CustomProvider) AuthorizationParams() map[string]string {
	return p.inner.AuthorizationParams()
}

// Enabled reports whether the provider accepts sign-ins.
func (p CustomProvider) Enabled() bool { return p.inner.Enabled() }

// EmailOptional reports whether users may sign in through the provider
// without an email claim.
func (p CustomProvider) EmailOptional() bool { return p.inner.EmailOptional() }

// Issuer returns an OIDC provider's issuer URL, or the empty string on an
// OAuth 2.0 provider.
func (p CustomProvider) Issuer() string { return p.inner.Issuer() }

// DiscoveryURL returns an OIDC provider's explicit discovery document URL, or
// the empty string when the issuer's well-known location serves it.
func (p CustomProvider) DiscoveryURL() string { return p.inner.DiscoveryURL() }

// SkipNonceCheck reports whether an OIDC provider skips ID token nonce
// verification.
func (p CustomProvider) SkipNonceCheck() bool { return p.inner.SkipNonceCheck() }

// DiscoveryDocument returns the OIDC discovery document the server fetched
// from the issuer when the provider was registered or last updated, reporting
// false on an OAuth 2.0 provider or before any document is stored.
func (p CustomProvider) DiscoveryDocument() (OIDCDiscoveryDocument, bool) {
	inner, ok := p.inner.DiscoveryDocument()
	return OIDCDiscoveryDocument{inner: inner}, ok
}

// AuthorizationURL returns an OAuth 2.0 provider's authorization endpoint, or
// the empty string on an OIDC provider, whose endpoints live in [CustomProvider.DiscoveryDocument].
func (p CustomProvider) AuthorizationURL() string { return p.inner.AuthorizationURL() }

// TokenURL returns an OAuth 2.0 provider's token endpoint, or the empty
// string on an OIDC provider.
func (p CustomProvider) TokenURL() string { return p.inner.TokenURL() }

// UserinfoURL returns an OAuth 2.0 provider's userinfo endpoint, or the empty
// string on an OIDC provider.
func (p CustomProvider) UserinfoURL() string { return p.inner.UserinfoURL() }

// JWKSURI returns an OAuth 2.0 provider's JWKS endpoint, or the empty string
// when it has none.
func (p CustomProvider) JWKSURI() string { return p.inner.JWKSURI() }

// CreatedAt returns when the provider was registered.
func (p CustomProvider) CreatedAt() time.Time { return p.inner.CreatedAt() }

// UpdatedAt returns when the provider was last changed.
func (p CustomProvider) UpdatedAt() time.Time { return p.inner.UpdatedAt() }

// OIDCDiscoveryDocument is the OpenID Connect discovery document the Auth
// server fetched from a provider's issuer, answered by
// [CustomProvider.DiscoveryDocument]. Slice
// accessors return copies the caller may retain and modify freely, nil when
// the document declares no value.
type OIDCDiscoveryDocument struct {
	inner customprovider.DiscoveryDocument
}

// Issuer returns the issuer the document declares.
func (d OIDCDiscoveryDocument) Issuer() string { return d.inner.Issuer() }

// AuthorizationEndpoint returns the discovered authorization endpoint.
func (d OIDCDiscoveryDocument) AuthorizationEndpoint() string { return d.inner.AuthorizationEndpoint() }

// TokenEndpoint returns the discovered token endpoint.
func (d OIDCDiscoveryDocument) TokenEndpoint() string { return d.inner.TokenEndpoint() }

// UserinfoEndpoint returns the discovered userinfo endpoint, or the empty
// string when the document declares none.
func (d OIDCDiscoveryDocument) UserinfoEndpoint() string { return d.inner.UserinfoEndpoint() }

// JWKSURI returns the discovered JWKS endpoint.
func (d OIDCDiscoveryDocument) JWKSURI() string { return d.inner.JWKSURI() }

// ScopesSupported returns the scopes the document declares.
func (d OIDCDiscoveryDocument) ScopesSupported() []string { return d.inner.ScopesSupported() }

// ResponseTypesSupported returns the response types the document declares.
func (d OIDCDiscoveryDocument) ResponseTypesSupported() []string {
	return d.inner.ResponseTypesSupported()
}

// GrantTypesSupported returns the grant types the document declares.
func (d OIDCDiscoveryDocument) GrantTypesSupported() []string { return d.inner.GrantTypesSupported() }

// SubjectTypesSupported returns the subject types the document declares.
func (d OIDCDiscoveryDocument) SubjectTypesSupported() []string {
	return d.inner.SubjectTypesSupported()
}

// parseCustomProvider decodes a provider response.
func parseCustomProvider(body []byte) (*CustomProvider, error) {
	inner, err := customprovider.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("auth: decoding custom provider: %w", err)
	}
	return &CustomProvider{inner: inner}, nil
}

// parseCustomProviderList decodes a provider listing response.
func parseCustomProviderList(body []byte) ([]CustomProvider, error) {
	inners, err := customprovider.ParseList(body)
	if err != nil {
		return nil, fmt.Errorf("auth: decoding custom provider list: %w", err)
	}
	providers := make([]CustomProvider, len(inners))
	for i, inner := range inners {
		providers[i] = CustomProvider{inner: inner}
	}
	return providers, nil
}
