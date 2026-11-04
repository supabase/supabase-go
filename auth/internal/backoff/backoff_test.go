package backoff_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/supabase/supabase-go/auth/internal/backoff"
)

func TestWait(t *testing.T) {
	cases := []struct {
		name       string
		attempt    int
		retryAfter time.Duration
		random     float64
		wantWait   time.Duration
		wantLast   bool
	}{
		{name: "the first wait is 200ms", attempt: 0, random: 0.5, wantWait: 200 * time.Millisecond},
		{name: "each wait doubles", attempt: 3, random: 0.5, wantWait: 1600 * time.Millisecond},
		{name: "the eighth wait is 25.6s", attempt: 7, random: 0.5, wantWait: 25600 * time.Millisecond},
		{name: "the ninth wait reaches the ceiling and makes the next retry the last", attempt: 8, random: 0.5, wantWait: 30 * time.Second, wantLast: true},
		{name: "a later attempt stays at the ceiling", attempt: 40, random: 0.5, wantWait: 30 * time.Second, wantLast: true},
		{name: "random 0 shortens a wait by a quarter", attempt: 1, random: 0, wantWait: 300 * time.Millisecond},
		{name: "random above a half lengthens a wait", attempt: 1, random: 0.75, wantWait: 450 * time.Millisecond},
		{name: "the ceiling applies before variation", attempt: 8, random: 0.75, wantWait: 33750 * time.Millisecond, wantLast: true},
		{name: "a longer Retry-After replaces the wait", attempt: 0, retryAfter: 2 * time.Second, random: 0.5, wantWait: 2 * time.Second},
		{name: "a shorter Retry-After is ignored", attempt: 3, retryAfter: time.Second, random: 0.5, wantWait: 1600 * time.Millisecond},
		{name: "a Retry-After of thirty seconds makes the next retry the last", attempt: 0, retryAfter: 30 * time.Second, random: 0.5, wantWait: 30 * time.Second, wantLast: true},
		{name: "a Retry-After beyond thirty seconds is waited in full", attempt: 0, retryAfter: 31 * time.Second, random: 0.5, wantWait: 31 * time.Second, wantLast: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			wait, last := backoff.Wait(testCase.attempt, testCase.retryAfter, testCase.random)
			if wait != testCase.wantWait || last != testCase.wantLast {
				t.Errorf("Wait(%d, %v, %v) = (%v, %v), want (%v, %v)", testCase.attempt, testCase.retryAfter, testCase.random, wait, last, testCase.wantWait, testCase.wantLast)
			}
		})
	}
}

func TestWaitsSumToEightyOneSecondsWithoutVariation(t *testing.T) {
	var total time.Duration
	retries := 0
	for attempt := 0; ; attempt++ {
		wait, last := backoff.Wait(attempt, 0, 0.5)
		total += wait
		retries++
		if last {
			break
		}
	}
	if retries != 9 || total != 81*time.Second {
		t.Errorf("the schedule runs %d retries with %v of waits, want 9 and 81s", retries, total)
	}
}

func TestSleep(t *testing.T) {
	t.Run("a full pause returns nil", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			if err := backoff.Sleep(t.Context(), 30*time.Second); err != nil {
				t.Errorf("Sleep = %v, want nil", err)
			}
			if elapsed := time.Since(start); elapsed != 30*time.Second {
				t.Errorf("Sleep paused for %v, want 30s", elapsed)
			}
		})
	})

	t.Run("a context that ends first returns its error", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			start := time.Now()
			if err := backoff.Sleep(ctx, 30*time.Second); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("Sleep = %v, want context.DeadlineExceeded", err)
			}
			if elapsed := time.Since(start); elapsed != time.Second {
				t.Errorf("Sleep paused for %v, want the context's second", elapsed)
			}
		})
	})
}
