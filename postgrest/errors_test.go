package postgrest_test

import (
	"errors"
	"testing"

	"github.com/supabase/supabase-go/core/responses"
	"github.com/supabase/supabase-go/postgrest"
)

// TestErrorRendering pins Error's two rendered forms - with and without a
// server-supplied code - since these strings are what consumers log.
func TestErrorRendering(t *testing.T) {
	testCases := []struct {
		name  string
		value *postgrest.Error
		want  string
	}{
		{
			name:  "with code",
			value: &postgrest.Error{HTTPError: responses.HTTPError{HTTPStatus: 404, Code: "42P01", Message: "relation does not exist"}},
			want:  `postgrest: relation does not exist (code 42P01, HTTP 404)`,
		},
		{
			name:  "without code",
			value: &postgrest.Error{HTTPError: responses.HTTPError{HTTPStatus: 502, Message: "bad gateway"}},
			want:  `postgrest: bad gateway (HTTP 502)`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.value.Error(); got != testCase.want {
				t.Errorf("Error() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestErrorMatchesWithErrorsAs pins the error-model contract that callers
// branch on *Error with a single errors.As, and that a body-parsed Error
// has no underlying cause to unwrap.
func TestErrorMatchesWithErrorsAs(t *testing.T) {
	var err error = &postgrest.Error{HTTPError: responses.HTTPError{HTTPStatus: 404, Code: "42P01", Message: "missing"}}

	var matched *postgrest.Error
	if !errors.As(err, &matched) {
		t.Fatal("errors.As failed to match *postgrest.Error")
	}
	if matched.Code != "42P01" || matched.HTTPStatus != 404 {
		t.Errorf("matched fields = %+v", matched)
	}
	if errors.Unwrap(err) != nil {
		t.Error("body-parsed Error should have no underlying cause")
	}
}
