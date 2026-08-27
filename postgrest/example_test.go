package postgrest_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
)

func ExampleNew() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	fmt.Println(client != nil && err == nil)
	// Output: true
}

// ExampleFrom demonstrates a Database read for consumers who import
// this module directly instead of the root supabase package.
func ExampleFrom() {
	type Instrument struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	instruments, response, err := postgrest.Collect(
		context.Background(),
		client,
		postgrest.
			From[Instrument]("instruments").
			Select("id, name"),
	)
	if err != nil {
		var postgrestError *postgrest.Error
		if errors.As(err, &postgrestError) {
			fmt.Println(postgrestError.Code, postgrestError.Hint)
			return
		}
		fmt.Println(err)
		return
	}
	fmt.Println(len(instruments), response.HTTPStatus)
}

// ExampleCollect_schemaDriven demonstrates the dynamic escape hatch: when row
// shapes are not known at compile time, instantiate Collect with a generic
// container instead of a named struct. A bare From reads every column, which
// suits a container that names none.
func ExampleCollect_schemaDriven() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	rows, response, err := postgrest.Collect(
		context.Background(),
		client,
		postgrest.From[map[string]any]("instruments"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, row := range rows {
		fmt.Println(row["name"])
	}
	fmt.Println(response.HTTPStatus)
}

type Instrument struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// The query is a pure value, so it may be declared once at package level,
// before any client exists, and reused across calls.
var instrumentsByName = postgrest.From[Instrument]("instruments").Select("id, name")

// ExampleFrom_packageLevel demonstrates that queries carry no client: this
// one is a package-level variable, with a client supplied only at the
// executing read function.
func ExampleFrom_packageLevel() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	instruments, response, err := postgrest.Collect[Instrument](context.Background(), client, instrumentsByName)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(instruments), response.HTTPStatus)
}

// ExampleCollect_requestTimeout demonstrates a construction-time request
// timeout influencing a read: the client abandons any request still in
// flight when its http.Client's Timeout elapses - connecting, awaiting
// headers and reading the response body all count - and Collect surfaces
// the failure as an error matching context.DeadlineExceeded, exactly as an
// expired per-request context deadline would.
func ExampleCollect_requestTimeout() {
	type Instrument struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	client, err := postgrest.New(
		"https://PROJECT_ID.supabase.co",
		"API_KEY",
		configuration.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	instruments, _, err := postgrest.Collect(
		context.Background(),
		client,
		postgrest.
			From[Instrument]("instruments").
			Select("id, name"),
	)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Println("the read did not finish within the client's timeout")
			return
		}
		fmt.Println(err)
		return
	}
	fmt.Println(len(instruments))
}
