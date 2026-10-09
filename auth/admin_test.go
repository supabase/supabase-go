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
)

// cSpell:ignore uuid

// recordedRequest is one request an adminServer served, as the wire saw it.
type recordedRequest struct {
	method        string
	path          string
	contentType   string
	apiKey        string
	authorization string
	body          []byte
}

// adminServer stands in for the Auth server's admin endpoints, recording every
// request and answering each with the configured status and body.
type adminServer struct {
	server   *httptest.Server
	status   int
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
			contentType:   request.Header.Get("Content-Type"),
			apiKey:        request.Header.Get("apikey"),
			authorization: request.Header.Get("Authorization"),
			body:          requestBody,
		})
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
