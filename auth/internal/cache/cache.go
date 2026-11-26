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
	backoffUntil time.Time
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
func (c *Cache) Key(ctx context.Context, keyID string, now time.Time) (key.Key, bool, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if now.Sub(c.fetchedAt) >= ttl {
		if err := c.download(ctx, now); err != nil {
			return key.Key{}, false, err
		}
		cached, ok := c.keys[keyID]
		return cached, ok, nil
	}
	if cached, ok := c.keys[keyID]; ok {
		return cached, true, nil
	}
	// Inside the backoff window after fruitless or failed forced fetches, a miss
	// is answered from the cached set rather than forcing another, so a storm
	// of bogus key ids decays to at most one fetch per maximumFetchBackoff.
	if now.Before(c.backoffUntil) {
		return key.Key{}, false, nil
	}
	if err := c.download(ctx, now); err != nil {
		c.widenBackoff(now)
		return key.Key{}, false, err
	}
	cached, ok := c.keys[keyID]
	if ok {
		c.fetchBackoff = 0
	} else {
		c.widenBackoff(now)
	}
	return cached, ok, nil
}

// download replaces the cached set with a fetched one, leaving it as it was
// when the fetch fails.
func (c *Cache) download(ctx context.Context, now time.Time) error {
	fetched, err := c.fetch(ctx)
	if err != nil {
		return err
	}
	c.keys = make(map[string]key.Key, len(fetched))
	for _, fetchedKey := range fetched {
		c.keys[fetchedKey.ID()] = fetchedKey
	}
	c.fetchedAt = now
	return nil
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
