package postgrest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/supabase/supabase-go/postgrest/internal/request"
)

// queryState carries a query's accumulated request through the sealed
// [Query] interface. Its type parameter binds the row type the query
// decodes into, keeping Query[A] and Query[B] distinct interface types and
// Row inferable at read-function call sites.
type queryState[Row any] struct {
	request request.Request
}

// Query is a fully-specified query awaiting execution by a read function
// such as [Collect]. Satisfying types include [QueryBuilder], [FilterBuilder],
// [OrderedFilterBuilder] and [OrderedDescendingFilterBuilder].
type Query[Row any] interface {
	// state returns the query's accumulated request, bound to its row type.
	state() queryState[Row]
}

// Collect executes the query through client and returns every row of the
// result decoded into Row, which is typically a struct whose fields carry
// json tags. Response fields with no matching Row field are ignored, so Row
// may decode any subset of the selected columns. Row may also be a dynamic
// container such as map[string]any or [json.RawMessage] when column shapes
// are not known at compile time. An empty result yields an empty slice. The
// client supplies the HTTP connection and base URL, and the read functions
// are the only place I/O is performed. The context governs cancellation and
// deadline for the entire request. Options adjust how the read executes:
// [WithRetry] overrides the client's automatic-retry default for this call
// alone. On success it also returns a [Response] carrying the HTTP status
// and, when the server reported one, the total row count.
//
// On failure the returned slice is nil, the Response is the zero value, and
// the error is one of:
//   - an [*Error], when PostgREST answers with a non-2xx status, carrying the
//     HTTP status and the parsed error body.
//   - [ErrMissingClient], when client is nil. No I/O is performed in this
//     case.
//   - [ErrMissingTable], when the builder was created with an empty table
//     name. No I/O is performed in this case.
//   - a wrapped transport or decoding failure.
func Collect[Row any](ctx context.Context, client *Client, query Query[Row], options ...Option) ([]Row, Response, error) {
	return collect[Row, []Row](ctx, client, query, options...)
}

// CollectSingle executes the query through client and returns the single row
// of the result decoded into Row. It requests PostgREST's singular response
// format (an Accept header of application/vnd.pgrst.object+json), so the
// server answers with one JSON object rather than an array and refuses the
// request when the query matches zero rows or more than one, surfacing as an
// [*Error] carrying HTTP status 406 and code "PGRST116". On failure the
// returned Row and Response are their zero values. Context, decoding into
// Row and Options otherwise behave as documented on [Collect].
func CollectSingle[Row any](ctx context.Context, client *Client, query Query[Row], options ...Option) (Row, Response, error) {
	options = append(
		slices.Clip(options),
		withAccept("application/vnd.pgrst.object+json"),
	)

	return collect[Row, Row](ctx, client, query, options...)
}

func collect[Row any, T any](ctx context.Context, client *Client, query Query[Row], options ...Option) (T, Response, error) {
	responseBody, response, err := execute(ctx, client, query, options...)
	var decoded T

	if err != nil {
		return decoded, Response{}, err
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return decoded, Response{}, fmt.Errorf("postgrest: decoding response: %w", err)
	}
	return decoded, response, nil
}

// state implements [Query].
func (f FilterBuilder[T]) state() queryState[T] {
	return queryState[T](f)
}

// Compile-time proof that every builder state in this package satisfies
// [Query]. If the interface or a builder's embedding drifts so that one of
// these no longer holds, the build fails here rather than at a distant
// [Collect] call site.
var (
	_ Query[any] = QueryBuilder[any]{}
	_ Query[any] = FilterBuilder[any]{}
	_ Query[any] = OrderedFilterBuilder[any]{}
	_ Query[any] = OrderedDescendingFilterBuilder[any]{}
)

// execute sends the query and returns the raw response body alongside its
// [Response] metadata. It is the single I/O path shared by the generic read
// functions, re-sending retryable failures per the automatic-retry contract
// documented on [Client].
func execute[T any](ctx context.Context, client *Client, query Query[T], options ...Option) ([]byte, Response, error) {
	if client == nil {
		return nil, Response{}, ErrMissingClient
	}
	requestState := query.state().request
	if path := requestState.Path(); len(path) == 0 || slices.Contains(path, "") {
		return nil, Response{}, ErrMissingTable
	}

	var settings readSettings
	for _, option := range options {
		option(&settings)
	}

	retry := client.retry
	switch settings.retry {
	case retryEnabled:
		retry = true
	case retryDisabled:
		retry = false
	}
	retryable := retry && retryableMethods[requestState.Method()]

	for attempt := 0; ; attempt++ {
		httpRequest, err := requestState.HTTPRequest(ctx, client.baseURL)
		if err != nil {
			return nil, Response{}, fmt.Errorf("postgrest: building request: %w", err)
		}

		if settings.acceptHeaderValue == "" {
			httpRequest.Header.Set("Accept", "application/json")
		} else {
			httpRequest.Header.Set("Accept", settings.acceptHeaderValue)
		}

		if attempt > 0 {
			httpRequest.Header.Set("X-Retry-Count", strconv.Itoa(attempt))
		}

		httpResponse, err := client.httpClient.Do(httpRequest)
		if err != nil {
			if retryable && attempt < maximumRetries && ctx.Err() == nil &&
				retrySleep(ctx, retryDelay(attempt, "")) == nil {
				continue
			}
			return nil, Response{}, fmt.Errorf("postgrest: executing request: %w", err)
		}

		if retryable && attempt < maximumRetries && retryableStatusCodes[httpResponse.StatusCode] {
			delay := retryDelay(attempt, httpResponse.Header.Get("Retry-After"))
			_, _ = io.Copy(io.Discard, httpResponse.Body)
			_ = httpResponse.Body.Close()
			if retrySleep(ctx, delay) == nil {
				continue
			}
			return nil, Response{}, fmt.Errorf("postgrest: executing request: %w", ctx.Err())
		}

		responseBody, err := io.ReadAll(httpResponse.Body)
		_ = httpResponse.Body.Close()
		if err != nil {
			return nil, Response{}, fmt.Errorf("postgrest: reading response: %w", err)
		}

		if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
			return nil, Response{}, newError(httpResponse.StatusCode, responseBody)
		}

		return responseBody, Response{
			HTTPStatus: httpResponse.StatusCode,
			Count:      parseContentRangeTotal(httpResponse.Header.Get("Content-Range")),
		}, nil
	}
}

// Option adjusts how a single read executes. The read functions apply
// options in the order they are supplied.
type Option func(*readSettings)

// readSettings collects the execution adjustments carried by a read call's
// Options.
type readSettings struct {
	retry             retryPolicy
	acceptHeaderValue string
}

// retryPolicy is a read call's automatic-retry override. The zero value
// leaves the executing client's own default in force.
type retryPolicy int

const (
	// retryEnabled requires automatic retries for this read.
	retryEnabled retryPolicy = iota + 1
	// retryDisabled forbids automatic retries for this read.
	retryDisabled
)

// WithRetry overrides the executing client's automatic-retry default for one
// read, in either direction. A later WithRetry replaces an earlier one. The
// retry contract - which requests qualify, on which failures, with what
// backoff - is documented on [Client].
func WithRetry(enabled bool) Option {
	return func(settings *readSettings) {
		if enabled {
			settings.retry = retryEnabled
		} else {
			settings.retry = retryDisabled
		}
	}
}

// withAccept overrides the executing read request's default Accept header.
func withAccept(value string) Option {
	return func(settings *readSettings) {
		settings.acceptHeaderValue = value
	}
}

// maximumRetries is how many times one query is re-sent after its first
// attempt fails in a retryable way.
const maximumRetries = 3

// retryableMethods holds the HTTP methods whose requests are safe to repeat
// and so may be retried automatically.
var retryableMethods = map[string]bool{
	http.MethodGet:  true,
	http.MethodHead: true,
}

// retryableStatusCodes holds the response statuses treated as transient:
// 503, sent while the service cannot reach or is rebuilding its view of the
// database, and 520, sent by fronting infrastructure for a transient origin
// failure.
var retryableStatusCodes = map[int]bool{
	http.StatusServiceUnavailable: true,
	520:                           true,
}

// retryDelay returns the wait before the retry that follows the zero-based
// attempt: one second doubled per attempt, or the whole seconds requested by
// a parseable non-negative retryAfterHeader.
func retryDelay(attempt int, retryAfterHeader string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfterHeader)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Second << attempt
}

// retrySleep pauses for the given duration, returning early with ctx's error
// when ctx ends first and nil after a full pause. It is a variable so tests
// substitute an instantaneous recorder.
var retrySleep = func(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
