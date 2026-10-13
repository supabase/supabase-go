package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
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
	_, err := a.send(ctx, http.MethodDelete, a.adminBaseURL.JoinPath("users", userID), payload)
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
func (a *Admin) InviteUser(ctx context.Context, email string, options ...InviteUserOption) (*User, error) {
	var request inviteRequest
	for _, option := range options {
		option(&request)
	}
	endpoint := a.baseURL.JoinPath("invite")
	if request.redirectTo != "" {
		query := url.Values{}
		query.Set("redirect_to", request.redirectTo)
		endpoint.RawQuery = query.Encode()
	}
	body, err := a.send(ctx, http.MethodPost, endpoint, invitePayload{Email: email, Data: request.data})
	if err != nil {
		return nil, err
	}
	return parseUser(body)
}

// inviteRequest collects what one invite call's options chose. The redirect
// is kept apart from the payload because the server reads it from the query
// string, not the body.
type inviteRequest struct {
	data       map[string]any
	redirectTo string
}

// invitePayload is the invite request body.
type invitePayload struct {
	Email string         `json:"email"`
	Data  map[string]any `json:"data,omitzero"`
}

// InviteUserOption adjusts how one [Admin.InviteUser] call invites. Options
// are applied in the order they are supplied.
type InviteUserOption func(*inviteRequest)

// WithUserMetadata stores metadata on the invited user, where the
// invitation's accept page can read it.
func WithUserMetadata(metadata map[string]any) InviteUserOption {
	return func(request *inviteRequest) {
		request.data = metadata
	}
}

// WithRedirectTo names where the accepted invitation lands. A URL that is
// not on the project's redirect allow list is ignored in favor of the
// project's defaults rather than rejected.
func WithRedirectTo(redirectURL string) InviteUserOption {
	return func(request *inviteRequest) {
		request.redirectTo = redirectURL
	}
}

// SignOut revokes sessions of the user jwt authenticates. A bare call sends
// no scope, so the server's default governs, and that default is revoking
// every session the user holds; [WithLocalScope] and [WithOthersScope] narrow
// the revocation. The jwt is the end user's access token - the one admin
// request that carries a user token, naming whose sessions to revoke - and an
// empty jwt returns [ErrMissingJWT] before any request is sent.
//
// Revocation ends sessions, not tokens: a revoked session's unexpired access
// tokens still verify locally with [Client.GetClaims] until they expire, but
// fail [Client.GetUser], which consults the session.
func (a *Admin) SignOut(ctx context.Context, jwt string, options ...SignOutOption) error {
	if jwt == "" {
		return ErrMissingJWT
	}
	var request signOutRequest
	for _, option := range options {
		option(&request)
	}
	endpoint := a.baseURL.JoinPath("logout")
	if request.scope != "" {
		query := url.Values{}
		query.Set("scope", request.scope)
		endpoint.RawQuery = query.Encode()
	}
	_, _, err := a.exchange(ctx, http.MethodPost, endpoint, nil, jwt)
	return err
}

// signOutRequest collects what one sign-out call's options chose. The scope
// rides the query string, built and sent only when an option narrowed it, so
// a bare sign-out states nothing and the server's default applies.
type signOutRequest struct {
	scope string
}

// SignOutOption narrows which of the user's sessions [Admin.SignOut] revokes.
// Options are applied in the order they are supplied, the later wins.
type SignOutOption func(*signOutRequest)

// WithLocalScope revokes only the session the presented token belongs to.
func WithLocalScope() SignOutOption {
	return func(request *signOutRequest) {
		request.scope = "local"
	}
}

// WithOthersScope revokes every session except the one the presented token
// belongs to.
func WithOthersScope() SignOutOption {
	return func(request *signOutRequest) {
		request.scope = "others"
	}
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
func (a *Admin) UpdateMFAFactor(ctx context.Context, userID, factorID string, options ...MFAFactorOption) (*Factor, error) {
	if !isCanonicalUUID(userID) {
		return nil, ErrInvalidUserID
	}
	if !isCanonicalUUID(factorID) {
		return nil, ErrInvalidFactorID
	}
	var request updateMFAFactorRequest
	for _, option := range options {
		option(&request)
	}
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

// MFAFactorOption sets one field on an [Admin.UpdateMFAFactor] call. Options
// are applied in the order they are supplied.
type MFAFactorOption func(*updateMFAFactorRequest)

// WithFriendlyName renames the factor.
func WithFriendlyName(name string) MFAFactorOption {
	return func(request *updateMFAFactorRequest) {
		request.FriendlyName = name
	}
}

// WithPhone moves a phone factor to a new number, in E.164 format, which the
// server validates. On any other factor type the server ignores the number
// while still answering with success.
func WithPhone(phone string) MFAFactorOption {
	return func(request *updateMFAFactorRequest) {
		request.Phone = phone
	}
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
// users to sign in through, built with [NewCustomProvider], and returns the
// provider as stored. An identifier already registered is the server's
// rejection, an [*Error] whose Code is "conflict".
func (a *Admin) CreateCustomProvider(ctx context.Context, definition CustomProviderDefinition) (*CustomProvider, error) {
	body, err := a.send(ctx, http.MethodPost, a.adminBaseURL.JoinPath("custom-providers"), definition.providerDefinition().Payload())
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
// lists every provider; [WithOIDCOnly] and [WithOAuth2Only] narrow the
// listing to one type.
func (a *Admin) ListCustomProviders(ctx context.Context, options ...ListCustomProvidersOption) ([]CustomProvider, error) {
	var request listCustomProvidersRequest
	for _, option := range options {
		option(&request)
	}
	endpoint := a.adminBaseURL.JoinPath("custom-providers")
	if request.providerType != "" {
		query := url.Values{}
		query.Set("type", request.providerType)
		endpoint.RawQuery = query.Encode()
	}
	body, err := a.send(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return parseCustomProviderList(body)
}

// listCustomProvidersRequest collects what one listing call's options chose.
// The type filter rides the query string, built and sent only when an option
// narrowed it, so a bare listing states nothing and every provider answers.
type listCustomProvidersRequest struct {
	providerType string
}

// ListCustomProvidersOption narrows which providers [Admin.ListCustomProviders]
// lists. Options are applied in the order they are supplied, the later wins.
type ListCustomProvidersOption func(*listCustomProvidersRequest)

// WithOIDCOnly narrows the listing to OIDC providers.
func WithOIDCOnly() ListCustomProvidersOption {
	return func(request *listCustomProvidersRequest) {
		request.providerType = "oidc"
	}
}

// WithOAuth2Only narrows the listing to OAuth 2.0 providers.
func WithOAuth2Only() ListCustomProvidersOption {
	return func(request *listCustomProvidersRequest) {
		request.providerType = "oauth2"
	}
}

// UpdateCustomProvider changes what the options set on one custom provider,
// addressed by its "custom:"-prefixed identifier, and returns the provider as
// stored. A call with no options changes nothing and still succeeds. The type
// and the identifier are the two things no option can change: the server
// fixes both at creation. An empty identifier returns [ErrMissingIdentifier]
// before any request is sent, and an unknown or unprefixed one is the
// server's rejection, an [*Error] whose HTTPStatus is 404 or 400.
func (a *Admin) UpdateCustomProvider(ctx context.Context, identifier string, options ...CustomProviderOption) (*CustomProvider, error) {
	if identifier == "" {
		return nil, ErrMissingIdentifier
	}
	var request updateCustomProviderRequest
	for _, option := range options {
		option(&request)
	}
	body, err := a.send(ctx, http.MethodPut, a.adminBaseURL.JoinPath("custom-providers", url.PathEscape(identifier)), request)
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

// updateCustomProviderRequest is the custom provider update request body:
// only what the options set is sent, and a bare call sends the empty object
// the server answers without changing anything. Deviating booleans are
// pointers because the field's absence, not falseness, is what leaves the
// stored value alone.
type updateCustomProviderRequest struct {
	Name                  string            `json:"name,omitzero"`
	ClientID              string            `json:"client_id,omitzero"`
	ClientSecret          string            `json:"client_secret,omitzero"`
	Scopes                []string          `json:"scopes,omitzero"`
	AcceptableClientIDs   []string          `json:"acceptable_client_ids,omitzero"`
	CustomClaimsAllowlist []string          `json:"custom_claims_allowlist,omitzero"`
	AttributeMapping      map[string]any    `json:"attribute_mapping,omitzero"`
	AuthorizationParams   map[string]string `json:"authorization_params,omitzero"`
	PKCEEnabled           *bool             `json:"pkce_enabled,omitzero"`
	Enabled               *bool             `json:"enabled,omitzero"`
	EmailOptional         *bool             `json:"email_optional,omitzero"`
	Issuer                string            `json:"issuer,omitzero"`
	DiscoveryURL          string            `json:"discovery_url,omitzero"`
	SkipNonceCheck        *bool             `json:"skip_nonce_check,omitzero"`
	AuthorizationURL      string            `json:"authorization_url,omitzero"`
	TokenURL              string            `json:"token_url,omitzero"`
	UserinfoURL           string            `json:"userinfo_url,omitzero"`
	JwksURI               string            `json:"jwks_uri,omitzero"`
}

// CustomProviderOption sets one field on an [Admin.UpdateCustomProvider]
// call: an option carried is an instruction, an option absent leaves the
// stored value alone. Options are applied in the order they are supplied. An
// option for the other type's fields - [WithIssuer] on an OAuth 2.0 provider,
// [WithTokenURL] on an OIDC one - is ignored by the server, which still
// answers with success.
type CustomProviderOption func(*updateCustomProviderRequest)

// WithName renames the provider.
func WithName(name string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.Name = name
	}
}

// WithClientID replaces the OAuth client id registered with the provider.
func WithClientID(clientID string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.ClientID = clientID
	}
}

// WithClientSecret rotates the provider's client secret. The server encrypts
// it at rest and never serves it back.
func WithClientSecret(clientSecret string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.ClientSecret = clientSecret
	}
}

// WithScopes replaces the scopes requested at authorization. A call with no
// arguments clears the list, and the server keeps "openid" present on OIDC
// providers either way.
func WithScopes(scopes ...string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.Scopes = append([]string{}, scopes...)
	}
}

// WithAcceptableClientIDs replaces the additional client ids accepted in the
// provider's tokens. A call with no arguments clears the list.
func WithAcceptableClientIDs(ids ...string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.AcceptableClientIDs = append([]string{}, ids...)
	}
}

// WithCustomClaimsAllowlist replaces the provider claim keys copied verbatim
// into the user's custom claims. A call with no arguments clears the list.
func WithCustomClaimsAllowlist(claims ...string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.CustomClaimsAllowlist = append([]string{}, claims...)
	}
}

// WithAttributeMapping replaces the mapping of provider claims to user
// attributes. A nil or empty map clears the stored mapping. The request holds
// its own clone of the map.
func WithAttributeMapping(mapping map[string]any) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.AttributeMapping = clonedOrEmpty(mapping)
	}
}

// WithAuthorizationParams replaces the extra parameters the authorization
// request carries. A nil or empty map clears the stored parameters. The
// request holds its own clone of the map.
func WithAuthorizationParams(params map[string]string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.AuthorizationParams = clonedOrEmpty(params)
	}
}

// WithPKCEEnabled turns PKCE on or off for the provider's authorization flow.
func WithPKCEEnabled(enabled bool) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.PKCEEnabled = &enabled
	}
}

// WithEnabled turns sign-ins through the provider on or off, the kill switch
// that keeps the configuration while the provider is distrusted.
func WithEnabled(enabled bool) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.Enabled = &enabled
	}
}

// WithEmailOptional sets whether users may sign in through the provider
// without an email claim.
func WithEmailOptional(optional bool) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.EmailOptional = &optional
	}
}

// WithIssuer moves an OIDC provider to a new issuer URL. The server fetches
// and validates the new issuer's discovery document before storing it.
func WithIssuer(issuer string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.Issuer = issuer
	}
}

// WithDiscoveryURL states where an OIDC provider's discovery document lives
// when it is not at the issuer's well-known location. The server fetches and
// validates the document before storing the URL.
func WithDiscoveryURL(discoveryURL string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.DiscoveryURL = discoveryURL
	}
}

// WithSkipNonceCheck sets whether an OIDC provider skips ID token nonce
// verification.
func WithSkipNonceCheck(skip bool) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.SkipNonceCheck = &skip
	}
}

// WithAuthorizationURL replaces an OAuth 2.0 provider's authorization
// endpoint.
func WithAuthorizationURL(authorizationURL string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.AuthorizationURL = authorizationURL
	}
}

// WithTokenURL replaces an OAuth 2.0 provider's token endpoint.
func WithTokenURL(tokenURL string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.TokenURL = tokenURL
	}
}

// WithUserinfoURL replaces an OAuth 2.0 provider's userinfo endpoint.
func WithUserinfoURL(userinfoURL string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.UserinfoURL = userinfoURL
	}
}

// WithJWKSURI replaces where an OAuth 2.0 provider publishes its signing
// keys.
func WithJWKSURI(jwksURI string) CustomProviderOption {
	return func(request *updateCustomProviderRequest) {
		request.JwksURI = jwksURI
	}
}

// clonedOrEmpty clones a caller's map, normalizing nil to an empty map so an
// option carried always sends its field: on the wire an empty object clears
// where an absent key keeps.
func clonedOrEmpty[V any](source map[string]V) map[string]V {
	cloned := make(map[string]V, len(source))
	maps.Copy(cloned, source)
	return cloned
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
