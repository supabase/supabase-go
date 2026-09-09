package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/supabase/supabase-go/core/configuration"
)

// jwkSetTTL is how long a fetched JWK Set is trusted before the next lookup
// re-fetches it. It matches the Cache-Control the Auth server sets on the
// discovery endpoint, so the client's cache and the server's edge cache expire
// on the same clock.
const jwkSetTTL = 10 * time.Minute

// jwkSetCache resolves a signing key by its key id, fetching the project's JWK
// Set from the discovery endpoint and holding it for [jwkSetTTL]. It is safe
// for concurrent use by multiple goroutines: the lock is held across a fetch,
// so concurrent lookups on a cold or stale cache collapse into one network
// request rather than a stampede.
type jwkSetCache struct {
	httpClient configuration.HTTPClient
	endpoint   *url.URL

	mutex     sync.Mutex
	keys      map[string]jsonWebKey
	fetchedAt time.Time
}

// key returns the signing key for keyID and whether it was found. A cached key
// is returned when the cache is fresh and holds it; otherwise the JWK Set is
// re-fetched first, so a key id absent from a fresh cache - a token signed by a
// freshly rotated key - still triggers a lookup. When the key remains absent
// after a refetch, found is false and the caller falls back to server
// verification. A fetch failure is returned and fails the call.
func (c *jwkSetCache) key(ctx context.Context, keyID string, now time.Time) (key jsonWebKey, found bool, err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if cached, ok := c.keys[keyID]; ok && now.Sub(c.fetchedAt) < jwkSetTTL {
		return cached, true, nil
	}

	fetched, err := c.fetch(ctx)
	if err != nil {
		return jsonWebKey{}, false, err
	}
	c.keys = fetched
	c.fetchedAt = now

	cached, ok := c.keys[keyID]
	return cached, ok, nil
}

// fetch retrieves and parses the JWK Set from the discovery endpoint, keyed by
// key id. An empty set - a project with no asymmetric signing keys - yields an
// empty map.
func (c *jwkSetCache) fetch(ctx context.Context) (map[string]jsonWebKey, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("auth: building JWK Set request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("auth: fetching JWK Set: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("auth: reading JWK Set: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, newError(response.StatusCode, body)
	}

	var document struct {
		Keys []jsonWebKey `json:"keys"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("auth: parsing JWK Set: %w", err)
	}

	keys := make(map[string]jsonWebKey, len(document.Keys))
	for _, key := range document.Keys {
		keys[key.KeyID] = key
	}
	return keys, nil
}
