package auth

import (
	"context"
	"net/http"

	"github.com/supabase/supabase-go/auth/internal/call_options"
	"github.com/supabase/supabase-go/auth/messaging"
	"github.com/supabase/supabase-go/auth/signup"
)

// SignUpByEmail registers an account with an email address and password.
// When the project confirms accounts at once, the result carries the new
// user's session. Otherwise it carries none, and the server emails the
// address a confirmation.
//
// An empty email returns [ErrMissingEmailOrPhone] before any request is
// sent. The server's rejections are returned as [*Error], with a Code such as:
//   - user_already_exists, for an address that already has an account
//   - weak_password, for a password the project's policy refuses
//   - email_address_invalid, for an address that fails the project's email
//     validation
//   - signup_disabled, when the project accepts no sign-ups
//   - over_request_rate_limit, when the project's rate limit is reached
func (c *Client) SignUpByEmail(ctx context.Context, email, password string, options ...signup.EmailOption) (*SignUpResult, error) {
	parameters := call_options.Resolve(options, call_options.Parameters{})
	body := registrationPayload{Email: email, Password: password, Data: parameters.UserMetadata}
	return c.signUp(ctx, email, body, parameters.RedirectTo)
}

// SignUpByPhone registers an account with a phone number and password. When
// the project confirms accounts at once, the result carries the new user's
// session. Otherwise it carries none, and the server sends the number a
// code that [Client.VerifyOTPByPhone] redeems to confirm it. The code goes
// by text message unless [signup.WithChannel] chooses another channel.
//
// An empty phone returns [ErrMissingEmailOrPhone] before any request is
// sent. The server's rejections are returned as [*Error], with a Code such as:
//   - user_already_exists, for a number that already has an account
//   - weak_password, for a password the project's policy refuses
//   - phone_provider_disabled, when the project accepts no phone sign-ups
//   - signup_disabled, when the project accepts no sign-ups
//   - over_request_rate_limit, when the project's rate limit is reached
func (c *Client) SignUpByPhone(ctx context.Context, phone, password string, options ...signup.PhoneOption) (*SignUpResult, error) {
	parameters := call_options.Resolve(options, call_options.Parameters{Channel: messaging.ChannelSMS})
	body := registrationPayload{Phone: phone, Password: password, Channel: parameters.Channel, Data: parameters.UserMetadata}
	return c.signUp(ctx, phone, body, "")
}

// signUp sends body to the signup endpoint, with redirectTo on the query
// string when set, and shapes the answer: a token response into a result
// with a session, a bare user into one without. It refuses an empty
// identifier before sending, since the server would route a body with
// neither an email nor a phone to its anonymous sign-in.
func (c *Client) signUp(ctx context.Context, identifier string, body registrationPayload, redirectTo string) (*SignUpResult, error) {
	if identifier == "" {
		return nil, ErrMissingEmailOrPhone
	}
	endpoint := c.baseURL.JoinPath("signup")
	endpoint.RawQuery = redirectQuery(redirectTo)
	response, err := send(ctx, c.httpClient, http.MethodPost, endpoint, body, "")
	if err != nil {
		return nil, err
	}
	if !carriesSession(response) {
		user, userErr := parseUser(response)
		if userErr != nil {
			return nil, userErr
		}
		return &SignUpResult{user: user}, nil
	}
	state, err := parseTokenResponse(response)
	if err != nil {
		return nil, err
	}
	user, err := parseUser(state.User())
	if err != nil {
		return nil, err
	}
	return &SignUpResult{user: user, session: c.newSession(state, nil)}, nil
}

// registrationPayload is the wire body of a sign-up. password is always
// stated, since the server requires it, and channel whenever a phone is.
type registrationPayload struct {
	Email    string            `json:"email,omitzero"`
	Phone    string            `json:"phone,omitzero"`
	Password string            `json:"password"`
	Channel  messaging.Channel `json:"channel,omitzero"`
	Data     map[string]any    `json:"data,omitzero"`
}

// SignUpResult is the answer to [Client.SignUpByEmail] and
// [Client.SignUpByPhone].
type SignUpResult struct {
	user    *User
	session *Session
}

// User returns the user the sign-up registered. It is never nil.
//
// A project that requires confirmation can answer a sign-up for an address
// or number that already has an account as it would a new one, so as not to
// reveal the account. The user is then a stand-in whose ID matches no
// account.
func (r *SignUpResult) User() *User { return r.user }

// Session returns the new user's session, or nil when the project requires
// the user to confirm their address or number first.
func (r *SignUpResult) Session() *Session { return r.session }
