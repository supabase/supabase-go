package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
)

// PasswordCredentials identifies a user to [Client.SignInWithPassword] by
// email or phone and their password. Exactly one of Email and Phone is set,
// and the server rejects a value with both or neither. The value is read once
// during the call and never retained.
type PasswordCredentials struct {
	Email    string `json:"email,omitzero"`
	Phone    string `json:"phone,omitzero"`
	Password string `json:"password"`
}

// SignInWithPassword signs a user in with their email or phone and password.
// The result carries their session and, when the server sent one, its
// advisory that the password falls short of the project's current
// requirements. A wrong password, an unknown account and an account that can
// only sign in another way are all answered with the same invalid_credentials
// error, and an unconfirmed email with email_not_confirmed, each as [*Error].
func (c *Client) SignInWithPassword(ctx context.Context, credentials PasswordCredentials) (*SignInWithPasswordResult, error) {
	endpoint := c.baseURL.JoinPath("token")
	endpoint.RawQuery = url.Values{"grant_type": {"password"}}.Encode()
	body, err := send(ctx, c.httpClient, http.MethodPost, endpoint, credentials, "")
	if err != nil {
		return nil, err
	}
	state, err := parseTokenResponse(body)
	if err != nil {
		return nil, err
	}
	var advisory struct {
		WeakPassword *struct {
			Message string   `json:"message"`
			Reasons []string `json:"reasons"`
		} `json:"weak_password"`
	}
	if err := json.Unmarshal(body, &advisory); err != nil {
		return nil, fmt.Errorf("auth: decoding token response: %w", err)
	}
	result := &SignInWithPasswordResult{session: c.newSession(state, nil)}
	if advisory.WeakPassword != nil {
		result.weakPassword = &WeakPassword{message: advisory.WeakPassword.Message, reasons: advisory.WeakPassword.Reasons}
	}
	return result, nil
}

// SignInWithPasswordResult is the answer to [Client.SignInWithPassword]: the
// new [Session] and, when the server sent one, the advisory that the accepted
// password falls short of the project's current requirements.
type SignInWithPasswordResult struct {
	session      *Session
	weakPassword *WeakPassword
}

// Session returns the signed-in user's session. It is never nil.
func (r *SignInWithPasswordResult) Session() *Session { return r.session }

// WeakPassword returns the server's advisory that the accepted password falls
// short of the project's current password requirements, or nil when the
// server sent none.
func (r *SignInWithPasswordResult) WeakPassword() *WeakPassword { return r.weakPassword }

// WeakPassword is the server's advisory, sent beside a successful sign-in,
// that the accepted password falls short of the project's current password
// requirements. A WeakPassword is immutable and safe for concurrent use by
// multiple goroutines.
type WeakPassword struct {
	message string
	reasons []string
}

// Message returns the server's description of the shortfall.
func (w *WeakPassword) Message() string { return w.message }

// Reasons returns the server's codes for each requirement the password fails,
// such as "length" or "characters". The result is a copy the caller may
// retain and modify freely.
func (w *WeakPassword) Reasons() []string { return slices.Clone(w.reasons) }
