package postgrest

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
)

// cSpell:ignore ilike imatch isdistinct phraseto phfts plainto plfts tsquery websearch wfts

// Eq matches only rows where column equals value, sent as the eq operator.
// Equality never matches rows holding null in column - [FilterBuilder.IsNull]
// matches those.
func (f FilterBuilder[T]) Eq(column string, value any) FilterBuilder[T] {
	return f.appendFilter(column, "eq."+renderFilterValue(value))
}

// Neq matches only rows where column does not equal value, sent as the neq
// operator. Rows holding null in column never match -
// [FilterBuilder.IsDistinct] treats null as a comparable value.
func (f FilterBuilder[T]) Neq(column string, value any) FilterBuilder[T] {
	return f.appendFilter(column, "neq."+renderFilterValue(value))
}

// Gt matches only rows where column is greater than value, sent as the gt
// operator.
func (f FilterBuilder[T]) Gt(column string, value any) FilterBuilder[T] {
	return f.appendFilter(column, "gt."+renderFilterValue(value))
}

// Gte matches only rows where column is greater than or equal to value, sent
// as the gte operator.
func (f FilterBuilder[T]) Gte(column string, value any) FilterBuilder[T] {
	return f.appendFilter(column, "gte."+renderFilterValue(value))
}

// Lt matches only rows where column is less than value, sent as the lt
// operator.
func (f FilterBuilder[T]) Lt(column string, value any) FilterBuilder[T] {
	return f.appendFilter(column, "lt."+renderFilterValue(value))
}

// Lte matches only rows where column is less than or equal to value, sent as
// the lte operator.
func (f FilterBuilder[T]) Lte(column string, value any) FilterBuilder[T] {
	return f.appendFilter(column, "lte."+renderFilterValue(value))
}

// Like matches only rows where column matches pattern case-sensitively, sent
// as the like operator. In the pattern, % matches any sequence of characters
// (the server also accepts * as its alias) and _ matches exactly one.
func (f FilterBuilder[T]) Like(column, pattern string) FilterBuilder[T] {
	return f.appendFilter(column, "like."+pattern)
}

// LikeAllOf matches only rows where column matches every one of patterns
// case-sensitively, sent as the like operator with its all modifier over a
// pattern list.
func (f FilterBuilder[T]) LikeAllOf(column string, patterns ...string) FilterBuilder[T] {
	return f.appendFilter(column, "like(all)."+renderFilterList('{', patterns, '}'))
}

// LikeAnyOf matches only rows where column matches at least one of patterns
// case-sensitively, sent as the like operator with its any modifier over a
// pattern list.
func (f FilterBuilder[T]) LikeAnyOf(column string, patterns ...string) FilterBuilder[T] {
	return f.appendFilter(column, "like(any)."+renderFilterList('{', patterns, '}'))
}

// ILike matches only rows where column matches pattern case-insensitively,
// sent as the ilike operator. In the pattern, % matches any sequence of
// characters (the server also accepts * as its alias) and _ matches exactly
// one.
func (f FilterBuilder[T]) ILike(column, pattern string) FilterBuilder[T] {
	return f.appendFilter(column, "ilike."+pattern)
}

// ILikeAllOf matches only rows where column matches every one of patterns
// case-insensitively, sent as the ilike operator with its all modifier over
// a pattern list.
func (f FilterBuilder[T]) ILikeAllOf(column string, patterns ...string) FilterBuilder[T] {
	return f.appendFilter(column, "ilike(all)."+renderFilterList('{', patterns, '}'))
}

// ILikeAnyOf matches only rows where column matches at least one of patterns
// case-insensitively, sent as the ilike operator with its any modifier over
// a pattern list.
func (f FilterBuilder[T]) ILikeAnyOf(column string, patterns ...string) FilterBuilder[T] {
	return f.appendFilter(column, "ilike(any)."+renderFilterList('{', patterns, '}'))
}

// RegexMatch matches only rows where column matches the POSIX regular
// expression pattern case-sensitively, sent as the match operator.
func (f FilterBuilder[T]) RegexMatch(column, pattern string) FilterBuilder[T] {
	return f.appendFilter(column, "match."+pattern)
}

// RegexIMatch matches only rows where column matches the POSIX regular
// expression pattern case-insensitively, sent as the imatch operator.
func (f FilterBuilder[T]) RegexIMatch(column, pattern string) FilterBuilder[T] {
	return f.appendFilter(column, "imatch."+pattern)
}

// IsNull matches only rows where column is null, sent as is.null.
func (f FilterBuilder[T]) IsNull(column string) FilterBuilder[T] {
	return f.appendFilter(column, "is.null")
}

// IsNotNull matches only rows where column holds a non-null value, sent as
// is.not_null.
func (f FilterBuilder[T]) IsNotNull(column string) FilterBuilder[T] {
	return f.appendFilter(column, "is.not_null")
}

// IsTrue matches only rows where column is true, sent as is.true.
func (f FilterBuilder[T]) IsTrue(column string) FilterBuilder[T] {
	return f.appendFilter(column, "is.true")
}

// IsFalse matches only rows where column is false, sent as is.false.
func (f FilterBuilder[T]) IsFalse(column string) FilterBuilder[T] {
	return f.appendFilter(column, "is.false")
}

// IsUnknown matches only rows where column is unknown, sent as is.unknown.
// Only boolean columns hold unknown, the boolean spelling of null.
func (f FilterBuilder[T]) IsUnknown(column string) FilterBuilder[T] {
	return f.appendFilter(column, "is.unknown")
}

// IsDistinct matches only rows where column is distinct from value, sent as
// the isdistinct operator: a not-equal that treats null as a comparable
// value, so rows holding null match whenever value is not null, and a nil
// value matches every row holding any non-null value.
func (f FilterBuilder[T]) IsDistinct(column string, value any) FilterBuilder[T] {
	return f.appendFilter(column, "isdistinct."+renderFilterValue(value))
}

// In matches only rows where column equals one of values, sent as the in
// operator over a list. Values travel in the given order, duplicates
// included.
func (f FilterBuilder[T]) In(column string, values ...any) FilterBuilder[T] {
	return f.appendFilter(column, "in."+renderFilterList('(', values, ')'))
}

// NotIn matches only rows where column equals none of values, sent as the in
// operator negated. Values travel in the given order, duplicates included.
func (f FilterBuilder[T]) NotIn(column string, values ...any) FilterBuilder[T] {
	return f.appendFilter(column, "not.in."+renderFilterList('(', values, ')'))
}

// Contains matches only rows where the array in column contains every one of
// values, sent as the cs operator over an array literal.
// [FilterBuilder.ContainsRange] and [FilterBuilder.ContainsJSON] cover range
// and jsonb columns.
func (f FilterBuilder[T]) Contains(column string, values ...any) FilterBuilder[T] {
	return f.appendFilter(column, "cs."+renderFilterList('{', values, '}'))
}

// ContainsRange matches only rows where the range in column contains
// rangeLiteral, sent as the cs operator. The literal travels verbatim, its
// brackets and parentheses choosing inclusive or exclusive bounds, for
// example [2,7) or (1,5).
func (f FilterBuilder[T]) ContainsRange(column, rangeLiteral string) FilterBuilder[T] {
	return f.appendFilter(column, "cs."+rangeLiteral)
}

// ContainsJSON matches only rows where the jsonb document in column contains
// value, sent as the cs operator with value marshaled as JSON. It panics
// when value cannot be marshaled by [encoding/json].
func (f FilterBuilder[T]) ContainsJSON(column string, value any) FilterBuilder[T] {
	return f.appendFilter(column, "cs."+renderFilterJSON(value))
}

// ContainedBy matches only rows where every element of the array in column
// appears among values, sent as the cd operator over an array literal.
// [FilterBuilder.ContainedByRange] and [FilterBuilder.ContainedByJSON] cover
// range and jsonb columns.
func (f FilterBuilder[T]) ContainedBy(column string, values ...any) FilterBuilder[T] {
	return f.appendFilter(column, "cd."+renderFilterList('{', values, '}'))
}

// ContainedByRange matches only rows where the range in column lies entirely
// within rangeLiteral, sent as the cd operator. The literal travels verbatim,
// its brackets and parentheses choosing inclusive or exclusive bounds, for
// example [2,7) or (1,5).
func (f FilterBuilder[T]) ContainedByRange(column, rangeLiteral string) FilterBuilder[T] {
	return f.appendFilter(column, "cd."+rangeLiteral)
}

// ContainedByJSON matches only rows where the jsonb document in column is
// contained by value, sent as the cd operator with value marshaled as JSON.
// It panics when value cannot be marshaled by [encoding/json].
func (f FilterBuilder[T]) ContainedByJSON(column string, value any) FilterBuilder[T] {
	return f.appendFilter(column, "cd."+renderFilterJSON(value))
}

// Overlaps matches only rows where the array in column shares at least one
// element with values, sent as the ov operator over an array literal.
// [FilterBuilder.OverlapsRange] covers range columns.
func (f FilterBuilder[T]) Overlaps(column string, values ...any) FilterBuilder[T] {
	return f.appendFilter(column, "ov."+renderFilterList('{', values, '}'))
}

// OverlapsRange matches only rows where the range in column has values in
// common with rangeLiteral, sent as the ov operator. The literal travels
// verbatim, its brackets and parentheses choosing inclusive or exclusive
// bounds, for example [2,7) or (1,5).
func (f FilterBuilder[T]) OverlapsRange(column, rangeLiteral string) FilterBuilder[T] {
	return f.appendFilter(column, "ov."+rangeLiteral)
}

// RangeGt matches only rows where the range in column is strictly right of
// rangeLiteral - every value in it greater than every value in the literal -
// sent as the sr operator. The literal travels verbatim, its brackets and
// parentheses choosing inclusive or exclusive bounds, for example [2,7).
func (f FilterBuilder[T]) RangeGt(column, rangeLiteral string) FilterBuilder[T] {
	return f.appendFilter(column, "sr."+rangeLiteral)
}

// RangeGte matches only rows where the range in column does not extend to
// the left of rangeLiteral - no value in it below the literal's start - sent
// as the nxl operator. The literal travels verbatim, its brackets and
// parentheses choosing inclusive or exclusive bounds, for example [2,7).
func (f FilterBuilder[T]) RangeGte(column, rangeLiteral string) FilterBuilder[T] {
	return f.appendFilter(column, "nxl."+rangeLiteral)
}

// RangeLt matches only rows where the range in column is strictly left of
// rangeLiteral - every value in it less than every value in the literal -
// sent as the sl operator. The literal travels verbatim, its brackets and
// parentheses choosing inclusive or exclusive bounds, for example [2,7).
func (f FilterBuilder[T]) RangeLt(column, rangeLiteral string) FilterBuilder[T] {
	return f.appendFilter(column, "sl."+rangeLiteral)
}

// RangeLte matches only rows where the range in column does not extend to
// the right of rangeLiteral - no value in it above the literal's end - sent
// as the nxr operator. The literal travels verbatim, its brackets and
// parentheses choosing inclusive or exclusive bounds, for example [2,7).
func (f FilterBuilder[T]) RangeLte(column, rangeLiteral string) FilterBuilder[T] {
	return f.appendFilter(column, "nxr."+rangeLiteral)
}

// RangeAdjacent matches only rows where the range in column shares no values
// with rangeLiteral yet leaves no gap between them, sent as the adj
// operator. The literal travels verbatim, its brackets and parentheses
// choosing inclusive or exclusive bounds, for example [2,7).
func (f FilterBuilder[T]) RangeAdjacent(column, rangeLiteral string) FilterBuilder[T] {
	return f.appendFilter(column, "adj."+rangeLiteral)
}

// TextSearch matches only rows where column matches the full-text search
// query, with query parsed by to_tsquery and sent as the fts operator. A
// non-empty configuration names the text search configuration to search
// with (for example english), while empty relies on the server's default.
func (f FilterBuilder[T]) TextSearch(column, query, configuration string) FilterBuilder[T] {
	return f.appendFilter(column, renderTextSearch("fts", configuration, query))
}

// TextSearchPlain matches only rows where column matches the full-text
// search query, with query parsed by plainto_tsquery and sent as the plfts
// operator. A non-empty configuration names the text search configuration
// to search with (for example english), while empty relies on the server's
// default.
func (f FilterBuilder[T]) TextSearchPlain(column, query, configuration string) FilterBuilder[T] {
	return f.appendFilter(column, renderTextSearch("plfts", configuration, query))
}

// TextSearchPhrase matches only rows where column matches the full-text
// search query, with query parsed by phraseto_tsquery and sent as the phfts
// operator. A non-empty configuration names the text search configuration
// to search with (for example english), while empty relies on the server's
// default.
func (f FilterBuilder[T]) TextSearchPhrase(column, query, configuration string) FilterBuilder[T] {
	return f.appendFilter(column, renderTextSearch("phfts", configuration, query))
}

// TextSearchWebsearch matches only rows where column matches the full-text
// search query, with query parsed by websearch_to_tsquery and sent as the
// wfts operator. A non-empty configuration names the text search
// configuration to search with (for example english), while empty relies on
// the server's default.
func (f FilterBuilder[T]) TextSearchWebsearch(column, query, configuration string) FilterBuilder[T] {
	return f.appendFilter(column, renderTextSearch("wfts", configuration, query))
}

// Match matches only rows where every key of query equals its value, sent as
// one eq filter per pair in ascending key order. An empty or nil query adds
// nothing.
func (f FilterBuilder[T]) Match(query map[string]any) FilterBuilder[T] {
	for _, column := range slices.Sorted(maps.Keys(query)) {
		f = f.Eq(column, query[column])
	}
	return f
}

// Not negates a single filter, sent as the given operator prefixed with not.
// The operator and value travel verbatim as PostgREST filter syntax, for
// example Not("status", "eq", "OFFLINE"), so the caller owns any quoting the
// value needs.
func (f FilterBuilder[T]) Not(column, operator, value string) FilterBuilder[T] {
	return f.appendFilter(column, "not."+operator+"."+value)
}

// Or matches only rows satisfying at least one of filters, a group written
// in PostgREST filter syntax - conditions of the form column.operator.value,
// comma-separated, nesting through and(...), or(...) and a not. prefix - sent
// verbatim inside or=(...), for example
// Or("age.lt.18,age.gt.21"). The caller owns any quoting the values need.
// Each Or call appends its own group and every group must be satisfied.
func (f FilterBuilder[T]) Or(filters string) FilterBuilder[T] {
	return FilterBuilder[T]{request: f.request.WithParameter("or", "("+filters+")")}
}

// Filter matches only rows satisfying one verbatim PostgREST condition, sent
// as operator.value with no rendering or quoting: the escape hatch for
// operators and forms without a dedicated method, for example
// Filter("status", "eq(any)", "{ONLINE,OFFLINE}"). The caller owns the
// entire condition's syntax.
func (f FilterBuilder[T]) Filter(column, operator, value string) FilterBuilder[T] {
	return f.appendFilter(column, operator+"."+value)
}

// appendFilter returns a builder carrying one more filter pair for column.
func (f FilterBuilder[T]) appendFilter(column, condition string) FilterBuilder[T] {
	return FilterBuilder[T]{request: f.request.WithParameter(column, condition)}
}

// renderFilterValue renders value as PostgREST filter-value text following
// the type table documented on [FilterBuilder].
func renderFilterValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", typed)
	case float32:
		return strconv.FormatFloat(float64(typed), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case time.Time:
		return typed.Format(time.RFC3339Nano)
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprintf("%v", typed)
	}
}

// filterListStructure holds every character that forces a list element into
// double quotes: the delimiters the list grammar reads as structure plus the
// quote and escape characters themselves.
const filterListStructure = `,(){}"\`

// renderFilterList renders elements as one delimited list, each element
// rendered by renderFilterValue and quoted by quoteFilterListElement, for
// example ("a,b",plain) or {1,2}. An empty elements slice renders just the
// bare delimiters.
func renderFilterList[Element any](opening byte, elements []Element, closing byte) string {
	var text strings.Builder
	text.WriteByte(opening)
	for index, element := range elements {
		if index > 0 {
			text.WriteByte(',')
		}
		text.WriteString(quoteFilterListElement(renderFilterValue(element)))
	}
	text.WriteByte(closing)
	return text.String()
}

// quoteFilterListElement returns rendered ready for embedding as one list
// element: double-quoted with backslash-escaped \ and " when it is empty,
// carries edge whitespace or contains a character the list grammar reads as
// structure, and untouched otherwise.
func quoteFilterListElement(rendered string) string {
	if rendered != "" && rendered == strings.TrimSpace(rendered) && !strings.ContainsAny(rendered, filterListStructure) {
		return rendered
	}
	escaped := strings.ReplaceAll(rendered, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// renderTextSearch renders one full-text-search condition, placing a
// non-empty configuration in parentheses between the operator and the query.
func renderTextSearch(operator, configuration, query string) string {
	if configuration == "" {
		return operator + "." + query
	}
	return operator + "(" + configuration + ")." + query
}

// renderFilterJSON renders value as compact JSON for a jsonb filter,
// panicking when value cannot be marshaled.
func renderFilterJSON(value any) string {
	rendered, err := json.Marshal(value)
	if err != nil {
		panic("postgrest: rendering JSON filter value: " + err.Error())
	}
	return string(rendered)
}
