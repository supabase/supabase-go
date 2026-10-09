package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/supabase/supabase-go/auth/internal/profile"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/core/pagination"
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

// UserAttributes describes a user to [Admin.CreateUser] and
// [Admin.UpdateUser]. A zero-valued field is not sent on the wire: creating
// from the zero value describes nothing, which the server rejects for want
// of an email or phone number, and updating with it changes nothing.
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
	// confirmation email is ever sent for it. An update can confirm but not
	// undo a confirmation: false is simply not sent.
	EmailConfirm bool `json:"email_confirm,omitzero"`

	// PhoneConfirm marks the phone number already confirmed, so no
	// confirmation message is ever sent for it. An update can confirm but not
	// undo a confirmation, as for EmailConfirm.
	PhoneConfirm bool `json:"phone_confirm,omitzero"`

	// UserMetadata is the user-controlled metadata, the fields a user may
	// edit about themselves. An update merges it per key, deleting each key
	// whose value is nil.
	UserMetadata map[string]any `json:"user_metadata,omitzero"`

	// AppMetadata is the application-controlled metadata, readable by the
	// user but never writable with their token. An update merges it per key,
	// as for UserMetadata.
	AppMetadata map[string]any `json:"app_metadata,omitzero"`

	// Role is the Postgres role the user's access tokens carry. New users
	// default to "authenticated".
	Role string `json:"role,omitzero"`

	// BanDuration bans the user for a duration given in [time.ParseDuration]
	// syntax, such as "24h". The literal "none" lifts a ban.
	BanDuration string `json:"ban_duration,omitzero"`

	// ID sets the created user's id, a UUID in canonical hyphenated form, in
	// place of a generated one. An update ignores it.
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

// UpdateUser changes the fields attributes sets and leaves the rest as they
// are, returning the user as stored afterwards. The id must be a UUID in its
// canonical hyphenated form, or [ErrInvalidUserID] is returned before any
// request is sent. An unknown id is the server's rejection with HTTP 404.
func (a *Admin) UpdateUser(ctx context.Context, userID string, attributes UserAttributes) (*User, error) {
	if !isCanonicalUUID(userID) {
		return nil, ErrInvalidUserID
	}
	body, err := a.send(ctx, http.MethodPut, a.baseURL.JoinPath("users", userID), attributes)
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

// ListUsers fetches one page of the project's users. A bare call sends no
// pagination parameters, so the server's documented defaults govern;
// [pagination.WithPage] and [pagination.WithSize] choose a page and its size
// explicitly.
func (a *Admin) ListUsers(ctx context.Context, options ...pagination.Option) (*UserPage, error) {
	endpoint := a.baseURL.JoinPath("users")
	parameters := pagination.Resolve(options...)
	query := url.Values{}
	if number, ok := parameters.Page(); ok {
		query.Set("page", strconv.Itoa(number))
	}
	if items, ok := parameters.Size(); ok {
		query.Set("per_page", strconv.Itoa(items))
	}
	endpoint.RawQuery = query.Encode()

	body, header, err := a.exchange(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return parseUserPage(body, header)
}

// UserPage is one page of the project's users, answered by [Admin.ListUsers].
type UserPage struct {
	users    []User
	total    int
	nextPage int
	lastPage int
	hasNext  bool
	hasLast  bool
}

// Users returns the page's users. The result is a copy the caller may retain
// and modify freely.
func (p *UserPage) Users() []User { return slices.Clone(p.users) }

// Total returns how many users match across every page, the response's
// X-Total-Count header.
func (p *UserPage) Total() int { return p.total }

// NextPage returns the number of the page after this one, reporting false
// from the last page. A walk passes it to the next call with
// [pagination.WithPage], restating its page size when it chose one, because
// the server computes page boundaries from the size each request states.
func (p *UserPage) NextPage() (int, bool) { return p.nextPage, p.hasNext }

// LastPage returns the number of the final page, reporting false when the
// server sent no Link header.
func (p *UserPage) LastPage() (int, bool) { return p.lastPage, p.hasLast }

// parseUserPage decodes one admin list response: the users from the body and
// the page numbers from the X-Total-Count and Link headers. A header that is
// absent or unreadable leaves its accessor reporting absence rather than
// failing the page.
func parseUserPage(body []byte, header http.Header) (*UserPage, error) {
	var wire struct {
		Users []json.RawMessage `json:"users"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("auth: decoding user list: %w", err)
	}

	page := &UserPage{users: make([]User, 0, len(wire.Users))}
	for _, rawUser := range wire.Users {
		user, err := profile.Parse(rawUser)
		if err != nil {
			return nil, fmt.Errorf("auth: decoding user list: %w", err)
		}
		page.users = append(page.users, User{inner: user})
	}
	if total, err := strconv.Atoi(header.Get("X-Total-Count")); err == nil {
		page.total = total
	}
	page.nextPage, page.hasNext = pageLink(header, "next")
	page.lastPage, page.hasLast = pageLink(header, "last")
	return page, nil
}

// pageLink reads the page number carried by one rel of the response's Link
// header, whose shape is <url>; rel="next", <url>; rel="last". It reports
// false when the header holds no such rel or no readable page number.
func pageLink(header http.Header, rel string) (int, bool) {
	for _, link := range strings.Split(header.Get("Link"), ",") {
		target, parameters, found := strings.Cut(link, ";")
		if !found || !strings.Contains(parameters, `rel="`+rel+`"`) {
			continue
		}
		target = strings.TrimSpace(target)
		target = strings.TrimPrefix(target, "<")
		target = strings.TrimSuffix(target, ">")
		parsed, err := url.Parse(target)
		if err != nil {
			return 0, false
		}
		number, err := strconv.Atoi(parsed.Query().Get("page"))
		if err != nil {
			return 0, false
		}
		return number, true
	}
	return 0, false
}

// send executes one admin request through exchange for the calls that read
// no response headers.
func (a *Admin) send(ctx context.Context, method string, endpoint *url.URL, payload any) ([]byte, error) {
	body, _, err := a.exchange(ctx, method, endpoint, payload)
	return body, err
}

// exchange executes one admin request and returns the response body and
// headers. A payload is JSON-encoded, a nil payload sends no body, and a
// non-2xx answer is shaped into [*Error].
func (a *Admin) exchange(ctx context.Context, method string, endpoint *url.URL, payload any) ([]byte, http.Header, error) {
	var requestBody io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("auth: encoding request body: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: building request: %w", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := a.httpClient.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: executing request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: reading response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, newError(response.StatusCode, responseBody)
	}
	return responseBody, response.Header, nil
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
