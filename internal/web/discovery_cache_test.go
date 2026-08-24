package web

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
)

func TestDiscoveryCacheCachesSuccessfulResultsAndReturnsCopies(t *testing.T) {
	cache := newDiscoveryCache(8)
	key := discoveryCacheKey{baseURL: "https://example.test", feeItemID: 409, appID: 34, kind: "campuses"}
	loads := 0
	loader := func(context.Context) ([]charge.Option, error) {
		loads++
		return []charge.Option{{Value: "A", Name: "campus A"}}, nil
	}

	first, cached, err := cache.Get(context.Background(), key, loader)
	if err != nil || cached {
		t.Fatalf("first get cached=%v err=%v", cached, err)
	}
	first[0].Name = "mutated"

	second, cached, err := cache.Get(context.Background(), key, loader)
	if err != nil || !cached {
		t.Fatalf("second get cached=%v err=%v", cached, err)
	}
	if loads != 1 || second[0].Name != "campus A" {
		t.Fatalf("loads=%d options=%+v", loads, second)
	}
}

func TestDiscoveryCacheCoalescesConcurrentLoads(t *testing.T) {
	cache := newDiscoveryCache(8)
	key := discoveryCacheKey{baseURL: "https://example.test", feeItemID: 409, appID: 34, kind: "rooms", campus: "A", building: "B"}
	started := make(chan struct{})
	release := make(chan struct{})
	var loads atomic.Int32
	loader := func(context.Context) ([]charge.Option, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		<-release
		return []charge.Option{{Value: "101", Name: "101"}}, nil
	}

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() {
		defer wg.Done()
		_, _, err := cache.Get(context.Background(), key, loader)
		errs <- err
	}()
	<-started
	go func() {
		defer wg.Done()
		_, _, err := cache.Get(context.Background(), key, loader)
		errs <- err
	}()
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("loads = %d, want 1", loads.Load())
	}
}

func TestDiscoveryCacheDoesNotCacheErrors(t *testing.T) {
	cache := newDiscoveryCache(8)
	key := discoveryCacheKey{baseURL: "https://example.test", feeItemID: 409, appID: 34, kind: "buildings", campus: "A"}
	wantErr := errors.New("temporary")
	loads := 0
	if _, _, err := cache.Get(context.Background(), key, func(context.Context) ([]charge.Option, error) {
		loads++
		return nil, wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("first error = %v", err)
	}
	options, cached, err := cache.Get(context.Background(), key, func(context.Context) ([]charge.Option, error) {
		loads++
		return []charge.Option{{Value: "B", Name: "building B"}}, nil
	})
	if err != nil || cached || loads != 2 || len(options) != 1 {
		t.Fatalf("cached=%v loads=%d options=%+v err=%v", cached, loads, options, err)
	}
}
