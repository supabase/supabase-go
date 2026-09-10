// Package cache holds a project's published JWK Set between fetches. A
// [Cache] resolves a signing key by its key id, trusting a fetched set for a
// TTL and re-fetching on both expiry and an unknown key id, so a freshly
// rotated key is found without waiting out the TTL. Fetching crosses the
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

	mutex     sync.Mutex
	keys      map[string]key.Key
	fetchedAt time.Time
}

// NewCache constructs a [Cache] resolving keys through fetch.
func NewCache(fetch FetchFunc) *Cache {
	return &Cache{fetch: fetch}
}

// Key returns the signing key for keyID and whether it was found. A cached key
// is returned when the cache is fresh and holds it; otherwise the JWK Set is
// re-fetched first, so a key id absent from a fresh cache - a token signed by a
// freshly rotated key - still triggers a lookup. When the key remains absent
// after a refetch, found is false and the caller decides the fallback. A fetch
// failure is returned verbatim and fails the call.
func (c *Cache) Key(ctx context.Context, keyID string, now time.Time) (key.Key, bool, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if cached, ok := c.keys[keyID]; ok && now.Sub(c.fetchedAt) < ttl {
		return cached, true, nil
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
	return cached, ok, nil
}
