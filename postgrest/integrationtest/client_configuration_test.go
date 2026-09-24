package integrationtest

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/postgrest/internal/testkit"
)

// TestRequestTimeoutCancelsInFlightRequest proves the construction-time
// request timeout against real PostgREST: reading the slow_instruments view
// (a two-second server-side sleep) with a 250ms http.Client.Timeout fails
// fast with a timeout error. The elapsed bound is what shows the request was
// canceled in flight rather than run to completion. Automatic retries are
// disabled because a per-attempt timeout is a retryable transport failure -
// left on, the read would lawfully take four attempts plus backoff.
func TestRequestTimeoutCancelsInFlightRequest(t *testing.T) {
	client := newIntegrationClient(t,
		configuration.WithHTTPClient(&http.Client{Timeout: 250 * time.Millisecond}),
		configuration.WithRetry(false))

	started := time.Now()
	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.From[seededInstrument]("slow_instruments"),
	)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("Collect succeeded, want a timeout error")
	}
	var urlError *url.Error
	if !errors.As(err, &urlError) {
		t.Fatalf("want *url.Error in the chain, got %T: %v", err, err)
	}
	if !urlError.Timeout() {
		t.Errorf("Timeout() = false, want true: %v", urlError)
	}
	testkit.AssertNoResults(t, rows, response)
	if elapsed >= time.Second {
		t.Errorf("Collect returned after %v, want well under the view's 2s sleep (canceled in flight)", elapsed)
	}
}

// TestRequestTimeoutGenerousAllowsCompletion is the control: a configured
// timeout that is not hit leaves the happy path untouched.
func TestRequestTimeoutGenerousAllowsCompletion(t *testing.T) {
	client := newIntegrationClient(t,
		configuration.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}))

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.From[seededInstrument]("instruments"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	testkit.AssertOKResponse(t, response)
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3 (seed drifted?)", len(rows))
	}
}

// TestContextDeadlineWinsOverClientTimeout live-proves that the per-request
// context and the construction-time client timeout merge the Go-native way:
// whichever deadline fires first cancels. The client would allow 30 seconds,
// the context 250 milliseconds - the read of the slow view must fail fast.
// The elapsed bound does the discrimination, because a client-timeout error
// also matches context.DeadlineExceeded.
func TestContextDeadlineWinsOverClientTimeout(t *testing.T) {
	client := newIntegrationClient(t,
		configuration.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}))

	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()

	started := time.Now()
	rows, response, err := postgrest.Collect(
		ctx,
		client,
		postgrest.From[seededInstrument]("slow_instruments"),
	)
	elapsed := time.Since(started)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(err, context.DeadlineExceeded) = false, want true: %v", err)
	}
	testkit.AssertNoResults(t, rows, response)
	if elapsed >= time.Second {
		t.Errorf("Collect returned after %v, want well under the view's 2s sleep and the client's 30s allowance", elapsed)
	}
}

// TestRetryDisabledReadSucceeds proves the client-level retry switch leaves
// the happy path untouched against real PostgREST. A healthy stack cannot
// emit the transient failures that would exercise the loop itself - that is
// covered hermetically by the postgrest package's unit tests.
func TestRetryDisabledReadSucceeds(t *testing.T) {
	client := newIntegrationClient(t, configuration.WithRetry(false))

	rows, response, err := postgrest.Collect(
		t.Context(),
		client,
		postgrest.From[seededInstrument]("instruments"),
	)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	testkit.AssertOKResponse(t, response)
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3 (seed drifted?)", len(rows))
	}
}

// TestPerReadRetryOverrideSucceeds proves the per-read override leaves live
// reads untouched in both directions, composing with a full query chain.
func TestPerReadRetryOverrideSucceeds(t *testing.T) {
	client := newIntegrationClient(t)

	t.Run("opt in", func(t *testing.T) {
		rows, response, err := postgrest.Collect(
			t.Context(),
			client,
			postgrest.From[seededInstrument]("instruments"),
			postgrest.WithRetry(true),
		)
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		testkit.AssertOKResponse(t, response)
		if len(rows) != 3 {
			t.Fatalf("row count = %d, want 3 (seed drifted?)", len(rows))
		}
	})

	t.Run("opt out on a chained query", func(t *testing.T) {
		rows, response, err := postgrest.Collect(
			t.Context(),
			client,
			postgrest.
				From[seededInstrument]("instruments").
				Select("id, name").
				Order("name").
				Limit(2),
			postgrest.WithRetry(false),
		)
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		testkit.AssertOKResponse(t, response)
		if len(rows) != 2 {
			t.Fatalf("row count = %d, want 2 (limited)", len(rows))
		}
	})
}
