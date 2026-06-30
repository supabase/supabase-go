package supabase_test

import (
	"fmt"

	supabase "github.com/supabase/supabase-go"
)

func ExampleNewClient() {
	client, err := supabase.NewClient("https://project.supabase.co", "anon-key")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(client != nil)
	// Output: true
}
