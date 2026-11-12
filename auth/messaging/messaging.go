// Package messaging carries the channel vocabulary of the SDK's calls that
// send a code to a phone number: whether the code travels by text message or
// over WhatsApp.
package messaging

// Channel is the route a code takes to a phone.
type Channel string

const (
	// ChannelSMS is delivery by text message.
	ChannelSMS Channel = "sms"

	// ChannelWhatsApp is delivery over WhatsApp, which the project's SMS
	// provider must support.
	ChannelWhatsApp Channel = "whatsapp"
)
