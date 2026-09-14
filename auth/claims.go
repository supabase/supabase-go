package auth

import (
	"time"

	"github.com/supabase/supabase-go/auth/internal/token"
)

// Claims is the verified payload of an end-user JWT: who the user is and under
// which authentication circumstances the token was issued. A Claims is
// immutable, reporting the token exactly as verified, and is safe for
// concurrent use by multiple goroutines. Instances come only from
// [Client.GetClaims].
type Claims struct {
	inner token.Claims
}

// Issuer returns the iss claim: the Auth server that issued the token.
func (c *Claims) Issuer() string { return c.inner.Issuer() }

// Subject returns the sub claim: the authenticated user's ID.
func (c *Claims) Subject() string { return c.inner.Subject() }

// Audience returns the aud claim: the token's intended audiences, usually the
// single value "authenticated". The result is a copy the caller may retain and
// modify freely.
func (c *Claims) Audience() []string { return c.inner.Audience() }

// ExpiresAt returns the exp claim: the instant after which the token is no
// longer valid.
func (c *Claims) ExpiresAt() time.Time { return c.inner.ExpiresAt() }

// IssuedAt returns the iat claim: the instant the token was issued, or the zero
// time when the token carried no iat.
func (c *Claims) IssuedAt() time.Time { return c.inner.IssuedAt() }

// Role returns the role claim: the Postgres role the database applies Row Level
// Security policies for, usually "authenticated".
func (c *Claims) Role() string { return c.inner.Role() }

// AuthenticatorAssuranceLevel returns the aal claim: "aal1" for a single factor
// or "aal2" when a second factor was satisfied.
func (c *Claims) AuthenticatorAssuranceLevel() string { return c.inner.AuthenticatorAssuranceLevel() }

// SessionID returns the session_id claim: the ID of the session the token
// belongs to.
func (c *Claims) SessionID() string { return c.inner.SessionID() }

// Email returns the email claim, or the empty string when the token carried
// none.
func (c *Claims) Email() string { return c.inner.Email() }

// Phone returns the phone claim, or the empty string when the token carried
// none.
func (c *Claims) Phone() string { return c.inner.Phone() }

// IsAnonymous returns the is_anonymous claim: whether the token authenticates
// an anonymous user rather than a signed-in one.
func (c *Claims) IsAnonymous() bool { return c.inner.IsAnonymous() }

// CustomClaim returns the named claim and whether the token carried it, for
// claims outside the accessors above - ones a project's Custom Access Token
// hook added, or amr. The value is decoded as [encoding/json] decodes into an
// any, so JSON objects are map[string]any, arrays are []any and numbers are
// float64.
func (c *Claims) CustomClaim(name string) (any, bool) { return c.inner.CustomClaim(name) }
