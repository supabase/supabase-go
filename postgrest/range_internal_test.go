package postgrest

import (
	"math"
	"testing"
	"time"
)

func TestRange(t *testing.T) {
	testCases := []struct {
		create func() Range
		want   string
	}{
		{func() Range { return NewRange[int]() }, "(,)"},                                   // bare constructor is unbounded both ends
		{func() Range { return NewRange[int]().FromInclusive(2).ToExclusive(7) }, "[2,7)"}, // canonical discrete form
		{func() Range { return NewRange[int]().FromExclusive(1).ToExclusive(5) }, "(1,5)"},
		{func() Range { return NewRange[int]().FromInclusive(1).ToInclusive(5) }, "[1,5]"},
		{func() Range { return NewRange[int]().ToInclusive(5) }, "(,5]"},                                     // upper-only path
		{func() Range { return NewRange[int]().FromInclusive(5) }, "[5,)"},                                   // lower-only path
		{func() Range { return NewRange[time.Time]().FromInclusive(spookyTime) }, "[2026-10-31T20:00:00Z,)"}, // renderFilterValue reuse
		{func() Range { return NewRange[float64]().FromInclusive(math.Inf(1)) }, "[Infinity,)"},              // bound at infinity vs no bound
		{func() Range { return NewRange[float64]().FromInclusive(1.5).ToExclusive(2.5) }, "[1.5,2.5)"},
		{func() Range { return NewRange[string]().FromInclusive("a,b").ToExclusive("z") }, `["a,b",z)`}, // comma triggers quoting
		{func() Range { return NewRange[string]().FromInclusive("").ToExclusive("z") }, `["",z)`},       // empty bound quotes, unbounded does not
		{func() Range { return NewRange[string]().FromInclusive(`say "hi"`) }, `["say \"hi\"",)`},
		{func() Range { return NewRange[string]().FromInclusive(`back\slash`) }, `["back\\slash",)`},
		{func() Range { return NewRange[string]().FromInclusive(" padded ") }, `[" padded ",)`}, // over-quoting, harmless and explicit
		{func() Range { return EmptyRange() }, "empty"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.want, func(t *testing.T) {
			if got := testCase.create().String(); got != testCase.want {
				t.Errorf(".String() = %v, want %v", got, testCase.want)
			}
		})
	}
}
