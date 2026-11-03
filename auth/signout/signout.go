// Package signout carries the scope vocabulary the SDK's sign-out calls
// take: which of the user's sessions a revocation reaches, expressed as
// functional options. Every call states its scope on the wire, and a call
// made with no options states the global scope, the server's own default.
package signout

import "github.com/supabase/supabase-go/auth/internal/scopes"

// Parameters is the resolved set of sign-out parameters for one call. The
// zero value holds none.
type Parameters struct {
	scope string
}

// Option sets one sign-out parameter. Options are applied in the order they
// are supplied, the last setting of a parameter winning.
type Option func(*Parameters)

// Resolve applies options over the global scope and returns the result.
func Resolve(options ...Option) Parameters {
	parameters := Parameters{scope: scopes.Global}
	for _, option := range options {
		option(&parameters)
	}
	return parameters
}

// WithLocalScope revokes only the session the presented token belongs to.
func WithLocalScope() Option {
	return func(parameters *Parameters) {
		parameters.scope = scopes.Local
	}
}

// WithOthersScope revokes every session except the one the presented token
// belongs to.
func WithOthersScope() Option {
	return func(parameters *Parameters) {
		parameters.scope = scopes.Others
	}
}

// Scope returns the chosen scope, global when no option narrowed the
// revocation.
func (p Parameters) Scope() string { return p.scope }
