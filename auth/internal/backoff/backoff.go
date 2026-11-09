// Package backoff schedules the retries of a request the server failed to
// judge: a wait before each retry that starts at 200 milliseconds and doubles
// to a ceiling of thirty seconds, varied by up to a quarter either way and
// lengthened to the wait a Retry-After header asks for when that is longer,
// in a sequence whose last retry is the one after the first wait to reach the
// ceiling.
package backoff

import (
	"context"
	"time"
)

const (
	// initial is the wait before the first retry.
	initial = 200 * time.Millisecond

	// ceiling is the longest wait the doubling reaches, and the wait after
	// which one retry remains.
	ceiling = 30 * time.Second

	// spread is the fraction of a wait by which it varies either way.
	spread = 0.25
)

// Wait returns the wait before the retry that follows the zero-based attempt,
// and whether that retry is the last. The wait is initial doubled attempt
// times, to at most ceiling, multiplied by a factor between 1-spread and
// 1+spread that random, a number in [0, 1), selects, and is replaced by
// retryAfter, the wait the failed attempt's answer asked for, when that is
// longer. The retry is the last when the wait before variation or retryAfter
// reaches ceiling.
func Wait(attempt int, retryAfter time.Duration, random float64) (wait time.Duration, last bool) {
	base := initial
	for range attempt {
		if base >= ceiling {
			break
		}
		base *= 2
	}
	base = min(base, ceiling)

	wait = vary(base, random)
	if retryAfter > wait {
		wait = retryAfter
	}
	return wait, base >= ceiling || retryAfter >= ceiling
}

// vary returns wait multiplied by a factor between 1-spread and 1+spread that
// random, a number in [0, 1), selects.
func vary(wait time.Duration, random float64) time.Duration {
	return time.Duration(float64(wait) * (1 - spread + 2*spread*random))
}

// Sleep pauses for wait and returns nil, or returns ctx's error when ctx ends
// first.
func Sleep(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
