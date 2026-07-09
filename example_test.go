package supabase_test

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/supabase/supabase-go"
	"github.com/supabase/supabase-go/configuration"
)

func ExampleNewClient() {
	supabase, err := supabase.NewClient("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(supabase != nil)
	// Output: true
}

// ExampleNewClient_options customizes the client with the shared functional
// options from the configuration package: a caller-supplied HTTP client and
// global headers sent on every request.
func ExampleNewClient_options() {
	supabase, err := supabase.NewClient(
		"https://PROJECT_ID.supabase.co",
		"API_KEY",
		configuration.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
		configuration.WithHeader("X-Client-Info", "supabase-go/0.1"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(supabase != nil)
	// Output: true
}

// ExampleNewClient_invalidInput shows that NewClient surfaces the configuration
// package's sentinel errors unchanged, so a caller can match them with
// errors.Is.
func ExampleNewClient_invalidInput() {
	_, err := supabase.NewClient("https://PROJECT_ID.supabase.co", "")
	fmt.Println(errors.Is(err, configuration.ErrMissingKey))
	// Output: true
}
