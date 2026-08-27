package web

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// ipLimiter 是按 IP 计数的令牌桶限流器，用于保护公开接口免受单点滥用。
//
// 令牌按时间线性补充，桶容量即允许的突发请求数；同一 IP 的连续请求会被
// 均匀摊薄。条目惰性回收：长时间无请求的客户端自然重建，内存占用只与
// 活跃客户端数相关，不需要后台清理协程。
type ipLimiter struct {
	mu      sync.Mutex
	rate    float64 // 每秒补充的令牌数
	burst   float64 // 桶容量（允许的突发量）
	buckets map[string]*ipBucket
}

type ipBucket struct {
	tokens float64
	last   time.Time
}

func newIPLimiter(perMinute, burst int) *ipLimiter {
	return &ipLimiter{
		rate:    float64(perMinute) / 60.0,
		burst:   float64(burst),
		buckets: make(map[string]*ipBucket),
	}
}

// Allow 报告 key 是否可以放行一个请求；可放行时消耗一个令牌。
func (l *ipLimiter) Allow(key string) bool {
	return l.AllowAt(key, time.Now())
}

// AllowAt 与 Allow 相同，但允许注入时钟（便于测试）。
func (l *ipLimiter) AllowAt(key string, now time.Time) bool {
	if l == nil || key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxLimiterKeys {
			l.sweep(now)
		}
		b = &ipBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = minFloat(l.burst, b.tokens+elapsed*l.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// sweep 控制内存上限：先清理 10 分钟内无请求的冷桶；
// 若仍超过上限（如攻击者持续换新 IP），强制随机驱逐以保证硬性内存上限。
func (l *ipLimiter) sweep(now time.Time) {
	cutoff := now.Add(-10 * time.Minute)
	for key, b := range l.buckets {
		if b.last.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
	if len(l.buckets) >= maxLimiterKeys {
		for key := range l.buckets {
			if len(l.buckets) < maxLimiterKeys {
				break
			}
			delete(l.buckets, key)
		}
	}
}

const maxLimiterKeys = 8192

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// clientIP 返回请求的客户端地址。
//
// 应用部署在 Caddy 等 TLS 终止反代之后、且反代是唯一入口时，反代会用真实
// 对端地址覆写 X-Forwarded-For（Caddy 默认行为），外部客户端无法伪造，
// 因此取 X-Forwarded-For 的第一个有效地址；若直连则回退到 RemoteAddr。
func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			addr := strings.TrimSpace(part)
			if ip, err := netip.ParseAddr(addr); err == nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.String()
	}
	return ""
}
