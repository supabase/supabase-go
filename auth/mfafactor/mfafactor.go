// Package mfafactor carries the vocabulary the SDK's factor-update call
// takes: the fields an administrator may change on an enrolled MFA factor,
// expressed as functional options. A call made with no options changes
// nothing.
package mfafactor

// Parameters is the resolved set of factor changes for one call. The zero
// value holds none.
type Parameters struct {
	friendlyName string
	phone        string
}

// Option sets one factor change. Options are applied in the order they are
// supplied, the last setting of a parameter winning.
type Option func(*Parameters)

// Resolve applies options to an empty Parameters and returns the result.
func Resolve(options ...Option) Parameters {
	var parameters Parameters
	for _, option := range options {
		option(&parameters)
	}
	return parameters
}

// WithFriendlyName renames the factor.
func WithFriendlyName(name string) Option {
	return func(parameters *Parameters) {
		parameters.friendlyName = name
	}
}

// WithPhone moves a phone factor to a new number, in E.164 format, which
// the server validates. On any other factor type the server ignores the
// number while still answering with success.
func WithPhone(phone string) Option {
	return func(parameters *Parameters) {
		parameters.phone = phone
	}
}

// FriendlyName returns the chosen name, empty when no option renamed the
// factor.
func (p Parameters) FriendlyName() string { return p.friendlyName }

// Phone returns the chosen number, empty when no option moved the factor.
func (p Parameters) Phone() string { return p.phone }
