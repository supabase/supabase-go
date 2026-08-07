package postgrest

import (
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/supabase/supabase-go/postgrest/internal/request"
)

// From begins a query against the given table or view.
// Chain a verb such as [QueryBuilder.Select], then pass the finished query,
// together with a [Client], to a generic read function such as [Collect].
// The returned builder is a pure value carrying only query state, so queries
// may be composed and stored anywhere - including package-level variables -
// before any Client exists.
func From[T any](table string) QueryBuilder[T] {
	return QueryBuilder[T]{request: request.New(http.MethodGet, table)}
}

// QueryBuilder represents a query scoped to one table or view, ready for a verb.
// A QueryBuilder is an immutable value - every method returns a new independent
// builder - so builders may be stored, forked into divergent chains, and used
// concurrently by multiple goroutines.
type QueryBuilder[T any] struct {
	request request.Request
}

// FilterBuilder represents a fully-specified query awaiting execution.
// A FilterBuilder is an immutable value - every method that returns a builder
// returns a new independent builder - so builders may be stored, forked into
// divergent chains, and used concurrently by multiple goroutines.
type FilterBuilder[T any] struct {
	request request.Request
}

// OrderedBuilder represents a query whose newest order column may still
// take a direction and a null placement. Descending and NullsFirst refine
// that column; every other method is that of the embedded [FilterBuilder]
// and ends the refinement.
type OrderedBuilder[T any] struct {
	FilterBuilder[T]
}

// OrderedDescendingBuilder represents a query whose newest order column
// sorts descending and may still take a null placement.
// NullsLast refines that column; every other method is that of the embedded
// [FilterBuilder] and ends the refinement.
type OrderedDescendingBuilder[T any] struct {
	FilterBuilder[T]
}

// Select performs a SELECT-style read of the given columns, returning a
// [FilterBuilder] ready to execute. Columns are comma-separated and may use
// PostgREST's renaming and embedding syntax. Whitespace is removed except
// inside double-quoted identifiers. An empty columns string selects all
// columns, exactly as "*" does.
func (q QueryBuilder[T]) Select(columns string) FilterBuilder[T] {
	return FilterBuilder[T]{request: q.request.WithParameter("select", cleanSelectColumns(columns))}
}

// Limit caps the query result at count rows, sent verbatim, so zero requests
// zero rows. When Limit is called more than once on a chain, the last call wins.
func (f FilterBuilder[T]) Limit(count int) FilterBuilder[T] {
	return FilterBuilder[T]{request: f.request.WithParameterReplacing("limit", strconv.Itoa(count))}
}

// Order sorts the result by column, ascending with nulls last unless
// refined through the returned [OrderedBuilder]. The column is sent
// verbatim as one PostgREST order term, so an invalid column is rejected by
// the server rather than rewritten. Calling Order again on the same chain
// appends a lower-precedence sort column to the same query.
func (f FilterBuilder[T]) Order(column string) OrderedBuilder[T] {
	return OrderedBuilder[T]{FilterBuilder[T]{request: f.request.WithParameterJoining("order", column)}}
}

// Descending sorts the newest order column from highest to lowest value and
// places rows holding a null in it before every non-null row, unless the
// returned builder's NullsLast says otherwise.
func (o OrderedBuilder[T]) Descending() OrderedDescendingBuilder[T] {
	return OrderedDescendingBuilder[T]{FilterBuilder[T]{request: o.request.WithParameterValueAppended("order", ".desc")}}
}

// NullsFirst places rows holding a null in the newest order column before
// every non-null row.
func (o OrderedBuilder[T]) NullsFirst() FilterBuilder[T] {
	return FilterBuilder[T]{request: o.request.WithParameterValueAppended("order", ".nullsfirst")}
}

// NullsLast places rows holding a null in the newest order column after
// every non-null row.
func (o OrderedDescendingBuilder[T]) NullsLast() FilterBuilder[T] {
	return FilterBuilder[T]{request: o.request.WithParameterValueAppended("order", ".nullslast")}
}

// cleanSelectColumns strips whitespace from a PostgREST column list except
// inside double-quoted identifiers. An empty list means all columns.
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
