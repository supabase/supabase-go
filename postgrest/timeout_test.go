package postgrest_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/supabase/supabase-go/core"
	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/postgrest/internal/testkit"
)

// TestCollectHonorsClientTimeout pins the construction-time request timeout
// end to end through Collect: a client built with http.Client.Timeout gives
// up on a stalled server, surfacing an error that reports itself as a
// timeout through both idiomatic detection routes. The handler blocks until
// the test finishes, so the only way Collect can return is the deadline -
// no wall-clock assertions are needed.
func TestCollectHonorsClientTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		select {
		case <-release:
		case <-request.Context().Done(): // the client hung up first, as intended
		}
	}))
	defer server.Close()
	defer close(release) // unblocks the handler before Close waits on it

	projectConfiguration, err := configuration.New(core.ModulePathPostgrest, server.URL, "TEST_API_KEY",
		configuration.WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}))
	if err != nil {
		t.Fatalf("configuration.New: %v", err)
	}

	rows, response, err := postgrest.Collect(
		t.Context(),
		postgrest.NewFromConfiguration(projectConfiguration),
		postgrest.From[instrument]("instruments"),
	)

	if err == nil {
		t.Fatal("Collect succeeded, want a timeout error")
	}
	// The documented contract: Client.Do returns a *url.Error whose Timeout
	// method reports true when the request timed out.
	var urlError *url.Error
	if !errors.As(err, &urlError) {
		t.Fatalf("want *url.Error in the chain, got %T: %v", err, err)
	}
	if !urlError.Timeout() {
		t.Errorf("Timeout() = false, want true: %v", urlError)
	}
	// The errors.Is route holds too: net/http's client-timeout error matches
	// context.DeadlineExceeded by design.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false, want true: %v", err)
	}
	testkit.AssertNoResults(t, rows, response)
}
