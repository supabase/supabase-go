package configuration_test

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
)

func ExampleNew() {
	projectConfiguration, err := configuration.New(
		core.ModulePathRoot,
		"https://PROJECT_ID.supabase.co",
		"API_KEY",
		configuration.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
		configuration.WithHeader("X-App-Version", "1.0.0+user.generated"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(projectConfiguration.BaseURL())
	// Output: https://PROJECT_ID.supabase.co
}

// ExampleNew_invalidInput tells the validation failures apart with
// errors.Is against the exported sentinels.
func ExampleNew_invalidInput() {
	_, err := configuration.New(core.ModulePathRoot, "", "API_KEY")
	fmt.Println(errors.Is(err, configuration.ErrMissingURL))

	_, err = configuration.New(core.ModulePathRoot, "ftp://example.com", "API_KEY")
	fmt.Println(errors.Is(err, configuration.ErrInvalidURL))
	// Output:
	// true
	// true
}

// ExampleWithLogger routes the SDK's log emissions to a debug-enabled text
// handler on stderr. Without WithLogger the SDK is silent.
func ExampleWithLogger() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	projectConfiguration, err := configuration.New(
		core.ModulePathRoot,
		"https://PROJECT_ID.supabase.co",
		"API_KEY",
		configuration.WithLogger(logger),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(projectConfiguration.BaseURL())
	// Output: https://PROJECT_ID.supabase.co
}
