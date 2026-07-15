package postgrest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/supabase/supabase-go/postgrest/internal/request"
)

// QueryBuilder is the result of [Client.From]: a query scoped to one table or
// view, ready for a verb. This block provides [QueryBuilder.Select]; write
// verbs arrive in later blocks. A QueryBuilder is an immutable value - every
// method returns a new independent builder - so builders may be stored,
// forked into divergent chains and used concurrently by multiple goroutines.
type QueryBuilder struct {
	client  *Client
	request request.Request
}

// FilterBuilder is the result of choosing a verb, such as
// [QueryBuilder.Select]: a fully-specified query awaiting execution. Filters
// and modifiers chain here in later blocks. Like [QueryBuilder] it is an
// immutable value, safe to fork and share across goroutines; the terminal
// [FilterBuilder.Execute] performs the request.
type FilterBuilder struct {
	client  *Client
	request request.Request
}

// Select performs a SELECT-style read of the given columns, returning a
// [FilterBuilder] ready to execute. Columns are comma-separated and may use
// PostgREST's renaming and embedding syntax. Whitespace is removed except
// inside double-quoted identifiers. An empty columns string selects all
// columns, exactly as "*" does.
//
// Select consumes the [QueryBuilder]: the returned [FilterBuilder] has no
// Select, so a query's column list is chosen exactly once, enforced at
// compile time.
func (q QueryBuilder) Select(columns string) FilterBuilder {
	return FilterBuilder{
		client:  q.client,
		request: q.request.WithParameter("select", cleanSelectColumns(columns)),
	}
}

// Execute sends the query and decodes the JSON response into destination,
// which must be a non-nil pointer, typically to a slice of structs whose
// fields carry json tags. It is the terminal of the builder chain and the
// only method that performs I/O. The context governs cancellation and
// deadline for the entire request. On success it returns a [Response]
// carrying the HTTP status and, when the server reported one, the total row
// count.
//
// When PostgREST answers with a non-2xx status, Execute returns an [*Error]
// carrying the HTTP status and the parsed error body; match it with
// [errors.As]. Transport and decoding failures are returned wrapped, with
// their underlying errors recoverable via [errors.Is] and [errors.As]. If the
// builder was created with an empty table name, Execute reports
// [ErrMissingTable] without performing any I/O. On any error the returned
// Response is the zero value.
func (f FilterBuilder) Execute(ctx context.Context, destination any) (Response, error) {
	if f.request.Path() == "" {
		return Response{}, ErrMissingTable
	}

	httpRequest, err := f.request.HTTPRequest(ctx, f.client.baseURL)
	if err != nil {
		return Response{}, fmt.Errorf("postgrest: building request: %w", err)
	}

	httpResponse, err := f.client.httpClient.Do(httpRequest)
	if err != nil {
		return Response{}, fmt.Errorf("postgrest: executing request: %w", err)
	}
	defer func() { _ = httpResponse.Body.Close() }()

	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return Response{}, fmt.Errorf("postgrest: reading response: %w", err)
	}

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return Response{}, newError(httpResponse.StatusCode, responseBody)
	}

	if err := json.Unmarshal(responseBody, destination); err != nil {
		return Response{}, fmt.Errorf("postgrest: decoding response: %w", err)
	}

	return Response{
		HTTPStatus: httpResponse.StatusCode,
		Count:      parseContentRangeTotal(httpResponse.Header.Get("Content-Range")),
	}, nil
}

// cleanSelectColumns strips whitespace from a PostgREST column list except
// inside double-quoted identifiers, mirroring the reference implementation
// shared by every Supabase SDK. An empty list means all columns.
func cleanSelectColumns(columns string) string {
	if columns == "" {
		return "*"
	}
	var cleaned strings.Builder
	quoted := false
	for _, character := range columns {
		if unicode.IsSpace(character) && !quoted {
			continue
		}
		if character == '"' {
			quoted = !quoted
		}
		cleaned.WriteRune(character)
	}
	return cleaned.String()
}
