package configuration_test

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
)

// ExampleWithLogger routes the SDK's log emissions to a debug-enabled text
// handler on stderr. Without WithLogger the SDK is silent.
func ExampleWithLogger() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	projectConfiguration, err := configuration.New(
		core.ModulePathRoot,
		"https://PROJECT_ID.supabase.co",
		"sb_publishable_...",
		configuration.WithLogger(logger),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(projectConfiguration.BaseURL())
	// Output: https://PROJECT_ID.supabase.co
}
