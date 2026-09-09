package http

import (
	"testing"
	"time"
)

// TestRetryDelay pins the delay computation: exponential doubling from one
// second, displaced by a parseable non-negative whole-seconds Retry-After.
func TestRetryDelay(t *testing.T) {
	testCases := []struct {
		name       string
		attempt    int
		retryAfter string
		want       time.Duration
	}{
		{"first backoff", 0, "", time.Second},
		{"second backoff", 1, "", 2 * time.Second},
		{"third backoff", 2, "", 4 * time.Second},
		{"retry-after replaces backoff", 0, "7", 7 * time.Second},
		{"retry-after zero", 2, "0", 0},
		{"retry-after with edge whitespace", 0, " 7 ", 7 * time.Second},
		{"negative retry-after ignored", 1, "-1", 2 * time.Second},
		{"malformed retry-after ignored", 0, "soon", time.Second},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := retryDelay(testCase.attempt, testCase.retryAfter); got != testCase.want {
				t.Errorf("retryDelay(%d, %q) = %v, want %v", testCase.attempt, testCase.retryAfter, got, testCase.want)
			}
		})
	}
}
