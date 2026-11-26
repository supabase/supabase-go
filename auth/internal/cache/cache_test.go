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

// lookupResult is what one call of Key returned.
type lookupResult struct {
	cached key.Key
	found  bool
	err    error
}

// lookUp makes one call of Key in a goroutine of its own and returns the
// channel its result arrives on.
func lookUp(ctx context.Context, keyCache *cache.Cache, keyID string, now time.Time) <-chan lookupResult {
	looked := make(chan lookupResult, 1)
	go func() {
		cached, found, err := keyCache.Key(ctx, keyID, now)
		looked <- lookupResult{cached: cached, found: found, err: err}
	}()
	return looked
}

// holdForcedDownload returns a cache holding the set that publishes kid-1,
// with a download in flight that the unknown key id bogus forced at base. At
// the test's cleanup the download serves the same set, and its lookup must
// return not found.
func holdForcedDownload(t *testing.T, base time.Time) *cache.Cache {
	t.Helper()
	published, err := key.ParseSet(testkit.JWKSetDocument(t, testkit.NewES256(t, "kid-1")))
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var fetches atomic.Int32
	keyCache := cache.NewCache(func(_ context.Context) ([]key.Key, error) {
		// The first download serves at once, and the second waits for release.
		if fetches.Add(1) == 2 {
			close(entered)
			<-release
		}
		return published, nil
	})
	if _, found, err := keyCache.Key(t.Context(), "kid-1", base); err != nil || !found {
		t.Fatalf("first lookup: found=%v err=%v", found, err)
	}

	forced := lookUp(t.Context(), keyCache, "bogus", base)
	<-entered
	t.Cleanup(func() {
		close(release)
		if result := <-forced; result.err != nil || result.found {
			t.Errorf("forced lookup: found=%v err=%v, want not found", result.found, result.err)
		}
	})
	return keyCache
}

// waitObservingContext closes waiting the first time its Done channel is
// asked for, which a lookup does only once it waits on a download another
// lookup started.
type waitObservingContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func newWaitObservingContext(parent context.Context) *waitObservingContext {
	return &waitObservingContext{Context: parent, waiting: make(chan struct{})}
}

func (c *waitObservingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

// awaitWaiting returns once the lookup given observed waits on a download in
// flight, failing the test if it has not within five seconds.
func awaitWaiting(t *testing.T, observed *waitObservingContext) {
	t.Helper()
	select {
	case <-observed.waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("the lookup never waited on the download in flight")
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

	if _, found, err := keyCache.Key(context.Background(), "kid-1", base); err != nil || !found {
		t.Fatalf("first lookup: found=%v err=%v", found, err)
	}
	// The cache trusts a fetched set for ten minutes, so a lookup eleven
	// minutes on must fetch again.
	if _, found, err := keyCache.Key(context.Background(), "kid-1", base.Add(11*time.Minute)); err != nil || !found {
		t.Fatalf("lookup after the TTL: found=%v err=%v", found, err)
	}
	if fetches.Load() != 2 {
		t.Errorf("JWK Set fetched %d times across the TTL boundary, want 2", fetches.Load())
	}
}

func TestCacheFruitlessForcedFetchesBackOff(t *testing.T) {
	fetches, fetch := staticFetch(t, testkit.JWKSetDocument(t, testkit.NewES256(t, "kid-1")))
	keyCache := cache.NewCache(fetch)
	base := time.Now()

	lookup := func(offset time.Duration, wantFetches int32) {
		t.Helper()
		if _, found, err := keyCache.Key(context.Background(), "bogus", base.Add(offset)); err != nil || found {
			t.Fatalf("lookup at +%v: found=%v err=%v", offset, found, err)
		}
		if fetches.Load() != wantFetches {
			t.Errorf("fetches after lookup at +%v = %d, want %d", offset, fetches.Load(), wantFetches)
		}
	}

	lookup(0, 1)                    // a first download lacking the key id opens no window
	lookup(0, 2)                    // so the next miss forces a fetch, opening a 1s window
	lookup(500*time.Millisecond, 2) // inside 1s: answered from the last fetch
	lookup(time.Second, 3)          // window elapsed: fetches, doubling to 2s
	lookup(2*time.Second, 3)        // only 1s after that fetch: gated
	lookup(3*time.Second, 4)        // 2s elapsed: fetches, doubling to 4s
	lookup(7*time.Second, 5)        // 4s elapsed: fetches, doubling to 8s
	lookup(15*time.Second, 6)       // 8s elapsed: fetches, doubling to 16s
	lookup(30*time.Second, 6)       // 15s elapsed: gated at the backstop
	lookup(31*time.Second, 7)       // 16s elapsed: one fetch per backstop from here
}

// TestCacheFailedForcedFetch pins what a forced fetch that fails leaves for the
// next miss while the endpoint keeps failing.
func TestCacheFailedForcedFetch(t *testing.T) {
	published, err := key.ParseSet(testkit.JWKSetDocument(t, testkit.NewES256(t, "kid-1")))
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}
	fetchFailure := errors.New("discovery unavailable")
	var fetches atomic.Int32
	keyCache := cache.NewCache(func(_ context.Context) ([]key.Key, error) {
		// The first fetch succeeds and every later one fails.
		if fetches.Add(1) > 1 {
			return nil, fetchFailure
		}
		return published, nil
	})
	base := time.Now()

	if _, found, err := keyCache.Key(context.Background(), "kid-1", base); err != nil || !found {
		t.Fatalf("first lookup: found=%v err=%v", found, err)
	}
	lookup := func(offset time.Duration, wantErr error, wantFetches int32) {
		t.Helper()
		if _, found, err := keyCache.Key(context.Background(), "bogus", base.Add(offset)); found || !errors.Is(err, wantErr) {
			t.Fatalf("lookup at +%v: found=%v err=%v, want not found and %v", offset, found, err, wantErr)
		}
		if fetches.Load() != wantFetches {
			t.Errorf("fetches after lookup at +%v = %d, want %d", offset, fetches.Load(), wantFetches)
		}
	}

	lookup(time.Second, fetchFailure, 2)   // the forced fetch fails, and so does the lookup: a 1s window
	lookup(time.Second, nil, 2)            // inside 1s: not found, with no fetch
	lookup(2*time.Second, fetchFailure, 3) // window elapsed: fails again, doubling to 2s
	lookup(3*time.Second, nil, 3)          // inside 2s: no fetch
}

// TestCacheExpiredDownloadAndTheBackoff pins how a download into an expired
// cache moves the window that fruitless forced fetches open.
func TestCacheExpiredDownloadAndTheBackoff(t *testing.T) {
	fetches, fetch := staticFetch(t, testkit.JWKSetDocument(t, testkit.NewES256(t, "kid-1")))
	keyCache := cache.NewCache(fetch)
	base := time.Now()

	lookup := func(offset time.Duration, keyID string, wantFetches int32) {
		t.Helper()
		if _, _, err := keyCache.Key(context.Background(), keyID, base.Add(offset)); err != nil {
			t.Fatalf("lookup of %s at +%v: %v", keyID, offset, err)
		}
		if fetches.Load() != wantFetches {
			t.Errorf("fetches after lookup of %s at +%v = %d, want %d", keyID, offset, fetches.Load(), wantFetches)
		}
	}

	lookup(0, "kid-1", 1)                          // first download
	lookup(time.Second, "bogus", 2)                // forced, fruitless: a 1s window
	lookup(11*time.Minute, "bogus", 3)             // expired download lacking the key id: the window stays as it was
	lookup(11*time.Minute+time.Second, "bogus", 4) // the 1s window closed long ago: forced, fruitless, doubling to 2s
	lookup(22*time.Minute, "kid-1", 5)             // expired download finding its key id: the window stays as it was
	lookup(22*time.Minute, "bogus", 6)             // forced, fruitless: the window doubles to 4s
	lookup(22*time.Minute+time.Second, "bogus", 6) // inside 4s: no fetch
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

	_, found, err := keyCache.Key(context.Background(), "kid-1", base)
	if err != nil || !found {
		t.Fatalf("first lookup: found=%v err=%v", found, err)
	}

	// A freshly rotated key: unknown to the cache but within TTL. The lookup
	// must refetch and then find it.
	published, err = key.ParseSet(testkit.JWKSetDocument(t, first, second))
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}
	_, found, err = keyCache.Key(context.Background(), "kid-2", base.Add(time.Minute))
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

func TestCacheResolvingFetchClearsBackoff(t *testing.T) {
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

	// After the first download, a fruitless forced fetch opens the backoff
	// window.
	_, found, err := keyCache.Key(context.Background(), "kid-1", base)
	if err != nil || !found {
		t.Fatalf("first lookup: found=%v err=%v", found, err)
	}
	_, found, err = keyCache.Key(context.Background(), "bogus", base)
	if err != nil || found {
		t.Fatalf("fruitless lookup: found=%v err=%v", found, err)
	}

	// The key rotates and its first token arrives once the window has elapsed:
	// the forced fetch resolves it and clears the backoff.
	published, err = key.ParseSet(testkit.JWKSetDocument(t, first, second))
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}
	if _, found, err := keyCache.Key(context.Background(), "kid-2", base.Add(time.Second)); err != nil || !found {
		t.Fatalf("rotated lookup: found=%v err=%v", found, err)
	}

	// With the window cleared, the very next unknown key id may force a fetch
	// immediately rather than waiting anything out.
	if _, found, err := keyCache.Key(context.Background(), "bogus-2", base.Add(time.Second)); err != nil || found {
		t.Fatalf("lookup after the window cleared: found=%v err=%v", found, err)
	}
	if fetches.Load() != 4 {
		t.Errorf("fetches = %d, want 4 (first, fruitless, resolving, immediately allowed)", fetches.Load())
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
		group.Go(func() {
			if _, found, err := keyCache.Key(context.Background(), "kid-1", base); err != nil || !found {
				t.Errorf("concurrent lookup: found=%v err=%v", found, err)
			}
		})
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

// TestCacheHitDuringDownload proves a lookup of a cached key is answered from
// the cached set while a download is in flight.
func TestCacheHitDuringDownload(t *testing.T) {
	base := time.Now()
	keyCache := holdForcedDownload(t, base)

	select {
	case result := <-lookUp(t.Context(), keyCache, "kid-1", base):
		if result.err != nil || !result.found || result.cached.ID() != "kid-1" {
			t.Errorf("lookup while the download was held: found=%v err=%v, want kid-1", result.found, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the lookup of a cached key waited for the download in flight")
	}
}

// TestCacheCanceledLookupDuringDownload proves a lookup waiting on a download
// in flight returns its context's error as soon as that context ends, while
// the download goes on.
func TestCacheCanceledLookupDuringDownload(t *testing.T) {
	base := time.Now()
	keyCache := holdForcedDownload(t, base)

	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	waiter := newWaitObservingContext(parent)
	looked := lookUp(waiter, keyCache, "bogus-2", base)
	awaitWaiting(t, waiter)
	cancel()
	select {
	case result := <-looked:
		if result.found || !errors.Is(result.err, context.Canceled) {
			t.Errorf("lookup whose context ended: found=%v err=%v, want context.Canceled", result.found, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the lookup went on waiting for the download after its context had ended")
	}
}

// TestCacheConcurrentFetchFailure proves the lookups waiting on a download
// into an empty cache share its failure, so the endpoint is asked once.
func TestCacheConcurrentFetchFailure(t *testing.T) {
	fetchFailure := errors.New("discovery unavailable")
	entered := make(chan struct{})
	release := make(chan struct{})
	var fetches atomic.Int32
	keyCache := cache.NewCache(func(_ context.Context) ([]key.Key, error) {
		// Every download fails, the first only once release is closed.
		if fetches.Add(1) == 1 {
			close(entered)
			<-release
		}
		return nil, fetchFailure
	})
	base := time.Now()

	const lookups = 4
	results := []<-chan lookupResult{lookUp(t.Context(), keyCache, "kid-1", base)}
	<-entered
	for range lookups - 1 {
		waiter := newWaitObservingContext(t.Context())
		results = append(results, lookUp(waiter, keyCache, "kid-1", base))
		awaitWaiting(t, waiter)
	}
	close(release)

	for i, looked := range results {
		if result := <-looked; result.found || !errors.Is(result.err, fetchFailure) {
			t.Errorf("lookup %d: found=%v err=%v, want the fetch failure", i, result.found, result.err)
		}
	}
	if fetches.Load() != 1 {
		t.Errorf("fetches = %d, want 1, shared by every lookup", fetches.Load())
	}
}

// TestCacheCanceledDownload pins what a download whose context ends before it
// finishes leaves for a lookup waiting on it. The download fails only the
// lookup that started it, and the waiting lookup returns its key from a
// download of its own.
func TestCacheCanceledDownload(t *testing.T) {
	published, err := key.ParseSet(testkit.JWKSetDocument(t, testkit.NewES256(t, "kid-1")))
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}
	entered := make(chan struct{})
	var fetches atomic.Int32
	keyCache := cache.NewCache(func(ctx context.Context) ([]key.Key, error) {
		// The first download lasts until its context ends, and later ones serve
		// at once.
		if fetches.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return published, nil
	})
	base := time.Now()

	starterContext, cancelStarter := context.WithCancel(t.Context())
	defer cancelStarter()
	started := lookUp(starterContext, keyCache, "kid-1", base)
	<-entered
	waiter := newWaitObservingContext(t.Context())
	waited := lookUp(waiter, keyCache, "kid-1", base)
	awaitWaiting(t, waiter)
	cancelStarter()

	if result := <-started; result.found || !errors.Is(result.err, context.Canceled) {
		t.Errorf("lookup that started the download: found=%v err=%v, want context.Canceled", result.found, result.err)
	}
	if result := <-waited; result.err != nil || !result.found || result.cached.ID() != "kid-1" {
		t.Errorf("lookup that waited on the download: found=%v err=%v, want kid-1", result.found, result.err)
	}
	if fetches.Load() != 2 {
		t.Errorf("fetches = %d, want 2, the canceled download and the waiting lookup's own", fetches.Load())
	}
}

// TestCachePanickingFetch pins what a fetch that panics leaves for the next
// lookup. The panic reaches the lookup that fetched, and the next lookup
// downloads again.
func TestCachePanickingFetch(t *testing.T) {
	published, err := key.ParseSet(testkit.JWKSetDocument(t, testkit.NewES256(t, "kid-1")))
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}
	var fetches atomic.Int32
	keyCache := cache.NewCache(func(_ context.Context) ([]key.Key, error) {
		// The first download panics, and later ones serve.
		if fetches.Add(1) == 1 {
			panic("fetch failure")
		}
		return published, nil
	})
	base := time.Now()

	func() {
		defer func() {
			if recovered := recover(); recovered != "fetch failure" {
				t.Errorf("recovered %v, want the fetch's panic passed on", recovered)
			}
		}()
		_, found, err := keyCache.Key(t.Context(), "kid-1", base)
		t.Errorf("Key = (found=%v, err=%v), want the fetch's panic passed on", found, err)
	}()
	select {
	case result := <-lookUp(t.Context(), keyCache, "kid-1", base):
		if result.err != nil || !result.found || result.cached.ID() != "kid-1" {
			t.Errorf("lookup after the panic: found=%v err=%v, want kid-1", result.found, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the lookup after the panic blocked on a lock or download the panic left held")
	}
	if fetches.Load() != 2 {
		t.Errorf("fetches = %d, want 2, the one that panicked and the next lookup's own", fetches.Load())
	}
}
