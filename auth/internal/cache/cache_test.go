package cache_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth/internal/cache"
	"github.com/supabase/supabase-go/auth/internal/key"
	"github.com/supabase/supabase-go/auth/internal/testkit"
)

// staticFetch builds a FetchFunc serving the keys of a JWK Set document and
// counting its calls.
func staticFetch(t *testing.T, document []byte) (*atomic.Int32, cache.FetchFunc) {
	t.Helper()
	keys, err := key.ParseSet(document)
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}
	var calls atomic.Int32
	return &calls, func(_ context.Context) ([]key.Key, error) {
		calls.Add(1)
		return keys, nil
	}
}

func TestCacheHitWithinTTL(t *testing.T) {
	fetches, fetch := staticFetch(t, testkit.JWKSetDocument(t, testkit.NewES256(t, "kid-1")))
	keyCache := cache.NewCache(fetch)
	base := time.Now()

	if _, found, err := keyCache.Key(context.Background(), "kid-1", base); err != nil || !found {
		t.Fatalf("first lookup: found=%v err=%v", found, err)
	}
	if _, found, err := keyCache.Key(context.Background(), "kid-1", base.Add(5*time.Minute)); err != nil || !found {
		t.Fatalf("second lookup: found=%v err=%v", found, err)
	}
	if fetches.Load() != 1 {
		t.Errorf("JWK Set fetched %d times within TTL, want 1", fetches.Load())
	}
}

func TestCacheRefetchAfterTTL(t *testing.T) {
	fetches, fetch := staticFetch(t, testkit.JWKSetDocument(t, testkit.NewES256(t, "kid-1")))
	keyCache := cache.NewCache(fetch)
	base := time.Now()

	_, _, _ = keyCache.Key(context.Background(), "kid-1", base)
	// The cache trusts a fetched set for ten minutes, so a lookup eleven
	// minutes on must fetch again.
	_, _, _ = keyCache.Key(context.Background(), "kid-1", base.Add(11*time.Minute))
	if fetches.Load() != 2 {
		t.Errorf("JWK Set fetched %d times across the TTL boundary, want 2", fetches.Load())
	}
}

func TestCacheRefetchOnUnknownKeyID(t *testing.T) {
	first := testkit.NewES256(t, "kid-1")
	second := testkit.NewES256(t, "kid-2")
	published, err := key.ParseSet(testkit.JWKSetDocument(t, first))
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}

	var fetches atomic.Int32
	keyCache := cache.NewCache(func(_ context.Context) ([]key.Key, error) {
		fetches.Add(1)
		return published, nil
	})
	base := time.Now()

	_, _, _ = keyCache.Key(context.Background(), "kid-1", base)

	// A freshly rotated key: unknown to the cache but within TTL. The lookup
	// must refetch and then find it.
	published, err = key.ParseSet(testkit.JWKSetDocument(t, first, second))
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}
	_, found, err := keyCache.Key(context.Background(), "kid-2", base.Add(time.Minute))
	if err != nil {
		t.Fatalf("lookup after rotation: %v", err)
	}
	if !found {
		t.Error("key not found after rotation, want found")
	}
	if fetches.Load() != 2 {
		t.Errorf("JWK Set fetched %d times, want 2 (initial + rotation refetch)", fetches.Load())
	}
}

func TestCacheUnknownKeyIDAfterFetch(t *testing.T) {
	_, fetch := staticFetch(t, testkit.JWKSetDocument(t, testkit.NewES256(t, "kid-1")))
	keyCache := cache.NewCache(fetch)

	_, found, err := keyCache.Key(context.Background(), "absent", time.Now())
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if found {
		t.Error("found = true for an absent key id, want false")
	}
}

func TestCacheEmptyKeySet(t *testing.T) {
	_, fetch := staticFetch(t, []byte(`{"keys":[]}`))
	keyCache := cache.NewCache(fetch)

	_, found, err := keyCache.Key(context.Background(), "any", time.Now())
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if found {
		t.Error("found = true against an empty JWK Set, want false")
	}
}

func TestCacheSingleFlight(t *testing.T) {
	fetches, fetch := staticFetch(t, testkit.JWKSetDocument(t, testkit.NewES256(t, "kid-1")))
	keyCache := cache.NewCache(fetch)
	base := time.Now()

	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, found, err := keyCache.Key(context.Background(), "kid-1", base); err != nil || !found {
				t.Errorf("concurrent lookup: found=%v err=%v", found, err)
			}
		}()
	}
	group.Wait()

	if fetches.Load() != 1 {
		t.Errorf("cold-cache stampede fetched %d times, want 1", fetches.Load())
	}
}

func TestCacheFetchFailure(t *testing.T) {
	fetchFailure := errors.New("boom")
	keyCache := cache.NewCache(func(_ context.Context) ([]key.Key, error) {
		return nil, fetchFailure
	})

	if _, _, err := keyCache.Key(context.Background(), "kid-1", time.Now()); !errors.Is(err, fetchFailure) {
		t.Errorf("Key() error = %v, want the fetch failure propagated verbatim", err)
	}
}
