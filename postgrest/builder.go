package postgrest

import (
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/supabase/supabase-go/postgrest/internal/request"
)

// From begins a query against the given table or view.
// The returned builder is already a complete query for every column: pass
// it, together with a [Client], to a generic read function such as
// [Collect], or narrow it first ([QueryBuilder.Select] projects columns).
// The returned builder is a pure value carrying only query state, so queries
// may be composed and stored anywhere - including package-level variables -
// before any Client exists.
func From[T any](table string) QueryBuilder[T] {
	return QueryBuilder[T]{FilterBuilder[T]{request: request.New(http.MethodGet, table)}}
}

// QueryBuilder represents a read of every column of one table or view whose
// projection may still narrow. Select projects columns; every other method
// is that of the embedded [FilterBuilder] and ends the projection window.
// A QueryBuilder is an immutable value - every method returns a new independent
// builder - so builders may be stored, forked into divergent chains, and used
// concurrently by multiple goroutines. It satisfies [Query] and may be
// passed to a read function such as [Collect].
type QueryBuilder[T any] struct {
	FilterBuilder[T]
}

// FilterBuilder represents a fully-specified query awaiting execution.
// A FilterBuilder is an immutable value - every method that returns a builder
// returns a new independent builder - so builders may be stored, forked into
// divergent chains, and used concurrently by multiple goroutines. It satisfies
// [Query] and may be passed to a read function such as [Collect].
//
// Filter methods narrow which rows the query returns and are named for the
// PostgREST operators they send. Filters chained onto one builder must all
// be satisfied.
// A filter value typed any renders as text by its Go type:
//   - string: sent verbatim
//   - []byte: bytea hex format (\x followed by two lowercase hex digits per byte)
//   - bool: true or false
//   - integer types: decimal digits
//   - float32 and float64: the shortest decimal text that round-trips,
//     with the special values as PostgreSQL's canonical Infinity,
//     -Infinity and NaN
//   - [time.Time]: RFC 3339 with up to nanosecond precision
//   - nil: null
//   - [Range]: its PostgreSQL range literal, such as [2,7) or empty
//   - [fmt.Stringer]: what String returns
//   - anything else: the fmt package's %v rendering
//
// Methods rendering several values into one list ([FilterBuilder.In],
// [FilterBuilder.ContainsAll] and their kin) also double-quote every element
// that is empty, carries edge whitespace or contains list structure - a
// comma, parenthesis, brace, double quote or backslash - escaping double
// quotes and backslashes within as \" and \\. Other elements travel bare,
// and no elements at all render an empty list, sent verbatim for the server
// to rule on.
type FilterBuilder[T any] struct {
	request request.Request
}

// OrderedFilterBuilder represents a query whose newest order column may still
// take a direction and a null placement. Descending and NullsFirst refine
// that column; every other method is that of the embedded [FilterBuilder]
// and ends the refinement. An OrderedFilterBuilder satisfies [Query] and
// may be passed to a read function such as [Collect].
type OrderedFilterBuilder[T any] struct {
	FilterBuilder[T]
}

// OrderedDescendingFilterBuilder represents a query whose newest order column
// sorts descending and may still take a null placement.
// NullsLast refines that column; every other method is that of the embedded
// [FilterBuilder] and ends the refinement. An OrderedDescendingFilterBuilder
// satisfies [Query] and may be passed to a read function such as [Collect].
type OrderedDescendingFilterBuilder[T any] struct {
	FilterBuilder[T]
}

// Select performs a SELECT-style read of the given columns, returning a
// [FilterBuilder] ready to execute. Columns are comma-separated and may use
// PostgREST's renaming and embedding syntax. Whitespace is removed except
// inside double-quoted identifiers. An empty columns string selects all
// columns, exactly as "*" does - though a query reading every column needs
// no Select at all, since [From] alone is already that query (implicit
// default of the PostgREST service).
func (q QueryBuilder[T]) Select(columns string) FilterBuilder[T] {
	return FilterBuilder[T]{request: q.request.WithParameter("select", cleanSelectColumns(columns))}
}

// Range narrows the query result to the rows at zero-based positions from
// through to, inclusive at both ends: Range(0, 9) requests the first ten
// rows and Range(2, 2) just the third. Positions are counted over the
// query's ordering, so chain Range after [FilterBuilder.Order] when the
// window must be deterministic. The window is sent as a start offset of
// from and a row cap of to-from+1, computed verbatim: Range(2, 1) requests
// zero rows, and any smaller to requests a negative cap the server rejects.
// Range and [FilterBuilder.Limit] both set the row cap - on any chain the
// last cap wins, while the start offset is replaced only by a later Range.
func (f FilterBuilder[T]) Range(from, to int) FilterBuilder[T] {
	return FilterBuilder[T]{request: f.request.
		WithParameterReplacing("offset", strconv.Itoa(from)).
		WithParameterReplacing("limit", strconv.Itoa(to-from+1))}
}

// Limit caps the query result at count rows, sent verbatim, so zero requests
// zero rows. Limit and [FilterBuilder.Range] both set the row cap: when either
// is called more than once on a chain the last cap wins, and Limit leaves any
// start offset set by an earlier Range standing.
func (f FilterBuilder[T]) Limit(count int) FilterBuilder[T] {
	return FilterBuilder[T]{request: f.request.WithParameterReplacing("limit", strconv.Itoa(count))}
}

// Order sorts the result by column, ascending with nulls last unless
// refined through the returned [OrderedFilterBuilder]. The column is sent
// verbatim as one PostgREST order term, so an invalid column is rejected by
// the server rather than rewritten. Calling Order again on the same chain
// appends a lower-precedence sort column to the same query.
func (f FilterBuilder[T]) Order(column string) OrderedFilterBuilder[T] {
	return OrderedFilterBuilder[T]{FilterBuilder[T]{request: f.request.WithParameterJoining("order", column)}}
}

// Descending sorts the newest order column from highest to lowest value and
// places rows holding a null in it before every non-null row, unless the
// returned builder's NullsLast says otherwise.
func (o OrderedFilterBuilder[T]) Descending() OrderedDescendingFilterBuilder[T] {
	return OrderedDescendingFilterBuilder[T]{FilterBuilder[T]{request: o.request.WithParameterValueAppended("order", ".desc")}}
}

// NullsFirst places rows holding a null in the newest order column before
// every non-null row.
func (o OrderedFilterBuilder[T]) NullsFirst() FilterBuilder[T] {
	return FilterBuilder[T]{request: o.request.WithParameterValueAppended("order", ".nullsfirst")}
}

// NullsLast places rows holding a null in the newest order column after
// every non-null row.
func (o OrderedDescendingFilterBuilder[T]) NullsLast() FilterBuilder[T] {
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
