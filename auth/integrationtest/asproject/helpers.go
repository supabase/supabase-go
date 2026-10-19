// Package asproject exercises the auth module against the live local Supabase
// stack that scripts/integration-test.sh starts, as the project: the secret
// key is in the package's environment, for the admin surface and for tests
// whose subject is the boundary between the project and a signed-in user.
package asproject

import (
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
