// Package asproject exercises the auth module against the live local Supabase
// stack that scripts/integration-test.sh starts, as the project: the secret
// key is in the package's environment, for the admin surface and for tests
// whose subject is the boundary between the project and a signed-in user.
package asproject

import (
	"errors"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/integration-testing/testkit"
)

// newAuthClient wires an Auth client holding the local stack's publishable
// key: the signed-in user's side of a boundary test.
func newAuthClient(t *testing.T) *auth.Client {
	t.Helper()
	projectURL, apiKey := testkit.Credentials(t)
	client, err := auth.New(projectURL, apiKey)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return client
}

// assertServerError asserts that err is an *auth.Error carrying status and
// code, and that result, the value returned beside it, is nil.
func assertServerError[T any](t *testing.T, result *T, err error, status int, code string) {
	t.Helper()
	var serverError *auth.Error
	if result != nil || !errors.As(err, &serverError) || serverError.HTTPStatus != status || serverError.Code != code {
		t.Errorf("result and error = (%v, %v), want nil and *auth.Error with HTTP %d and code %s", result, err, status, code)
	}
}
