package auth

import "github.com/supabase/supabase-go/auth/internal/token"

// JWTHeader is the decoded header of the token [Client.GetClaims] verified:
// the envelope metadata describing how the token was signed, kept apart from
// the identity assertions [Claims] carries. A JWTHeader is immutable and safe
// for concurrent use by multiple goroutines. Instances come only from
// [Client.GetClaims].
type JWTHeader struct {
	inner token.Header
}

// Algorithm returns the alg header value: the signing algorithm the token
// declares, for example "ES256". Verification never trusts this declaration -
// it binds the algorithm from the project's published signing key instead.
func (h JWTHeader) Algorithm() string { return h.inner.Algorithm() }

// KeyID returns the kid header value: the id of the project signing key the
// token names, or the empty string when the token carried none.
func (h JWTHeader) KeyID() string { return h.inner.KeyID() }

// Type returns the typ header value, "JWT" for Supabase access tokens, or the
// empty string when the token carried none.
func (h JWTHeader) Type() string { return h.inner.Type() }
