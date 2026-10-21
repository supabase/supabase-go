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

	"github.com/supabase/supabase-go/auth/customprovider"
	"github.com/supabase/supabase-go/auth/internal/profile"
	"github.com/supabase/supabase-go/auth/mfafactor"
	"github.com/supabase/supabase-go/auth/signout"
	"github.com/supabase/supabase-go/auth/userdelete"
	"github.com/supabase/supabase-go/auth/userinvite"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/core/pagination"
)

// Admin is the administration surface of the Auth service for one Supabase
// project, reached with [Client.Admin]. Its methods act directly on the
// project's users and on its custom sign-in providers, and require the Client
// to have been constructed with a secret API key.
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

	// baseURL is the Auth service root.
	baseURL *url.URL

	// adminBaseURL is the /admin path within the Auth service.
	adminBaseURL *url.URL
}

// Admin returns the administration surface for this client's project, a view
// over the same transport and configuration.
func (c *Client) Admin() *Admin {
	return &Admin{
		httpClient:   c.httpClient,
		baseURL:      c.baseURL,
		adminBaseURL: c.baseURL.JoinPath("admin"),
	}
}

// UserAttributes describes a user to [Admin.CreateUser] and
// [Admin.UpdateUser]. A zero-valued field is not sent on the wire: creating
// from the zero value describes nothing, which the server rejects for want
// of an email or phone number, and updating with it changes nothing. A call
// reads the value once when it is made and never retains it, so the caller
// may reuse or mutate the value freely once the call returns.
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
	body, err := a.send(ctx, http.MethodPost, a.adminBaseURL.JoinPath("users"), attributes)
	if err != nil {
		return nil, err
	}
	return parseUser(body)
}

// GetUser fetches one user by id. The id must be a UUID in its canonical
// hyphenated form, or [ErrInvalidUserID] is returned before any request is
// sent. An unknown id is the server's rejection, an [*Error] whose HTTPStatus
// is 404.
func (a *Admin) GetUser(ctx context.Context, userID string) (*User, error) {
	if !isCanonicalUUID(userID) {
		return nil, ErrInvalidUserID
	}
	body, err := a.send(ctx, http.MethodGet, a.adminBaseURL.JoinPath("users", userID), nil)
	if err != nil {
		return nil, err
	}
	return parseUser(body)
}

// UpdateUser changes the fields attributes sets and leaves the rest as they
// are, returning the user as stored afterwards. The id must be a UUID in its
// canonical hyphenated form, or [ErrInvalidUserID] is returned before any
// request is sent. An unknown id is the server's rejection, an [*Error] whose
// HTTPStatus is 404.
func (a *Admin) UpdateUser(ctx context.Context, userID string, attributes UserAttributes) (*User, error) {
	if !isCanonicalUUID(userID) {
		return nil, ErrInvalidUserID
	}
	body, err := a.send(ctx, http.MethodPut, a.adminBaseURL.JoinPath("users", userID), attributes)
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
func (a *Admin) DeleteUser(ctx context.Context, userID string, options ...userdelete.Option) error {
	if !isCanonicalUUID(userID) {
		return ErrInvalidUserID
	}
	var payload any
	if userdelete.Resolve(options...).SoftDelete() {
		payload = deleteUserPayload{ShouldSoftDelete: true}
	}
	_, err := a.send(ctx, http.MethodDelete, a.adminBaseURL.JoinPath("users", userID), payload)
	return err
}

// deleteUserPayload is the admin user-delete request body, built and sent
// only when options chose something, so a bare delete states nothing and the
// server's defaults apply.
type deleteUserPayload struct {
	ShouldSoftDelete bool `json:"should_soft_delete"`
}

// ListUsers fetches one page of the project's users. A bare call sends no
// pagination parameters, so the server's documented defaults govern;
// [pagination.WithPage] and [pagination.WithSize] choose a page and its size
// explicitly.
func (a *Admin) ListUsers(ctx context.Context, options ...pagination.Option) (*UserPage, error) {
	endpoint := a.adminBaseURL.JoinPath("users")
	parameters := pagination.Resolve(options...)
	query := url.Values{}
	if number, ok := parameters.Page(); ok {
		query.Set("page", strconv.Itoa(number))
	}
	if items, ok := parameters.Size(); ok {
		query.Set("per_page", strconv.Itoa(items))
	}
	endpoint.RawQuery = query.Encode()

	body, header, err := a.exchange(ctx, http.MethodGet, endpoint, nil, "")
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

// InviteUser creates the user and has the server email them an invitation
// link. An email already confirmed is the server's rejection, an [*Error]
// whose HTTPStatus is 422, and an invited-but-unaccepted one is re-invited.
func (a *Admin) InviteUser(ctx context.Context, email string, options ...userinvite.Option) (*User, error) {
	parameters := userinvite.Resolve(options...)
	endpoint := a.baseURL.JoinPath("invite")
	if redirectTo := parameters.RedirectTo(); redirectTo != "" {
		query := url.Values{}
		query.Set("redirect_to", redirectTo)
		endpoint.RawQuery = query.Encode()
	}
	body, err := a.send(ctx, http.MethodPost, endpoint, invitePayload{Email: email, Data: parameters.UserMetadata()})
	if err != nil {
		return nil, err
	}
	return parseUser(body)
}

// invitePayload is the invite request body. The redirect rides the query
// string instead, as the server reads it there, not from the body.
type invitePayload struct {
	Email string         `json:"email"`
	Data  map[string]any `json:"data,omitzero"`
}

// SignOut revokes sessions of the user jwt authenticates. A bare call sends
// no scope, so the server's default governs, and that default is revoking
// every session the user holds; [signout.WithLocalScope] and
// [signout.WithOthersScope] narrow the revocation. The jwt is the end user's access token - the one admin
// request that carries a user token, naming whose sessions to revoke - and an
// empty jwt returns [ErrMissingJWT] before any request is sent.
//
// Revocation ends sessions, not tokens: a revoked session's unexpired access
// tokens still verify locally with [Client.GetClaims] until they expire, but
// fail [Client.GetUser], which consults the session.
func (a *Admin) SignOut(ctx context.Context, jwt string, options ...signout.Option) error {
	if jwt == "" {
		return ErrMissingJWT
	}
	endpoint := a.baseURL.JoinPath("logout")
	if scope := signout.Resolve(options...).Scope(); scope != "" {
		query := url.Values{}
		query.Set("scope", scope)
		endpoint.RawQuery = query.Encode()
	}
	_, _, err := a.exchange(ctx, http.MethodPost, endpoint, nil, jwt)
	return err
}

// ListMFAFactors fetches the user's enrolled MFA factors. The id must be a
// UUID in its canonical hyphenated form, or [ErrInvalidUserID] is returned
// before any request is sent. An unknown id is the server's rejection, an
// [*Error] whose HTTPStatus is 404.
func (a *Admin) ListMFAFactors(ctx context.Context, userID string) ([]Factor, error) {
	if !isCanonicalUUID(userID) {
		return nil, ErrInvalidUserID
	}
	body, err := a.send(ctx, http.MethodGet, a.adminBaseURL.JoinPath("users", userID, "factors"), nil)
	if err != nil {
		return nil, err
	}
	return parseFactorList(body)
}

// UpdateMFAFactor changes what the options set on one of userID's factors and
// returns the factor as stored. A call with no options changes nothing and
// still succeeds. The ids must be UUIDs in their canonical hyphenated form,
// or [ErrInvalidUserID] or [ErrInvalidFactorID] is returned before any
// request is sent. An unknown id is the server's rejection, an [*Error] whose
// HTTPStatus is 404.
func (a *Admin) UpdateMFAFactor(ctx context.Context, userID, factorID string, options ...mfafactor.Option) (*Factor, error) {
	if !isCanonicalUUID(userID) {
		return nil, ErrInvalidUserID
	}
	if !isCanonicalUUID(factorID) {
		return nil, ErrInvalidFactorID
	}
	parameters := mfafactor.Resolve(options...)
	request := updateMFAFactorRequest{FriendlyName: parameters.FriendlyName(), Phone: parameters.Phone()}
	body, err := a.send(ctx, http.MethodPut, a.adminBaseURL.JoinPath("users", userID, "factors", factorID), request)
	if err != nil {
		return nil, err
	}
	return parseFactor(body)
}

// updateMFAFactorRequest is the admin factor-update request body: only what
// the options set is sent, and a bare call sends the empty object the server
// answers without changing anything.
type updateMFAFactorRequest struct {
	FriendlyName string `json:"friendly_name,omitzero"`
	Phone        string `json:"phone,omitzero"`
}

// DeleteMFAFactor removes one of userID's factors, ending the second-factor
// obligation it imposed on sign-in. The ids must be UUIDs in their canonical
// hyphenated form, or [ErrInvalidUserID] or [ErrInvalidFactorID] is returned
// before any request is sent. An unknown id is the server's rejection, an
// [*Error] whose HTTPStatus is 404.
func (a *Admin) DeleteMFAFactor(ctx context.Context, userID, factorID string) error {
	if !isCanonicalUUID(userID) {
		return ErrInvalidUserID
	}
	if !isCanonicalUUID(factorID) {
		return ErrInvalidFactorID
	}
	_, err := a.send(ctx, http.MethodDelete, a.adminBaseURL.JoinPath("users", userID, "factors", factorID), nil)
	return err
}

// CreateCustomProvider registers a custom OAuth 2.0 or OIDC provider for
// users to sign in through, built with [customprovider.New], and returns the
// provider as stored. An identifier already registered is the server's
// rejection, an [*Error] whose Code is "conflict".
func (a *Admin) CreateCustomProvider(ctx context.Context, definition customprovider.Definition) (*CustomProvider, error) {
	body, err := a.send(ctx, http.MethodPost, a.adminBaseURL.JoinPath("custom-providers"), definition)
	if err != nil {
		return nil, err
	}
	return parseCustomProvider(body)
}

// GetCustomProvider fetches one custom provider by its "custom:"-prefixed
// identifier. An empty identifier returns [ErrMissingIdentifier] before any
// request is sent, and an unknown or unprefixed one is the server's
// rejection, an [*Error] whose HTTPStatus is 404 or 400.
func (a *Admin) GetCustomProvider(ctx context.Context, identifier string) (*CustomProvider, error) {
	if identifier == "" {
		return nil, ErrMissingIdentifier
	}
	body, err := a.send(ctx, http.MethodGet, a.adminBaseURL.JoinPath("custom-providers", url.PathEscape(identifier)), nil)
	if err != nil {
		return nil, err
	}
	return parseCustomProvider(body)
}

// ListCustomProviders fetches the project's custom providers. A bare call
// lists every provider; [customprovider.WithOIDCOnly] and
// [customprovider.WithOAuth2Only] narrow the listing to one type.
func (a *Admin) ListCustomProviders(ctx context.Context, options ...customprovider.ListOption) ([]CustomProvider, error) {
	endpoint := a.adminBaseURL.JoinPath("custom-providers")
	if providerType := customprovider.ResolveList(options...).ProviderType(); providerType != "" {
		query := url.Values{}
		query.Set("type", providerType)
		endpoint.RawQuery = query.Encode()
	}
	body, err := a.send(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return parseCustomProviderList(body)
}

// UpdateCustomProvider changes what the options set on one custom provider,
// addressed by its "custom:"-prefixed identifier, and returns the provider as
// stored. A call with no options changes nothing and still succeeds. The type
// and the identifier are the two things no option can change: the server
// fixes both at creation. An empty identifier returns [ErrMissingIdentifier]
// before any request is sent, and an unknown or unprefixed one is the
// server's rejection, an [*Error] whose HTTPStatus is 404 or 400.
func (a *Admin) UpdateCustomProvider(ctx context.Context, identifier string, options ...customprovider.Option) (*CustomProvider, error) {
	if identifier == "" {
		return nil, ErrMissingIdentifier
	}
	body, err := a.send(ctx, http.MethodPut, a.adminBaseURL.JoinPath("custom-providers", url.PathEscape(identifier)), customprovider.Resolve(options...))
	if err != nil {
		return nil, err
	}
	return parseCustomProvider(body)
}

// DeleteCustomProvider removes one custom provider, addressed by its
// "custom:"-prefixed identifier, ending sign-ins through it. An empty
// identifier returns [ErrMissingIdentifier] before any request is sent, and
// an unknown or unprefixed one is the server's rejection, an [*Error] whose
// HTTPStatus is 404 or 400.
func (a *Admin) DeleteCustomProvider(ctx context.Context, identifier string) error {
	if identifier == "" {
		return ErrMissingIdentifier
	}
	_, err := a.send(ctx, http.MethodDelete, a.adminBaseURL.JoinPath("custom-providers", url.PathEscape(identifier)), nil)
	return err
}

// send executes one admin request through exchange for the calls that read
// no response headers and present no user token.
func (a *Admin) send(ctx context.Context, method string, endpoint *url.URL, payload any) ([]byte, error) {
	body, _, err := a.exchange(ctx, method, endpoint, payload, "")
	return body, err
}

// exchange executes one admin request and returns the response body and
// headers. A payload is JSON-encoded, a nil payload sends no body, a non-empty
// bearer carries an end user's token on the Authorization header in place of
// the configured key, and a non-2xx answer is shaped into [*Error].
func (a *Admin) exchange(ctx context.Context, method string, endpoint *url.URL, payload any, bearer string) ([]byte, http.Header, error) {
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
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
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
