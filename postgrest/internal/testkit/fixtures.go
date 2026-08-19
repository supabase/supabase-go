package testkit

import (
	"testing"
	"time"
)

// TimeRFC3339 uses [time.Parse] to interpret value as a [time.Time]
// using the [time.RFC3339] layout.
func TimeRFC3339(t *testing.T, value string) time.Time {
	t.Helper()
	time, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time.Parse failed for %v", value)
	}
	return time
}
