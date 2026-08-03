package postgrest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// Collect executes the query through client and returns every row of the
// result decoded into Row, which is typically a struct whose fields carry
// json tags. Response fields with no matching Row field are ignored, so Row
// may decode any subset of the selected columns. An empty result yields an
// empty slice. The client supplies the HTTP connection and base URL, and
// Collect is the only place I/O is performed. The context governs
// cancellation and deadline for the entire request. On success it also
// returns a [Response] carrying the HTTP status and, when the server
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
func Collect[Row any](ctx context.Context, client *Client, query FilterBuilder) ([]Row, Response, error) {
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

// execute sends the query and returns the raw response body alongside its
// [Response] metadata. It is the single I/O path shared by the generic read
// functions.
func execute(ctx context.Context, client *Client, query FilterBuilder) ([]byte, Response, error) {
	if client == nil {
		return nil, Response{}, ErrMissingClient
	}
	if query.request.Path() == "" {
		return nil, Response{}, ErrMissingTable
	}

	httpRequest, err := query.request.HTTPRequest(ctx, client.baseURL)
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
