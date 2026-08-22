// Package rate 实现一个线程安全的滑动窗口限流器。
package rate

import (
	"sync"
	"time"
)

// Limiter 实现滑动窗口限流。
// 零值 Limiter{} 表示不限流（Allow() 始终返回 true）。
type Limiter struct {
	mu     sync.Mutex
	window time.Duration
	rate   int
	times  []time.Time
}

// NewLimiter 创建一个新的限流器。
// ratePerMinute 为每分钟允许的请求次数。<=0 表示不限流。
func NewLimiter(ratePerMinute int) *Limiter {
	return &Limiter{
		window: 60 * time.Second,
		rate:   ratePerMinute,
		times:  make([]time.Time, 0, ratePerMinute),
	}
}

// Allow 尝试消耗一次额度。
// 返回 true 表示放行（并记账），false 表示超限。
func (l *Limiter) Allow() bool {
	if l.rate <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	// 移除窗口外的老记录
	i := 0
	for i < len(l.times) && l.times[i].Before(cutoff) {
		i++
	}
	l.times = l.times[i:]

	// 检查是否超限
	if len(l.times) >= l.rate {
		return false
	}

	l.times = append(l.times, now)
	return true
}

// SetRate 动态调整限流速率。
func (l *Limiter) SetRate(ratePerMinute int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rate = ratePerMinute
	// 如果速率调低，清理多余的记录
	if ratePerMinute > 0 && len(l.times) > ratePerMinute {
		l.times = l.times[len(l.times)-ratePerMinute:]
	}
}

// Rate 返回当前限流速率。
func (l *Limiter) Rate() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rate
}

// Reset 清空限流记录。
func (l *Limiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.times = l.times[:0]
}