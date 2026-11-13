// Package call_options resolves the options of the sign-up and one-time code
// calls into the [Parameters] they set. Every option type of packages signup
// and otp embeds a [Setter], through which [Resolve] applies it.
package call_options

import "github.com/supabase/supabase-go/auth/messaging"

// Parameters holds the parameters a call's options set.
type Parameters struct {
	RedirectTo          string
	Channel             messaging.Channel
	UserMetadata        map[string]any
	WithoutUserCreation bool
}

// Setter sets one parameter.
type Setter func(*Parameters)

func (s Setter) apply(parameters *Parameters) { s(parameters) }

// applier is satisfied by every option type that embeds a Setter.
type applier interface {
	apply(parameters *Parameters)
}

// Resolve applies options, in the order they are supplied, to parameters and
// returns the result. A nil option sets nothing.
func Resolve[O any](options []O, parameters Parameters) Parameters {
	for _, option := range options {
		if applicable, ok := any(option).(applier); ok {
			applicable.apply(&parameters)
		}
	}
	return parameters
}
