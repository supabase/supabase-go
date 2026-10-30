// Package customprovider owns the Auth server's custom OAuth and OIDC
// provider resource: [Parse] and [ParseList] decode the server's provider
// JSON and the opaque [CustomProvider] holds the result, readable only
// through accessors so no code outside this package can express a mutation of
// its contents. [Definition] is the resource's creation request, accumulated
// by the public builder and marshaled once at send time.
package customprovider

import (
	"encoding/json"
	"maps"
	"slices"
	"time"
)

// CustomProvider is the opaque state of one custom OAuth or OIDC provider.
// Timestamp accessors return the zero [time.Time] when the server supplied no
// value, and accessors for the other type's fields return zero values.
type CustomProvider struct {
	id                    string
	providerType          string
	identifier            string
	name                  string
	clientID              string
	acceptableClientIDs   []string
	scopes                []string
	pkceEnabled           bool
	attributeMapping      map[string]any
	customClaimsAllowlist []string
	authorizationParams   map[string]string
	enabled               bool
	emailOptional         bool
	issuer                string
	discoveryURL          string
	skipNonceCheck        bool
	discoveryDocument     *DiscoveryDocument
	authorizationURL      string
	tokenURL              string
	userinfoURL           string
	jwksURI               string
	createdAt             time.Time
	updatedAt             time.Time
}

// ID returns the provider's unique ID.
func (p CustomProvider) ID() string { return p.id }

// Type returns the provider's type.
func (p CustomProvider) Type() string { return p.providerType }

// Identifier returns the provider's identifier.
func (p CustomProvider) Identifier() string { return p.identifier }

// Name returns the provider's display name.
func (p CustomProvider) Name() string { return p.name }

// ClientID returns the OAuth client id registered with the provider.
func (p CustomProvider) ClientID() string { return p.clientID }

// AcceptableClientIDs returns a copy of the additional client ids accepted in
// tokens, or nil when there are none.
func (p CustomProvider) AcceptableClientIDs() []string {
	return slices.Clone(p.acceptableClientIDs)
}

// Scopes returns a copy of the scopes requested at authorization, or nil when
// there are none.
func (p CustomProvider) Scopes() []string { return slices.Clone(p.scopes) }

// PKCEEnabled reports whether the provider uses PKCE.
func (p CustomProvider) PKCEEnabled() bool { return p.pkceEnabled }

// AttributeMapping returns a copy of the claim-to-attribute mapping, or nil
// when there is none. The clone is shallow: nested values are shared
// references, a deliberate shallowness the public surface documents.
func (p CustomProvider) AttributeMapping() map[string]any {
	return maps.Clone(p.attributeMapping)
}

// CustomClaimsAllowlist returns a copy of the claim keys copied verbatim into
// custom claims, or nil when there are none.
func (p CustomProvider) CustomClaimsAllowlist() []string {
	return slices.Clone(p.customClaimsAllowlist)
}

// AuthorizationParams returns a copy of the extra authorization request
// parameters, or nil when there are none.
func (p CustomProvider) AuthorizationParams() map[string]string {
	return maps.Clone(p.authorizationParams)
}

// Enabled reports whether the provider accepts sign-ins.
func (p CustomProvider) Enabled() bool { return p.enabled }

// EmailOptional reports whether users may sign in without an email claim.
func (p CustomProvider) EmailOptional() bool { return p.emailOptional }

// Issuer returns an OIDC provider's issuer URL, or the empty string on an
// OAuth 2.0 provider.
func (p CustomProvider) Issuer() string { return p.issuer }

// DiscoveryURL returns an OIDC provider's explicit discovery document URL, or
// the empty string when the issuer's well-known location serves it.
func (p CustomProvider) DiscoveryURL() string { return p.discoveryURL }

// SkipNonceCheck reports whether an OIDC provider skips ID token nonce
// verification.
func (p CustomProvider) SkipNonceCheck() bool { return p.skipNonceCheck }

// DiscoveryDocument returns the OIDC discovery document the server fetched
// and cached, reporting false when the provider has none.
func (p CustomProvider) DiscoveryDocument() (DiscoveryDocument, bool) {
	if p.discoveryDocument == nil {
		return DiscoveryDocument{}, false
	}
	return *p.discoveryDocument, true
}

// AuthorizationURL returns an OAuth 2.0 provider's authorization endpoint, or
// the empty string on an OIDC provider.
func (p CustomProvider) AuthorizationURL() string { return p.authorizationURL }

// TokenURL returns an OAuth 2.0 provider's token endpoint, or the empty
// string on an OIDC provider.
func (p CustomProvider) TokenURL() string { return p.tokenURL }

// UserinfoURL returns an OAuth 2.0 provider's userinfo endpoint, or the empty
// string on an OIDC provider.
func (p CustomProvider) UserinfoURL() string { return p.userinfoURL }

// JWKSURI returns an OAuth 2.0 provider's JWKS endpoint, or the empty string
// when it has none.
func (p CustomProvider) JWKSURI() string { return p.jwksURI }

// CreatedAt returns when the provider was registered.
func (p CustomProvider) CreatedAt() time.Time { return p.createdAt }

// UpdatedAt returns when the provider was last changed.
func (p CustomProvider) UpdatedAt() time.Time { return p.updatedAt }

// DiscoveryDocument is the OIDC discovery document the Auth server fetched
// from a provider's issuer and stored alongside the provider.
type DiscoveryDocument struct {
	issuer                 string
	authorizationEndpoint  string
	tokenEndpoint          string
	userinfoEndpoint       string
	jwksURI                string
	scopesSupported        []string
	responseTypesSupported []string
	grantTypesSupported    []string
	subjectTypesSupported  []string
}

// Issuer returns the issuer the document declares.
func (d DiscoveryDocument) Issuer() string { return d.issuer }

// AuthorizationEndpoint returns the discovered authorization endpoint.
func (d DiscoveryDocument) AuthorizationEndpoint() string { return d.authorizationEndpoint }

// TokenEndpoint returns the discovered token endpoint.
func (d DiscoveryDocument) TokenEndpoint() string { return d.tokenEndpoint }

// UserinfoEndpoint returns the discovered userinfo endpoint, or the empty
// string when the document declares none.
func (d DiscoveryDocument) UserinfoEndpoint() string { return d.userinfoEndpoint }

// JWKSURI returns the discovered JWKS endpoint.
func (d DiscoveryDocument) JWKSURI() string { return d.jwksURI }

// ScopesSupported returns a copy of the scopes the document declares, or nil
// when it declares none.
func (d DiscoveryDocument) ScopesSupported() []string { return slices.Clone(d.scopesSupported) }

// ResponseTypesSupported returns a copy of the response types the document
// declares, or nil when it declares none.
func (d DiscoveryDocument) ResponseTypesSupported() []string {
	return slices.Clone(d.responseTypesSupported)
}

// GrantTypesSupported returns a copy of the grant types the document
// declares, or nil when it declares none.
func (d DiscoveryDocument) GrantTypesSupported() []string {
	return slices.Clone(d.grantTypesSupported)
}

// SubjectTypesSupported returns a copy of the subject types the document
// declares, or nil when it declares none.
func (d DiscoveryDocument) SubjectTypesSupported() []string {
	return slices.Clone(d.subjectTypesSupported)
}

// providerWire is the JSON shape of the Auth server's custom provider
// resource. The server never serves the client secret. Optional timestamps
// are pointers so an absent field stays the zero time rather than the Unix
// epoch.
type providerWire struct {
	ID                    string            `json:"id"`
	ProviderType          string            `json:"provider_type"`
	Identifier            string            `json:"identifier"`
	Name                  string            `json:"name"`
	ClientID              string            `json:"client_id"`
	AcceptableClientIDs   []string          `json:"acceptable_client_ids"`
	Scopes                []string          `json:"scopes"`
	PKCEEnabled           bool              `json:"pkce_enabled"`
	AttributeMapping      map[string]any    `json:"attribute_mapping"`
	CustomClaimsAllowlist []string          `json:"custom_claims_allowlist"`
	AuthorizationParams   map[string]string `json:"authorization_params"`
	Enabled               bool              `json:"enabled"`
	EmailOptional         bool              `json:"email_optional"`
	Issuer                string            `json:"issuer"`
	DiscoveryURL          string            `json:"discovery_url"`
	SkipNonceCheck        bool              `json:"skip_nonce_check"`
	DiscoveryDocument     *discoveryWire    `json:"discovery_document"`
	AuthorizationURL      string            `json:"authorization_url"`
	TokenURL              string            `json:"token_url"`
	UserinfoURL           string            `json:"userinfo_url"`
	JWKSURI               string            `json:"jwks_uri"`
	CreatedAt             *time.Time        `json:"created_at"`
	UpdatedAt             *time.Time        `json:"updated_at"`
}

// discoveryWire is the JSON shape of a stored OIDC discovery document.
type discoveryWire struct {
	Issuer                 string   `json:"issuer"`
	AuthorizationEndpoint  string   `json:"authorization_endpoint"`
	TokenEndpoint          string   `json:"token_endpoint"`
	UserinfoEndpoint       string   `json:"userinfo_endpoint"`
	JWKSURI                string   `json:"jwks_uri"`
	ScopesSupported        []string `json:"scopes_supported"`
	ResponseTypesSupported []string `json:"response_types_supported"`
	GrantTypesSupported    []string `json:"grant_types_supported"`
	SubjectTypesSupported  []string `json:"subject_types_supported"`
}

// Parse decodes the Auth server's provider JSON into a [CustomProvider],
// returning the decode error verbatim when the body is not a provider object;
// the caller owns the consumer-facing wrapping.
func Parse(body []byte) (CustomProvider, error) {
	var wire providerWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return CustomProvider{}, err
	}
	return fromWire(wire), nil
}

// ParseList decodes the Auth server's provider listing, whose wire shape
// wraps the providers in an envelope object.
func ParseList(body []byte) ([]CustomProvider, error) {
	var envelope struct {
		Providers []providerWire `json:"providers"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	providers := make([]CustomProvider, len(envelope.Providers))
	for i, wire := range envelope.Providers {
		providers[i] = fromWire(wire)
	}
	return providers, nil
}

// fromWire builds one [CustomProvider] from its decoded wire shape.
func fromWire(wire providerWire) CustomProvider {
	provider := CustomProvider{
		id:                    wire.ID,
		providerType:          wire.ProviderType,
		identifier:            wire.Identifier,
		name:                  wire.Name,
		clientID:              wire.ClientID,
		acceptableClientIDs:   wire.AcceptableClientIDs,
		scopes:                wire.Scopes,
		pkceEnabled:           wire.PKCEEnabled,
		attributeMapping:      wire.AttributeMapping,
		customClaimsAllowlist: wire.CustomClaimsAllowlist,
		authorizationParams:   wire.AuthorizationParams,
		enabled:               wire.Enabled,
		emailOptional:         wire.EmailOptional,
		issuer:                wire.Issuer,
		discoveryURL:          wire.DiscoveryURL,
		skipNonceCheck:        wire.SkipNonceCheck,
		authorizationURL:      wire.AuthorizationURL,
		tokenURL:              wire.TokenURL,
		userinfoURL:           wire.UserinfoURL,
		jwksURI:               wire.JWKSURI,
		createdAt:             timeOrZero(wire.CreatedAt),
		updatedAt:             timeOrZero(wire.UpdatedAt),
	}
	if wire.DiscoveryDocument != nil {
		provider.discoveryDocument = &DiscoveryDocument{
			issuer:                 wire.DiscoveryDocument.Issuer,
			authorizationEndpoint:  wire.DiscoveryDocument.AuthorizationEndpoint,
			tokenEndpoint:          wire.DiscoveryDocument.TokenEndpoint,
			userinfoEndpoint:       wire.DiscoveryDocument.UserinfoEndpoint,
			jwksURI:                wire.DiscoveryDocument.JWKSURI,
			scopesSupported:        wire.DiscoveryDocument.ScopesSupported,
			responseTypesSupported: wire.DiscoveryDocument.ResponseTypesSupported,
			grantTypesSupported:    wire.DiscoveryDocument.GrantTypesSupported,
			subjectTypesSupported:  wire.DiscoveryDocument.SubjectTypesSupported,
		}
	}
	return provider
}

// timeOrZero dereferences an optional wire timestamp, yielding the zero time
// when the field was absent.
func timeOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
