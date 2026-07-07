package core_test

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/supabase/supabase-go/core"
)

func ExampleNewConfiguration() {
	configuration, err := core.NewConfiguration(
		"https://project.supabase.co",
		"anon-key",
		core.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
		core.WithHeaders(map[string]string{"X-Client-Info": "supabase-go/0.1"}),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(configuration.BaseURL())
	// Output: https://project.supabase.co
}

// ExampleNewConfiguration_invalidInput tells the validation failures apart with
// errors.Is against the exported sentinels.
func ExampleNewConfiguration_invalidInput() {
	_, err := core.NewConfiguration("", "anon-key")
	fmt.Println(errors.Is(err, core.ErrMissingURL))

	_, err = core.NewConfiguration("ftp://example.com", "anon-key")
	fmt.Println(errors.Is(err, core.ErrInvalidURL))
	// Output:
	// true
	// true
}
