package postgrest_test

import (
	"fmt"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/postgrest"
)

func ExampleNew() {
	configuration, err := core.NewConfiguration("https://project.supabase.co", "anon-key")
	if err != nil {
		fmt.Println(err)
		return
	}
	client := postgrest.New(configuration)
	fmt.Println(client != nil)
	// Output: true
}
