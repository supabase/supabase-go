package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/supabase/supabase-go/auth/internal/session"
	"github.com/supabase/supabase-go/auth/messaging"
)

// UserUpdate describes changes to a user's own account. A zero field leaves
// its attribute unchanged. Application metadata is the project's to set, not
// the user's.
type UserUpdate struct {
	// Email is a new address, confirmed from that address before it takes
	// effect. A project that confirms a change from both addresses sends a
	// code to each.
	Email string `json:"email,omitzero"`

	// Phone is a new number, confirmed from that number before it takes
	// effect.
	Phone string `json:"phone,omitzero"`

	// Channel is the route a new phone's confirmation code takes,
	// [messaging.ChannelSMS] when unset.
	Channel messaging.Channel `json:"channel,omitzero"`

	// Password is a new password, checked against the project's strength
	// policy. The server rejects one equal to the current password with
	// same_password.
	Password string `json:"password,omitzero"`

	// CurrentPassword accompanies a new Password when the project requires
	// it, which the server states with current_password_required, and a wrong
	// one is rejected with current_password_mismatch.
	CurrentPassword string `json:"current_password,omitzero"`

	// Nonce is a reauthentication code, which accompanies a new Password
	// when the project requires reauthentication and the session is older
	// than a day, which the server states with reauthentication_needed.
	Nonce string `json:"nonce,omitzero"`

	// UserMetadata is merged into the user-controlled metadata per key.
	UserMetadata map[string]any `json:"data,omitzero"`

	// RedirectTo is where the confirmation link in an email lands. The server
	// replaces a URL outside the project's redirect allow-list with its Site
	// URL.
	RedirectTo string `json:"-"`
}

// UpdateUser applies update to the account jwt authenticates and returns the
// profile as the server now holds it. A new email or phone appears as the
// pending value, [User.NewEmail] or [User.NewPhone], until it is confirmed.
//
// An empty jwt returns [ErrMissingJWT] before any request is sent. The
// server's rejections are returned as [*Error], with a Code such as:
//   - email_exists or phone_exists, for an address or number another
//     account already has
//   - weak_password, for a password the project's policy refuses
//   - insufficient_aal, for a new email, phone or password from a user with
//     multi-factor authentication whose session has not passed it
//   - over_email_send_rate_limit or over_sms_send_rate_limit, when the
//     confirmation of a new email or phone exceeds the project's send limits
//   - over_request_rate_limit, when updates from the caller's IP address
//     exceed the project's limit
//
// The codes a password change's own requirements bring are named on
// [UserUpdate]'s fields.
func (c *Client) UpdateUser(ctx context.Context, jwt string, update UserUpdate) (*UpdateUserResult, error) {
	if jwt == "" {
		return nil, ErrMissingJWT
	}
	document, err := c.putUser(ctx, jwt, update)
	if err != nil {
		return nil, err
	}
	user, err := parseUser(document)
	if err != nil {
		return nil, err
	}
	return &UpdateUserResult{user: user}, nil
}

// UpdateUser applies update to this session's account, as [Client.UpdateUser]
// does with the session's access token, and replaces [Session.User] with the
// profile the server returns. It delivers [SessionEventUserUpdated] to the
// session's listeners once the session's store holds the new profile.
//
// The access token is obtained as [Session.AccessToken] obtains it, with the
// same errors.
//
// An update the server answers after the session has ended returns the
// server's answer and changes nothing locally. A store that cannot save the
// updated profile is reported with [ErrSessionNotSaved], the session holding
// the profile regardless.
func (s *Session) UpdateUser(ctx context.Context, update UserUpdate) (*UpdateUserResult, error) {
	bearer, err := s.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	document, err := s.client.putUser(ctx, bearer, update)
	if err != nil {
		return nil, err
	}
	user, err := parseUser(document)
	if err != nil {
		return nil, err
	}
	writeErr, err := s.inner.UpdateUser(ctx, document)
	if _, ended := errors.AsType[*session.EndedError](err); ended {
		return &UpdateUserResult{user: user}, nil
	}
	if err != nil {
		return nil, err
	}
	if writeErr != nil {
		return nil, storeSaveError(writeErr)
	}
	return &UpdateUserResult{user: user}, nil
}

// putUser sends update to the user resource with bearer and returns the
// profile document the server answers with.
func (c *Client) putUser(ctx context.Context, bearer string, update UserUpdate) ([]byte, error) {
	endpoint := c.baseURL.JoinPath("user")
	endpoint.RawQuery = redirectQuery(update.RedirectTo)
	// The wrapper states the channel on the wire whenever a phone is set.
	channel := update.Channel
	if update.Phone != "" && channel == "" {
		channel = messaging.ChannelSMS
	}
	body := struct {
		UserUpdate
		Channel messaging.Channel `json:"channel,omitzero"`
	}{update, channel}
	return send(ctx, c.httpClient, http.MethodPut, endpoint, body, bearer)
}
