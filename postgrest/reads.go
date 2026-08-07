package postgrest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

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
// such as [Collect]. Every builder state in this package satisfies it;
// nothing outside the package can, as its method is unexported.
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
// client supplies the HTTP connection and base URL, and Collect is the only
// place I/O is performed. The context governs cancellation and deadline for
// the entire request. On success it also returns a [Response] carrying the
// HTTP status and, when the server
// reported one, the total row count.
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
func Collect[Row any](ctx context.Context, client *Client, query Query[Row]) ([]Row, Response, error) {
	responseBody, response, err := execute(ctx, client, query)
	if err != nil {
		return nil, Response{}, err
	}
	var rows []Row
	if err := json.Unmarshal(responseBody, &rows); err != nil {
		return nil, Response{}, fmt.Errorf("postgrest: decoding response: %w", err)
	}
	return rows, response, nil
}

// state implements [Query].
func (f FilterBuilder[T]) state() queryState[T] {
	return queryState[T](f)
}

// execute sends the query and returns the raw response body alongside its
// [Response] metadata. It is the single I/O path shared by the generic read
// functions.
func execute[T any](ctx context.Context, client *Client, query Query[T]) ([]byte, Response, error) {
	if client == nil {
		return nil, Response{}, ErrMissingClient
	}
	requestState := query.state().request
	if requestState.Path() == "" {
		return nil, Response{}, ErrMissingTable
	}

	httpRequest, err := requestState.HTTPRequest(ctx, client.baseURL)
	if err != nil {
		return nil, Response{}, fmt.Errorf("postgrest: building request: %w", err)
	}

	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return nil, Response{}, fmt.Errorf("postgrest: executing request: %w", err)
	}
	defer func() { _ = httpResponse.Body.Close() }()

	responseBody, err := io.ReadAll(httpResponse.Body)
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
