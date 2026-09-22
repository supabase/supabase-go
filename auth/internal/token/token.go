// Package token owns the JWT wire format and the claims state a token
// carries: [Decode] splits a compact JWT into the parts routing and
// verification need, and [Token.Claims] parses the payload into an opaque
// [Claims] value. No crypto and no HTTP live here, and errors are
// deliberately shapeless - any failure means the token is malformed, and the
// caller owns the consumer-facing sentinel.
package token

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

// errMalformed is the single failure this package reports: the token is not a
// three-part base64url JWT carrying a JSON header and JSON claims.
var errMalformed = errors.New("malformed JWT")

// headerWire is the JSON shape of the JWT header fields routing, verification
// and the public surface need.
type headerWire struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Type      string `json:"typ"`
}

// Header is the opaque decoded header of one token: unexported fields readable
// only through accessors, mirroring [Claims].
type Header struct {
	algorithm string
	keyID     string
	typ       string
}

// Algorithm returns the raw alg value the token header declares.
func (h Header) Algorithm() string { return h.algorithm }

// KeyID returns the kid value the token header declares, or the empty string
// when it carries none.
func (h Header) KeyID() string { return h.keyID }

// Type returns the typ value the token header declares, or the empty string
// when it carries none.
func (h Header) Type() string { return h.typ }

// Token is a decoded JWT: the header for routing, the claims bytes and the
// signature over the signing input.
type Token struct {
	header       Header
	claimsBytes  []byte
	signature    []byte
	signingInput string
}

// Header returns the decoded token header.
func (t Token) Header() Header { return t.header }

// SigningInput returns the "header.payload" prefix a signature covers.
func (t Token) SigningInput() string { return t.signingInput }

// Signature returns the decoded signature bytes as a fresh copy each call, so
// no caller can reach the state another caller received.
func (t Token) Signature() []byte { return slices.Clone(t.signature) }

// Decode splits and base64url-decodes a JWT into its parts. Any error means
// the token is malformed.
func Decode(jwt string) (Token, error) {
	firstDot := strings.IndexByte(jwt, '.')
	lastDot := strings.LastIndexByte(jwt, '.')
	if firstDot <= 0 || lastDot <= firstDot || lastDot == len(jwt)-1 {
		return Token{}, errMalformed
	}
	rawHeader := jwt[:firstDot]
	rawPayload := jwt[firstDot+1 : lastDot]
	rawSignature := jwt[lastDot+1:]
	if strings.IndexByte(rawPayload, '.') != -1 {
		return Token{}, errMalformed
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(rawHeader)
	if err != nil {
		return Token{}, errMalformed
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(rawPayload)
	if err != nil {
		return Token{}, errMalformed
	}
	signature, err := base64.RawURLEncoding.DecodeString(rawSignature)
	if err != nil {
		return Token{}, errMalformed
	}

	var wire headerWire
	if err := json.Unmarshal(headerBytes, &wire); err != nil {
		return Token{}, errMalformed
	}

	return Token{
		header:       Header{algorithm: wire.Algorithm, keyID: wire.KeyID, typ: wire.Type},
		claimsBytes:  claimsBytes,
		signature:    signature,
		signingInput: jwt[:lastDot],
	}, nil
}

// Claims is the opaque claims state of one token: unexported fields readable
// only through accessors, so no code outside this package can express a
// mutation of its contents. The defensive copies live here, next to the state
// they protect.
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

// Issuer returns the iss claim.
func (c Claims) Issuer() string { return c.issuer }

// Subject returns the sub claim.
func (c Claims) Subject() string { return c.subject }

// Audience returns a copy of the aud claim.
func (c Claims) Audience() []string { return slices.Clone(c.audience) }

// ExpiresAt returns the exp claim, or the zero time when the token carried
// none.
func (c Claims) ExpiresAt() time.Time { return c.expiresAt }

// IssuedAt returns the iat claim, or the zero time when the token carried
// none.
func (c Claims) IssuedAt() time.Time { return c.issuedAt }

// Role returns the role claim.
func (c Claims) Role() string { return c.role }

// AuthenticatorAssuranceLevel returns the aal claim.
func (c Claims) AuthenticatorAssuranceLevel() string { return c.authenticatorAssuranceLevel }

// SessionID returns the session_id claim.
func (c Claims) SessionID() string { return c.sessionID }

// Email returns the email claim, or the empty string when the token carried
// none.
func (c Claims) Email() string { return c.email }

// Phone returns the phone claim, or the empty string when the token carried
// none.
func (c Claims) Phone() string { return c.phone }

// IsAnonymous returns the is_anonymous claim.
func (c Claims) IsAnonymous() bool { return c.isAnonymous }

// CustomClaim returns the named claim and whether the token carried it, for
// claims outside the typed accessors. Values are as [encoding/json] decodes
// into an any, and nested maps and slices are shared references - a deliberate
// shallowness the public surface documents.
func (c Claims) CustomClaim(name string) (any, bool) {
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
// Claims parsing can exclude them when building the custom-claim map.
var standardClaimNames = []string{
	"iss", "sub", "aud", "exp", "iat", "role",
	"aal", "session_id", "email", "phone", "is_anonymous",
}

// Claims parses the token's claims bytes. Claims beyond the typed accessors
// are retained for [Claims.CustomClaim]. Any error means the token is
// malformed.
func (t Token) Claims() (Claims, error) {
	var wire claimsWire
	if err := json.Unmarshal(t.claimsBytes, &wire); err != nil {
		return Claims{}, errMalformed
	}

	custom := map[string]any{}
	if err := json.Unmarshal(t.claimsBytes, &custom); err != nil {
		return Claims{}, errMalformed
	}
	for _, name := range standardClaimNames {
		delete(custom, name)
	}

	claims := Claims{
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
