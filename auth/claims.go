package auth

import (
	"encoding/json"
	"slices"
	"time"
)

// Claims is the verified payload of an end-user JWT: who the user is and under
// which authentication circumstances the token was issued. A Claims is
// immutable, reporting the token exactly as verified, and is safe for
// concurrent use by multiple goroutines. Instances come only from
// [Client.GetClaims].
type Claims struct {
	issuer                      string
	subject                     string
	audience                    []string
	expiresAt                   time.Time
	issuedAt                    time.Time
	role                        string
	authenticatorAssuranceLevel string
	sessionID                   string
	email                       string
	phone                       string
	isAnonymous                 bool
	custom                      map[string]any
}

// Issuer returns the iss claim: the Auth server that issued the token.
func (c *Claims) Issuer() string { return c.issuer }

// Subject returns the sub claim: the authenticated user's ID.
func (c *Claims) Subject() string { return c.subject }

// Audience returns the aud claim: the token's intended audiences, usually the
// single value "authenticated". The result is a copy the caller may retain and
// modify freely.
func (c *Claims) Audience() []string { return slices.Clone(c.audience) }

// ExpiresAt returns the exp claim: the instant after which the token is no
// longer valid.
func (c *Claims) ExpiresAt() time.Time { return c.expiresAt }

// IssuedAt returns the iat claim: the instant the token was issued, or the zero
// time when the token carried no iat.
func (c *Claims) IssuedAt() time.Time { return c.issuedAt }

// Role returns the role claim: the Postgres role the database applies Row Level
// Security policies for, usually "authenticated".
func (c *Claims) Role() string { return c.role }

// AuthenticatorAssuranceLevel returns the aal claim: "aal1" for a single factor
// or "aal2" when a second factor was satisfied.
func (c *Claims) AuthenticatorAssuranceLevel() string { return c.authenticatorAssuranceLevel }

// SessionID returns the session_id claim: the ID of the session the token
// belongs to.
func (c *Claims) SessionID() string { return c.sessionID }

// Email returns the email claim, or the empty string when the token carried
// none.
func (c *Claims) Email() string { return c.email }

// Phone returns the phone claim, or the empty string when the token carried
// none.
func (c *Claims) Phone() string { return c.phone }

// IsAnonymous returns the is_anonymous claim: whether the token authenticates
// an anonymous user rather than a signed-in one.
func (c *Claims) IsAnonymous() bool { return c.isAnonymous }

// CustomClaim returns the named claim and whether the token carried it, for
// claims outside the accessors above - ones a project's Custom Access Token
// hook added, or amr. The value is decoded as [encoding/json] decodes into an
// any, so JSON objects are map[string]any, arrays are []any and numbers are
// float64.
func (c *Claims) CustomClaim(name string) (any, bool) {
	value, ok := c.custom[name]
	return value, ok
}

// audience is the aud claim, which the wire encodes as either a single string
// or an array of strings.
type audience []string

func (a *audience) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = audience{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

// claimsWire is the JSON shape of the registered and Supabase claims a Claims
// exposes through typed accessors.
type claimsWire struct {
	Issuer                      string   `json:"iss"`
	Subject                     string   `json:"sub"`
	Audience                    audience `json:"aud"`
	ExpiresAt                   int64    `json:"exp"`
	IssuedAt                    int64    `json:"iat"`
	Role                        string   `json:"role"`
	AuthenticatorAssuranceLevel string   `json:"aal"`
	SessionID                   string   `json:"session_id"`
	Email                       string   `json:"email"`
	Phone                       string   `json:"phone"`
	IsAnonymous                 bool     `json:"is_anonymous"`
}

// standardClaimNames are the claim keys claimsWire maps to typed accessors, so
// parseClaims can exclude them when building the custom-claim map.
var standardClaimNames = []string{
	"iss", "sub", "aud", "exp", "iat", "role",
	"aal", "session_id", "email", "phone", "is_anonymous",
}

// parseClaims decodes a token's claims bytes into a Claims, reporting
// [ErrMalformedJWT] when the bytes are not a JSON object. Claims beyond the
// typed accessors are retained for [Claims.CustomClaim].
func parseClaims(claimsBytes []byte) (*Claims, error) {
	var wire claimsWire
	if err := json.Unmarshal(claimsBytes, &wire); err != nil {
		return nil, ErrMalformedJWT
	}

	custom := map[string]any{}
	if err := json.Unmarshal(claimsBytes, &custom); err != nil {
		return nil, ErrMalformedJWT
	}
	for _, name := range standardClaimNames {
		delete(custom, name)
	}

	claims := &Claims{
		issuer:                      wire.Issuer,
		subject:                     wire.Subject,
		audience:                    wire.Audience,
		role:                        wire.Role,
		authenticatorAssuranceLevel: wire.AuthenticatorAssuranceLevel,
		sessionID:                   wire.SessionID,
		email:                       wire.Email,
		phone:                       wire.Phone,
		isAnonymous:                 wire.IsAnonymous,
		custom:                      custom,
	}
	if wire.ExpiresAt != 0 {
		claims.expiresAt = time.Unix(wire.ExpiresAt, 0).UTC()
	}
	if wire.IssuedAt != 0 {
		claims.issuedAt = time.Unix(wire.IssuedAt, 0).UTC()
	}
	return claims, nil
}
