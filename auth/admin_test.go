package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/customprovider"
	"github.com/supabase/supabase-go/auth/mfafactor"
	"github.com/supabase/supabase-go/auth/signout"
	"github.com/supabase/supabase-go/auth/userdelete"
	"github.com/supabase/supabase-go/auth/userinvite"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/core/pagination"
)

// cSpell:ignore uuid

// recordedRequest is one request an adminServer served, as the wire saw it.
type recordedRequest struct {
	method        string
	path          string
	escapedPath   string
	query         string
	contentType   string
	apiKey        string
	apiVersion    string
	authorization string
	body          []byte
}

// adminServer stands in for the Auth server's admin endpoints, recording every
// request and answering each with the configured status, header and body.
type adminServer struct {
	server   *httptest.Server
	status   int
	header   map[string]string
	body     string
	requests []recordedRequest
}

// newAdminServer starts a recording server answering 200 with an empty JSON
// object until a test reconfigures its status and body.
func newAdminServer(t *testing.T) *adminServer {
	t.Helper()
	recorder := &adminServer{status: http.StatusOK, body: "{}"}
	recorder.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestBody, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		recorder.requests = append(recorder.requests, recordedRequest{
			method:        request.Method,
			path:          request.URL.Path,
			escapedPath:   request.URL.EscapedPath(),
			query:         request.URL.RawQuery,
			contentType:   request.Header.Get("Content-Type"),
			apiKey:        request.Header.Get("apikey"),
			apiVersion:    request.Header.Get("X-Supabase-Api-Version"),
			authorization: request.Header.Get("Authorization"),
			body:          requestBody,
		})
		for key, value := range recorder.header {
			writer.Header().Set(key, value)
		}
		writer.WriteHeader(recorder.status)
		_, _ = writer.Write([]byte(recorder.body))
	}))
	t.Cleanup(recorder.server.Close)
	return recorder
}

// admin builds the admin surface of a client wired to the recording server,
// configured by options.
func (s *adminServer) admin(t *testing.T, options ...configuration.Option) *auth.Admin {
	t.Helper()
	client, err := auth.New(s.server.URL, "test-secret-key", options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client.Admin()
}

func TestAdminCreateUserRequestShape(t *testing.T) {
	cases := []struct {
		name       string
		attributes auth.UserAttributes
		want       map[string]any
	}{
		{name: "the zero value sends an empty object", attributes: auth.UserAttributes{}, want: map[string]any{}},
		{name: "one set field sends exactly its key", attributes: auth.UserAttributes{Email: "ada@example.com"}, want: map[string]any{"email": "ada@example.com"}},
		{
			name: "every field maps to its wire name",
			attributes: auth.UserAttributes{
				Email:        "ada@example.com",
				Phone:        "15551234567",
				Password:     "correct horse battery staple",
				EmailConfirm: true,
				PhoneConfirm: true,
				UserMetadata: map[string]any{"display_name": "Ada"},
				AppMetadata:  map[string]any{"team": "platform"},
				Role:         "authenticated",
				BanDuration:  "24h",
				ID:           "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
				PasswordHash: "$2y$10$fixture",
			},
			want: map[string]any{
				"email":         "ada@example.com",
				"phone":         "15551234567",
				"password":      "correct horse battery staple",
				"email_confirm": true,
				"phone_confirm": true,
				"user_metadata": map[string]any{"display_name": "Ada"},
				"app_metadata":  map[string]any{"team": "platform"},
				"role":          "authenticated",
				"ban_duration":  "24h",
				"id":            "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
				"password_hash": "$2y$10$fixture",
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			recorder.body = `{"id":"a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d","email":"ada@example.com"}`
			admin := recorder.admin(t)

			result, err := admin.CreateUser(context.Background(), testCase.attributes)
			if err != nil {
				t.Fatalf("CreateUser: %v", err)
			}
			user := result.User()
			if user.ID() != "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d" || user.Email() != "ada@example.com" {
				t.Errorf("user = (%q, %q), want the response user", user.ID(), user.Email())
			}

			if len(recorder.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(recorder.requests))
			}
			request := recorder.requests[0]
			if request.method != http.MethodPost || request.path != "/auth/v1/admin/users" {
				t.Errorf("request = %s %s, want POST /auth/v1/admin/users", request.method, request.path)
			}
			if request.contentType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", request.contentType)
			}
			if request.apiKey != "test-secret-key" {
				t.Errorf("apikey = %q, want test-secret-key", request.apiKey)
			}
			if request.authorization != "" {
				t.Errorf("Authorization = %q, want it absent - admin calls authenticate with the apikey header alone", request.authorization)
			}
			var sent map[string]any
			if err := json.Unmarshal(request.body, &sent); err != nil {
				t.Fatalf("request body %q: %v", request.body, err)
			}
			if !reflect.DeepEqual(sent, testCase.want) {
				t.Errorf("body = %v, want %v", sent, testCase.want)
			}
		})
	}
}

func TestAdminGetUserRequestShape(t *testing.T) {
	recorder := newAdminServer(t)
	recorder.body = `{"id":"a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"}`
	admin := recorder.admin(t)

	// Uppercase is within the canonical form, so it reaches the wire verbatim.
	if _, err := admin.GetUser(context.Background(), "A1B2C3D4-E5F6-4A7B-8C9D-0E1F2A3B4C5D"); err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if len(recorder.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(recorder.requests))
	}
	request := recorder.requests[0]
	if request.method != http.MethodGet || request.path != "/auth/v1/admin/users/A1B2C3D4-E5F6-4A7B-8C9D-0E1F2A3B4C5D" {
		t.Errorf("request = %s %s, want GET of the user's path", request.method, request.path)
	}
	if request.contentType != "" || len(request.body) != 0 {
		t.Errorf("request carries a body (%q, %q), want none", request.contentType, request.body)
	}
}

func TestAdminRejectsMalformedUserID(t *testing.T) {
	malformed := []struct{ name, id string }{
		{"empty", ""},
		{"not a UUID at all", "not-a-uuid"},
		{"bare hexadecimal", "a1b2c3d4e5f64a7b8c9d0e1f2a3b4c5d"},
		{"braced", "{a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d}"},
		{"URN", "urn:uuid:a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"},
		{"one digit short", "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5"},
		{"non-hexadecimal character", "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5g"},
		{"misplaced hyphen", "a1b2c3d4e-5f6-4a7b-8c9d-0e1f2a3b4c5d"},
	}
	recorder := newAdminServer(t)
	admin := recorder.admin(t)
	operations := []struct {
		name string
		call func(id string) error
	}{
		{"GetUser", func(id string) error { _, err := admin.GetUser(context.Background(), id); return err }},
		{"UpdateUser", func(id string) error {
			_, err := admin.UpdateUser(context.Background(), id, auth.UserAttributes{})
			return err
		}},
		{"DeleteUser", func(id string) error { _, err := admin.DeleteUser(context.Background(), id); return err }},
		{"DeleteUser with WithSoftDelete", func(id string) error {
			_, err := admin.DeleteUser(context.Background(), id, userdelete.WithSoftDelete())
			return err
		}},
		{"ListMFAFactors", func(id string) error { _, err := admin.ListMFAFactors(context.Background(), id); return err }},
		{"UpdateMFAFactor", func(id string) error {
			_, err := admin.UpdateMFAFactor(context.Background(), id, "f0e1d2c3-b4a5-4968-8f7e-6d5c4b3a2918")
			return err
		}},
		{"DeleteMFAFactor", func(id string) error {
			_, err := admin.DeleteMFAFactor(context.Background(), id, "f0e1d2c3-b4a5-4968-8f7e-6d5c4b3a2918")
			return err
		}},
	}
	for _, testCase := range malformed {
		t.Run(testCase.name, func(t *testing.T) {
			for _, operation := range operations {
				if err := operation.call(testCase.id); !errors.Is(err, auth.ErrInvalidUserID) {
					t.Errorf("%s(%q) = %v, want ErrInvalidUserID", operation.name, testCase.id, err)
				}
			}
		})
	}
	if len(recorder.requests) != 0 {
		t.Errorf("requests = %d, want 0 - a malformed id must never reach the wire", len(recorder.requests))
	}
}

func TestAdminRejectsMalformedFactorID(t *testing.T) {
	const userID = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	recorder := newAdminServer(t)
	admin := recorder.admin(t)
	operations := []struct {
		name string
		call func(id string) error
	}{
		{"UpdateMFAFactor", func(id string) error {
			_, err := admin.UpdateMFAFactor(context.Background(), userID, id, mfafactor.WithFriendlyName("Renamed"))
			return err
		}},
		{"DeleteMFAFactor", func(id string) error {
			_, err := admin.DeleteMFAFactor(context.Background(), userID, id)
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.call("not-a-uuid"); !errors.Is(err, auth.ErrInvalidFactorID) {
				t.Errorf("%s(not-a-uuid) = %v, want ErrInvalidFactorID", operation.name, err)
			}
		})
	}
	if len(recorder.requests) != 0 {
		t.Errorf("requests = %d, want 0 - a malformed id must never reach the wire", len(recorder.requests))
	}
}

func TestAdminDeleteUserRequestShape(t *testing.T) {
	cases := []struct {
		name    string
		options []userdelete.Option
		want    map[string]any // nil wants no body at all, so the server's default governs
	}{
		{name: "a bare delete sends no body, leaving the mode to the server's default"},
		{name: "WithSoftDelete sends the flag true", options: []userdelete.Option{userdelete.WithSoftDelete()}, want: map[string]any{"should_soft_delete": true}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			admin := recorder.admin(t)

			// The server answers a user delete with an empty JSON object, which
			// decodes to nothing: success is the empty result and the nil error.
			if _, err := admin.DeleteUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", testCase.options...); err != nil {
				t.Fatalf("DeleteUser: %v", err)
			}

			if len(recorder.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(recorder.requests))
			}
			request := recorder.requests[0]
			if request.method != http.MethodDelete || request.path != "/auth/v1/admin/users/a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d" {
				t.Errorf("request = %s %s, want DELETE of the user's path", request.method, request.path)
			}
			if testCase.want == nil {
				if request.contentType != "" || len(request.body) != 0 {
					t.Errorf("request carries a body (%q, %q), want none so the server's default governs", request.contentType, request.body)
				}
				return
			}
			if request.contentType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", request.contentType)
			}
			var sent map[string]any
			if err := json.Unmarshal(request.body, &sent); err != nil {
				t.Fatalf("request body %q: %v", request.body, err)
			}
			if !reflect.DeepEqual(sent, testCase.want) {
				t.Errorf("body = %v, want %v", sent, testCase.want)
			}
		})
	}
}

func TestAdminErrorSurface(t *testing.T) {
	operations := []struct {
		name string
		call func(*auth.Admin) error
	}{
		{"CreateUser", func(admin *auth.Admin) error {
			_, err := admin.CreateUser(context.Background(), auth.UserAttributes{Email: "ada@example.com"})
			return err
		}},
		{"GetUser", func(admin *auth.Admin) error {
			_, err := admin.GetUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
			return err
		}},
		{"DeleteUser", func(admin *auth.Admin) error {
			_, err := admin.DeleteUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
			return err
		}},
		{"DeleteUser with WithSoftDelete", func(admin *auth.Admin) error {
			_, err := admin.DeleteUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", userdelete.WithSoftDelete())
			return err
		}},
		{"UpdateUser", func(admin *auth.Admin) error {
			_, err := admin.UpdateUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", auth.UserAttributes{Role: "editor"})
			return err
		}},
		{"ListUsers", func(admin *auth.Admin) error { _, err := admin.ListUsers(context.Background()); return err }},
		{"InviteUser", func(admin *auth.Admin) error {
			_, err := admin.InviteUser(context.Background(), "ada@example.com")
			return err
		}},
		{"GenerateLink", func(admin *auth.Admin) error {
			_, err := admin.GenerateLink(context.Background(), auth.NewLinkRequest("ada@example.com").Recovery())
			return err
		}},
		{"SignOut", func(admin *auth.Admin) error {
			_, err := admin.SignOut(context.Background(), "end-user-jwt", signout.WithLocalScope())
			return err
		}},
		{"ListMFAFactors", func(admin *auth.Admin) error {
			_, err := admin.ListMFAFactors(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
			return err
		}},
		{"UpdateMFAFactor", func(admin *auth.Admin) error {
			_, err := admin.UpdateMFAFactor(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", "f0e1d2c3-b4a5-4968-8f7e-6d5c4b3a2918", mfafactor.WithFriendlyName("Renamed"))
			return err
		}},
		{"DeleteMFAFactor", func(admin *auth.Admin) error {
			_, err := admin.DeleteMFAFactor(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", "f0e1d2c3-b4a5-4968-8f7e-6d5c4b3a2918")
			return err
		}},
		{"CreateCustomProvider", func(admin *auth.Admin) error {
			_, err := admin.CreateCustomProvider(context.Background(), customprovider.New("custom:acme", "Acme", "client-id", "client-secret").OIDC("https://id.acme.example"))
			return err
		}},
		{"GetCustomProvider", func(admin *auth.Admin) error {
			_, err := admin.GetCustomProvider(context.Background(), "custom:acme")
			return err
		}},
		{"ListCustomProviders", func(admin *auth.Admin) error {
			_, err := admin.ListCustomProviders(context.Background(), customprovider.WithOIDCOnly())
			return err
		}},
		{"UpdateCustomProvider", func(admin *auth.Admin) error {
			_, err := admin.UpdateCustomProvider(context.Background(), "custom:acme", customprovider.WithEnabled(false))
			return err
		}},
		{"DeleteCustomProvider", func(admin *auth.Admin) error {
			_, err := admin.DeleteCustomProvider(context.Background(), "custom:acme")
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			recorder.status = http.StatusForbidden
			recorder.body = `{"code":"not_admin","message":"User not allowed"}`
			admin := recorder.admin(t)

			err := operation.call(admin)
			var serverError *auth.Error
			if !errors.As(err, &serverError) {
				t.Fatalf("%s error = %v, want *auth.Error", operation.name, err)
			}
			if serverError.HTTPStatus != http.StatusForbidden || serverError.Code != "not_admin" || serverError.Message != "User not allowed" {
				t.Errorf("Error = %+v, want HTTP 403 not_admin \"User not allowed\"", serverError)
			}
		})
	}
}

func TestAdminUserParsing(t *testing.T) {
	recorder := newAdminServer(t)
	recorder.body = `{
		"id": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
		"aud": "authenticated",
		"role": "authenticated",
		"email": "ada@example.com",
		"email_confirmed_at": "2026-01-02T03:04:05Z",
		"invited_at": "2026-01-02T03:04:05Z",
		"confirmation_sent_at": "2026-01-02T03:04:05Z",
		"recovery_sent_at": "2026-01-02T03:04:07Z",
		"new_email": "ada@new.example.com",
		"email_change_sent_at": "2026-01-02T03:04:08Z",
		"new_phone": "15551234567",
		"phone_change_sent_at": "2026-01-02T03:04:09Z",
		"reauthentication_sent_at": "2026-01-02T03:04:10Z",
		"created_at": "2026-01-02T03:04:05Z",
		"updated_at": "2026-01-02T03:04:06Z",
		"banned_until": "2027-01-02T03:04:05Z",
		"user_metadata": {"display_name": "Ada"},
		"app_metadata": {"provider": "email"},
		"identities": [{"id": "ada-provider-id", "identity_id": "b2c3d4e5-f6a7-4b8c-9d0e-1f2a3b4c5d6e", "provider": "email", "email": "ada@example.com"}]
	}`
	admin := recorder.admin(t)

	result, err := admin.GetUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	user := result.User()
	if user.ID() != "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d" || user.Email() != "ada@example.com" || user.Role() != "authenticated" {
		t.Errorf("user = (%q, %q, %q)", user.ID(), user.Email(), user.Role())
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !user.EmailConfirmedAt().Equal(want) {
		t.Errorf("EmailConfirmedAt = %v, want %v", user.EmailConfirmedAt(), want)
	}
	if user.CreatedAt().IsZero() || user.UpdatedAt().IsZero() {
		t.Errorf("timestamps = (%v, %v), want both set", user.CreatedAt(), user.UpdatedAt())
	}
	if want := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC); !user.BannedUntil().Equal(want) {
		t.Errorf("BannedUntil = %v, want %v", user.BannedUntil(), want)
	}
	if !user.DeletedAt().IsZero() {
		t.Errorf("DeletedAt = %v, want the zero time", user.DeletedAt())
	}
	if user.UserMetadata()["display_name"] != "Ada" || user.AppMetadata()["provider"] != "email" {
		t.Errorf("metadata = (%v, %v)", user.UserMetadata(), user.AppMetadata())
	}
	if user.NewEmail() != "ada@new.example.com" || user.NewPhone() != "15551234567" {
		t.Errorf("pending changes = (%q, %q), want the fixture's new email and phone", user.NewEmail(), user.NewPhone())
	}
	sentTimestamps := []struct {
		name  string
		value time.Time
	}{
		{"InvitedAt", user.InvitedAt()},
		{"ConfirmationSentAt", user.ConfirmationSentAt()},
		{"RecoverySentAt", user.RecoverySentAt()},
		{"EmailChangeSentAt", user.EmailChangeSentAt()},
		{"PhoneChangeSentAt", user.PhoneChangeSentAt()},
		{"ReauthenticationSentAt", user.ReauthenticationSentAt()},
	}
	for _, timestamp := range sentTimestamps {
		if timestamp.value.IsZero() {
			t.Errorf("%s is zero, want the fixture's timestamp", timestamp.name)
		}
	}
	identities := user.Identities()
	if len(identities) != 1 || identities[0].Email() != "ada@example.com" || identities[0].Provider() != "email" {
		t.Errorf("identities = %v, want the fixture's one email identity", identities)
	}
}

func TestAdminUpdateUserRequestShape(t *testing.T) {
	cases := []struct {
		name       string
		attributes auth.UserAttributes
		want       map[string]any
	}{
		{name: "the zero value sends an empty object", attributes: auth.UserAttributes{}, want: map[string]any{}},
		{name: "a ban lift passes the literal none through", attributes: auth.UserAttributes{Role: "editor", BanDuration: "none"}, want: map[string]any{"role": "editor", "ban_duration": "none"}},
		{name: "a nil metadata value renders null, the key-delete request", attributes: auth.UserAttributes{UserMetadata: map[string]any{"locale": nil}}, want: map[string]any{"user_metadata": map[string]any{"locale": nil}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			recorder.body = `{"id":"a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d","role":"editor"}`
			admin := recorder.admin(t)

			result, err := admin.UpdateUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", testCase.attributes)
			if err != nil {
				t.Fatalf("UpdateUser: %v", err)
			}
			if user := result.User(); user.Role() != "editor" {
				t.Errorf("Role = %q, want the response user's editor", user.Role())
			}

			if len(recorder.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(recorder.requests))
			}
			request := recorder.requests[0]
			if request.method != http.MethodPut || request.path != "/auth/v1/admin/users/a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d" {
				t.Errorf("request = %s %s, want PUT of the user's path", request.method, request.path)
			}
			var sent map[string]any
			if err := json.Unmarshal(request.body, &sent); err != nil {
				t.Fatalf("request body %q: %v", request.body, err)
			}
			if !reflect.DeepEqual(sent, testCase.want) {
				t.Errorf("body = %v, want %v", sent, testCase.want)
			}
		})
	}
}

func TestAdminListUsersRequestShape(t *testing.T) {
	cases := []struct {
		name    string
		options []pagination.Option
		want    string
	}{
		{name: "a bare call sends no pagination parameters", options: nil, want: ""},
		{name: "WithPage alone sends only the page", options: []pagination.Option{pagination.WithPage(2)}, want: "page=2"},
		{name: "WithSize alone sends only the size", options: []pagination.Option{pagination.WithSize(50)}, want: "per_page=50"},
		{name: "together they send both", options: []pagination.Option{pagination.WithPage(2), pagination.WithSize(50)}, want: "page=2&per_page=50"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			recorder.body = `{"aud":"authenticated","users":[]}`
			admin := recorder.admin(t)

			if _, err := admin.ListUsers(context.Background(), testCase.options...); err != nil {
				t.Fatalf("ListUsers: %v", err)
			}

			if len(recorder.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(recorder.requests))
			}
			request := recorder.requests[0]
			if request.method != http.MethodGet || request.path != "/auth/v1/admin/users" {
				t.Errorf("request = %s %s, want GET /auth/v1/admin/users", request.method, request.path)
			}
			if request.query != testCase.want {
				t.Errorf("query = %q, want %q", request.query, testCase.want)
			}
			if request.contentType != "" || len(request.body) != 0 {
				t.Errorf("request carries a body (%q, %q), want none", request.contentType, request.body)
			}
		})
	}
}

func TestInviteUserRequestShape(t *testing.T) {
	cases := []struct {
		name      string
		options   []userinvite.Option
		want      map[string]any
		wantQuery string
	}{
		{name: "a bare invite sends the email alone", want: map[string]any{"email": "ada@example.com"}},
		{name: "WithUserMetadata adds the data field", options: []userinvite.Option{userinvite.WithUserMetadata(map[string]any{"workspace": "platform"})}, want: map[string]any{"email": "ada@example.com", "data": map[string]any{"workspace": "platform"}}},
		{name: "WithRedirectTo rides the query string, never the body", options: []userinvite.Option{userinvite.WithRedirectTo("https://app.example.com/accept")}, want: map[string]any{"email": "ada@example.com"}, wantQuery: "redirect_to=" + url.QueryEscape("https://app.example.com/accept")},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			admin := recorder.admin(t)

			if _, err := admin.InviteUser(context.Background(), "ada@example.com", testCase.options...); err != nil {
				t.Fatalf("InviteUser: %v", err)
			}
			if len(recorder.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(recorder.requests))
			}
			request := recorder.requests[0]
			if request.method != http.MethodPost || request.path != "/auth/v1/invite" {
				t.Errorf("request = %s %s, want POST of the invite path beside admin, not under it", request.method, request.path)
			}
			if request.query != testCase.wantQuery {
				t.Errorf("query = %q, want %q", request.query, testCase.wantQuery)
			}
			var sent map[string]any
			if err := json.Unmarshal(request.body, &sent); err != nil {
				t.Fatalf("request body %q: %v", request.body, err)
			}
			if !reflect.DeepEqual(sent, testCase.want) {
				t.Errorf("body = %v, want %v", sent, testCase.want)
			}
		})
	}
}

func TestAdminListUsersResultParsing(t *testing.T) {
	cases := []struct {
		name       string
		header     map[string]string
		body       string
		wantEmails []string
		wantTotal  int
		wantNext   int
		wantNextOK bool
		wantLast   int
		wantLastOK bool
	}{
		{
			name: "users, total and both page links parse",
			header: map[string]string{
				"X-Total-Count": "7",
				"Link":          `<http://stack.local/admin/users?page=2&per_page=2>; rel="next", <http://stack.local/admin/users?page=4&per_page=2>; rel="last"`,
			},
			body: `{"aud":"authenticated","users":[
				{"id":"a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d","email":"ada@example.com"},
				{"id":"b2c3d4e5-f6a7-4b8c-9d0e-1f2a3b4c5d6e","email":"grace@example.com"}
			]}`,
			wantEmails: []string{"ada@example.com", "grace@example.com"},
			wantTotal:  7, wantNext: 2, wantNextOK: true, wantLast: 4, wantLastOK: true,
		},
		{
			name: "the final page carries no next link",
			header: map[string]string{
				"X-Total-Count": "7",
				"Link":          `<http://stack.local/admin/users?page=4>; rel="last"`,
			},
			body:       `{"aud":"authenticated","users":[{"id":"a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d","email":"ada@example.com"}]}`,
			wantEmails: []string{"ada@example.com"},
			wantTotal:  7, wantLast: 4, wantLastOK: true,
		},
		{
			name: "an empty project lists page zero as last",
			header: map[string]string{
				"X-Total-Count": "0",
				"Link":          `<http://stack.local/admin/users?page=0>; rel="last"`,
			},
			body:       `{"aud":"authenticated","users":[]}`,
			wantLastOK: true,
		},
		{
			name: "absent headers leave every accessor reporting absence",
			body: `{"aud":"authenticated","users":[]}`,
		},
		{
			name:   "a malformed Link header is ignored rather than an error",
			header: map[string]string{"Link": "not a link header at all"},
			body:   `{"aud":"authenticated","users":[]}`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			recorder.header = testCase.header
			recorder.body = testCase.body
			admin := recorder.admin(t)

			result, err := admin.ListUsers(context.Background())
			if err != nil {
				t.Fatalf("ListUsers: %v", err)
			}
			users := result.Users()
			if len(users) != len(testCase.wantEmails) {
				t.Fatalf("Users = %d entries, want %d", len(users), len(testCase.wantEmails))
			}
			for i, want := range testCase.wantEmails {
				if users[i].Email() != want {
					t.Errorf("Users[%d].Email = %q, want %q", i, users[i].Email(), want)
				}
			}
			if result.Total() != testCase.wantTotal {
				t.Errorf("Total = %d, want %d", result.Total(), testCase.wantTotal)
			}
			if number, ok := result.NextPage(); ok != testCase.wantNextOK || number != testCase.wantNext {
				t.Errorf("NextPage = (%d, %t), want (%d, %t)", number, ok, testCase.wantNext, testCase.wantNextOK)
			}
			if number, ok := result.LastPage(); ok != testCase.wantLastOK || number != testCase.wantLast {
				t.Errorf("LastPage = (%d, %t), want (%d, %t)", number, ok, testCase.wantLast, testCase.wantLastOK)
			}
		})
	}
}

func TestAdminSignOutRequestShape(t *testing.T) {
	scopes := []struct {
		name    string
		options []signout.Option
		query   string
	}{
		{"a bare call states the global scope, the server's own default", nil, "scope=global"},
		{"WithLocalScope narrows to the presented session", []signout.Option{signout.WithLocalScope()}, "scope=local"},
		{"WithOthersScope spares the presented session", []signout.Option{signout.WithOthersScope()}, "scope=others"},
		{"of two scopes the later wins", []signout.Option{signout.WithLocalScope(), signout.WithOthersScope()}, "scope=others"},
	}
	for _, testCase := range scopes {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			// The server answers a sign-out with 204 and no body, so success is
			// the empty result and the nil error.
			recorder.status = http.StatusNoContent
			recorder.body = ""
			admin := recorder.admin(t)

			if _, err := admin.SignOut(context.Background(), "end-user-jwt", testCase.options...); err != nil {
				t.Fatalf("SignOut: %v", err)
			}

			if len(recorder.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(recorder.requests))
			}
			request := recorder.requests[0]
			if request.method != http.MethodPost || request.path != "/auth/v1/logout" {
				t.Errorf("request = %s %s, want POST of the logout path", request.method, request.path)
			}
			if request.query != testCase.query {
				t.Errorf("query = %q, want %q", request.query, testCase.query)
			}
			if request.authorization != "Bearer end-user-jwt" {
				t.Errorf("Authorization = %q, want the end user's token - it names whose sessions to revoke", request.authorization)
			}
			if request.contentType != "" || len(request.body) != 0 {
				t.Errorf("request carries a body (%q, %q), want none", request.contentType, request.body)
			}
		})
	}

	t.Run("an empty token is rejected before the wire", func(t *testing.T) {
		recorder := newAdminServer(t)
		admin := recorder.admin(t)
		if _, err := admin.SignOut(context.Background(), ""); !errors.Is(err, auth.ErrMissingJWT) {
			t.Errorf("SignOut with an empty token = %v, want ErrMissingJWT", err)
		}
		if len(recorder.requests) != 0 {
			t.Errorf("requests = %d, want 0", len(recorder.requests))
		}
	})
}

func TestAdminMFARequestShapes(t *testing.T) {
	const userID = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	const factorID = "f0e1d2c3-b4a5-4968-8f7e-6d5c4b3a2918"
	const factorsPath = "/auth/v1/admin/users/" + userID + "/factors"
	const factorPath = factorsPath + "/" + factorID

	t.Run("ListMFAFactors GETs the factors path with no body", func(t *testing.T) {
		recorder := newAdminServer(t)
		recorder.body = `[]`
		admin := recorder.admin(t)

		result, err := admin.ListMFAFactors(context.Background(), userID)
		if err != nil {
			t.Fatalf("ListMFAFactors: %v", err)
		}
		if factors := result.Factors(); len(factors) != 0 {
			t.Errorf("factors = %d, want 0 from an empty array", len(factors))
		}
		request := recorder.requests[0]
		if request.method != http.MethodGet || request.path != factorsPath {
			t.Errorf("request = %s %s, want GET %s", request.method, request.path, factorsPath)
		}
		if request.contentType != "" || len(request.body) != 0 {
			t.Errorf("request carries a body (%q, %q), want none", request.contentType, request.body)
		}
	})

	t.Run("DeleteMFAFactor DELETEs the factor's path with no body", func(t *testing.T) {
		recorder := newAdminServer(t)
		admin := recorder.admin(t)

		if _, err := admin.DeleteMFAFactor(context.Background(), userID, factorID); err != nil {
			t.Fatalf("DeleteMFAFactor: %v", err)
		}
		request := recorder.requests[0]
		if request.method != http.MethodDelete || request.path != factorPath {
			t.Errorf("request = %s %s, want DELETE %s", request.method, request.path, factorPath)
		}
		if request.contentType != "" || len(request.body) != 0 {
			t.Errorf("request carries a body (%q, %q), want none", request.contentType, request.body)
		}
	})

	updates := []struct {
		name    string
		options []mfafactor.Option
		want    map[string]any
	}{
		{name: "a bare update sends an empty object, changing nothing", want: map[string]any{}},
		{name: "WithFriendlyName sends only the name", options: []mfafactor.Option{mfafactor.WithFriendlyName("Work laptop")}, want: map[string]any{"friendly_name": "Work laptop"}},
		{name: "WithPhone sends only the phone", options: []mfafactor.Option{mfafactor.WithPhone("+15551234567")}, want: map[string]any{"phone": "+15551234567"}},
		{
			name:    "both options send both fields",
			options: []mfafactor.Option{mfafactor.WithFriendlyName("Work laptop"), mfafactor.WithPhone("+15551234567")},
			want:    map[string]any{"friendly_name": "Work laptop", "phone": "+15551234567"},
		},
	}
	for _, testCase := range updates {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			recorder.body = `{"id":"` + factorID + `","factor_type":"totp","status":"verified"}`
			admin := recorder.admin(t)

			result, err := admin.UpdateMFAFactor(context.Background(), userID, factorID, testCase.options...)
			if err != nil {
				t.Fatalf("UpdateMFAFactor: %v", err)
			}
			if factor := result.Factor(); factor.ID() != factorID {
				t.Errorf("ID = %q, want the served factor parsed", factor.ID())
			}
			request := recorder.requests[0]
			if request.method != http.MethodPut || request.path != factorPath {
				t.Errorf("request = %s %s, want PUT %s", request.method, request.path, factorPath)
			}
			if request.contentType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", request.contentType)
			}
			var sent map[string]any
			if err := json.Unmarshal(request.body, &sent); err != nil {
				t.Fatalf("request body %q: %v", request.body, err)
			}
			if !reflect.DeepEqual(sent, testCase.want) {
				t.Errorf("body = %v, want %v", sent, testCase.want)
			}
		})
	}
}

func TestFactorParsing(t *testing.T) {
	recorder := newAdminServer(t)
	recorder.body = `[
		{
			"id": "11111111-aaaa-4bbb-8ccc-222222222222",
			"factor_type": "totp",
			"status": "verified",
			"friendly_name": "Work laptop",
			"phone": "",
			"created_at": "2026-01-02T03:04:05Z",
			"updated_at": "2026-01-02T03:04:06Z",
			"last_challenged_at": "2026-01-03T04:05:06Z"
		},
		{
			"id": "33333333-bbbb-4ccc-8ddd-444444444444",
			"factor_type": "phone",
			"status": "unverified",
			"friendly_name": "",
			"phone": "+15551234567",
			"created_at": "2026-01-02T03:04:05Z",
			"updated_at": "2026-01-02T03:04:05Z",
			"last_challenged_at": null
		},
		{
			"id": "55555555-cccc-4ddd-8eee-666666666666",
			"factor_type": "webauthn",
			"status": "verified",
			"friendly_name": "Security key",
			"phone": "",
			"web_authn_aaguid": "77777777-dddd-4eee-8fff-888888888888",
			"last_webauthn_challenge_data": {"type": "webauthn", "challenge": {"id": "challenge-id"}},
			"created_at": "2026-01-02T03:04:05Z",
			"updated_at": "2026-01-02T03:04:05Z"
		},
		{
			"id": "99999999-eeee-4fff-8aaa-000000000000",
			"factor_type": "hardware_token",
			"status": "unverified"
		}
	]`
	admin := recorder.admin(t)

	result, err := admin.ListMFAFactors(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
	if err != nil {
		t.Fatalf("ListMFAFactors: %v", err)
	}
	factors := result.Factors()
	if len(factors) != 4 {
		t.Fatalf("factors = %d, want 4", len(factors))
	}

	totp := factors[0]
	if totp.ID() != "11111111-aaaa-4bbb-8ccc-222222222222" || totp.Type() != "totp" || totp.Status() != "verified" {
		t.Errorf("totp = (%q, %q, %q), want the served identity fields", totp.ID(), totp.Type(), totp.Status())
	}
	if totp.FriendlyName() != "Work laptop" || totp.Phone() != "" || totp.WebAuthnAAGUID() != "" {
		t.Errorf("totp = (%q, %q, %q), want only the friendly name set", totp.FriendlyName(), totp.Phone(), totp.WebAuthnAAGUID())
	}
	if !totp.CreatedAt().Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) ||
		!totp.UpdatedAt().Equal(time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC)) ||
		!totp.LastChallengedAt().Equal(time.Date(2026, 1, 3, 4, 5, 6, 0, time.UTC)) {
		t.Errorf("totp timestamps = (%v, %v, %v), want the served instants", totp.CreatedAt(), totp.UpdatedAt(), totp.LastChallengedAt())
	}
	if totp.LastWebAuthnChallengeData() != nil {
		t.Errorf("totp challenge data = %v, want nil on a TOTP factor", totp.LastWebAuthnChallengeData())
	}

	phone := factors[1]
	if phone.Type() != "phone" || phone.Status() != "unverified" || phone.Phone() != "+15551234567" {
		t.Errorf("phone = (%q, %q, %q), want the served phone factor", phone.Type(), phone.Status(), phone.Phone())
	}
	if phone.FriendlyName() != "" || !phone.LastChallengedAt().IsZero() {
		t.Errorf("phone = (%q, %v), want no name and the null challenge time zero", phone.FriendlyName(), phone.LastChallengedAt())
	}

	webAuthn := factors[2]
	if webAuthn.Type() != "webauthn" || webAuthn.WebAuthnAAGUID() != "77777777-dddd-4eee-8fff-888888888888" {
		t.Errorf("webauthn = (%q, %q), want the served AAGUID", webAuthn.Type(), webAuthn.WebAuthnAAGUID())
	}
	challengeData := webAuthn.LastWebAuthnChallengeData()
	if challengeData["type"] != "webauthn" {
		t.Errorf("challenge data = %v, want the served object", challengeData)
	}
	challengeData["type"] = "mutated"
	if webAuthn.LastWebAuthnChallengeData()["type"] != "webauthn" {
		t.Error("mutating the returned challenge data changed the factor, want a copy")
	}

	unknown := factors[3]
	if unknown.Type() != "hardware_token" {
		t.Errorf("Type = %q, want an unknown factor type passed through verbatim", unknown.Type())
	}
	if !unknown.CreatedAt().IsZero() || !unknown.UpdatedAt().IsZero() {
		t.Errorf("timestamps = (%v, %v), want zero when the server omits them", unknown.CreatedAt(), unknown.UpdatedAt())
	}
}

func TestAdminCreateCustomProviderRequestShape(t *testing.T) {
	cases := []struct {
		name       string
		definition customprovider.Definition
		want       map[string]any
	}{
		{
			name: "a minimal OIDC definition sends the required fields and leaves every default unstated",
			definition: customprovider.New("custom:acme", "Acme Corp SSO", "client-id-1", "client-secret-1").
				OIDC("https://id.acme.example"),
			want: map[string]any{
				"provider_type": "oidc",
				"identifier":    "custom:acme",
				"name":          "Acme Corp SSO",
				"client_id":     "client-id-1",
				"client_secret": "client-secret-1",
				"issuer":        "https://id.acme.example",
			},
		},
		{
			name: "every OIDC refinement maps to its wire name",
			definition: customprovider.New("custom:acme", "Acme Corp SSO", "client-id-1", "client-secret-1").
				OIDC("https://id.acme.example").
				Scopes("openid", "email").
				AcceptableClientIDs("client-id-2").
				CustomClaimsAllowlist("department").
				AttributeMapping(map[string]any{"full_name": "name"}).
				AuthorizationParams(map[string]string{"prompt": "consent"}).
				WithoutPKCE().
				StartDisabled().
				EmailOptional().
				DiscoveryURL("https://id.acme.example/.well-known/openid-configuration").
				SkipNonceCheck(),
			want: map[string]any{
				"provider_type":           "oidc",
				"identifier":              "custom:acme",
				"name":                    "Acme Corp SSO",
				"client_id":               "client-id-1",
				"client_secret":           "client-secret-1",
				"scopes":                  []any{"openid", "email"},
				"acceptable_client_ids":   []any{"client-id-2"},
				"custom_claims_allowlist": []any{"department"},
				"attribute_mapping":       map[string]any{"full_name": "name"},
				"authorization_params":    map[string]any{"prompt": "consent"},
				"pkce_enabled":            false,
				"enabled":                 false,
				"email_optional":          true,
				"issuer":                  "https://id.acme.example",
				"discovery_url":           "https://id.acme.example/.well-known/openid-configuration",
				"skip_nonce_check":        true,
			},
		},
		{
			name: "an OAuth2 definition carries its three endpoints and the JWKS URI",
			definition: customprovider.New("custom:legacy", "Legacy IdP", "client-id-9", "client-secret-9").
				OAuth2("https://id.example/authorize", "https://id.example/token", "https://id.example/userinfo").
				JWKSURI("https://id.example/jwks").
				Scopes("profile"),
			want: map[string]any{
				"provider_type":     "oauth2",
				"identifier":        "custom:legacy",
				"name":              "Legacy IdP",
				"client_id":         "client-id-9",
				"client_secret":     "client-secret-9",
				"scopes":            []any{"profile"},
				"authorization_url": "https://id.example/authorize",
				"token_url":         "https://id.example/token",
				"userinfo_url":      "https://id.example/userinfo",
				"jwks_uri":          "https://id.example/jwks",
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			// The server answers a create with 201 and the stored provider.
			recorder.status = http.StatusCreated
			recorder.body = `{"id":"11111111-2222-4333-8444-555555555555","identifier":"custom:acme"}`
			admin := recorder.admin(t)

			result, err := admin.CreateCustomProvider(context.Background(), testCase.definition)
			if err != nil {
				t.Fatalf("CreateCustomProvider: %v", err)
			}
			if provider := result.CustomProvider(); provider.Identifier() != "custom:acme" {
				t.Errorf("Identifier = %q, want the response provider", provider.Identifier())
			}

			if len(recorder.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(recorder.requests))
			}
			request := recorder.requests[0]
			if request.method != http.MethodPost || request.path != "/auth/v1/admin/custom-providers" {
				t.Errorf("request = %s %s, want POST /auth/v1/admin/custom-providers", request.method, request.path)
			}
			var sent map[string]any
			if err := json.Unmarshal(request.body, &sent); err != nil {
				t.Fatalf("request body %q: %v", request.body, err)
			}
			if !reflect.DeepEqual(sent, testCase.want) {
				t.Errorf("body = %v, want %v", sent, testCase.want)
			}
		})
	}
}

func TestCustomProviderBuilderCopySemantics(t *testing.T) {
	recorder := newAdminServer(t)
	recorder.status = http.StatusCreated
	recorder.body = `{"identifier":"custom:acme"}`
	admin := recorder.admin(t)

	// Refining a definition must fork it, never reach back into the value it
	// forked from.
	base := customprovider.New("custom:acme", "Acme", "client-id", "client-secret").OIDC("https://id.acme.example")
	refined := base.Scopes("openid")

	// A caller's later writes to a supplied map must not reach a definition
	// already built.
	mapping := map[string]any{"full_name": "name"}
	mapped := base.AttributeMapping(mapping)
	mapping["full_name"] = "mutated"

	for _, definition := range []customprovider.Definition{base, refined, mapped} {
		if _, err := admin.CreateCustomProvider(context.Background(), definition); err != nil {
			t.Fatalf("CreateCustomProvider: %v", err)
		}
	}
	if len(recorder.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(recorder.requests))
	}

	bodies := make([]map[string]any, 3)
	for i := range bodies {
		if err := json.Unmarshal(recorder.requests[i].body, &bodies[i]); err != nil {
			t.Fatalf("request %d body: %v", i, err)
		}
	}
	if _, found := bodies[0]["scopes"]; found {
		t.Error("base definition sent scopes, want the fork left untouched")
	}
	if !reflect.DeepEqual(bodies[1]["scopes"], []any{"openid"}) {
		t.Errorf("refined scopes = %v, want the fork carrying them", bodies[1]["scopes"])
	}
	if !reflect.DeepEqual(bodies[2]["attribute_mapping"], map[string]any{"full_name": "name"}) {
		t.Errorf("attribute_mapping = %v, want the clone taken before the caller's mutation", bodies[2]["attribute_mapping"])
	}
}

func TestAdminUpdateCustomProviderSendsOnlyChosenFields(t *testing.T) {
	cases := []struct {
		name    string
		options []customprovider.Option
		want    map[string]any
	}{
		{"a bare call sends an empty object, the server's no-op", nil, map[string]any{}},
		{"an option carried is an instruction", []customprovider.Option{customprovider.WithEnabled(false)}, map[string]any{"enabled": false}},
		{"a no-argument scopes call clears the list", []customprovider.Option{customprovider.WithScopes()}, map[string]any{"scopes": []any{}}},
		{"a nil mapping clears rather than keeps", []customprovider.Option{customprovider.WithAttributeMapping(nil)}, map[string]any{"attribute_mapping": map[string]any{}}},
		{
			name: "every option maps to its wire name",
			options: []customprovider.Option{
				customprovider.WithName("Acme Corp SSO"),
				customprovider.WithClientID("client-id-2"),
				customprovider.WithClientSecret("client-secret-2"),
				customprovider.WithScopes("openid", "profile"),
				customprovider.WithAcceptableClientIDs("client-id-3"),
				customprovider.WithCustomClaimsAllowlist("department"),
				customprovider.WithAttributeMapping(map[string]any{"full_name": "name"}),
				customprovider.WithAuthorizationParams(map[string]string{"prompt": "consent"}),
				customprovider.WithPKCEEnabled(true),
				customprovider.WithEnabled(true),
				customprovider.WithEmailOptional(false),
				customprovider.WithIssuer("https://id.acme.example"),
				customprovider.WithDiscoveryURL("https://id.acme.example/.well-known/openid-configuration"),
				customprovider.WithSkipNonceCheck(false),
				customprovider.WithAuthorizationURL("https://id.example/authorize"),
				customprovider.WithTokenURL("https://id.example/token"),
				customprovider.WithUserinfoURL("https://id.example/userinfo"),
				customprovider.WithJWKSURI("https://id.example/jwks"),
			},
			want: map[string]any{
				"name":                    "Acme Corp SSO",
				"client_id":               "client-id-2",
				"client_secret":           "client-secret-2",
				"scopes":                  []any{"openid", "profile"},
				"acceptable_client_ids":   []any{"client-id-3"},
				"custom_claims_allowlist": []any{"department"},
				"attribute_mapping":       map[string]any{"full_name": "name"},
				"authorization_params":    map[string]any{"prompt": "consent"},
				"pkce_enabled":            true,
				"enabled":                 true,
				"email_optional":          false,
				"issuer":                  "https://id.acme.example",
				"discovery_url":           "https://id.acme.example/.well-known/openid-configuration",
				"skip_nonce_check":        false,
				"authorization_url":       "https://id.example/authorize",
				"token_url":               "https://id.example/token",
				"userinfo_url":            "https://id.example/userinfo",
				"jwks_uri":                "https://id.example/jwks",
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			recorder.body = `{"identifier":"custom:acme"}`
			admin := recorder.admin(t)

			if _, err := admin.UpdateCustomProvider(context.Background(), "custom:acme", testCase.options...); err != nil {
				t.Fatalf("UpdateCustomProvider: %v", err)
			}
			if len(recorder.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(recorder.requests))
			}
			request := recorder.requests[0]
			if request.method != http.MethodPut || request.path != "/auth/v1/admin/custom-providers/custom:acme" {
				t.Errorf("request = %s %s, want PUT /auth/v1/admin/custom-providers/custom:acme", request.method, request.path)
			}
			var sent map[string]any
			if err := json.Unmarshal(request.body, &sent); err != nil {
				t.Fatalf("request body %q: %v", request.body, err)
			}
			if !reflect.DeepEqual(sent, testCase.want) {
				t.Errorf("body = %v, want %v", sent, testCase.want)
			}
		})
	}
}

func TestAdminListCustomProvidersFilter(t *testing.T) {
	cases := []struct {
		name    string
		options []customprovider.ListOption
		query   string
	}{
		{"a bare call sends no filter, listing every provider", nil, ""},
		{"WithOIDCOnly narrows to OIDC", []customprovider.ListOption{customprovider.WithOIDCOnly()}, "type=oidc"},
		{"WithOAuth2Only narrows to OAuth 2.0", []customprovider.ListOption{customprovider.WithOAuth2Only()}, "type=oauth2"},
		{"of two filters the later wins", []customprovider.ListOption{customprovider.WithOIDCOnly(), customprovider.WithOAuth2Only()}, "type=oauth2"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			recorder.body = `{"providers":[{"identifier":"custom:acme"},{"identifier":"custom:legacy"}]}`
			admin := recorder.admin(t)

			result, err := admin.ListCustomProviders(context.Background(), testCase.options...)
			if err != nil {
				t.Fatalf("ListCustomProviders: %v", err)
			}
			if providers := result.CustomProviders(); len(providers) != 2 || providers[0].Identifier() != "custom:acme" {
				t.Errorf("providers = %d with first %q, want the enveloped pair", len(providers), providers[0].Identifier())
			}

			if len(recorder.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(recorder.requests))
			}
			request := recorder.requests[0]
			if request.method != http.MethodGet || request.path != "/auth/v1/admin/custom-providers" {
				t.Errorf("request = %s %s, want GET /auth/v1/admin/custom-providers", request.method, request.path)
			}
			if request.query != testCase.query {
				t.Errorf("query = %q, want %q", request.query, testCase.query)
			}
		})
	}
}

func TestCustomProviderParsing(t *testing.T) {
	recorder := newAdminServer(t)
	recorder.body = `{"providers":[
		{
			"id": "11111111-2222-4333-8444-555555555555",
			"provider_type": "oidc",
			"identifier": "custom:acme",
			"name": "Acme Corp SSO",
			"client_id": "client-id-1",
			"acceptable_client_ids": ["client-id-2"],
			"scopes": ["openid", "email"],
			"pkce_enabled": true,
			"attribute_mapping": {"full_name": "name"},
			"custom_claims_allowlist": ["department"],
			"authorization_params": {"prompt": "consent"},
			"enabled": true,
			"email_optional": true,
			"issuer": "https://id.acme.example",
			"discovery_url": "https://id.acme.example/.well-known/openid-configuration",
			"skip_nonce_check": true,
			"discovery_document": {
				"issuer": "https://id.acme.example",
				"authorization_endpoint": "https://id.acme.example/authorize",
				"token_endpoint": "https://id.acme.example/token",
				"userinfo_endpoint": "https://id.acme.example/userinfo",
				"jwks_uri": "https://id.acme.example/jwks",
				"scopes_supported": ["openid", "email"],
				"response_types_supported": ["code"],
				"grant_types_supported": ["authorization_code"],
				"subject_types_supported": ["public"]
			},
			"created_at": "2026-01-02T03:04:05Z",
			"updated_at": "2026-01-03T03:04:05Z"
		},
		{
			"id": "66666666-7777-4888-9999-aaaaaaaaaaaa",
			"provider_type": "oauth2",
			"identifier": "custom:legacy",
			"name": "Legacy IdP",
			"client_id": "client-id-9",
			"pkce_enabled": false,
			"enabled": false,
			"email_optional": false,
			"authorization_url": "https://id.example/authorize",
			"token_url": "https://id.example/token",
			"userinfo_url": "https://id.example/userinfo",
			"jwks_uri": "https://id.example/jwks"
		}
	]}`
	admin := recorder.admin(t)

	result, err := admin.ListCustomProviders(context.Background())
	if err != nil {
		t.Fatalf("ListCustomProviders: %v", err)
	}
	providers := result.CustomProviders()
	if len(providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(providers))
	}

	oidc := providers[0]
	if oidc.ID() != "11111111-2222-4333-8444-555555555555" || oidc.Type() != "oidc" {
		t.Errorf("oidc = (%q, %q), want the served id and type", oidc.ID(), oidc.Type())
	}
	if oidc.Identifier() != "custom:acme" || oidc.Name() != "Acme Corp SSO" || oidc.ClientID() != "client-id-1" {
		t.Errorf("oidc = (%q, %q, %q), want the served identity fields", oidc.Identifier(), oidc.Name(), oidc.ClientID())
	}
	if !reflect.DeepEqual(oidc.AcceptableClientIDs(), []string{"client-id-2"}) {
		t.Errorf("AcceptableClientIDs = %v, want the served list", oidc.AcceptableClientIDs())
	}
	if !reflect.DeepEqual(oidc.Scopes(), []string{"openid", "email"}) {
		t.Errorf("Scopes = %v, want the served list", oidc.Scopes())
	}
	if !oidc.PKCEEnabled() || !oidc.Enabled() || !oidc.EmailOptional() || !oidc.SkipNonceCheck() {
		t.Error("booleans want every served true flag reported")
	}
	if !reflect.DeepEqual(oidc.AttributeMapping(), map[string]any{"full_name": "name"}) {
		t.Errorf("AttributeMapping = %v, want the served mapping", oidc.AttributeMapping())
	}
	if !reflect.DeepEqual(oidc.CustomClaimsAllowlist(), []string{"department"}) {
		t.Errorf("CustomClaimsAllowlist = %v, want the served list", oidc.CustomClaimsAllowlist())
	}
	if !reflect.DeepEqual(oidc.AuthorizationParams(), map[string]string{"prompt": "consent"}) {
		t.Errorf("AuthorizationParams = %v, want the served parameters", oidc.AuthorizationParams())
	}
	if oidc.Issuer() != "https://id.acme.example" || oidc.DiscoveryURL() != "https://id.acme.example/.well-known/openid-configuration" {
		t.Errorf("oidc endpoints = (%q, %q), want the served issuer and discovery URL", oidc.Issuer(), oidc.DiscoveryURL())
	}
	if oidc.AuthorizationURL() != "" || oidc.TokenURL() != "" || oidc.UserinfoURL() != "" || oidc.JWKSURI() != "" {
		t.Error("OAuth2 endpoint accessors on an OIDC provider want empty strings")
	}
	if oidc.CreatedAt().Format(time.RFC3339) != "2026-01-02T03:04:05Z" || oidc.UpdatedAt().Format(time.RFC3339) != "2026-01-03T03:04:05Z" {
		t.Errorf("timestamps = (%v, %v), want the served instants", oidc.CreatedAt(), oidc.UpdatedAt())
	}

	discovery, ok := oidc.DiscoveryDocument()
	if !ok {
		t.Fatal("DiscoveryDocument reported absent, want the served document")
	}
	if discovery.Issuer() != "https://id.acme.example" ||
		discovery.AuthorizationEndpoint() != "https://id.acme.example/authorize" ||
		discovery.TokenEndpoint() != "https://id.acme.example/token" ||
		discovery.UserinfoEndpoint() != "https://id.acme.example/userinfo" ||
		discovery.JWKSURI() != "https://id.acme.example/jwks" {
		t.Error("discovery endpoints want the served document's values")
	}
	if !reflect.DeepEqual(discovery.ScopesSupported(), []string{"openid", "email"}) ||
		!reflect.DeepEqual(discovery.ResponseTypesSupported(), []string{"code"}) ||
		!reflect.DeepEqual(discovery.GrantTypesSupported(), []string{"authorization_code"}) ||
		!reflect.DeepEqual(discovery.SubjectTypesSupported(), []string{"public"}) {
		t.Error("discovery capability lists want the served document's values")
	}

	// The accessors hand out copies: mutating a result never reaches the provider.
	scopes := oidc.Scopes()
	scopes[0] = "mutated"
	if oidc.Scopes()[0] != "openid" {
		t.Error("mutating the returned scopes changed the provider, want a copy")
	}
	mapping := oidc.AttributeMapping()
	mapping["full_name"] = "mutated"
	if oidc.AttributeMapping()["full_name"] != "name" {
		t.Error("mutating the returned mapping changed the provider, want a copy")
	}

	oauth2 := providers[1]
	if oauth2.Type() != "oauth2" || oauth2.Identifier() != "custom:legacy" {
		t.Errorf("oauth2 = (%q, %q), want the served type and identifier", oauth2.Type(), oauth2.Identifier())
	}
	if oauth2.AuthorizationURL() != "https://id.example/authorize" ||
		oauth2.TokenURL() != "https://id.example/token" ||
		oauth2.UserinfoURL() != "https://id.example/userinfo" ||
		oauth2.JWKSURI() != "https://id.example/jwks" {
		t.Error("oauth2 endpoints want the served URLs")
	}
	if oauth2.PKCEEnabled() || oauth2.Enabled() || oauth2.EmailOptional() {
		t.Error("booleans want every served false flag reported")
	}
	if oauth2.Issuer() != "" || oauth2.DiscoveryURL() != "" {
		t.Error("OIDC accessors on an OAuth2 provider want empty strings")
	}
	if _, ok := oauth2.DiscoveryDocument(); ok {
		t.Error("DiscoveryDocument on an OAuth2 provider reported present, want absent")
	}
	if oauth2.AcceptableClientIDs() != nil || oauth2.Scopes() != nil {
		t.Error("absent lists want nil")
	}
	if !oauth2.CreatedAt().IsZero() || !oauth2.UpdatedAt().IsZero() {
		t.Errorf("timestamps = (%v, %v), want zero when the server omits them", oauth2.CreatedAt(), oauth2.UpdatedAt())
	}
}

func TestCustomProviderIdentifierEscaping(t *testing.T) {
	// The identifier rides the path as one escaped segment: the colon the
	// server requires stays raw, while characters that would change the
	// path's shape are escaped.
	const identifier = "custom:acme corp/1"
	const wantPath = "/auth/v1/admin/custom-providers/custom:acme%20corp%2F1"
	recorder := newAdminServer(t)
	recorder.body = `{"identifier":"custom:acme corp/1"}`
	admin := recorder.admin(t)

	operations := []struct {
		name string
		call func() error
	}{
		{"GetCustomProvider", func() error {
			_, err := admin.GetCustomProvider(context.Background(), identifier)
			return err
		}},
		{"UpdateCustomProvider", func() error {
			_, err := admin.UpdateCustomProvider(context.Background(), identifier, customprovider.WithName("Renamed"))
			return err
		}},
		{"DeleteCustomProvider", func() error {
			_, err := admin.DeleteCustomProvider(context.Background(), identifier)
			return err
		}},
	}
	for i, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.call(); err != nil {
				t.Fatalf("%s: %v", operation.name, err)
			}
			if got := recorder.requests[i].escapedPath; got != wantPath {
				t.Errorf("escaped path = %q, want %q", got, wantPath)
			}
		})
	}
}

func TestAdminRejectsMissingIdentifier(t *testing.T) {
	recorder := newAdminServer(t)
	admin := recorder.admin(t)
	operations := []struct {
		name string
		call func() error
	}{
		{"GetCustomProvider", func() error {
			_, err := admin.GetCustomProvider(context.Background(), "")
			return err
		}},
		{"UpdateCustomProvider", func() error {
			_, err := admin.UpdateCustomProvider(context.Background(), "", customprovider.WithName("Renamed"))
			return err
		}},
		{"DeleteCustomProvider", func() error {
			_, err := admin.DeleteCustomProvider(context.Background(), "")
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.call(); !errors.Is(err, auth.ErrMissingIdentifier) {
				t.Errorf("%s(\"\") = %v, want ErrMissingIdentifier", operation.name, err)
			}
		})
	}
	if len(recorder.requests) != 0 {
		t.Errorf("requests = %d, want 0 - an empty identifier must never reach the wire", len(recorder.requests))
	}
}
