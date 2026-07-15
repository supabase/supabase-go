package supabase_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/supabase/supabase-go"
	"github.com/supabase/supabase-go/configuration"
	"github.com/supabase/supabase-go/postgrest"
)

func ExampleNewClient() {
	supabase, err := supabase.NewClient("https://PROJECT_ID.supabase.co", "API_KEY")
	fmt.Println(supabase != nil && err == nil)
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
	fmt.Println(supabase != nil && err == nil)
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

// ExampleClient_From runs a basic database query.
// It requires a reachable Supabase project, so it is compiled but not run by go test.
func ExampleClient_From() {
	type Instrument struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	supabase, err := supabase.NewClient("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	var instruments []Instrument
	response, err := supabase.From("instruments").Select("id, name").Execute(context.Background(), &instruments)
	if err != nil {
		var postgrestError *postgrest.Error
		if errors.As(err, &postgrestError) {
			// Branch on the stable code, not the message text. When the
			// database knows the fix it says so in Hint.
			fmt.Println(postgrestError.Code, postgrestError.Hint)
			return
		}
		fmt.Println(err)
		return
	}
	fmt.Println(len(instruments), response.HTTPStatus)
}
