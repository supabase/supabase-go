package supabase_test

import (
	"errors"
	"fmt"

	supabase "github.com/supabase/supabase-go"
	"github.com/supabase/supabase-go/core"
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

// ExampleNewClient_invalidInput shows that NewClient surfaces the core package's
// sentinel errors unchanged, so a caller can match them with errors.Is.
func ExampleNewClient_invalidInput() {
	_, err := supabase.NewClient("https://project.supabase.co", "")
	fmt.Println(errors.Is(err, core.ErrMissingKey))
	// Output: true
}
