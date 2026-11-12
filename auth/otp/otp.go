// Package otp carries the vocabulary of the SDK's one-time code calls: the
// options a send takes and the kinds of code a verification names. Each
// option is typed to the sends that take it: to an email address, to a phone
// number or to either.
package otp

import (
	"github.com/supabase/supabase-go/auth/internal/call_options"
	"github.com/supabase/supabase-go/auth/messaging"
)

// EmailType is the kind of a code sent to an email address, as the server
// names it. The constants are the kinds the server issues today.
type EmailType string

const (
	// TypeEmail is a sign-in or sign-up code.
	TypeEmail EmailType = "email"

	// TypeRecovery is a password-recovery code.
	TypeRecovery EmailType = "recovery"

	// TypeInvite is an invitation code.
	TypeInvite EmailType = "invite"

	// TypeEmailChange is a code confirming a new email address.
	TypeEmailChange EmailType = "email_change"
)

// PhoneType is the kind of a code sent to a phone number, as the server
// names it. The constants are the kinds the server issues today.
type PhoneType string

const (
	// TypeSMS is a sign-in or sign-up code.
	TypeSMS PhoneType = "sms"

	// TypePhoneChange is a code confirming a new phone number.
	TypePhoneChange PhoneType = "phone_change"
)

// EmailOption sets one parameter of a send to an email address. Options are
// applied in the order they are supplied, the last setting of a parameter
// winning.
type EmailOption interface {
	emailOption()
}

// PhoneOption sets one parameter of a send to a phone number. Options are
// applied in the order they are supplied, the last setting of a parameter
// winning.
type PhoneOption interface {
	phoneOption()
}

// Option sets one parameter that a send to either an email address or a
// phone number takes.
type Option interface {
	EmailOption
	PhoneOption
}

// emailOnly is an option that a send to an email address alone takes.
type emailOnly struct{ call_options.Setter }

func (emailOnly) emailOption() {}

// phoneOnly is an option that a send to a phone number alone takes.
type phoneOnly struct{ call_options.Setter }

func (phoneOnly) phoneOption() {}

// either is an option that a send to either takes.
type either struct{ call_options.Setter }

func (either) emailOption() {}

func (either) phoneOption() {}

// WithRedirectTo names where the link in the email lands. A URL that is not
// on the project's redirect allow list is ignored in favor of the project's
// defaults rather than rejected.
func WithRedirectTo(redirectURL string) EmailOption {
	return emailOnly{func(parameters *call_options.Parameters) {
		parameters.RedirectTo = redirectURL
	}}
}

// WithChannel chooses the route the code takes to the phone.
func WithChannel(channel messaging.Channel) PhoneOption {
	return phoneOnly{func(parameters *call_options.Parameters) {
		parameters.Channel = channel
	}}
}

// WithUserMetadata stores metadata, which the user can later change, on an
// account the send creates. It is ignored for an address or number that
// already has an account.
func WithUserMetadata(metadata map[string]any) Option {
	return either{func(parameters *call_options.Parameters) {
		parameters.UserMetadata = metadata
	}}
}

// WithoutUserCreation has the server refuse an address or number with no
// account, with otp_disabled, rather than create one for it as it does by
// default.
func WithoutUserCreation() Option {
	return either{func(parameters *call_options.Parameters) {
		parameters.WithoutUserCreation = true
	}}
}
