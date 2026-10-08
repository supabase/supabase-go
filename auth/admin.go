package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/supabase/supabase-go/auth/internal/profile"
	"github.com/supabase/supabase-go/core/configuration"
)

// Admin is the administration surface of the Auth service for one Supabase
// project, reached with [Client.Admin]. Its methods act on the project's users
// directly, with no user token involved, and require the Client to have been
// constructed with a secret API key.
//
// Secret keys bypass Row Level Security, so a client holding one belongs in
// server-controlled environments only - never in a browser, a mobile app or any
// other binary that is distributed.
//
// A server rejection surfaces as [*Error] carrying the server's code, message
// and HTTP status. An Admin sends each request once, is immutable and is safe
// for concurrent use by multiple goroutines.
type Admin struct {
	httpClient configuration.HTTPClient
	baseURL    *url.URL
}

// Admin returns the administration surface for this client's project, a view
// over the same transport and configuration.
func (c *Client) Admin() *Admin {
	return &Admin{httpClient: c.httpClient, baseURL: c.baseURL.JoinPath("admin")}
}

// UserAttributes describes a user to [Admin.CreateUser]. A zero-valued field
// is not sent on the wire, so the zero value describes nothing and the server
// rejects it for want of an email or phone number.
type UserAttributes struct {
	// Email is the user's email address.
	Email string `json:"email,omitzero"`

	// Phone is the user's phone number.
	Phone string `json:"phone,omitzero"`

	// Password is the user's password in plain text, checked against the
	// project's strength policy. For importing an existing credential, see
	// PasswordHash.
	Password string `json:"password,omitzero"`

	// EmailConfirm marks the email address already confirmed, so no
	// confirmation email is ever sent for it.
	EmailConfirm bool `json:"email_confirm,omitzero"`

	// PhoneConfirm marks the phone number already confirmed, so no
	// confirmation message is ever sent for it.
	PhoneConfirm bool `json:"phone_confirm,omitzero"`

	// UserMetadata is the user-controlled metadata, the fields a user may
	// edit about themselves.
	UserMetadata map[string]any `json:"user_metadata,omitzero"`

	// AppMetadata is the application-controlled metadata, readable by the
	// user but never writable with their token.
	AppMetadata map[string]any `json:"app_metadata,omitzero"`

	// Role is the Postgres role the user's access tokens carry. New users
	// default to "authenticated".
	Role string `json:"role,omitzero"`

	// BanDuration bans the user for a duration given in [time.ParseDuration]
	// syntax, such as "24h". The literal "none" lifts a ban.
	BanDuration string `json:"ban_duration,omitzero"`

	// ID sets the created user's id, a UUID in canonical hyphenated form, in
	// place of a generated one.
	ID string `json:"id,omitzero"`

	// PasswordHash sets the user's password from an existing bcrypt, scrypt
	// or argon2 hash rather than plain text. Set it or Password, never both.
	PasswordHash string `json:"password_hash,omitzero"`
}

// CreateUser creates a user directly and returns the user as stored. No
// confirmation email is sent: the EmailConfirm and PhoneConfirm attributes
// decide whether the user starts confirmed. The server requires at least an
// email or a phone number.
func (a *Admin) CreateUser(ctx context.Context, attributes UserAttributes) (*User, error) {
	body, err := a.send(ctx, http.MethodPost, a.baseURL.JoinPath("users"), attributes)
	if err != nil {
		return nil, err
	}
	return parseUser(body)
}

// GetUser fetches one user by id. The id must be a UUID in its canonical
// hyphenated form, or [ErrInvalidUserID] is returned before any request is
// sent. An unknown id is the server's rejection with HTTP 404.
func (a *Admin) GetUser(ctx context.Context, userID string) (*User, error) {
	if !isCanonicalUUID(userID) {
		return nil, ErrInvalidUserID
	}
	body, err := a.send(ctx, http.MethodGet, a.baseURL.JoinPath("users", userID), nil)
	if err != nil {
		return nil, err
	}
	return parseUser(body)
}

// DeleteUser removes the user, together with the identities, sessions and
// factors the Auth service holds for them, unless an option chooses a gentler
// mode. A bare call sends no request body, so the server's default governs,
// and that default is the hard delete this comment describes. What happens to
// rows other services hold against the user's id is decided by those
// services' own schemas - see [Deleting users]. The id must be a UUID in its
// canonical hyphenated form, or [ErrInvalidUserID] is returned before any
// request is sent.
//
// [Deleting users]: https://supabase.com/docs/guides/auth/managing-user-data#deleting-users
func (a *Admin) DeleteUser(ctx context.Context, userID string, options ...DeleteUserOption) error {
	if !isCanonicalUUID(userID) {
		return ErrInvalidUserID
	}
	var payload any
	if len(options) > 0 {
		var request deleteUserRequest
		for _, option := range options {
			option(&request)
		}
		payload = request
	}
	_, err := a.send(ctx, http.MethodDelete, a.baseURL.JoinPath("users", userID), payload)
	return err
}

// deleteUserRequest is the admin user-delete request body, built and sent
// only when options chose something, so a bare delete states nothing and the
// server's defaults apply.
type deleteUserRequest struct {
	ShouldSoftDelete bool `json:"should_soft_delete"`
}

// DeleteUserOption adjusts how one [Admin.DeleteUser] call deletes. Options
// are applied in the order they are supplied.
type DeleteUserOption func(*deleteUserRequest)

// WithSoftDelete deactivates the user instead of removing them: sign-ins are
// disabled, every session and factor ends, the email and phone are replaced
// with obfuscated values, and the user stays readable through [Admin.GetUser]
// with [User.DeletedAt] set. A repeat soft delete succeeds and changes
// nothing.
func WithSoftDelete() DeleteUserOption {
	return func(request *deleteUserRequest) {
		request.ShouldSoftDelete = true
	}
}

// send executes one admin request and returns the response body. A payload is
// JSON-encoded, a nil payload sends no body, and a non-2xx answer is shaped
// into [*Error].
func (a *Admin) send(ctx context.Context, method string, endpoint *url.URL, payload any) ([]byte, error) {
	var requestBody io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("auth: encoding request body: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return nil, fmt.Errorf("auth: building request: %w", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := a.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("auth: executing request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("auth: reading response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, newError(response.StatusCode, responseBody)
	}
	return responseBody, nil
}

// parseUser decodes a user response through the same profile parsing the
// session calls use.
func parseUser(body []byte) (*User, error) {
	user, err := profile.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("auth: decoding user: %w", err)
	}
	return &User{inner: user}, nil
}

// isCanonicalUUID reports whether id is a UUID in its canonical hyphenated
// form: groups of 8, 4, 4, 4 and 12 hexadecimal digits, in either case.
func isCanonicalUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i := range 36 {
		character := id[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		isDigit := character >= '0' && character <= '9'
		isLowerHexadecimal := character >= 'a' && character <= 'f'
		isUpperHexadecimal := character >= 'A' && character <= 'F'
		if !isDigit && !isLowerHexadecimal && !isUpperHexadecimal {
			return false
		}
	}
	return true
}
