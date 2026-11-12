package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/supabase/supabase-go/auth/internal/call_options"
	"github.com/supabase/supabase-go/auth/messaging"
	"github.com/supabase/supabase-go/auth/otp"
)

// SignInWithOTPByEmail emails an address a one-time code, or a magic link
// when the project's email template carries one instead.
// [Client.VerifyOTPByEmail] redeems the code, and [Client.VerifyOTPByTokenHash]
// the token hash a link carries. An address with no account gets one, unless
// [otp.WithoutUserCreation] is supplied.
//
// The server's rejections are returned as [*Error], with a Code such as:
//   - otp_disabled, for an address with no account under
//     [otp.WithoutUserCreation]
//   - over_email_send_rate_limit, for a send to an address within the
//     project's minimum interval since the last send to it, or beyond the
//     project's limit on emails sent
//   - over_request_rate_limit, when sends from the caller's IP address
//     exceed the project's limit
func (c *Client) SignInWithOTPByEmail(ctx context.Context, email string, options ...otp.EmailOption) (*SignInWithOTPResult, error) {
	parameters := call_options.Resolve(options, call_options.Parameters{})
	body := otpPayload{Email: email, CreateUser: !parameters.WithoutUserCreation, Data: parameters.UserMetadata}
	return c.sendOTP(ctx, body, parameters.RedirectTo)
}

// SignInWithOTPByPhone sends a phone number a one-time code, for
// [Client.VerifyOTPByPhone] to redeem. The code goes by text message unless
// [otp.WithChannel] chooses another channel. A number with no account gets
// one, unless [otp.WithoutUserCreation] is supplied.
//
// The server's rejections are returned as [*Error], with a Code such as:
//   - otp_disabled, for a number with no account under
//     [otp.WithoutUserCreation]
//   - phone_provider_disabled, when the project has no phone sign-in
//   - over_sms_send_rate_limit, for a send to a number within the project's
//     minimum interval since the last send to it, or beyond the project's
//     limit on messages sent to phone numbers
//   - over_request_rate_limit, when sends from the caller's IP address
//     exceed the project's limit
func (c *Client) SignInWithOTPByPhone(ctx context.Context, phone string, options ...otp.PhoneOption) (*SignInWithOTPResult, error) {
	parameters := call_options.Resolve(options, call_options.Parameters{Channel: messaging.ChannelSMS})
	body := otpPayload{Phone: phone, Channel: parameters.Channel, CreateUser: !parameters.WithoutUserCreation, Data: parameters.UserMetadata}
	return c.sendOTP(ctx, body, "")
}

// sendOTP sends body to the otp endpoint, with redirectTo on the query
// string when set.
func (c *Client) sendOTP(ctx context.Context, body otpPayload, redirectTo string) (*SignInWithOTPResult, error) {
	endpoint := c.baseURL.JoinPath("otp")
	endpoint.RawQuery = redirectQuery(redirectTo)
	if _, err := send(ctx, c.httpClient, http.MethodPost, endpoint, body, ""); err != nil {
		return nil, err
	}
	return &SignInWithOTPResult{}, nil
}

// otpPayload is the wire body of a send. create_user is always stated, since
// the server's default is to create, and channel whenever a phone is.
type otpPayload struct {
	Email      string            `json:"email,omitzero"`
	Phone      string            `json:"phone,omitzero"`
	Channel    messaging.Channel `json:"channel,omitzero"`
	CreateUser bool              `json:"create_user"`
	Data       map[string]any    `json:"data,omitzero"`
}

// SignInWithOTPResult is the answer to [Client.SignInWithOTPByEmail] and
// [Client.SignInWithOTPByPhone]. It carries nothing: a send signs nobody in.
type SignInWithOTPResult struct{}

// VerifyOTPByEmail redeems token, a code of the given kind that was sent to
// email, signing its user in.
//
// The server's rejections are returned as [*Error], with a Code such as:
//   - otp_expired, for a wrong or expired code
//   - over_request_rate_limit, when verifications from the caller's IP
//     address exceed the project's limit
func (c *Client) VerifyOTPByEmail(ctx context.Context, email, token string, kind otp.EmailType) (*VerifyOTPResult, error) {
	return c.verifyOTP(ctx, verifyPayload{Email: email, Token: token, Type: string(kind)})
}

// VerifyOTPByPhone redeems token, a code of the given kind that was sent to
// phone, signing its user in.
//
// The server's rejections are returned as [*Error], with a Code such as:
//   - otp_expired, for a wrong or expired code
//   - over_request_rate_limit, when verifications from the caller's IP
//     address exceed the project's limit
func (c *Client) VerifyOTPByPhone(ctx context.Context, phone, token string, kind otp.PhoneType) (*VerifyOTPResult, error) {
	return c.verifyOTP(ctx, verifyPayload{Phone: phone, Token: token, Type: string(kind)})
}

// VerifyOTPByTokenHash redeems the code of the given kind that tokenHash
// names, signing its user in. The token hash is the one an email's link
// carries, or [GenerateLinkResult.HashedToken], and names the code and its
// address on its own.
//
// The server's rejections are returned as [*Error], with a Code such as:
//   - otp_expired, for a wrong or expired hash
//   - over_request_rate_limit, when verifications from the caller's IP
//     address exceed the project's limit
func (c *Client) VerifyOTPByTokenHash(ctx context.Context, tokenHash string, kind otp.EmailType) (*VerifyOTPResult, error) {
	return c.verifyOTP(ctx, verifyPayload{TokenHash: tokenHash, Type: string(kind)})
}

// verifyOTP sends body to the verify endpoint and shapes the answer: a token
// response into a result with a session, an acknowledgment into one without.
func (c *Client) verifyOTP(ctx context.Context, body verifyPayload) (*VerifyOTPResult, error) {
	response, err := send(ctx, c.httpClient, http.MethodPost, c.baseURL.JoinPath("verify"), body, "")
	if err != nil {
		return nil, err
	}
	if !carriesSession(response) {
		return &VerifyOTPResult{}, nil
	}
	state, err := parseTokenResponse(response)
	if err != nil {
		return nil, err
	}
	return &VerifyOTPResult{session: c.newSession(state, nil)}, nil
}

// verifyPayload is the wire body of a verification. type is always stated,
// since the server requires it.
type verifyPayload struct {
	Email     string `json:"email,omitzero"`
	Phone     string `json:"phone,omitzero"`
	Token     string `json:"token,omitzero"`
	TokenHash string `json:"token_hash,omitzero"`
	Type      string `json:"type"`
}

// VerifyOTPResult is the answer to [Client.VerifyOTPByEmail],
// [Client.VerifyOTPByPhone] and [Client.VerifyOTPByTokenHash].
type VerifyOTPResult struct {
	session *Session
}

// Session returns the session of the user the code signed in. It is nil
// when the code confirms the first address of a secure email change, which
// signs nobody in until the other address is confirmed too.
func (r *VerifyOTPResult) Session() *Session { return r.session }

// redirectQuery encodes redirectTo as the redirect_to query parameter, and
// returns "" when it is unset. The server reads the value from the query
// string alone, never from a request body.
func redirectQuery(redirectTo string) string {
	if redirectTo == "" {
		return ""
	}
	return url.Values{"redirect_to": {redirectTo}}.Encode()
}

// carriesSession reports whether a 200 answer from a sign-in endpoint is a
// token response rather than a bare user or an acknowledgment: the two are
// told apart by the presence of access_token.
func carriesSession(body []byte) bool {
	var probe struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		// A body that is not JSON is reported by the decode that follows.
		return true
	}
	return probe.AccessToken != ""
}
