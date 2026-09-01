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
// so [Execute] accepts writes alone. [MutationBuilder] is the satisfying type.
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
// created rows, with [MutationBuilder.Returning] narrowing their columns. An
// insert is never retried automatically.
func (q QueryBuilder[T]) Insert(rows ...T) MutationBuilder[T] {
	if rows == nil {
		rows = []T{}
	}
	post := q.request.WithMethod(http.MethodPost)
	body, err := json.Marshal(rows)
	if err != nil {
		return MutationBuilder[T]{request: post.WithError(fmt.Errorf("postgrest: encoding insert rows: %w", err))}
	}
	return MutationBuilder[T]{request: post.WithBody(body)}
}

// Update changes columns on every row the preceding filters chose, sent as a
// PATCH request: the row-choosing filters chain first, exactly as they do on
// a read, and the verb ends the chain. Called with no filters, directly on
// [From]'s builder, it updates every row of the table. The changes value
// marshals with [encoding/json] to one JSON object of column assignments: a
// map[string]any is the recommended shape, where a key carrying nil clears
// that column to SQL null and an absent key leaves the column untouched. A
// struct works too, with the caution that every marshaled field is assigned,
// its zero value included.
//
// The executing function chooses what the database returns, exactly as it does
// for an insert: [Execute] applies the update and reads nothing back, while
// [Collect] and its kin return the affected rows, with
// [MutationBuilder.Returning] narrowing their columns. An update is never
// retried automatically.
func (f FilterBuilder[T]) Update(changes any) MutationBuilder[T] {
	patch := f.request.WithMethod(http.MethodPatch)
	body, err := json.Marshal(changes)
	if err != nil {
		return MutationBuilder[T]{request: patch.WithError(fmt.Errorf("postgrest: encoding update changes: %w", err))}
	}
	return MutationBuilder[T]{request: patch.WithBody(body)}
}

// Delete removes every row the preceding filters chose, sent as a DELETE
// request with no body: the row-choosing filters chain first, exactly as they
// do on a read, and the verb ends the chain. Called with no filters, directly
// on [From]'s builder, it removes every row of the table.
//
// The executing function chooses what the database returns, exactly as it does
// for an update: [Execute] applies the delete and reads nothing back, while
// [Collect] and its kin return the removed rows, with
// [MutationBuilder.Returning] narrowing their columns. A delete is never
// retried automatically.
func (f FilterBuilder[T]) Delete() MutationBuilder[T] {
	return MutationBuilder[T]{request: f.request.WithMethod(http.MethodDelete)}
}

// MutationBuilder represents a write awaiting execution.
// Pass it to [Execute] to apply the write and read nothing back, or to a read
// function such as [Collect] to have the affected rows returned.
// A MutationBuilder is an immutable value.
type MutationBuilder[T any] struct {
	request request.Request
}

// Returning narrows the columns that a representation-returning execution,
// such as [Collect], reports for the affected rows, exactly as
// [QueryBuilder.Select] projects a read: columns are comma-separated, cleaned
// of whitespace outside quoted identifiers, and an empty string means every
// column. It has no effect under [Execute], which reads nothing back. A later
// Returning replaces an earlier one, along with any projection a
// [QueryBuilder.Select] wrote earlier in the chain.
func (m MutationBuilder[T]) Returning(columns string) MutationBuilder[T] {
	return MutationBuilder[T]{request: m.request.WithParameterReplacing("select", cleanSelectColumns(columns))}
}

// state implements [Query].
func (m MutationBuilder[T]) state() queryState[T] {
	return queryState[T](m)
}

// mutation implements [Mutation].
func (m MutationBuilder[T]) mutation() {}

// Compile-time proof that MutationBuilder satisfies both sealed interfaces:
// [Query], so a read function decodes the rows a write returns, and
// [Mutation], so [Execute] sends it. A read builder satisfies only Query, so
// passing one to Execute is the compile error this arrangement pins.
var (
	_ Query[any]    = MutationBuilder[any]{}
	_ Mutation[any] = MutationBuilder[any]{}
)
