package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestKeyCacheHitWithinTTL(t *testing.T) {
	signer := newES256Signer(t, "kid-1")
	mock := newMockAuthServer(t, signer.publicJWK)
	cache := mock.client(t).keyCache
	base := time.Now()

	if _, found, err := cache.key(context.Background(), "kid-1", base); err != nil || !found {
		t.Fatalf("first lookup: found=%v err=%v", found, err)
	}
	if _, found, err := cache.key(context.Background(), "kid-1", base.Add(5*time.Minute)); err != nil || !found {
		t.Fatalf("second lookup: found=%v err=%v", found, err)
	}
	if fetches := mock.jwkSetFetches.Load(); fetches != 1 {
		t.Errorf("JWK Set fetched %d times within TTL, want 1", fetches)
	}
}

func TestKeyCacheRefetchAfterTTL(t *testing.T) {
	signer := newES256Signer(t, "kid-1")
	mock := newMockAuthServer(t, signer.publicJWK)
	cache := mock.client(t).keyCache
	base := time.Now()

	_, _, _ = cache.key(context.Background(), "kid-1", base)
	_, _, _ = cache.key(context.Background(), "kid-1", base.Add(jwkSetTTL+time.Minute))
	if fetches := mock.jwkSetFetches.Load(); fetches != 2 {
		t.Errorf("JWK Set fetched %d times across TTL boundary, want 2", fetches)
	}
}

func TestKeyCacheRefetchOnUnknownKeyID(t *testing.T) {
	first := newES256Signer(t, "kid-1")
	second := newES256Signer(t, "kid-2")
	mock := newMockAuthServer(t, first.publicJWK)
	cache := mock.client(t).keyCache
	base := time.Now()

	_, _, _ = cache.key(context.Background(), "kid-1", base)

	// A freshly rotated key: unknown to the cache but within TTL. The lookup
	// must refetch and then find it.
	mock.keys = []jsonWebKey{first.publicJWK, second.publicJWK}
	_, found, err := cache.key(context.Background(), "kid-2", base.Add(time.Minute))
	if err != nil {
		t.Fatalf("lookup after rotation: %v", err)
	}
	if !found {
		t.Error("key not found after rotation, want found")
	}
	if fetches := mock.jwkSetFetches.Load(); fetches != 2 {
		t.Errorf("JWK Set fetched %d times, want 2 (initial + rotation refetch)", fetches)
	}
}

func TestKeyCacheUnknownKeyIDAfterFetch(t *testing.T) {
	signer := newES256Signer(t, "kid-1")
	mock := newMockAuthServer(t, signer.publicJWK)
	cache := mock.client(t).keyCache

	_, found, err := cache.key(context.Background(), "absent", time.Now())
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if found {
		t.Error("found = true for an absent key id, want false")
	}
}

func TestKeyCacheEmptyKeySet(t *testing.T) {
	mock := newMockAuthServer(t)
	cache := mock.client(t).keyCache

	_, found, err := cache.key(context.Background(), "any", time.Now())
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if found {
		t.Error("found = true against an empty JWK Set, want false")
	}
}

func TestKeyCacheSingleFlight(t *testing.T) {
	signer := newES256Signer(t, "kid-1")
	mock := newMockAuthServer(t, signer.publicJWK)
	cache := mock.client(t).keyCache
	base := time.Now()

	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, found, err := cache.key(context.Background(), "kid-1", base); err != nil || !found {
				t.Errorf("concurrent lookup: found=%v err=%v", found, err)
			}
		}()
	}
	group.Wait()

	if fetches := mock.jwkSetFetches.Load(); fetches != 1 {
		t.Errorf("cold-cache stampede fetched %d times, want 1", fetches)
	}
}

func TestKeyCacheFetchFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"msg":"boom"}`))
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, "test-anon-key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := client.keyCache.key(context.Background(), "kid-1", time.Now()); err == nil {
		t.Error("key() error = nil, want the fetch failure surfaced")
	}
}
