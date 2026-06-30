package core_test

import (
	"fmt"
	"net/http"
	"time"

	"github.com/supabase/supabase-go/core"
)

func ExampleNewConfiguration() {
	configuration, err := core.NewConfiguration(
		"https://project.supabase.co",
		"anon-key",
		core.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
		core.WithHeaders(map[string]string{"X-Client-Info": "supabase-go/0.1"}),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(configuration.BaseURL())
	// Output: https://project.supabase.co
}
