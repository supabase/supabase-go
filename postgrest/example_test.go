package postgrest_test

import (
	"fmt"

	"github.com/supabase/supabase-go/configuration"
	"github.com/supabase/supabase-go/postgrest"
)

func ExampleNew() {
	projectConfiguration, err := configuration.New("https://PROJECT_ID.supabase.co", "API_KEY")
	if err != nil {
		fmt.Println(err)
		return
	}
	client := postgrest.New(projectConfiguration)
	fmt.Println(client != nil)
	// Output: true
}
