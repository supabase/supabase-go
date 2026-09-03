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

// RawQuery is a fully-specified request whose entire response body decodes
// into Row through [CollectRaw], without the per-row array unwrapping the
// [Collect] family performs. Every [Query] is a RawQuery, and so is the
// scalar-returning Value shape of a function call declared through [RPC].
type RawQuery[Row any] interface {
	// rawState returns the accumulated request, bound to its decode type.
	rawState() queryState[Row]
}

// Query is a fully-specified request awaiting execution by a read function
// such as [Collect], which decodes the rows it returns into Row. Every read
// builder satisfies it, and so does every mutation builder, since executing a
// write through a read function returns the rows it affects. Satisfying types
// include [QueryBuilder], [FilterBuilder], [OrderedFilterBuilder],
// [OrderedDescendingFilterBuilder] and [MutationBuilder].
type Query[Row any] interface {
	RawQuery[Row]
	// state returns the accumulated request, bound to its row type.
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

// CollectSingleMaybe executes the query through client for a result expected
// to hold at most one row. Exactly one matching row is returned decoded into
// Row alongside true. An empty result is an ordinary outcome, not a failure:
// it yields Row's zero value alongside false with a nil error. In both cases
// the [Response] carries the request's metadata. The query travels the wire
// in PostgREST's default array format, exactly as [Collect]'s does, and
// cardinality is enforced client-side: a result of more than one row fails
// with [ErrTooManyRows]. On failure the returned Row, boolean and Response
// are their zero values. Context, decoding into Row and Options otherwise
// behave as documented on [Collect].
func CollectSingleMaybe[Row any](ctx context.Context, client *Client, query Query[Row], options ...Option) (Row, bool, Response, error) {
	var zero Row
	rows, response, err := Collect(ctx, client, query, options...)
	if err != nil {
		return zero, false, Response{}, err
	}
	switch len(rows) {
	case 0:
		return zero, false, response, nil
	case 1:
		return rows[0], true, response, nil
	default:
		return zero, false, Response{}, ErrTooManyRows
	}
}

// CollectRaw executes the query through client and decodes the entire response
// body into Row, skipping the per-row array unwrapping the [Collect] family
// performs. It is the decode path for the scalar-returning Value shape of a
// function called through [RPC], whose body is one bare JSON value:
// CollectRaw[int] over such a function returning 3 yields 3. It equally reads
// any other whole-body shape - an object, or an array into a slice. Client,
// context, Options and failure modes behave as documented on [Collect], except
// the body is decoded whole rather than as a row array. On failure the returned
// Row and Response are their zero values.
func CollectRaw[Row any](ctx context.Context, client *Client, query RawQuery[Row], options ...Option) (Row, Response, error) {
	return decodeWhole[Row](ctx, client, query.rawState().request, options...)
}

// collect runs the query through the [Collect] family and decodes the whole
// response body into T: Collect asks for []Row, CollectSingle for Row under the
// singular Accept header.
func collect[Row any, T any](ctx context.Context, client *Client, query Query[Row], options ...Option) (T, Response, error) {
	return decodeWhole[T](ctx, client, query.state().request, options...)
}

// decodeWhole executes requestState and decodes the entire response body into
// T. It is the shared core of the [Collect] family and [CollectRaw], which
// differ only in the sealed accessor that supplied the request.
func decodeWhole[T any](ctx context.Context, client *Client, requestState request.Request, options ...Option) (T, Response, error) {
	options = append(slices.Clip(options), withRepresentation())
	responseBody, response, err := execute(ctx, client, requestState, options...)
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

// rawState implements [RawQuery].
func (f FilterBuilder[T]) rawState() queryState[T] {
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

// execute sends requestState and returns the raw response body alongside its
// [Response] metadata. It is the single I/O path shared by the executing
// functions, re-sending retryable failures per the automatic-retry contract
// documented on [Client].
func execute(ctx context.Context, client *Client, requestState request.Request, options ...Option) ([]byte, Response, error) {
	if client == nil {
		return nil, Response{}, ErrMissingClient
	}
	if path := requestState.Path(); len(path) == 0 || slices.Contains(path, "") {
		if len(path) == 2 && path[0] == rpcPathSegment {
			return nil, Response{}, ErrMissingFunction
		}
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

		// The Prefer header is composed here, the single place it is set: the
		// builder's own preference (an upsert's resolution) followed by the
		// representation this execution asks for. Appending keeps both as
		// separate field-lines the server reads as one comma-separated list per
		// RFC 7240.
		if preference := requestState.Preference(); preference != "" {
			httpRequest.Header.Add("Prefer", preference)
		}
		if settings.requestRepresentation &&
			httpRequest.Method != http.MethodGet && httpRequest.Method != http.MethodHead {
			httpRequest.Header.Add("Prefer", "return=representation")
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
	retry                 retryPolicy
	acceptHeaderValue     string
	requestRepresentation bool
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

// withRepresentation makes execute ask for the affected rows of a write back,
// adding Prefer: return=representation when the request is not a GET or HEAD.
// The read functions set it, so a write passed to one returns its rows, while
// a read is unaffected: its GET never carries the preference and stays
// byte-identical on the wire.
func withRepresentation() Option {
	return func(settings *readSettings) {
		settings.requestRepresentation = true
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
