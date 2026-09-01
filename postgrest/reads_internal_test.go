package postgrest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/supabase/supabase-go/core/configuration"
)

// The tests in this file substitute the package-level retrySleep seam, so
// they must not call t.Parallel: parallel tests would race on the variable.

// stubRetrySleep replaces retrySleep for the duration of the test with an
// instantaneous recorder of the delays execute asked for. Like the real
// sleep, the stub reports the context's error, instead of recording, once
// the context has ended.
func stubRetrySleep(t *testing.T) *[]time.Duration {
	t.Helper()
	original := retrySleep
	t.Cleanup(func() { retrySleep = original })
	recorded := &[]time.Duration{}
	retrySleep = func(ctx context.Context, duration time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		*recorded = append(*recorded, duration)
		return nil
	}
	return recorded
}

// scriptedResponse is one step of a scriptedServer's answer sequence.
type scriptedResponse struct {
	status     int
	retryAfter string
	body       string
}

// scriptedServer starts a test server answering each request with the next
// scripted response, repeating the final one once the script is exhausted.
// It also returns a recorder of the X-Retry-Count header observed on each
// request, whose length is therefore the number of requests served.
func scriptedServer(t *testing.T, script ...scriptedResponse) (*httptest.Server, *[]string) {
	t.Helper()
	var (
		mutex  sync.Mutex
		served int
	)
	retryCounts := &[]string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		*retryCounts = append(*retryCounts, request.Header.Get("X-Retry-Count"))
		step := script[min(served, len(script)-1)]
		served++
		if step.retryAfter != "" {
			writer.Header().Set("Retry-After", step.retryAfter)
		}
		writer.WriteHeader(step.status)
		_, _ = writer.Write([]byte(step.body))
	}))
	t.Cleanup(server.Close)
	return server, retryCounts
}

// newRetryTestClient builds a client at the given test server with the given
// options.
func newRetryTestClient(t *testing.T, serverURL string, options ...configuration.Option) *Client {
	t.Helper()
	client, err := New(serverURL, "TEST_API_KEY", options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

// TestCollectRetriesOn520UntilSuccess drives the whole loop: two transient
// 520s then success, with the exponential delays and the X-Retry-Count
// tagging observable on the way through.
func TestCollectRetriesOn520UntilSuccess(t *testing.T) {
	sleeps := stubRetrySleep(t)
	server, retryCounts := scriptedServer(
		t,
		scriptedResponse{status: 520},
		scriptedResponse{status: 520},
		scriptedResponse{status: http.StatusOK, body: "[]"},
	)
	client := newRetryTestClient(t, server.URL)

	rows, response, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want %d", response.HTTPStatus, http.StatusOK)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %v, want empty", rows)
	}
	if want := []string{"", "1", "2"}; !slices.Equal(*retryCounts, want) {
		t.Errorf("X-Retry-Count sequence = %q, want %q", *retryCounts, want)
	}
	if want := []time.Duration{time.Second, 2 * time.Second}; !slices.Equal(*sleeps, want) {
		t.Errorf("delays = %v, want %v", *sleeps, want)
	}
}

// TestCollectRetriesOn503HonoringRetryAfter proves a parseable Retry-After
// header replaces the computed backoff.
func TestCollectRetriesOn503HonoringRetryAfter(t *testing.T) {
	sleeps := stubRetrySleep(t)
	server, retryCounts := scriptedServer(
		t,
		scriptedResponse{status: http.StatusServiceUnavailable, retryAfter: "7"},
		scriptedResponse{status: http.StatusOK, body: "[]"},
	)
	client := newRetryTestClient(t, server.URL)

	_, response, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want %d", response.HTTPStatus, http.StatusOK)
	}
	if got := len(*retryCounts); got != 2 {
		t.Errorf("requests = %d, want 2", got)
	}
	if want := []time.Duration{7 * time.Second}; !slices.Equal(*sleeps, want) {
		t.Errorf("delays = %v, want %v", *sleeps, want)
	}
}

// TestCollectStopsAfterMaximumRetries proves the loop gives up: a server
// that never recovers sees the initial attempt plus maximumRetries re-sends,
// and the final failure surfaces as the typed PostgREST error.
func TestCollectStopsAfterMaximumRetries(t *testing.T) {
	sleeps := stubRetrySleep(t)
	server, retryCounts := scriptedServer(t, scriptedResponse{status: 520})
	client := newRetryTestClient(t, server.URL)

	rows, response, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
	var postgrestError *Error
	if !errors.As(err, &postgrestError) {
		t.Fatalf("want *Error in the chain, got %T: %v", err, err)
	}
	if postgrestError.HTTPStatus != 520 {
		t.Errorf("HTTPStatus = %d, want 520", postgrestError.HTTPStatus)
	}
	if got := len(*retryCounts); got != 1+maximumRetries {
		t.Errorf("requests = %d, want %d", got, 1+maximumRetries)
	}
	if want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}; !slices.Equal(*sleeps, want) {
		t.Errorf("delays = %v, want %v", *sleeps, want)
	}
	if rows != nil || response.HTTPStatus != 0 {
		t.Errorf("rows, response = %v, %+v, want nil and zero", rows, response)
	}
}

// TestCollectDoesNotRetryNonRetryableStatus proves a non-transient failure
// is never re-sent.
func TestCollectDoesNotRetryNonRetryableStatus(t *testing.T) {
	sleeps := stubRetrySleep(t)
	server, retryCounts := scriptedServer(t, scriptedResponse{
		status: http.StatusBadRequest,
		body:   `{"message":"malformed filter","code":"PGRST100"}`,
	})
	client := newRetryTestClient(t, server.URL)

	_, _, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
	var postgrestError *Error
	if !errors.As(err, &postgrestError) {
		t.Fatalf("want *Error in the chain, got %T: %v", err, err)
	}
	if got := len(*retryCounts); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
	if len(*sleeps) != 0 {
		t.Errorf("delays = %v, want none", *sleeps)
	}
}

// faultyTransport fails a fixed number of round trips before delegating to
// the default transport, simulating a transiently unreachable server.
type faultyTransport struct {
	remainingFailures int
}

func (f *faultyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if f.remainingFailures > 0 {
		f.remainingFailures--
		return nil, errors.New("simulated transport failure")
	}
	return http.DefaultTransport.RoundTrip(request)
}

// TestCollectRetriesTransportError proves transport-level failures retry
// without any server response to script: two failed round trips, then the
// request reaches the server and succeeds.
func TestCollectRetriesTransportError(t *testing.T) {
	sleeps := stubRetrySleep(t)
	server, retryCounts := scriptedServer(t, scriptedResponse{status: http.StatusOK, body: "[]"})
	client := newRetryTestClient(t, server.URL,
		configuration.WithHTTPClient(&http.Client{Transport: &faultyTransport{remainingFailures: 2}}))

	_, response, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want %d", response.HTTPStatus, http.StatusOK)
	}
	// Only the third attempt reached the server, already tagged as retry 2.
	if want := []string{"2"}; !slices.Equal(*retryCounts, want) {
		t.Errorf("X-Retry-Count sequence = %q, want %q", *retryCounts, want)
	}
	if want := []time.Duration{time.Second, 2 * time.Second}; !slices.Equal(*sleeps, want) {
		t.Errorf("delays = %v, want %v", *sleeps, want)
	}
}

// TestCollectDoesNotRetryWhenDisabledByClient proves the construction-time
// switch: with WithRetry(false) a transient failure surfaces immediately.
func TestCollectDoesNotRetryWhenDisabledByClient(t *testing.T) {
	sleeps := stubRetrySleep(t)
	server, retryCounts := scriptedServer(t, scriptedResponse{status: 520})
	client := newRetryTestClient(t, server.URL, configuration.WithRetry(false))

	_, _, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
	if err == nil {
		t.Fatal("Collect succeeded, want an error")
	}
	if got := len(*retryCounts); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
	if len(*sleeps) != 0 {
		t.Errorf("delays = %v, want none", *sleeps)
	}
}

// TestCollectPerReadRetryOverridesClient proves the per-call WithRetry
// option wins over the client default in both directions and applies to
// that call alone - the same client and query without the option follow
// the client default again.
func TestCollectPerReadRetryOverridesClient(t *testing.T) {
	t.Run("opts a retrying client out", func(t *testing.T) {
		stubRetrySleep(t)
		server, retryCounts := scriptedServer(t, scriptedResponse{status: 520})
		client := newRetryTestClient(t, server.URL)
		query := From[map[string]any]("instruments")

		if _, _, err := Collect(t.Context(), client, query, WithRetry(false)); err == nil {
			t.Fatal("Collect succeeded, want an error")
		}
		if got := len(*retryCounts); got != 1 {
			t.Errorf("requests with WithRetry(false) = %d, want 1", got)
		}

		// The override was scoped to that call: the same read without the
		// option retries in full.
		if _, _, err := Collect(t.Context(), client, query); err == nil {
			t.Fatal("Collect succeeded, want an error")
		}
		if got := len(*retryCounts) - 1; got != 1+maximumRetries {
			t.Errorf("requests without the option = %d, want %d", got, 1+maximumRetries)
		}
	})

	t.Run("opts a non-retrying client in", func(t *testing.T) {
		stubRetrySleep(t)
		server, retryCounts := scriptedServer(t, scriptedResponse{status: 520})
		client := newRetryTestClient(t, server.URL, configuration.WithRetry(false))
		query := From[map[string]any]("instruments")

		if _, _, err := Collect(t.Context(), client, query, WithRetry(true)); err == nil {
			t.Fatal("Collect succeeded, want an error")
		}
		if got := len(*retryCounts); got != 1+maximumRetries {
			t.Errorf("requests with WithRetry(true) = %d, want %d", got, 1+maximumRetries)
		}

		// The override was scoped to that call: the same read without the
		// option sends exactly once.
		if _, _, err := Collect(t.Context(), client, query); err == nil {
			t.Fatal("Collect succeeded, want an error")
		}
		if got := len(*retryCounts) - (1 + maximumRetries); got != 1 {
			t.Errorf("requests without the option = %d, want 1", got)
		}
	})
}

// TestCollectAbandonsRetryWhenContextEnds proves the caller's context is the
// stop authority: once it is cancelled no retry begins. The cancellation
// races the in-flight response, so the first attempt may fail as a transport
// error or deliver its 520 - both paths must stop retrying and surface the
// cancellation.
func TestCollectAbandonsRetryWhenContextEnds(t *testing.T) {
	stubRetrySleep(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var (
		mutex    sync.Mutex
		requests int
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mutex.Lock()
		requests++
		mutex.Unlock()
		cancel()
		writer.WriteHeader(520)
	}))
	t.Cleanup(server.Close)
	client := newRetryTestClient(t, server.URL)

	_, _, err := Collect(ctx, client, From[map[string]any]("instruments"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false, want true: %v", err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if requests != 1 {
		t.Errorf("requests = %d, want 1", requests)
	}
}

// TestRetryDelay pins the delay computation: exponential doubling from one
// second, displaced by a parseable non-negative whole-seconds Retry-After.
func TestRetryDelay(t *testing.T) {
	testCases := []struct {
		name       string
		attempt    int
		retryAfter string
		want       time.Duration
	}{
		{"first backoff", 0, "", time.Second},
		{"second backoff", 1, "", 2 * time.Second},
		{"third backoff", 2, "", 4 * time.Second},
		{"retry-after replaces backoff", 0, "7", 7 * time.Second},
		{"retry-after zero", 2, "0", 0},
		{"retry-after with edge whitespace", 0, " 7 ", 7 * time.Second},
		{"negative retry-after ignored", 1, "-1", 2 * time.Second},
		{"malformed retry-after ignored", 0, "soon", time.Second},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := retryDelay(testCase.attempt, testCase.retryAfter); got != testCase.want {
				t.Errorf("retryDelay(%d, %q) = %v, want %v", testCase.attempt, testCase.retryAfter, got, testCase.want)
			}
		})
	}
}

// TestInsertIsNeverRetried proves a write is never re-sent, even with retries
// forced on: a POST answered with a transient 503 sees exactly one attempt,
// because only GET and HEAD are retryable.
func TestInsertIsNeverRetried(t *testing.T) {
	stubRetrySleep(t)
	server, retryCounts := scriptedServer(t, scriptedResponse{status: http.StatusServiceUnavailable})
	client := newRetryTestClient(t, server.URL)

	_, err := Execute(
		t.Context(),
		client,
		From[map[string]any]("instruments").Insert(map[string]any{"name": "violin"}),
		WithRetry(true),
	)
	if err == nil {
		t.Fatal("Execute succeeded, want an error")
	}
	if got := len(*retryCounts); got != 1 {
		t.Errorf("requests = %d, want 1 (a write is never retried, even with WithRetry(true))", got)
	}
}

// TestUpdateIsNeverRetried proves an update is never re-sent either: a PATCH
// answered with a transient 503 sees exactly one attempt even with retries
// forced on, because only GET and HEAD are retryable.
func TestUpdateIsNeverRetried(t *testing.T) {
	stubRetrySleep(t)
	server, retryCounts := scriptedServer(t, scriptedResponse{status: http.StatusServiceUnavailable})
	client := newRetryTestClient(t, server.URL)

	_, err := Execute(
		t.Context(),
		client,
		From[map[string]any]("instruments").Update(map[string]any{"name": "violin"}),
		WithRetry(true),
	)
	if err == nil {
		t.Fatal("Execute succeeded, want an error")
	}
	if got := len(*retryCounts); got != 1 {
		t.Errorf("requests = %d, want 1 (a write is never retried, even with WithRetry(true))", got)
	}
}

// TestDeleteIsNeverRetried proves a delete is never re-sent either: a DELETE
// answered with a transient 503 sees exactly one attempt even with retries
// forced on, because only GET and HEAD are retryable.
func TestDeleteIsNeverRetried(t *testing.T) {
	stubRetrySleep(t)
	server, retryCounts := scriptedServer(t, scriptedResponse{status: http.StatusServiceUnavailable})
	client := newRetryTestClient(t, server.URL)

	_, err := Execute(
		t.Context(),
		client,
		From[map[string]any]("instruments").Delete(),
		WithRetry(true),
	)
	if err == nil {
		t.Fatal("Execute succeeded, want an error")
	}
	if got := len(*retryCounts); got != 1 {
		t.Errorf("requests = %d, want 1 (a write is never retried, even with WithRetry(true))", got)
	}
}
