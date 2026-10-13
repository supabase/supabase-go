package customprovider

import "maps"

// Definition is the state of one provider creation request. Methods return
// modified copies, and the map and slice setters clone their arguments, so
// forked builders never alias.
type Definition struct {
	providerType          string
	identifier            string
	name                  string
	clientID              string
	clientSecret          string
	scopes                []string
	acceptableClientIDs   []string
	customClaimsAllowlist []string
	attributeMapping      map[string]any
	authorizationParams   map[string]string
	pkceDisabled          bool
	startDisabled         bool
	emailOptional         bool
	issuer                string
	discoveryURL          string
	skipNonceCheck        bool
	authorizationURL      string
	tokenURL              string
	userinfoURL           string
	jwksURI               string
}

// New starts a definition holding what every provider needs, with no type
// chosen.
func New(identifier, name, clientID, clientSecret string) Definition {
	return Definition{identifier: identifier, name: name, clientID: clientID, clientSecret: clientSecret}
}

// WithOIDC returns the definition typed oidc with its issuer set.
func (d Definition) WithOIDC(issuer string) Definition {
	d.providerType = "oidc"
	d.issuer = issuer
	return d
}

// WithOAuth2 returns the definition typed oauth2 with its endpoints set.
func (d Definition) WithOAuth2(authorizationURL, tokenURL, userinfoURL string) Definition {
	d.providerType = "oauth2"
	d.authorizationURL = authorizationURL
	d.tokenURL = tokenURL
	d.userinfoURL = userinfoURL
	return d
}

// WithScopes returns the definition holding its own copy of scopes.
func (d Definition) WithScopes(scopes []string) Definition {
	d.scopes = append([]string{}, scopes...)
	return d
}

// WithAcceptableClientIDs returns the definition holding its own copy of ids.
func (d Definition) WithAcceptableClientIDs(ids []string) Definition {
	d.acceptableClientIDs = append([]string{}, ids...)
	return d
}

// WithCustomClaimsAllowlist returns the definition holding its own copy of
// claims.
func (d Definition) WithCustomClaimsAllowlist(claims []string) Definition {
	d.customClaimsAllowlist = append([]string{}, claims...)
	return d
}

// WithAttributeMapping returns the definition holding its own clone of
// mapping, so a caller's later writes to the map never reach a definition
// already built.
func (d Definition) WithAttributeMapping(mapping map[string]any) Definition {
	d.attributeMapping = maps.Clone(mapping)
	return d
}

// WithAuthorizationParams returns the definition holding its own clone of
// params, as WithAttributeMapping clones its mapping.
func (d Definition) WithAuthorizationParams(params map[string]string) Definition {
	d.authorizationParams = maps.Clone(params)
	return d
}

// WithoutPKCE returns the definition with PKCE disabled.
func (d Definition) WithoutPKCE() Definition {
	d.pkceDisabled = true
	return d
}

// WithStartDisabled returns the definition creating the provider disabled.
func (d Definition) WithStartDisabled() Definition {
	d.startDisabled = true
	return d
}

// WithEmailOptional returns the definition allowing sign-ins without an email
// claim.
func (d Definition) WithEmailOptional() Definition {
	d.emailOptional = true
	return d
}

// WithDiscoveryURL returns the definition with an explicit OIDC discovery
// document URL set.
func (d Definition) WithDiscoveryURL(discoveryURL string) Definition {
	d.discoveryURL = discoveryURL
	return d
}

// WithSkipNonceCheck returns the definition with OIDC nonce verification
// skipped.
func (d Definition) WithSkipNonceCheck() Definition {
	d.skipNonceCheck = true
	return d
}

// WithJWKSURI returns the definition with an OAuth 2.0 JWKS endpoint set.
func (d Definition) WithJWKSURI(jwksURI string) Definition {
	d.jwksURI = jwksURI
	return d
}

// definitionPayload is the provider creation wire body. The fields every
// provider requires are always sent, empty or not, because the server rejects
// their absence and their emptiness with the same message; the rest is sent
// only when set, deviating booleans as pointers because their absence is what
// elects the server's defaults.
type definitionPayload struct {
	ProviderType          string            `json:"provider_type"`
	Identifier            string            `json:"identifier"`
	Name                  string            `json:"name"`
	ClientID              string            `json:"client_id"`
	ClientSecret          string            `json:"client_secret"`
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
	JwksURI               string            `json:"jwks_uri,omitzero"`
}

// Payload returns the definition's wire body, ready for JSON encoding.
func (d Definition) Payload() any {
	payload := definitionPayload{
		ProviderType:          d.providerType,
		Identifier:            d.identifier,
		Name:                  d.name,
		ClientID:              d.clientID,
		ClientSecret:          d.clientSecret,
		Scopes:                d.scopes,
		AcceptableClientIDs:   d.acceptableClientIDs,
		CustomClaimsAllowlist: d.customClaimsAllowlist,
		AttributeMapping:      d.attributeMapping,
		AuthorizationParams:   d.authorizationParams,
		Issuer:                d.issuer,
		DiscoveryURL:          d.discoveryURL,
		AuthorizationURL:      d.authorizationURL,
		TokenURL:              d.tokenURL,
		UserinfoURL:           d.userinfoURL,
		JwksURI:               d.jwksURI,
	}
	if d.pkceDisabled {
		payload.PKCEEnabled = pointerTo(false)
	}
	if d.startDisabled {
		payload.Enabled = pointerTo(false)
	}
	if d.emailOptional {
		payload.EmailOptional = pointerTo(true)
	}
	if d.skipNonceCheck {
		payload.SkipNonceCheck = pointerTo(true)
	}
	return payload
}

// pointerTo returns a pointer to value, for the payload fields whose absence
// elects a server default.
func pointerTo(value bool) *bool { return &value }
