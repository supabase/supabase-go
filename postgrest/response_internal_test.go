package postgrest

import "testing"

// TestParseContentRangeTotal pins the Content-Range tail parse, including
// the distinction between a counted empty result ("*/0" is 0) and an
// unreported total ("*" is -1), and that malformed forms degrade to -1
// rather than failing the query.
func TestParseContentRangeTotal(t *testing.T) {
	testCases := []struct {
		name         string
		contentRange string
		want         int64
	}{
		{name: "range with total", contentRange: "0-2/3", want: 3},
		{name: "large total", contentRange: "0-24/3573", want: 3573},
		{name: "empty result with total", contentRange: "*/0", want: 0},
		{name: "unknown total", contentRange: "0-2/*", want: -1},
		{name: "empty header", contentRange: "", want: -1},
		{name: "no slash", contentRange: "0-2", want: -1},
		{name: "malformed total", contentRange: "0-2/many", want: -1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := parseContentRangeTotal(testCase.contentRange); got != testCase.want {
				t.Errorf("parseContentRangeTotal(%q) = %d, want %d", testCase.contentRange, got, testCase.want)
			}
		})
	}
}
