package postgrest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/supabase/supabase-go/core/configuration"
	"github.com/supabase/supabase-go/postgrest/internal/exchange"
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
//   - [ErrMissingAccessToken], when an attached access-token provider is nil
//     or resolves to an empty token - before any I/O at the call's first
//     resolution, or on a renewal re-ask after the server rejected the sent
//     token.
//   - an attached [configuration.AccessTokenProvider]'s own error, wrapped -
//     likewise at first resolution or on a renewal re-ask.
//   - a wrapped transport or decoding failure.
func Collect[Row any](ctx context.Context, client *Client, query Query[Row], options ...Option) ([]Row, Response, error) {
	return decodeQuery[Row, []Row](ctx, client, query, options...)
}

// CollectSingle executes the query through client and returns the single row
// of the result decoded into Row. It requests PostgREST's singular response
// format (an Accept header of application/vnd.pgrst.object+json), so the
// server answers with one JSON object rather than an array and refuses the
// request when the query matches zero rows or more than one, surfacing as an
// [*Error] carrying HTTP status 406 and code "PGRST116" that wraps
// [ErrNoRows] or [ErrTooManyRows]. On failure the returned Row and Response
// are their zero values. Context, decoding into Row and Options otherwise
// behave as documented on [Collect].
func CollectSingle[Row any](ctx context.Context, client *Client, query Query[Row], options ...Option) (Row, Response, error) {
	options = append(slices.Clip(options), withAccept(singularMediaType))
	row, response, err := decodeQuery[Row, Row](ctx, client, query, options...)
	return row, response, classifySingular(err)
}

// singularMediaType is the Accept media type that asks PostgREST for one
// JSON object rather than an array, and to refuse a result of any other
// cardinality with HTTP 406 and code singularityCode.
const singularMediaType = "application/vnd.pgrst.object+json"

// CollectSingleMaybe executes the query through client for a result expected
// to hold at most one row. Exactly one matching row is returned decoded into
// Row alongside true. An empty result is an ordinary outcome, not a failure:
// it yields Row's zero value alongside false with a nil error. A result of
// more than one row fails with [ErrTooManyRows]. On failure the returned
// Row, boolean and Response are their zero values. Context, decoding into
// Row and Options otherwise behave as documented on [Collect].
//
// A write - an insert, upsert, update or delete - travels the wire in
// PostgREST's singular response format, so the server counts the rows it
// affects inside the transaction and rolls back a write that affects more
// than one, which then changes nothing and fails with the server's [*Error]
// wrapping [ErrTooManyRows]. A write that affects no row is answered 406 by
// the server, which resolves to the empty result with a Response carrying
// that status and no count.
//
// A read, or a function call whatever its method, travels in the default
// array format exactly as [Collect]'s does, and its rows are counted from
// the range the Content-Range response header reports, or from the body's
// elements when the header is absent. A body that is a one-element array
// yields its element, and any other body counted as one row, such as the
// single array value a function returns, decodes whole into Row.
func CollectSingleMaybe[Row any](ctx context.Context, client *Client, query Query[Row], options ...Option) (Row, bool, Response, error) {
	requestState := query.state().request
	if isWrite(requestState) {
		return singleMaybeWrite[Row](ctx, client, requestState, options...)
	}
	return singleMaybeRead[Row](ctx, client, requestState, options...)
}

// isWrite reports whether requestState is an insert, upsert, update or delete
// on a relation: any method but GET or HEAD, on a path that is not a function
// call.
func isWrite(requestState request.Request) bool {
	if method := requestState.Method(); method == http.MethodGet || method == http.MethodHead {
		return false
	}
	path := requestState.Path()
	return len(path) == 0 || path[0] != rpcPathSegment
}

// singleMaybeWrite sends the write in the singular format and resolves the
// server's count: the object as the row, a 406 for no row as the empty result
// and a 406 for more than one row as the server's [*Error] wrapping
// [ErrTooManyRows].
func singleMaybeWrite[Row any](ctx context.Context, client *Client, requestState request.Request, options ...Option) (Row, bool, Response, error) {
	options = append(slices.Clip(options), withAccept(singularMediaType))
	row, response, err := decodeWhole[Row](ctx, client, requestState, options...)
	err = classifySingular(err)
	switch {
	case errors.Is(err, ErrNoRows):
		return row, false, Response{HTTPStatus: http.StatusNotAcceptable, Count: -1}, nil
	case err != nil:
		return row, false, Response{}, err
	}
	return row, true, response, nil
}

// singleMaybeRead sends the read in the array format and counts the rows of
// its result from the Content-Range header, or from the body without one.
func singleMaybeRead[Row any](ctx context.Context, client *Client, requestState request.Request, options ...Option) (Row, bool, Response, error) {
	var zero Row
	options = append(slices.Clip(options), withRepresentation())
	result, response, err := send(ctx, client, requestState, options...)
	if err != nil {
		return zero, false, Response{}, err
	}
	rows, err := decodeBody[[]Row](result.Body)
	if err != nil {
		return zero, false, Response{}, err
	}
	count, counted := parseContentRangeRows(result.ContentRange)
	if !counted {
		count = len(rows)
	}
	switch {
	case count == 0:
		return zero, false, response, nil
	case count > 1:
		return zero, false, Response{}, fmt.Errorf("%w (%d rows)", ErrTooManyRows, count)
	case len(rows) == 1:
		return rows[0], true, response, nil
	}
	// One row whose body is not a one-element array is a function's single
	// array value, and that value is the row.
	row, err := decodeBody[Row](result.Body)
	if err != nil {
		return zero, false, Response{}, err
	}
	return row, true, response, nil
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

// decodeQuery runs the query through the [Collect] family and decodes the
// whole response body into T: Collect asks for []Row, CollectSingle for Row
// under the singular Accept header.
func decodeQuery[Row any, T any](ctx context.Context, client *Client, query Query[Row], options ...Option) (T, Response, error) {
	return decodeWhole[T](ctx, client, query.state().request, options...)
}

// decodeWhole executes requestState and decodes the entire response body into
// T. It is the shared core of the [Collect] family and [CollectRaw], which
// differ only in the sealed accessor that supplied the request.
func decodeWhole[T any](ctx context.Context, client *Client, requestState request.Request, options ...Option) (T, Response, error) {
	options = append(slices.Clip(options), withRepresentation())
	result, response, err := send(ctx, client, requestState, options...)
	if err != nil {
		var zero T
		return zero, Response{}, err
	}
	decoded, err := decodeBody[T](result.Body)
	if err != nil {
		return decoded, Response{}, err
	}
	return decoded, response, nil
}

// decodeBody decodes body, a whole response, into T. A failure is returned
// wrapped as a decoding failure, with T's zero value.
func decodeBody[T any](body []byte) (T, error) {
	var decoded T
	if err := json.Unmarshal(body, &decoded); err != nil {
		var zero T
		return zero, fmt.Errorf("postgrest: decoding response: %w", err)
	}
	return decoded, nil
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

// send runs requestState through a per-call copy of the client's HTTP
// client, derived by the options, and returns the completed exchange
// alongside its [Response] metadata. It is the single boundary the executing
// functions share: the client and the request's path are validated here
// before any I/O, and a completed exchange's non-2xx status is shaped into
// an [*Error] here. The exchange itself - header assembly, automatic retries
// and access-token renewal - follows the contract documented on [Client].
func send(ctx context.Context, client *Client, requestState request.Request, options ...Option) (exchange.Result, Response, error) {
	if client == nil {
		return exchange.Result{}, Response{}, ErrMissingClient
	}
	if path := requestState.Path(); len(path) == 0 || slices.Contains(path, "") {
		if len(path) == 2 && path[0] == rpcPathSegment {
			return exchange.Result{}, Response{}, ErrMissingFunction
		}
		return exchange.Result{}, Response{}, ErrMissingTable
	}

	settings := readSettings{exchange: client.exchange}
	for _, option := range options {
		option(&settings)
	}

	result, err := settings.exchange.Do(ctx, requestState)
	if err != nil {
		return exchange.Result{}, Response{}, err
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return exchange.Result{}, Response{}, newError(result.StatusCode, result.Body)
	}
	return result, Response{
		HTTPStatus: result.StatusCode,
		Count:      parseContentRangeTotal(result.ContentRange),
	}, nil
}

// Option adjusts how a single read executes. The read functions apply
// options in the order they are supplied.
type Option func(*readSettings)

// readSettings carries the per-call copy of the executing client's HTTP
// client. Each Option derives a further copy of that value, so an applied
// option can never reach the client the copy was taken from.
type readSettings struct {
	exchange exchange.Client
}

// WithRetry overrides the executing client's automatic-retry default for one
// read, in either direction. A later WithRetry replaces an earlier one. The
// retry contract - which requests qualify, on which failures, with what
// backoff - is documented on [Client].
func WithRetry(enabled bool) Option {
	return func(settings *readSettings) {
		settings.exchange = settings.exchange.WithRetry(enabled)
	}
}

// WithAccessTokenProvider executes one call as a signed-in end user, resolving
// the user's access token through provider and sending it as the
// credentials of the Bearer authentication scheme on the Authorization
// header, so the database applies that user's Row Level Security policies.
// For this call alone it replaces a provider attached by
// [Client.WithAccessTokenProvider], which is then not invoked, and a later
// WithAccessTokenProvider replaces an earlier one. The project API key
// continues to travel on the apikey header. A resolved token the server
// rejects is renewed and re-sent as documented on [Client]. A nil provider
// fails the call with [ErrMissingAccessToken].
func WithAccessTokenProvider(provider configuration.AccessTokenProvider) Option {
	return func(settings *readSettings) {
		settings.exchange = settings.exchange.WithTokenResolver(tokenResolver(provider))
	}
}

// withAccept overrides the executing read request's default Accept header.
func withAccept(value string) Option {
	return func(settings *readSettings) {
		settings.exchange = settings.exchange.WithAccept(value)
	}
}

// withRepresentation makes the exchange ask for the affected rows of a write
// back, adding Prefer: return=representation when the request is not a GET or
// HEAD. The read functions set it, so a write passed to one returns its rows,
// while a read is unaffected: its GET never carries the preference and stays
// byte-identical on the wire.
func withRepresentation() Option {
	return func(settings *readSettings) {
		settings.exchange = settings.exchange.WithRepresentation()
	}
}
