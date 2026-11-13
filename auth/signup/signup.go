// Package signup carries the options the SDK's sign-up calls take: where the
// confirmation link in an email lands, the channel a phone's confirmation code
// takes and the metadata stored on the new account. Each option is typed to
// the sign-ups that take it: by email address, by phone number or by either.
package signup

import (
	"github.com/supabase/supabase-go/auth/internal/call_options"
	"github.com/supabase/supabase-go/auth/messaging"
)

// EmailOption sets one parameter of a sign-up by email address. Options are
// applied in the order they are supplied, the last setting of a parameter
// winning.
type EmailOption interface {
	emailOption()
}

// PhoneOption sets one parameter of a sign-up by phone number. Options are
// applied in the order they are supplied, the last setting of a parameter
// winning.
type PhoneOption interface {
	phoneOption()
}

// Option sets one parameter that a sign-up by either an email address or a
// phone number takes.
type Option interface {
	EmailOption
	PhoneOption
}

// emailOnly is an option that a sign-up by email address alone takes.
type emailOnly struct{ call_options.Setter }

func (emailOnly) emailOption() {}

// phoneOnly is an option that a sign-up by phone number alone takes.
type phoneOnly struct{ call_options.Setter }

func (phoneOnly) phoneOption() {}

// either is an option that a sign-up by either takes.
type either struct{ call_options.Setter }

func (either) emailOption() {}

func (either) phoneOption() {}

// WithRedirectTo names where the confirmation link in the email lands. A URL
// that is not on the project's redirect allow list is ignored in favor of the
// project's defaults rather than rejected.
func WithRedirectTo(redirectURL string) EmailOption {
	return emailOnly{func(parameters *call_options.Parameters) {
		parameters.RedirectTo = redirectURL
	}}
}

// WithChannel chooses the route the confirmation code takes to the phone.
func WithChannel(channel messaging.Channel) PhoneOption {
	return phoneOnly{func(parameters *call_options.Parameters) {
		parameters.Channel = channel
	}}
}

// WithUserMetadata stores metadata, which the user can later change, on the
// new account. It is ignored for an address or number that already has an
// account.
func WithUserMetadata(metadata map[string]any) Option {
	return either{func(parameters *call_options.Parameters) {
		parameters.UserMetadata = metadata
	}}
}
