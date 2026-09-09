package postgrest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/supabase/supabase-go/core/configuration"
	internalHttp "github.com/supabase/supabase-go/postgrest/internal/http"
)

// The tests in this file substitute internal/http's RetrySleep seam, so they
// must not call t.Parallel: parallel tests would race on the variable.

// stubRetrySleep replaces RetrySleep for the duration of the test with an
// instantaneous recorder of the delays the exchange asked for. Like the real
// sleep, the stub reports the context's error, instead of recording, once
// the context has ended.
func stubRetrySleep(t *testing.T) *[]time.Duration {
	t.Helper()
	original := internalHttp.RetrySleep
	t.Cleanup(func() { internalHttp.RetrySleep = original })
	recorded := &[]time.Duration{}
	internalHttp.RetrySleep = func(ctx context.Context, duration time.Duration) error {
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
// that never recovers sees the initial attempt plus MaximumRetries re-sends,
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
	if got := len(*retryCounts); got != 1+internalHttp.MaximumRetries {
		t.Errorf("requests = %d, want %d", got, 1+internalHttp.MaximumRetries)
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
		if got := len(*retryCounts) - 1; got != 1+internalHttp.MaximumRetries {
			t.Errorf("requests without the option = %d, want %d", got, 1+internalHttp.MaximumRetries)
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
		if got := len(*retryCounts); got != 1+internalHttp.MaximumRetries {
			t.Errorf("requests with WithRetry(true) = %d, want %d", got, 1+internalHttp.MaximumRetries)
		}

		// The override was scoped to that call: the same read without the
		// option sends exactly once.
		if _, _, err := Collect(t.Context(), client, query); err == nil {
			t.Fatal("Collect succeeded, want an error")
		}
		if got := len(*retryCounts) - (1 + internalHttp.MaximumRetries); got != 1 {
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

// TestUpsertIsNeverRetried proves an upsert is never re-sent either: a POST
// answered with a transient 503 sees exactly one attempt even with retries
// forced on, because only GET and HEAD are retryable.
func TestUpsertIsNeverRetried(t *testing.T) {
	stubRetrySleep(t)
	server, retryCounts := scriptedServer(t, scriptedResponse{status: http.StatusServiceUnavailable})
	client := newRetryTestClient(t, server.URL)

	_, err := Execute(
		t.Context(),
		client,
		From[map[string]any]("products").Upsert(map[string]any{"sku": "X1"}),
		WithRetry(true),
	)
	if err == nil {
		t.Fatal("Execute succeeded, want an error")
	}
	if got := len(*retryCounts); got != 1 {
		t.Errorf("requests = %d, want 1 (a write is never retried, even with WithRetry(true))", got)
	}
}

// The access-token resolution and renewal tests share the RetrySleep stub, so
// like the rest of this file they must not call t.Parallel.

// recordingScriptedServer starts a test server answering each request with the
// next scripted response (repeating the last once the script is exhausted) and
// recording a clone of each request's headers. The recorder's length is the
// number of requests served, and each entry carries the Authorization and
// X-Retry-Count the request arrived with.
func recordingScriptedServer(t *testing.T, script ...scriptedResponse) (*httptest.Server, *[]http.Header) {
	t.Helper()
	var (
		mutex  sync.Mutex
		served int
	)
	headers := &[]http.Header{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		*headers = append(*headers, request.Header.Clone())
		step := script[min(served, len(script)-1)]
		served++
		if step.retryAfter != "" {
			writer.Header().Set("Retry-After", step.retryAfter)
		}
		writer.WriteHeader(step.status)
		_, _ = writer.Write([]byte(step.body))
	}))
	t.Cleanup(server.Close)
	return server, headers
}

// countingProvider returns a provider vending tokens[i] on its i-th call
// (repeating the last once exhausted) alongside a pointer to its invocation
// count. execute calls the provider on the test's own goroutine, so the count
// is read safely once Collect has returned.
func countingProvider(tokens ...string) (configuration.AccessTokenProvider, *int) {
	calls := 0
	provider := func(context.Context) (string, error) {
		token := tokens[min(calls, len(tokens)-1)]
		calls++
		return token, nil
	}
	return provider, &calls
}

// unauthorized is the scripted 401 a PostgREST token rejection takes (PGRST301,
// a JWT that could not be decoded or validated).
var unauthorized = scriptedResponse{
	status: http.StatusUnauthorized,
	body:   `{"message":"Provided JWT couldn't be decoded or it is invalid","code":"PGRST301"}`,
}

// TestExecuteRetryReusesResolvedAccessToken proves a transient retry reuses the
// resolved token rather than re-asking the provider: a 503 then 200 sees the
// provider called once and both attempts carrying the identical bearer token,
// the second tagged as a retry.
func TestExecuteRetryReusesResolvedAccessToken(t *testing.T) {
	stubRetrySleep(t)
	server, headers := recordingScriptedServer(
		t,
		scriptedResponse{status: http.StatusServiceUnavailable},
		scriptedResponse{status: http.StatusOK, body: "[]"},
	)
	provider, calls := countingProvider("token-1", "token-2")
	client := newRetryTestClient(t, server.URL).WithAccessTokenProvider(provider)

	if _, _, err := Collect(t.Context(), client, From[map[string]any]("instruments")); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if *calls != 1 {
		t.Errorf("provider calls = %d, want 1 (transient retries reuse the token)", *calls)
	}
	recorded := *headers
	if len(recorded) != 2 {
		t.Fatalf("requests = %d, want 2", len(recorded))
	}
	if got := recorded[0].Get("Authorization"); got != "Bearer token-1" {
		t.Errorf("first Authorization = %q, want %q", got, "Bearer token-1")
	}
	if got := recorded[1].Get("Authorization"); got != "Bearer token-1" {
		t.Errorf("second Authorization = %q, want %q (reused, not re-resolved)", got, "Bearer token-1")
	}
	if got := recorded[1].Get("X-Retry-Count"); got != "1" {
		t.Errorf("second X-Retry-Count = %q, want 1", got)
	}
}

// TestExecuteRenewsAccessTokenAfterRejection proves a 401 re-asks the provider
// and re-sends the renewed token at once: a 401 then 200 sees the provider
// called twice, the second request carrying the fresh token and a retry tag,
// and no backoff along the way.
func TestExecuteRenewsAccessTokenAfterRejection(t *testing.T) {
	sleeps := stubRetrySleep(t)
	server, headers := recordingScriptedServer(
		t,
		unauthorized,
		scriptedResponse{status: http.StatusOK, body: "[]"},
	)
	provider, calls := countingProvider("token-1", "token-2")
	client := newRetryTestClient(t, server.URL).WithAccessTokenProvider(provider)

	_, response, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
	}
	if *calls != 2 {
		t.Errorf("provider calls = %d, want 2 (initial plus one renewal)", *calls)
	}
	recorded := *headers
	if len(recorded) != 2 {
		t.Fatalf("requests = %d, want 2", len(recorded))
	}
	if got := recorded[0].Get("Authorization"); got != "Bearer token-1" {
		t.Errorf("first Authorization = %q, want %q", got, "Bearer token-1")
	}
	if got := recorded[1].Get("Authorization"); got != "Bearer token-2" {
		t.Errorf("second Authorization = %q, want %q (renewed)", got, "Bearer token-2")
	}
	if got := recorded[1].Get("X-Retry-Count"); got != "1" {
		t.Errorf("second X-Retry-Count = %q, want 1", got)
	}
	if len(*sleeps) != 0 {
		t.Errorf("sleeps = %v, want none (renewal does not back off)", *sleeps)
	}
}

// TestExecuteRenewalAppliesToMutations proves renewal is exempt from the
// GET/HEAD gate that bounds transient retries: an Insert (POST) answered 401
// then 201 renews and succeeds, because a 401 precedes statement execution.
func TestExecuteRenewalAppliesToMutations(t *testing.T) {
	stubRetrySleep(t)
	server, headers := recordingScriptedServer(
		t,
		unauthorized,
		scriptedResponse{status: http.StatusCreated, body: "[]"},
	)
	provider, calls := countingProvider("token-1", "token-2")
	client := newRetryTestClient(t, server.URL).WithAccessTokenProvider(provider)

	if _, err := Execute(
		t.Context(), client,
		From[map[string]any]("instruments").Insert(map[string]any{"name": "viola"}),
	); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if *calls != 2 {
		t.Errorf("provider calls = %d, want 2 (renewal applies to a POST)", *calls)
	}
	recorded := *headers
	if len(recorded) != 2 {
		t.Fatalf("requests = %d, want 2", len(recorded))
	}
	if got := recorded[1].Get("Authorization"); got != "Bearer token-2" {
		t.Errorf("second Authorization = %q, want %q", got, "Bearer token-2")
	}
}

// TestExecuteRenewalIgnoresRetryToggle proves the retry toggle governs only
// transient re-sends: with retries disabled a 401 still renews, while a 503
// surfaces at once.
func TestExecuteRenewalIgnoresRetryToggle(t *testing.T) {
	t.Run("a 401 still renews with retries disabled", func(t *testing.T) {
		stubRetrySleep(t)
		server, headers := recordingScriptedServer(
			t,
			unauthorized,
			scriptedResponse{status: http.StatusOK, body: "[]"},
		)
		provider, calls := countingProvider("token-1", "token-2")
		client := newRetryTestClient(t, server.URL, configuration.WithRetry(false)).
			WithAccessTokenProvider(provider)

		if _, _, err := Collect(t.Context(), client, From[map[string]any]("instruments")); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if *calls != 2 {
			t.Errorf("provider calls = %d, want 2 (renewal ignores the retry toggle)", *calls)
		}
		if got := len(*headers); got != 2 {
			t.Errorf("requests = %d, want 2", got)
		}
	})

	t.Run("a 503 still surfaces immediately with retries disabled", func(t *testing.T) {
		stubRetrySleep(t)
		server, headers := recordingScriptedServer(t, scriptedResponse{status: http.StatusServiceUnavailable})
		provider, calls := countingProvider("token-1")
		client := newRetryTestClient(t, server.URL, configuration.WithRetry(false)).
			WithAccessTokenProvider(provider)

		_, _, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
		var postgrestError *Error
		if !errors.As(err, &postgrestError) {
			t.Fatalf("want *Error, got %T: %v", err, err)
		}
		if postgrestError.HTTPStatus != http.StatusServiceUnavailable {
			t.Errorf("HTTPStatus = %d, want 503", postgrestError.HTTPStatus)
		}
		if got := len(*headers); got != 1 {
			t.Errorf("requests = %d, want 1 (the toggle governs transient re-sends)", got)
		}
		if *calls != 1 {
			t.Errorf("provider calls = %d, want 1 (no renewal on a non-401)", *calls)
		}
	})
}

// TestExecuteRenewalBudgetExhausted proves renewal shares the per-call cap: a
// server that always answers 401 sees the initial send plus MaximumRetries
// renewals, each bearing a distinct token, then the 401 surfaces with no
// backoff along the way.
func TestExecuteRenewalBudgetExhausted(t *testing.T) {
	sleeps := stubRetrySleep(t)
	server, headers := recordingScriptedServer(t, unauthorized)
	provider, calls := countingProvider("token-1", "token-2", "token-3", "token-4")
	client := newRetryTestClient(t, server.URL).WithAccessTokenProvider(provider)

	_, _, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
	var postgrestError *Error
	if !errors.As(err, &postgrestError) {
		t.Fatalf("want *Error, got %T: %v", err, err)
	}
	if postgrestError.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("HTTPStatus = %d, want 401", postgrestError.HTTPStatus)
	}
	if *calls != 1+internalHttp.MaximumRetries {
		t.Errorf("provider calls = %d, want %d (initial plus %d renewals)", *calls, 1+internalHttp.MaximumRetries, internalHttp.MaximumRetries)
	}
	recorded := *headers
	if len(recorded) != 1+internalHttp.MaximumRetries {
		t.Fatalf("requests = %d, want %d", len(recorded), 1+internalHttp.MaximumRetries)
	}
	for index, want := range []string{"Bearer token-1", "Bearer token-2", "Bearer token-3", "Bearer token-4"} {
		if got := recorded[index].Get("Authorization"); got != want {
			t.Errorf("request %d Authorization = %q, want %q (each renewal re-sends a distinct token)", index, got, want)
		}
	}
	if len(*sleeps) != 0 {
		t.Errorf("sleeps = %v, want none (renewal does not back off)", *sleeps)
	}
}

// TestExecuteRenewalReSendsUnchangedToken proves no token comparison guards the
// re-send: a constant provider answered 401 then 200 for the byte-identical
// token still recovers, so clock-skew and signing-key-rotation windows are not
// mistaken for a dead credential.
func TestExecuteRenewalReSendsUnchangedToken(t *testing.T) {
	stubRetrySleep(t)
	server, headers := recordingScriptedServer(
		t,
		unauthorized,
		scriptedResponse{status: http.StatusOK, body: "[]"},
	)
	provider, calls := countingProvider("token-unchanged")
	client := newRetryTestClient(t, server.URL).WithAccessTokenProvider(provider)

	if _, _, err := Collect(t.Context(), client, From[map[string]any]("instruments")); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if *calls != 2 {
		t.Errorf("provider calls = %d, want 2 (the re-send is never guarded by a token comparison)", *calls)
	}
	recorded := *headers
	if len(recorded) != 2 {
		t.Fatalf("requests = %d, want 2", len(recorded))
	}
	for index, header := range recorded {
		if got := header.Get("Authorization"); got != "Bearer token-unchanged" {
			t.Errorf("request %d Authorization = %q, want the unchanged token", index, got)
		}
	}
}

// TestExecuteRenewalProviderFailureFailsCall proves a failing renewal ends the
// call: an error is wrapped and matchable, an empty token surfaces
// ErrMissingAccessToken, and neither re-sends.
func TestExecuteRenewalProviderFailureFailsCall(t *testing.T) {
	t.Run("a renewal error fails the call wrapped", func(t *testing.T) {
		stubRetrySleep(t)
		server, headers := recordingScriptedServer(t, unauthorized)
		sentinel := errors.New("refresh failed")
		calls := 0
		provider := func(context.Context) (string, error) {
			calls++
			if calls == 1 {
				return "token-1", nil
			}
			return "", sentinel
		}
		client := newRetryTestClient(t, server.URL).WithAccessTokenProvider(provider)

		_, _, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
		if !errors.Is(err, sentinel) {
			t.Fatalf("errors.Is(err, sentinel) = false, want true: %v", err)
		}
		if !strings.Contains(err.Error(), "postgrest: resolving access token") {
			t.Errorf("error = %q, want it to mention resolving the access token", err.Error())
		}
		if got := len(*headers); got != 1 {
			t.Errorf("requests = %d, want 1 (the failing renewal precedes any re-send)", got)
		}
	})

	t.Run("a renewal empty token fails with ErrMissingAccessToken", func(t *testing.T) {
		stubRetrySleep(t)
		server, headers := recordingScriptedServer(t, unauthorized)
		calls := 0
		provider := func(context.Context) (string, error) {
			calls++
			if calls == 1 {
				return "token-1", nil
			}
			return "", nil
		}
		client := newRetryTestClient(t, server.URL).WithAccessTokenProvider(provider)

		_, _, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
		if !errors.Is(err, ErrMissingAccessToken) {
			t.Fatalf("errors.Is(err, ErrMissingAccessToken) = false, want true: %v", err)
		}
		if got := len(*headers); got != 1 {
			t.Errorf("requests = %d, want 1", got)
		}
	})
}

// TestExecuteSharedBudgetAcrossTransientAndRenewal proves transient retries and
// renewals draw on one per-call budget: a GET answered 503, 401, 503, 401 sends
// four requests total, re-asks the provider once (the renewal made while budget
// remained), backs off only for the two 503s and surfaces the final 401.
func TestExecuteSharedBudgetAcrossTransientAndRenewal(t *testing.T) {
	sleeps := stubRetrySleep(t)
	server, headers := recordingScriptedServer(
		t,
		scriptedResponse{status: http.StatusServiceUnavailable},
		unauthorized,
		scriptedResponse{status: http.StatusServiceUnavailable},
		unauthorized,
	)
	provider, calls := countingProvider("token-1", "token-2")
	client := newRetryTestClient(t, server.URL).WithAccessTokenProvider(provider)

	_, _, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
	var postgrestError *Error
	if !errors.As(err, &postgrestError) {
		t.Fatalf("want *Error, got %T: %v", err, err)
	}
	if postgrestError.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("HTTPStatus = %d, want 401", postgrestError.HTTPStatus)
	}
	recorded := *headers
	if len(recorded) != 4 {
		t.Fatalf("requests = %d, want 4 (one shared per-call budget)", len(recorded))
	}
	if *calls != 2 {
		t.Errorf("provider calls = %d, want 2 (initial plus the one renewal made while budget remained)", *calls)
	}
	if len(*sleeps) != 2 {
		t.Errorf("sleeps = %d, want 2 (only the two transient 503s back off)", len(*sleeps))
	}
	if got := recorded[2].Get("Authorization"); got != "Bearer token-2" {
		t.Errorf("third Authorization = %q, want %q (renewed mid-sequence)", got, "Bearer token-2")
	}
}

// TestExecuteNoRenewalWithoutProvider proves the renewal branch needs a
// provider-resolved token on the wire: a base client answered 401 sends exactly
// one request and surfaces the error directly.
func TestExecuteNoRenewalWithoutProvider(t *testing.T) {
	stubRetrySleep(t)
	server, headers := recordingScriptedServer(t, scriptedResponse{
		status: http.StatusUnauthorized,
		body:   `{"message":"anonymous role disabled","code":"PGRST302"}`,
	})
	client := newRetryTestClient(t, server.URL)

	_, _, err := Collect(t.Context(), client, From[map[string]any]("instruments"))
	var postgrestError *Error
	if !errors.As(err, &postgrestError) {
		t.Fatalf("want *Error, got %T: %v", err, err)
	}
	if postgrestError.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("HTTPStatus = %d, want 401", postgrestError.HTTPStatus)
	}
	if got := len(*headers); got != 1 {
		t.Errorf("requests = %d, want 1 (renewal needs a provider-resolved token on the wire)", got)
	}
}
