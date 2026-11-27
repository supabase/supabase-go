// Package cache holds a project's published JWK Set between fetches. A
// [Cache] resolves a signing key by its key id, trusting a fetched set for a
// TTL and re-fetching on both expiry and an unknown key id, so a freshly
// rotated key is found without waiting out the TTL. Fetches forced by unknown
// key ids back off exponentially while they fail, or fail to resolve one, so
// neither bogus key ids nor an unreachable endpoint can fetch at the sender's
// rate. Fetching crosses the package boundary as a [FetchFunc] the caller
// supplies, so HTTP and the consumer-facing error model stay outside.
package cache

import (
	"context"
	"sync"
	"time"

	"github.com/supabase/supabase-go/auth/internal/key"
)

// ttl is how long a fetched JWK Set is trusted before the next lookup
// re-fetches it. It matches the Cache-Control the Auth server sets on the
// discovery endpoint, so this cache and the server's edge cache expire on the
// same clock.
const ttl = 10 * time.Minute

// Backoff bounds for fetches forced by an unknown key id. A forced fetch that
// fails, or that does not resolve its key id, doubles the wait before the next
// one, from initialFetchBackoff to at most maximumFetchBackoff, and one that
// resolves its key id clears the wait. A download into an empty or expired
// cache leaves the wait as it was. Attacker-minted key ids therefore cost at
// most one fetch per maximumFetchBackoff once the ramp completes, while a
// genuine rotation resolves on the first fetch and never waits.
const (
	initialFetchBackoff = time.Second
	maximumFetchBackoff = 16 * time.Second
)

// FetchFunc returns the current JWK Set. The caller supplies one that performs
// the network exchange and shapes failures with its own error model, which
// [Cache.Key] propagates verbatim. A [Cache] calls it once per download, from
// the goroutine of the lookup that started the download and with that
// lookup's context. Every lookup waiting on that download shares its result.
type FetchFunc func(ctx context.Context) ([]key.Key, error)

// Cache resolves a signing key by its key id, fetching the JWK Set through its
// [FetchFunc] and trusting the result for ten minutes. It is safe for
// concurrent use by multiple goroutines, and concurrent lookups on a cold or
// stale cache collapse into one fetch rather than a stampede. Construct with
// [NewCache].
type Cache struct {
	fetch FetchFunc

	// mutex guards the fields below and is never held across a fetch. It is
	// taken only by the locked method, so a panic never leaves it held.
	mutex        sync.Mutex
	keys         map[string]key.Key
	fetchedAt    time.Time
	fetchBackoff time.Duration
	backoffUntil time.Time
	inflight     *flight
}

// flight is one download of the JWK Set in flight. forced is whether a lookup
// whose key id a fresh cache lacked started it, and done is closed once it has
// finished. keys, err and abandoned are read only after that: the set it
// fetched, its failure and whether it was abandoned. A download is abandoned
// when its starter's context ends before it succeeds or its fetch panics, and
// the lookups waiting on it then start over.
type flight struct {
	forced    bool
	done      chan struct{}
	keys      map[string]key.Key
	err       error
	abandoned bool
}

// NewCache constructs a [Cache] resolving keys through fetch.
func NewCache(fetch FetchFunc) *Cache {
	return &Cache{fetch: fetch}
}

// Key returns the signing key for keyID and whether it was found. An empty or
// expired cache downloads the JWK Set first, and a key id the download lacks
// is not found. A key id absent from a fresh cache - a token signed by a
// freshly rotated key - forces a fetch, unless the backoff window is open,
// when it is not found with no fetch. A forced fetch that fails, or that
// lacks the key id, opens or widens the window, and one that finds it closes
// the window. When found is false the caller decides the fallback. A fetch
// failure leaves the cached set as it was and is returned verbatim, failing
// the call.
//
// A lookup the cached set can answer never waits for a download. One that
// needs a download while another lookup's is in flight waits for that one and
// takes its outcome, its failure included. It returns its own context's error
// if that context ends first. A download whose starter's context ends before
// it succeeds, or whose fetch panics, is abandoned rather than shared, and the
// lookups waiting on it start over.
func (c *Cache) Key(ctx context.Context, keyID string, now time.Time) (key.Key, bool, error) {
	for {
		var cached key.Key
		var found, answered bool
		var joined, started *flight
		c.locked(func() {
			fresh := now.Sub(c.fetchedAt) < ttl
			cached, found = c.keys[keyID]
			switch {
			// A fresh cache answers a key id it holds. Inside the backoff window
			// after fruitless or failed forced fetches, a miss is answered from the
			// cached set rather than forcing another, so a storm of bogus key ids
			// decays to at most one fetch per maximumFetchBackoff.
			case fresh && (found || now.Before(c.backoffUntil)):
				answered = true
			case c.inflight != nil:
				joined = c.inflight
			default:
				started = &flight{forced: fresh, done: make(chan struct{})}
				c.inflight = started
			}
		})
		switch {
		case answered:
			return cached, found, nil
		case started != nil:
			return c.download(ctx, started, keyID, now)
		}

		select {
		case <-joined.done:
		case <-ctx.Done():
			return key.Key{}, false, ctx.Err()
		}
		if !joined.abandoned {
			cached, found = joined.keys[keyID]
			return cached, found, joined.err
		}
	}
}

// download fetches the JWK Set with ctx for the lookup of keyID that started
// the flight, then settles the flight and returns that lookup's result. A
// fetched set replaces the cached one, and a forced fetch moves the backoff
// window by whether it found keyID. The flight is abandoned when the fetch
// fails after ctx has ended, or panics.
func (c *Cache) download(ctx context.Context, started *flight, keyID string, now time.Time) (key.Key, bool, error) {
	settled := false
	defer func() {
		if settled {
			return
		}
		c.locked(func() {
			c.inflight = nil
			started.abandoned = true
		})
		close(started.done)
	}()

	fetched, err := c.fetch(ctx)

	var cached key.Key
	var found bool
	c.locked(func() {
		c.inflight = nil
		if err == nil {
			c.keys = make(map[string]key.Key, len(fetched))
			for _, fetchedKey := range fetched {
				c.keys[fetchedKey.ID()] = fetchedKey
			}
			c.fetchedAt = now
			started.keys = c.keys
			cached, found = c.keys[keyID]
		}
		started.err = err
		started.abandoned = err != nil && ctx.Err() != nil
		if started.forced {
			if found {
				c.fetchBackoff = 0
			} else {
				c.widenBackoff(now)
			}
		}
	})
	settled = true
	close(started.done)
	return cached, found, err
}

// locked calls f with mutex held and releases it however f ends, a panic
// included.
func (c *Cache) locked(f func()) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	f()
}

// widenBackoff opens the backoff window at initialFetchBackoff, or doubles it
// to at most maximumFetchBackoff, from now.
func (c *Cache) widenBackoff(now time.Time) {
	if c.fetchBackoff == 0 {
		c.fetchBackoff = initialFetchBackoff
	} else {
		c.fetchBackoff = min(2*c.fetchBackoff, maximumFetchBackoff)
	}
	c.backoffUntil = now.Add(c.fetchBackoff)
}
