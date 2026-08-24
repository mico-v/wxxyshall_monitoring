// Package rate 提供严格按请求间隔放行的线程安全节拍器。
package rate

import (
	"context"
	"sync"
	"time"
)

// Limiter 保证任意两次成功放行之间至少间隔 1 分钟/rate。
// 所有调用方共享同一实例时，能够按真实上游 HTTP 请求数限速。
type Limiter struct {
	waitMu  sync.Mutex
	mu      sync.RWMutex
	rate    int
	last    time.Time
	changed chan struct{}
}

func NewLimiter(ratePerMinute int) *Limiter {
	return &Limiter{rate: normalizeRate(ratePerMinute), changed: make(chan struct{})}
}

// Wait 等待到下一个允许发送请求的时刻。取消 context 会立即退出且不消耗额度。
func (l *Limiter) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	l.waitMu.Lock()
	defer l.waitMu.Unlock()

	for {
		interval := l.Interval()
		l.mu.RLock()
		last := l.last
		changed := l.changed
		l.mu.RUnlock()

		wait := time.Until(last.Add(interval))
		if last.IsZero() || wait <= 0 {
			l.mu.Lock()
			l.last = time.Now()
			l.mu.Unlock()
			return nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
			// 重新读取 rate；等待期间配置可能已热更新。
			continue
		case <-changed:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			continue
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		}
	}
}

// SetRate 动态更新每分钟请求数，下一次放行立即使用新间隔。
func (l *Limiter) SetRate(ratePerMinute int) {
	normalized := normalizeRate(ratePerMinute)
	l.mu.Lock()
	if l.rate == normalized {
		l.mu.Unlock()
		return
	}
	l.rate = normalized
	close(l.changed)
	l.changed = make(chan struct{})
	l.mu.Unlock()
}

func (l *Limiter) Rate() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.rate
}

func (l *Limiter) Interval() time.Duration {
	rate := l.Rate()
	divisor := time.Duration(rate)
	return (time.Minute + divisor - 1) / divisor
}

// Reset 仅用于测试或显式重新开始节拍。
func (l *Limiter) Reset() {
	l.waitMu.Lock()
	l.mu.Lock()
	l.last = time.Time{}
	l.mu.Unlock()
	l.waitMu.Unlock()
}

func normalizeRate(rate int) int {
	if rate <= 0 {
		return 1
	}
	return rate
}
