package web

import (
	"context"
	"sync"

	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
)

const maxDiscoveryCacheEntries = 4096

type discoveryCacheKey struct {
	baseURL   string
	feeItemID int
	appID     int
	kind      string
	campus    string
	building  string
}

type discoveryFlight struct {
	done    chan struct{}
	options []charge.Option
	err     error
}

// discoveryCache 在进程生命周期内缓存学校接口返回的校区、楼栋和房间选项。
// 错误不会进入缓存；相同参数的并发首次查询只会执行一次上游请求。
type discoveryCache struct {
	mu         sync.Mutex
	entries    map[discoveryCacheKey][]charge.Option
	inflight   map[discoveryCacheKey]*discoveryFlight
	maxEntries int
}

func newDiscoveryCache(maxEntries int) *discoveryCache {
	if maxEntries <= 0 {
		maxEntries = maxDiscoveryCacheEntries
	}
	return &discoveryCache{
		entries:    make(map[discoveryCacheKey][]charge.Option),
		inflight:   make(map[discoveryCacheKey]*discoveryFlight),
		maxEntries: maxEntries,
	}
}

// Get 返回缓存结果，或调用 load 获取并缓存结果。cached 表示本次没有执行 load。
func (c *discoveryCache) Get(
	ctx context.Context,
	key discoveryCacheKey,
	load func(context.Context) ([]charge.Option, error),
) (options []charge.Option, cached bool, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.Lock()
	if options, ok := c.entries[key]; ok {
		c.mu.Unlock()
		return cloneDiscoveryOptions(options), true, nil
	}
	if flight, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-flight.done:
			return cloneDiscoveryOptions(flight.options), flight.err == nil, flight.err
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}

	flight := &discoveryFlight{done: make(chan struct{})}
	c.inflight[key] = flight
	c.mu.Unlock()

	options, err = load(ctx)
	result := cloneDiscoveryOptions(options)

	c.mu.Lock()
	if err == nil && len(c.entries) < c.maxEntries {
		c.entries[key] = cloneDiscoveryOptions(result)
	}
	flight.options = result
	flight.err = err
	delete(c.inflight, key)
	close(flight.done)
	c.mu.Unlock()

	return cloneDiscoveryOptions(result), false, err
}

func cloneDiscoveryOptions(options []charge.Option) []charge.Option {
	return append([]charge.Option(nil), options...)
}
