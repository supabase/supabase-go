// Package userdelete carries the vocabulary the SDK's user-delete call
// takes: the mode of deletion, expressed as functional options. A call made
// with no options states nothing, leaving the server's documented default -
// the hard delete - in charge.
package userdelete

// Parameters is the resolved set of deletion parameters for one call. The
// zero value holds none.
type Parameters struct {
	softDelete bool
}

// Option sets one deletion parameter. Options are applied in the order they
// are supplied.
type Option func(*Parameters)

// Resolve applies options to an empty Parameters and returns the result.
func Resolve(options ...Option) Parameters {
	var parameters Parameters
	for _, option := range options {
		option(&parameters)
	}
	return parameters
}

// WithSoftDelete deactivates the user instead of removing them: sign-ins
// are disabled, every session and factor ends, the email and phone are
// replaced with obfuscated values, and the user stays readable through the
// admin surface with their deletion time set. A repeat soft delete succeeds
// and changes nothing.
func WithSoftDelete() Option {
	return func(parameters *Parameters) {
		parameters.softDelete = true
	}
}

// SoftDelete reports whether the soft mode was chosen.
func (p Parameters) SoftDelete() bool { return p.softDelete }
