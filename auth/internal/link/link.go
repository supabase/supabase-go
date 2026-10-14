// Package link owns the admin generate-link request: the [Request] value the
// public builder and mode types wrap, accumulated phase by phase and marshaled
// once at send time.
package link

import "maps"

// Request is the state of one generate-link request. Methods return modified
// copies, and WithData clones its map, so forked builders never alias.
type Request struct {
	mode       string
	email      string
	password   string
	newEmail   string
	redirectTo string
	data       map[string]any
}

// New starts a request for email with no mode chosen.
func New(email string) Request { return Request{email: email} }

// WithRedirectTo returns the request with the landing URL set.
func (r Request) WithRedirectTo(redirectURL string) Request {
	r.redirectTo = redirectURL
	return r
}

// WithMode returns the request with the wire's type set.
func (r Request) WithMode(mode string) Request {
	r.mode = mode
	return r
}

// WithPassword returns the request with the signup password set.
func (r Request) WithPassword(password string) Request {
	r.password = password
	return r
}

// WithNewEmail returns the request with the email-change target set.
func (r Request) WithNewEmail(newEmail string) Request {
	r.newEmail = newEmail
	return r
}

// WithData returns the request holding its own clone of data, so a caller's
// later writes to the map never reach a request already built.
func (r Request) WithData(data map[string]any) Request {
	r.data = maps.Clone(data)
	return r
}

// payload is the generate_link wire body. Type and email are always sent,
// empty or not, because the server requires both and rejects their absence
// with its own message; the rest is sent only when set.
type payload struct {
	Type       string         `json:"type"`
	Email      string         `json:"email"`
	NewEmail   string         `json:"new_email,omitzero"`
	Password   string         `json:"password,omitzero"`
	Data       map[string]any `json:"data,omitzero"`
	RedirectTo string         `json:"redirect_to,omitzero"`
}

// Payload returns the request's wire body, ready for JSON encoding.
func (r Request) Payload() any {
	return payload{
		Type:       r.mode,
		Email:      r.email,
		NewEmail:   r.newEmail,
		Password:   r.password,
		Data:       r.data,
		RedirectTo: r.redirectTo,
	}
}
