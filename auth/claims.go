package auth

import (
	"time"

	"github.com/supabase/supabase-go/auth/internal/token"
)

// Claims is the verified payload of an end-user JWT: who the user is and under
// which authentication circumstances the token was issued. A Claims is
// immutable, reporting the token exactly as verified, and is safe for
// concurrent use by multiple goroutines.
type Claims struct {
	inner token.Claims
}

// Issuer returns the iss claim (see RFC 7519, Section 4.1.1): the Auth server
// that issued the token.
func (c *Claims) Issuer() string { return c.inner.Issuer() }

// Subject returns the sub claim (see RFC 7519, Section 4.1.2): the authenticated
// user's ID.
func (c *Claims) Subject() string { return c.inner.Subject() }

// Audience returns the aud claim (see RFC 7519, Section 4.1.3): the token's
// intended audiences, usually the single value "authenticated" (see
// [Supabase audience values]). The result is a copy the caller may retain
// and modify freely.
//
// [Supabase audience values]: https://supabase.com/docs/guides/auth/jwt-fields#audience-values--aud-
func (c *Claims) Audience() []string { return c.inner.Audience() }

// ExpiresAt returns the exp claim (see RFC 7519, Section 4.1.4): the instant
// after which the token is no longer valid.
func (c *Claims) ExpiresAt() time.Time { return c.inner.ExpiresAt() }

// IssuedAt returns the iat claim (see RFC 7519, Section 4.1.6): the instant the
// token was issued, or the zero time when the token carried no iat.
func (c *Claims) IssuedAt() time.Time { return c.inner.IssuedAt() }

// Role returns the role claim (see [Supabase JWT fields]): the Postgres role the
// database applies Row Level Security policies for, usually "authenticated".
//
// [Supabase JWT fields]: https://supabase.com/docs/guides/auth/jwt-fields#role-values--role-
func (c *Claims) Role() string { return c.inner.Role() }

// AuthenticatorAssuranceLevel returns the aal claim (see [Supabase JWT fields]):
// "aal1" for a single factor or "aal2" when a second factor was satisfied.
//
// [Supabase JWT fields]: https://supabase.com/docs/guides/auth/jwt-fields#authenticator-assurance-level--aal-
func (c *Claims) AuthenticatorAssuranceLevel() string { return c.inner.AuthenticatorAssuranceLevel() }

// SessionID returns the session_id claim (see [Supabase JWT fields]): the ID of
// the session the token belongs to.
//
// [Supabase JWT fields]: https://supabase.com/docs/guides/auth/jwt-fields#required-claims
func (c *Claims) SessionID() string { return c.inner.SessionID() }

// Email returns the email claim (see [Supabase JWT fields]), or the empty string
// when the token carried none.
//
// [Supabase JWT fields]: https://supabase.com/docs/guides/auth/jwt-fields#required-claims
func (c *Claims) Email() string { return c.inner.Email() }

// Phone returns the phone claim (see [Supabase JWT fields]), or the empty string
// when the token carried none.
//
// [Supabase JWT fields]: https://supabase.com/docs/guides/auth/jwt-fields#required-claims
func (c *Claims) Phone() string { return c.inner.Phone() }

// IsAnonymous returns the is_anonymous claim (see [Supabase JWT fields]): whether
// the token authenticates an anonymous user rather than a signed-in one.
//
// [Supabase JWT fields]: https://supabase.com/docs/guides/auth/jwt-fields#required-claims
func (c *Claims) IsAnonymous() bool { return c.inner.IsAnonymous() }

// CustomClaim returns the named claim and whether the token carried it, for
// claims without a dedicated accessor. That covers claims a project's
// [Custom Access Token hook] added and the Supabase [optional claims] with no
// dedicated accessor here, amr for example. The value is decoded as
// [encoding/json] decodes into an any, so JSON objects are map[string]any,
// arrays are []any and numbers are float64.
//
// [Custom Access Token hook]: https://supabase.com/docs/guides/auth/auth-hooks/custom-access-token-hook
// [optional claims]: https://supabase.com/docs/guides/auth/jwt-fields#optional-claims
func (c *Claims) CustomClaim(name string) (any, bool) { return c.inner.CustomClaim(name) }
