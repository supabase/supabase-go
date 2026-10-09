package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/core/pagination"
)

// cSpell:ignore uuid

// recordedRequest is one request an adminServer served, as the wire saw it.
type recordedRequest struct {
	method        string
	path          string
	query         string
	contentType   string
	apiKey        string
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
			query:         request.URL.RawQuery,
			contentType:   request.Header.Get("Content-Type"),
			apiKey:        request.Header.Get("apikey"),
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

// admin builds the admin surface of a client wired to the recording server.
func (s *adminServer) admin(t *testing.T) *auth.Admin {
	t.Helper()
	client, err := auth.New(s.server.URL, "test-secret-key")
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
		{
			name:       "the zero value sends an empty object",
			attributes: auth.UserAttributes{},
			want:       map[string]any{},
		},
		{
			name:       "one set field sends exactly its key",
			attributes: auth.UserAttributes{Email: "ada@example.com"},
			want:       map[string]any{"email": "ada@example.com"},
		},
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

			user, err := admin.CreateUser(context.Background(), testCase.attributes)
			if err != nil {
				t.Fatalf("CreateUser: %v", err)
			}
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
	for _, testCase := range malformed {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := admin.GetUser(context.Background(), testCase.id); !errors.Is(err, auth.ErrInvalidUserID) {
				t.Errorf("GetUser(%q) = %v, want ErrInvalidUserID", testCase.id, err)
			}
			if _, err := admin.UpdateUser(context.Background(), testCase.id, auth.UserAttributes{}); !errors.Is(err, auth.ErrInvalidUserID) {
				t.Errorf("UpdateUser(%q) = %v, want ErrInvalidUserID", testCase.id, err)
			}
			if err := admin.DeleteUser(context.Background(), testCase.id); !errors.Is(err, auth.ErrInvalidUserID) {
				t.Errorf("DeleteUser(%q) = %v, want ErrInvalidUserID", testCase.id, err)
			}
			if err := admin.DeleteUser(context.Background(), testCase.id, auth.WithSoftDelete()); !errors.Is(err, auth.ErrInvalidUserID) {
				t.Errorf("DeleteUser(%q, WithSoftDelete) = %v, want ErrInvalidUserID", testCase.id, err)
			}
		})
	}
	if len(recorder.requests) != 0 {
		t.Errorf("requests = %d, want 0 - a malformed id must never reach the wire", len(recorder.requests))
	}
}

func TestAdminDeleteUserRequestShape(t *testing.T) {
	t.Run("a bare delete sends no body, leaving the mode to the server's default", func(t *testing.T) {
		recorder := newAdminServer(t)
		admin := recorder.admin(t)

		// The server answers a user delete with an empty JSON object, which
		// decodes to nothing: success is the nil error alone.
		if err := admin.DeleteUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}

		if len(recorder.requests) != 1 {
			t.Fatalf("requests = %d, want 1", len(recorder.requests))
		}
		request := recorder.requests[0]
		if request.method != http.MethodDelete || request.path != "/auth/v1/admin/users/a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d" {
			t.Errorf("request = %s %s, want DELETE of the user's path", request.method, request.path)
		}
		if request.contentType != "" || len(request.body) != 0 {
			t.Errorf("request carries a body (%q, %q), want none so the server's default governs", request.contentType, request.body)
		}
	})

	t.Run("WithSoftDelete sends the flag true", func(t *testing.T) {
		recorder := newAdminServer(t)
		admin := recorder.admin(t)

		if err := admin.DeleteUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", auth.WithSoftDelete()); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}

		if len(recorder.requests) != 1 {
			t.Fatalf("requests = %d, want 1", len(recorder.requests))
		}
		request := recorder.requests[0]
		if request.contentType != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", request.contentType)
		}
		var sent map[string]any
		if err := json.Unmarshal(request.body, &sent); err != nil {
			t.Fatalf("request body %q: %v", request.body, err)
		}
		want := map[string]any{"should_soft_delete": true}
		if !reflect.DeepEqual(sent, want) {
			t.Errorf("body = %v, want %v", sent, want)
		}
	})
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
			return admin.DeleteUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
		}},
		{"DeleteUser with WithSoftDelete", func(admin *auth.Admin) error {
			return admin.DeleteUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", auth.WithSoftDelete())
		}},
		{"UpdateUser", func(admin *auth.Admin) error {
			_, err := admin.UpdateUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", auth.UserAttributes{Role: "editor"})
			return err
		}},
		{"ListUsers", func(admin *auth.Admin) error {
			_, err := admin.ListUsers(context.Background())
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			recorder.status = http.StatusForbidden
			recorder.body = `{"error_code":"not_admin","msg":"User not allowed"}`
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
		"created_at": "2026-01-02T03:04:05Z",
		"updated_at": "2026-01-02T03:04:06Z",
		"banned_until": "2027-01-02T03:04:05Z",
		"user_metadata": {"display_name": "Ada"},
		"app_metadata": {"provider": "email"}
	}`
	admin := recorder.admin(t)

	user, err := admin.GetUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
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
}

func TestAdminUpdateUserRequestShape(t *testing.T) {
	cases := []struct {
		name       string
		attributes auth.UserAttributes
		want       map[string]any
	}{
		{
			name:       "the zero value sends an empty object",
			attributes: auth.UserAttributes{},
			want:       map[string]any{},
		},
		{
			name:       "a ban lift passes the literal none through",
			attributes: auth.UserAttributes{Role: "editor", BanDuration: "none"},
			want:       map[string]any{"role": "editor", "ban_duration": "none"},
		},
		{
			name:       "a nil metadata value renders null, the key-delete request",
			attributes: auth.UserAttributes{UserMetadata: map[string]any{"locale": nil}},
			want:       map[string]any{"user_metadata": map[string]any{"locale": nil}},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := newAdminServer(t)
			recorder.body = `{"id":"a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d","role":"editor"}`
			admin := recorder.admin(t)

			user, err := admin.UpdateUser(context.Background(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", testCase.attributes)
			if err != nil {
				t.Fatalf("UpdateUser: %v", err)
			}
			if user.Role() != "editor" {
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
		{
			name:    "a bare call sends no pagination parameters",
			options: nil,
			want:    "",
		},
		{
			name:    "WithPage alone sends only the page",
			options: []pagination.Option{pagination.WithPage(2)},
			want:    "page=2",
		},
		{
			name:    "WithSize alone sends only the size",
			options: []pagination.Option{pagination.WithSize(50)},
			want:    "per_page=50",
		},
		{
			name:    "together they send both",
			options: []pagination.Option{pagination.WithPage(2), pagination.WithSize(50)},
			want:    "page=2&per_page=50",
		},
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

func TestAdminUserPageParsing(t *testing.T) {
	t.Run("users, total and both page links parse", func(t *testing.T) {
		recorder := newAdminServer(t)
		recorder.header = map[string]string{
			"X-Total-Count": "7",
			"Link":          `<http://stack.local/admin/users?page=2&per_page=2>; rel="next", <http://stack.local/admin/users?page=4&per_page=2>; rel="last"`,
		}
		recorder.body = `{"aud":"authenticated","users":[
			{"id":"a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d","email":"ada@example.com"},
			{"id":"b2c3d4e5-f6a7-4b8c-9d0e-1f2a3b4c5d6e","email":"grace@example.com"}
		]}`
		admin := recorder.admin(t)

		page, err := admin.ListUsers(context.Background())
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		users := page.Users()
		if len(users) != 2 || users[0].Email() != "ada@example.com" || users[1].Email() != "grace@example.com" {
			t.Errorf("Users = %d entries, want the body's two users", len(users))
		}
		if page.Total() != 7 {
			t.Errorf("Total = %d, want 7", page.Total())
		}
		if number, ok := page.NextPage(); !ok || number != 2 {
			t.Errorf("NextPage = (%d, %t), want (2, true)", number, ok)
		}
		if number, ok := page.LastPage(); !ok || number != 4 {
			t.Errorf("LastPage = (%d, %t), want (4, true)", number, ok)
		}
	})

	t.Run("the final page carries no next link", func(t *testing.T) {
		recorder := newAdminServer(t)
		recorder.header = map[string]string{
			"X-Total-Count": "7",
			"Link":          `<http://stack.local/admin/users?page=4>; rel="last"`,
		}
		recorder.body = `{"aud":"authenticated","users":[{"id":"a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"}]}`
		admin := recorder.admin(t)

		page, err := admin.ListUsers(context.Background())
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if _, ok := page.NextPage(); ok {
			t.Error("NextPage reports options, want false on the final page")
		}
		if number, ok := page.LastPage(); !ok || number != 4 {
			t.Errorf("LastPage = (%d, %t), want (4, true)", number, ok)
		}
	})

	t.Run("an empty project lists page zero as last", func(t *testing.T) {
		recorder := newAdminServer(t)
		recorder.header = map[string]string{
			"X-Total-Count": "0",
			"Link":          `<http://stack.local/admin/users?page=0>; rel="last"`,
		}
		recorder.body = `{"aud":"authenticated","users":[]}`
		admin := recorder.admin(t)

		page, err := admin.ListUsers(context.Background())
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(page.Users()) != 0 || page.Total() != 0 {
			t.Errorf("page = %d users with Total %d, want an empty page", len(page.Users()), page.Total())
		}
		if _, ok := page.NextPage(); ok {
			t.Error("NextPage reports a page, want false")
		}
		if number, ok := page.LastPage(); !ok || number != 0 {
			t.Errorf("LastPage = (%d, %t), want (0, true) - the header said page zero", number, ok)
		}
	})

	t.Run("absent headers leave every accessor reporting absence", func(t *testing.T) {
		recorder := newAdminServer(t)
		recorder.body = `{"aud":"authenticated","users":[]}`
		admin := recorder.admin(t)

		page, err := admin.ListUsers(context.Background())
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if page.Total() != 0 {
			t.Errorf("Total = %d, want 0", page.Total())
		}
		if _, ok := page.NextPage(); ok {
			t.Error("NextPage reports a page, want false")
		}
		if _, ok := page.LastPage(); ok {
			t.Error("LastPage reports a page, want false")
		}
	})

	t.Run("a malformed Link header is ignored rather than an error", func(t *testing.T) {
		recorder := newAdminServer(t)
		recorder.header = map[string]string{"Link": "not a link header at all"}
		recorder.body = `{"aud":"authenticated","users":[]}`
		admin := recorder.admin(t)

		page, err := admin.ListUsers(context.Background())
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if _, ok := page.NextPage(); ok {
			t.Error("NextPage reports a page, want false")
		}
		if _, ok := page.LastPage(); ok {
			t.Error("LastPage reports a page, want false")
		}
	})
}
