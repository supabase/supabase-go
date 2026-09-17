// Command database-standalone queries the Database through the scoped
// postgrest module alone - no root client import - pulling only the postgrest
// and core modules into the consumer's module graph.
//
// It runs against the local Supabase stack started by
// scripts/integration-test.sh, reading SUPABASE_URL and
// SUPABASE_PUBLISHABLE_KEY from the environment.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/supabase/supabase-go/postgrest"
)

// instrument maps a row of the seeded public.instruments table.
type instrument struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	projectURL := os.Getenv("SUPABASE_URL")
	publishableKey := os.Getenv("SUPABASE_PUBLISHABLE_KEY")
	if projectURL == "" || publishableKey == "" {
		fmt.Fprintln(os.Stderr, "SUPABASE_URL and SUPABASE_PUBLISHABLE_KEY must be set - run via scripts/integration-test.sh")
		os.Exit(1)
	}

	client, err := postgrest.New(projectURL, publishableKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "postgrest.New:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	instruments, _, err := postgrest.Collect(ctx, client,
		postgrest.From[instrument]("instruments").Select("id, name"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Collect:", err)
		os.Exit(1)
	}
	for _, row := range instruments {
		fmt.Printf("%d: %s\n", row.ID, row.Name)
	}
}
