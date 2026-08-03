package postgrest

import (
	"net/http"
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

// Select performs a SELECT-style read of the given columns, returning a
// [FilterBuilder] ready to execute. Columns are comma-separated and may use
// PostgREST's renaming and embedding syntax. Whitespace is removed except
// inside double-quoted identifiers. An empty columns string selects all
// columns, exactly as "*" does.
func (q QueryBuilder[T]) Select(columns string) FilterBuilder[T] {
	return FilterBuilder[T]{request: q.request.WithParameter("select", cleanSelectColumns(columns))}
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
