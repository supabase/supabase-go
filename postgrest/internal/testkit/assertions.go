// Package testkit provides assertion helpers for tests of the postgrest
// module. Each assertion reports mismatches as test errors attributed to its
// caller and lets the test continue.
package testkit

import (
	"net/http"
	"testing"

	"github.com/supabase/supabase-go/postgrest"
)

// AssertOKResponse pins the Response surface shared by the success-path
// tests, whose handlers serve no Content-Range header: HTTP 200 with the
// total unreported (-1).
func AssertOKResponse(t *testing.T, response postgrest.Response) {
	t.Helper()
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
	}
	if response.Count != -1 {
		t.Errorf("Count = %d, want -1 (no Content-Range served)", response.Count)
	}
}

// AssertNoResults pins the failure half of the return contract shared by
// every error path: rows stay nil and Response stays the zero value.
func AssertNoResults[T any](t *testing.T, rows []T, response postgrest.Response) {
	t.Helper()
	if rows != nil {
		t.Errorf("rows = %+v, want nil on error", rows)
	}
	if response != (postgrest.Response{}) {
		t.Errorf("response = %+v, want zero value on error", response)
	}
}
