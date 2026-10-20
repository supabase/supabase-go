package asuser

import (
	"errors"
	"net/http"
	"testing"

	"github.com/supabase/supabase-go/auth"
)

// TestAdminRejectsPublishableKey proves the trust boundary from the user's
// side: admin calls on a publishable-key client are rejected by the server as
// unauthorized. No account is created, so the address is a fixed literal.
func TestAdminRejectsPublishableKey(t *testing.T) {
	admin := newAuthClient(t).Admin()

	operations := []struct {
		name string
		call func() error
	}{
		{"CreateUser", func() error {
			_, err := admin.CreateUser(t.Context(), auth.UserAttributes{Email: "rejected@example.com"})
			return err
		}},
		{"GetUser", func() error {
			_, err := admin.GetUser(t.Context(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
			return err
		}},
		{"DeleteUser", func() error {
			return admin.DeleteUser(t.Context(), "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			err := operation.call()
			var serverError *auth.Error
			if !errors.As(err, &serverError) {
				t.Fatalf("%s = %v, want *auth.Error", operation.name, err)
			}
			if serverError.HTTPStatus != http.StatusUnauthorized && serverError.HTTPStatus != http.StatusForbidden {
				t.Errorf("HTTPStatus = %d, want 401 or 403", serverError.HTTPStatus)
			}
		})
	}
}
