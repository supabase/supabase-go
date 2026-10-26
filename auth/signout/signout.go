// Package signout carries the scope vocabulary the SDK's sign-out calls
// take: which of the user's sessions a revocation reaches, expressed as
// functional options. A call made with no options states no scope, leaving
// the server's documented default in charge.
package signout

// Parameters is the resolved set of sign-out parameters for one call. The
// zero value holds none.
type Parameters struct {
	scope string
}

// Option sets one sign-out parameter. Options are applied in the order they
// are supplied, the last setting of a parameter winning.
type Option func(*Parameters)

// Resolve applies options to an empty Parameters and returns the result.
func Resolve(options ...Option) Parameters {
	var parameters Parameters
	for _, option := range options {
		option(&parameters)
	}
	return parameters
}

// WithLocalScope revokes only the session the presented token belongs to.
func WithLocalScope() Option {
	return func(parameters *Parameters) {
		parameters.scope = "local"
	}
}

// WithOthersScope revokes every session except the one the presented token
// belongs to.
func WithOthersScope() Option {
	return func(parameters *Parameters) {
		parameters.scope = "others"
	}
}

// Scope returns the chosen scope, empty when no option narrowed the
// revocation.
func (p Parameters) Scope() string { return p.scope }
