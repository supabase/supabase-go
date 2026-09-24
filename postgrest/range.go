package postgrest

import (
	"fmt"
	"strings"
)

// Range is a Postgres range value accepted by the range filter methods, such
// as [FilterBuilder.Contains], and by any filter method whose value is typed
// any, such as [FilterBuilder.Eq], which sends it as its range literal. Build
// one with [NewRange], setting either bound through the returned builder and
// leaving a bound unset to make it unbounded, or take the empty range from
// [EmptyRange]. The builder satisfies Range at every stage, so a range with
// one or both ends unbounded is itself a Range. String hands the same literal
// to the verbatim escape hatches, such as [FilterBuilder.Not].
type Range interface {
	// String returns the Postgres range literal for the value, for
	// example [2,7) or empty.
	fmt.Stringer

	// isRange ensures only this package's types satisfy Range. Interfaces
	// are satisfied structurally in Go, so with String alone any
	// fmt.Stringer would pass as a Range. No type outside this package can
	// declare a method matching an unexported name, which closes the set
	// to values built by NewRange and EmptyRange, whose literals are
	// well-formed by construction.
	isRange()
}

// NewRange begins a Postgres range whose bounds hold values of type T, for a
// range filter method such as [FilterBuilder.Contains]. The returned builder is
// already a complete Range, unbounded at both ends, and its methods fix either
// bound.
func NewRange[T any]() RangeBuilder[T] {
	return RangeBuilder[T]{}
}

// EmptyRange returns the Postgres empty range, the range containing no points,
// for a range filter method such as [FilterBuilder.Contains]. It is distinct
// from the unbounded range that [NewRange] alone produces, which contains every
// point.
func EmptyRange() Range {
	return emptyRange{}
}

// RangeBuilder builds a Postgres [Range] whose bounds hold values of type T.
// FromInclusive and FromExclusive fix the lower bound and return a
// [RangeFromBuilder] for the upper. Every other method is that of the embedded
// RangeFromBuilder and fixes the upper bound, leaving the lower unbounded. A
// RangeBuilder is an immutable value and, before any bound is fixed, is the
// range unbounded at both ends that satisfies [Range] directly.
type RangeBuilder[T any] struct {
	RangeFromBuilder[T]
}

// RangeFromBuilder builds a Postgres [Range] whose lower bound is fixed and
// whose upper bound is not. ToInclusive and ToExclusive fix the upper bound and
// return the finished [Range]. A RangeFromBuilder is an immutable value and,
// before its upper bound is fixed, is the range unbounded above that satisfies
// [Range] directly.
type RangeFromBuilder[T any] struct {
	rangeValue
}

// FromInclusive fixes the lower bound at value, with value itself inside the
// range, and returns a builder for the upper bound.
func (b RangeBuilder[T]) FromInclusive(value T) RangeFromBuilder[T] {
	return RangeFromBuilder[T]{rangeValue{lower: rangeEndpoint{value: value, inclusive: true, bounded: true}, upper: b.upper}}
}

// FromExclusive fixes the lower bound at value, with value itself outside the
// range, and returns a builder for the upper bound.
func (b RangeBuilder[T]) FromExclusive(value T) RangeFromBuilder[T] {
	return RangeFromBuilder[T]{rangeValue{lower: rangeEndpoint{value: value, bounded: true}, upper: b.upper}}
}

// ToInclusive fixes the upper bound at value, with value itself inside the
// range, and returns the finished range.
func (b RangeFromBuilder[T]) ToInclusive(value T) Range {
	return rangeValue{lower: b.lower, upper: rangeEndpoint{value: value, inclusive: true, bounded: true}}
}

// ToExclusive fixes the upper bound at value, with value itself outside the
// range, and returns the finished range.
func (b RangeFromBuilder[T]) ToExclusive(value T) Range {
	return rangeValue{lower: b.lower, upper: rangeEndpoint{value: value, bounded: true}}
}

// rangeEndpoint is one end of a range: a bound value with its inclusivity, or an
// unbounded end when bounded is false. The zero value is an unbounded end.
type rangeEndpoint struct {
	value     any
	inclusive bool
	bounded   bool
}

// rangeValue is a lower and an upper [rangeEndpoint] together, the concrete
// [Range] a finished builder chain resolves to. Its zero value is the range
// unbounded at both ends.
type rangeValue struct {
	lower rangeEndpoint
	upper rangeEndpoint
}

// rangeBoundGrammar holds the characters Postgres reads as range syntax when
// they appear in a bound value of a range literal.
const rangeBoundGrammar = `()[],"\`

// String renders the range as one Postgres range literal, for example
// [2,7) or (,2026-01-01T00:00:00Z]. Each bound value is rendered by
// renderFilterValue and quoted by quoteIfNeeded, and an unbounded end renders as
// no bound at all.
func (r rangeValue) String() string {
	var b strings.Builder
	if r.lower.inclusive {
		b.WriteByte('[')
	} else {
		b.WriteByte('(')
	}
	if r.lower.bounded {
		b.WriteString(quoteIfNeeded(rangeBoundGrammar, renderFilterValue(r.lower.value)))
	}
	b.WriteByte(',')
	if r.upper.bounded {
		b.WriteString(quoteIfNeeded(rangeBoundGrammar, renderFilterValue(r.upper.value)))
	}
	if r.upper.inclusive {
		b.WriteByte(']')
	} else {
		b.WriteByte(')')
	}
	return b.String()
}

// isRange has no behavior. See [Range] for the compile-time job it does.
func (rangeValue) isRange() {}

// emptyRange is the Postgres empty range, a field-less singleton that renders
// as the bare literal empty rather than through the bracket grammar.
type emptyRange struct{}

// String renders the empty range as the bare literal empty.
func (emptyRange) String() string {
	return "empty"
}

// isRange has no behavior. See [Range] for the compile-time job it does.
func (emptyRange) isRange() {}

// Compile-time proof that every concrete [Range] in this package satisfies the
// interface. If a type drifts so a proof no longer holds, the build fails here
// rather than at a distant filter call site.
var (
	_ Range = RangeBuilder[any]{}
	_ Range = RangeFromBuilder[any]{}
	_ Range = rangeValue{}
	_ Range = emptyRange{}
)
