package postgrest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/supabase/supabase-go/postgrest/internal/request"
)

// Mutation is a fully-specified write awaiting execution by [Execute], which
// applies it and reads nothing back. Because every Mutation is also a [Query],
// the same write may instead be passed to a read function such as [Collect] to
// return the rows it affects, decoded into Row. A read query is not a Mutation,
// so [Execute] accepts writes alone. Satisfying types include [InsertBuilder].
type Mutation[Row any] interface {
	Query[Row]
	// mutation seals this interface to the package's own write builders and
	// marks a request that [Execute] may send.
	mutation()
}

// Execute sends the mutation through client and returns its [Response]
// metadata, decoding no response body: the write applies and the server
// returns no rows, PostgREST's default for a write. To have the affected rows
// returned and decoded instead, pass the same mutation to a read function such
// as [Collect]. The client, context, options and failure modes behave as
// documented on [Collect], except that no rows are decoded.
func Execute[Row any](ctx context.Context, client *Client, mutation Mutation[Row], options ...Option) (Response, error) {
	_, response, err := execute[Row](ctx, client, mutation, options...)
	if err != nil {
		return Response{}, err
	}
	return response, nil
}

// Insert creates the given rows in the query's table, sent as one JSON array
// in a POST request. Each row marshals with [encoding/json], so its struct
// tags choose the columns sent, and every row must marshal to the same set of
// keys - the server rejects a ragged batch with code PGRST102. Calling Insert
// with no rows sends the empty array, left for the server to rule on.
//
// The executing function chooses what the database returns: [Execute] applies
// the insert and reads nothing back, while [Collect] and its kin return the
// created rows, with [InsertBuilder.Returning] narrowing their columns. An
// insert is never retried automatically.
func (q QueryBuilder[T]) Insert(rows ...T) InsertBuilder[T] {
	if rows == nil {
		rows = []T{}
	}
	post := q.request.WithMethod(http.MethodPost)
	body, err := json.Marshal(rows)
	if err != nil {
		return InsertBuilder[T]{request: post.WithError(fmt.Errorf("postgrest: encoding insert rows: %w", err))}
	}
	return InsertBuilder[T]{request: post.WithBody(body)}
}

// InsertBuilder represents an insert awaiting execution. Pass it to [Execute]
// to apply the insert and read nothing back, or to a read function such as
// [Collect] to have the created rows returned and decoded into T. An
// InsertBuilder is an immutable value, like every builder in this package.
type InsertBuilder[T any] struct {
	request request.Request
}

// Returning narrows the columns that a representation-returning execution,
// such as [Collect], reports for the created rows, exactly as
// [QueryBuilder.Select] projects a read: columns are comma-separated, cleaned
// of whitespace outside quoted identifiers, and an empty string means every
// column. It has no effect under [Execute], which reads nothing back. A later
// Returning replaces an earlier one.
func (i InsertBuilder[T]) Returning(columns string) InsertBuilder[T] {
	return InsertBuilder[T]{request: i.request.WithParameterReplacing("select", cleanSelectColumns(columns))}
}

// state implements [Query].
func (i InsertBuilder[T]) state() queryState[T] {
	return queryState[T](i)
}

// mutation implements [Mutation].
func (i InsertBuilder[T]) mutation() {}

// Compile-time proof that InsertBuilder satisfies both sealed interfaces:
// [Query], so a read function decodes the rows an insert returns, and
// [Mutation], so [Execute] sends it. A read builder satisfies only Query, so
// passing one to Execute is the compile error this arrangement pins.
var (
	_ Query[any]    = InsertBuilder[any]{}
	_ Mutation[any] = InsertBuilder[any]{}
)
