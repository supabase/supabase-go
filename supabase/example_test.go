package supabase_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/supabase"
)

func ExampleNew() {
	supabase, err := supabase.New("https://PROJECT_ID.supabase.co", "API_KEY")
	fmt.Println(supabase != nil && err == nil)
	// Output: true
}

// ExampleNew_options customizes the client with the shared functional
// options from the configuration package: a caller-supplied HTTP client and
// global headers sent on every request.
func ExampleNew_options() {
	supabase, err := supabase.New(
		"https://PROJECT_ID.supabase.co",
		"API_KEY",
		configuration.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
		configuration.WithHeader("X-App-Version", "1.0.0+user.generated"),
	)
	fmt.Println(supabase != nil && err == nil)
	// Output: true
}

// ExampleNew_invalidInput shows that supabase.New surfaces the configuration
// package's sentinel errors unchanged, so a caller can match them with
// errors.Is.
func ExampleNew_invalidInput() {
	_, err := supabase.New("https://PROJECT_ID.supabase.co", "")
	fmt.Println(errors.Is(err, configuration.ErrMissingKey))
	// Output: true
}

// ExampleClient_Database_collect runs a basic Database query for zero or more rows.
// It requires a reachable Supabase project, so it is compiled but not run by go test.
func ExampleClient_Database_collect() {
	type Instrument struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	supabase, err := supabase.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	instruments, response, err := postgrest.Collect(
		context.Background(),
		supabase.Database(),
		postgrest.
			From[Instrument]("instruments").
			Select("id, name"),
	)
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

// ExampleClient_Database_collectSingle runs a basic Database query for a single row.
// It requires a reachable Supabase project, so it is compiled but not run by go test.
func ExampleClient_Database_collectSingle() {
	type Instrument struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	supabase, err := supabase.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	instrument, response, err := postgrest.CollectSingle(
		context.Background(),
		supabase.Database(),
		postgrest.
			From[Instrument]("instruments").
			Select("name").
			Eq("id", 1234),
	)
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
	fmt.Println(instrument, response.HTTPStatus)
}

// ExampleClient_Auth verifies an inbound end-user access token and then
// queries the Database as that user, the composed backend flow: Row Level
// Security confines the result to the verified user's own rows.
// It requires a reachable Supabase project, so it is compiled but not run by go test.
func ExampleClient_Auth() {
	type PracticeLog struct {
		UserID string `json:"user_id"`
		Piece  string `json:"piece"`
	}

	supabase, err := supabase.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	// The token arrives on an inbound request, for example from its
	// Authorization: Bearer header.
	token := "INBOUND_USER_JWT"

	claims, _, _, err := supabase.Auth().GetClaims(context.Background(), token)
	if err != nil {
		fmt.Println(err)
		return
	}

	userDatabase := supabase.Database().WithAccessTokenProvider(
		func(context.Context) (string, error) { return token, nil },
	)
	logs, _, err := postgrest.Collect(
		context.Background(),
		userDatabase,
		postgrest.From[PracticeLog]("practice_logs"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(claims.Subject(), len(logs))
}
