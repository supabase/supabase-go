package postgrest_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/supabase/supabase-go/postgrest"
)

func ExampleNew() {
	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	fmt.Println(client != nil && err == nil)
	// Output: true
}

// ExampleClient_From demonstrates a Database read for consumers who import
// this module directly instead of the root supabase package.
func ExampleClient_From() {
	type Instrument struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	client, err := postgrest.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}

	instruments, response, err := postgrest.Collect[Instrument](
		context.Background(),
		client.
			From("instruments").
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
