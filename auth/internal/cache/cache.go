// Package cache holds a project's published JWK Set between fetches. A
// [Cache] resolves a signing key by its key id, trusting a fetched set for a
// TTL and re-fetching on both expiry and an unknown key id, so a freshly
// rotated key is found without waiting out the TTL. Fetches forced by unknown
// key ids back off exponentially while they fail to resolve one, so bogus key
// ids cannot fetch at the sender's rate. Fetching crosses the
// package boundary as a [FetchFunc] the caller supplies, so HTTP and the
// consumer-facing error model stay outside.
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
// still does not resolve its key id doubles the wait before the next one, from
// initialFetchBackoff to at most maximumFetchBackoff, and a fetch that
// resolves its key id clears the wait. Attacker-minted key ids therefore cost
// at most one fetch per maximumFetchBackoff once the ramp completes, while a
// genuine rotation resolves on the first fetch and never waits.
const (
	initialFetchBackoff = time.Second
	maximumFetchBackoff = 16 * time.Second
)

// FetchFunc returns the current JWK Set. The caller supplies one that performs
// the network exchange and shapes failures with its own error model, which
// [Cache.Key] propagates verbatim.
type FetchFunc func(ctx context.Context) ([]key.Key, error)

// Cache resolves a signing key by its key id, fetching the JWK Set through its
// [FetchFunc] and trusting the result for ten minutes. It is safe for
// concurrent use by multiple goroutines: the lock is held across a fetch, so
// concurrent lookups on a cold or stale cache collapse into one fetch rather
// than a stampede. Construct with [NewCache].
type Cache struct {
	fetch FetchFunc

	mutex        sync.Mutex
	keys         map[string]key.Key
	fetchedAt    time.Time
	fetchBackoff time.Duration
}

// NewCache constructs a [Cache] resolving keys through fetch.
func NewCache(fetch FetchFunc) *Cache {
	return &Cache{fetch: fetch}
}

// Key returns the signing key for keyID and whether it was found. A cached key
// is returned when the cache is fresh and holds it; otherwise the JWK Set is
// re-fetched first, so a key id absent from a fresh cache - a token signed by a
// freshly rotated key - still triggers a lookup. Fruitless forced fetches back
// off exponentially: while consecutive fetches fail to resolve the key id that
// forced them, misses inside the growing backoff window are answered not-found
// from the last fetch, and a fetch that resolves its key id clears the window.
// When the key remains absent after a re-fetch, found is false and the caller
// decides the fallback. A fetch failure is returned verbatim and fails the
// call, leaving the backoff untouched.
func (c *Cache) Key(ctx context.Context, keyID string, now time.Time) (key.Key, bool, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if cached, ok := c.keys[keyID]; ok && now.Sub(c.fetchedAt) < ttl {
		return cached, true, nil
	}
	// Inside the backoff window after fruitless forced fetches, a miss is
	// answered from the last fetch rather than forcing another, so a storm of
	// bogus key ids decays to at most one fetch per maximumFetchBackoff.
	if now.Sub(c.fetchedAt) < c.fetchBackoff {
		return key.Key{}, false, nil
	}

	fetched, err := c.fetch(ctx)
	if err != nil {
		return key.Key{}, false, err
	}
	c.keys = make(map[string]key.Key, len(fetched))
	for _, fetchedKey := range fetched {
		c.keys[fetchedKey.ID()] = fetchedKey
	}
	c.fetchedAt = now

	cached, ok := c.keys[keyID]
	switch {
	case ok:
		c.fetchBackoff = 0
	case c.fetchBackoff == 0:
		c.fetchBackoff = initialFetchBackoff
	default:
		c.fetchBackoff = min(2*c.fetchBackoff, maximumFetchBackoff)
	}
	return cached, ok, nil
}
