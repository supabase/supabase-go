package configuration_test

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	configuration "github.com/supabase/supabase-go/configuration"
)

func ExampleNew() {
	projectConfiguration, err := configuration.New(
		"https://PROJECT_ID.supabase.co",
		"API_KEY",
		configuration.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
		configuration.WithHeader("X-Client-Info", "supabase-go/0.1"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(projectConfiguration.BaseURL())
	// Output: https://PROJECT_ID.supabase.co
}

// ExampleNew_invalidInput tells the validation failures apart with
// errors.Is against the exported sentinels.
func ExampleNew_invalidInput() {
	_, err := configuration.New("", "API_KEY")
	fmt.Println(errors.Is(err, configuration.ErrMissingURL))

	_, err = configuration.New("ftp://example.com", "API_KEY")
	fmt.Println(errors.Is(err, configuration.ErrInvalidURL))
	// Output:
	// true
	// true
}
