package postgrest_test

import (
	"errors"
	"testing"

	"github.com/supabase/supabase-go/postgrest"
)

func TestErrorRendering(t *testing.T) {
	testCases := []struct {
		name  string
		value *postgrest.Error
		want  string
	}{
		{
			name:  "with code",
			value: &postgrest.Error{HTTPStatus: 404, Code: "42P01", Message: "relation does not exist"},
			want:  `postgrest: relation does not exist (code 42P01, HTTP 404)`,
		},
		{
			name:  "without code",
			value: &postgrest.Error{HTTPStatus: 502, Message: "bad gateway"},
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

func TestErrorMatchesWithErrorsAs(t *testing.T) {
	var err error = &postgrest.Error{HTTPStatus: 404, Code: "42P01", Message: "missing"}

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
