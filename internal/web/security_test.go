package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// ---- ipLimiter ----

func TestIPLimiterBurstAndRefill(t *testing.T) {
	l := newIPLimiter(60, 10) // 1 token/s，突发 10
	now := time.Unix(1000, 0)

	for i := 0; i < 10; i++ {
		if !l.AllowAt("a", now) {
			t.Fatalf("burst request %d should pass", i)
		}
	}
	if l.AllowAt("a", now) {
		t.Fatal("empty bucket must reject")
	}
	// 2 秒后应补充 2 个令牌
	now = now.Add(2 * time.Second)
	if !l.AllowAt("a", now) {
		t.Fatal("refill token 1")
	}
	if !l.AllowAt("a", now) {
		t.Fatal("refill token 2")
	}
	if l.AllowAt("a", now) {
		t.Fatal("refilled quota exceeded")
	}
	// 长时间空闲后应恢复满桶
	now = now.Add(time.Hour)
	for i := 0; i < 10; i++ {
		if !l.AllowAt("a", now) {
			t.Fatalf("restored burst request %d", i)
		}
	}
}

func TestIPLimiterKeysAreIndependent(t *testing.T) {
	l := newIPLimiter(60, 1) // 突发 1，便于打空
	now := time.Unix(1000, 0)
	if !l.AllowAt("a", now) {
		t.Fatal("a first should pass")
	}
	if l.AllowAt("a", now) {
		t.Fatal("a second should be limited")
	}
	if !l.AllowAt("b", now) {
		t.Fatal("b must not be affected by a")
	}
}

func TestIPLimiterNilOrEmptyKeyAllows(t *testing.T) {
	var nilLimiter *ipLimiter
	if !nilLimiter.Allow("x") {
		t.Fatal("nil limiter must not block")
	}
	l := newIPLimiter(1, 1)
	if !l.Allow("") {
		t.Fatal("empty key must not block")
	}
}

func TestIPLimiterSweepLimitsMemory(t *testing.T) {
	l := newIPLimiter(60, 1)
	now := time.Unix(1000, 0)
	for i := 0; i < maxLimiterKeys+100; i++ {
		l.AllowAt(string(rune('a'+i%26))+string(rune('a'+i/26)), now)
	}
	if len(l.buckets) > maxLimiterKeys {
		t.Fatalf("bucket count %d exceeds cap %d", len(l.buckets), maxLimiterKeys)
	}
}

// ---- clientIP ----

func TestClientIPParsing(t *testing.T) {
	tests := []struct {
		name   string
		xff    string
		remote string
		want   string
	}{
		{name: "xff single", xff: "203.0.113.9", remote: "10.0.0.1:1234", want: "203.0.113.9"},
		{name: "xff list", xff: "203.0.113.9, 10.0.0.2", remote: "10.0.0.1:1234", want: "203.0.113.9"},
		{name: "xff first invalid", xff: "not-an-ip, 203.0.113.9", remote: "10.0.0.1:1234", want: "203.0.113.9"},
		{name: "xff ipv6", xff: "2001:db8::1", remote: "10.0.0.1:1234", want: "2001:db8::1"},
		{name: "no xff", xff: "", remote: "192.0.2.1:1234", want: "192.0.2.1"},
		{name: "no xff no port", xff: "", remote: "192.0.2.1", want: "192.0.2.1"},
		{name: "everything invalid", xff: "garbage", remote: "nonsense", want: ""},
		{name: "nil request", xff: "", remote: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			if tc.name != "nil request" {
				req = httptest.NewRequest(http.MethodGet, "/api/readings", nil)
				req.RemoteAddr = tc.remote
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := clientIP(req); got != tc.want {
				t.Fatalf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---- /api/readings 每 IP 限流 ----

func TestReadingsPerIPRateLimit(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	allow := func() bool {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/readings", nil))
		switch rec.Code {
		case http.StatusOK:
			return true
		case http.StatusTooManyRequests:
			return false
		default:
			t.Fatalf("unexpected status %d body=%s", rec.Code, rec.Body.String())
			return false
		}
	}

	if server.readingsLimiter == nil {
		t.Fatal("readingsLimiter not initialized")
	}
	// 突发容量内全部放行
	for i := 0; i < readingsBurstLimit; i++ {
		if !allow() {
			t.Fatalf("burst request %d should pass", i)
		}
	}
	// 同一 IP 超过突发容量后应 429
	if allow() {
		t.Fatal("over-quota request must be rejected")
	}
	// 不同 IP 不受影响
	req := httptest.NewRequest(http.MethodGet, "/api/readings", nil)
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("other IP status = %d, want 200", rec.Code)
	}
}

// ---- /api/events 每 IP 并发上限 ----

func TestReadingsSemaphoreQuickFailsWhenFull(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	// 占满信号量，模拟重查询并发高峰
	for i := 0; i < maxConcurrentReadings; i++ {
		server.readingsSem <- struct{}{}
	}
	defer func() {
		// 排空剩余令牌，避免影响其它测试
		for {
			select {
			case <-server.readingsSem:
			default:
				return
			}
		}
	}()

	// 其后的请求应快速失败为 503，而不是排队拖慢整个连接池
	start := time.Now()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/readings?days=7", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("busy status = %d body=%s, want 503", rec.Code, rec.Body.String())
	}
	if elapsed := time.Since(start); elapsed > 2*readingsSemWait {
		t.Fatalf("busy request took %v, want quick failure", elapsed)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Fatal("503 must carry Retry-After")
	}

	// 释放一个槽位后应恢复
	<-server.readingsSem
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/readings?days=7", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("after release status = %d, want 200", rec.Code)
	}
}

// statusRecorder 记录 WriteHeader 的状态码，支持 Flush/Unwrap，
// 用于在并发的 SSE 处理中观察初始状态。
type statusRecorder struct {
	http.ResponseWriter
	mu   sync.Mutex
	code int
}

func (w *statusRecorder) writeHeader(code int) {
	w.mu.Lock()
	w.code = code
	w.mu.Unlock()
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) WriteHeader(code int) { w.writeHeader(code) }

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.writeHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusRecorder) status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.code
}

func startSSE(t *testing.T, server *Server, remoteAddr string) (*statusRecorder, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	req.RemoteAddr = remoteAddr
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	return rec, cancel, done
}

func waitStatus(t *testing.T, rec *statusRecorder) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if code := rec.status(); code != 0 {
			return code
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for SSE status")
	return 0
}

func TestSSEEnforcesPerIPConcurrentLimit(t *testing.T) {
	server := newTestServer(t)
	const ip = "192.0.2.55:1234"

	recs := make([]*statusRecorder, 0, maxSSEPerIP+1)
	cancels := make([]context.CancelFunc, 0, maxSSEPerIP+1)
	doned := make([]<-chan struct{}, 0, maxSSEPerIP+1)
	for i := 0; i < maxSSEPerIP+1; i++ {
		rec, cancel, done := startSSE(t, server, ip)
		recs = append(recs, rec)
		cancels = append(cancels, cancel)
		doned = append(doned, done)
	}

	ok, limited := 0, 0
	for _, rec := range recs {
		switch waitStatus(t, rec) {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected SSE status %d", rec.status())
		}
	}
	if ok != maxSSEPerIP {
		t.Fatalf("accepted = %d, want %d", ok, maxSSEPerIP)
	}
	if limited != 1 {
		t.Fatalf("rejected = %d, want 1", limited)
	}

	// 释放全部连接后，同一 IP 应能重新连接
	for _, cancel := range cancels {
		cancel()
	}
	for _, done := range doned {
		<-done
	}
	rec, cancel, done := startSSE(t, server, ip)
	if code := waitStatus(t, rec); code != http.StatusOK {
		t.Fatalf("reconnect after release status = %d, want 200", code)
	}
	cancel()
	<-done
}

func TestSSEAllowedForDifferentIPs(t *testing.T) {
	server := newTestServer(t)
	// 第一组：不同 IP 各占一条连接，全部应放行（配额互相独立）
	ips := []string{"10.0.0.1:1", "10.0.0.2:1", "10.0.0.3:1", "10.0.0.4:1"}
	var recs []*statusRecorder
	var cancels []context.CancelFunc
	var doned []<-chan struct{}
	for _, ip := range ips {
		rec, cancel, done := startSSE(t, server, ip)
		recs = append(recs, rec)
		cancels = append(cancels, cancel)
		doned = append(doned, done)
	}
	// 第二组：同一个 IP 占满自己的配额（maxSSEPerIP 条）
	fullIP := "10.9.9.9:1"
	for i := 0; i < maxSSEPerIP; i++ {
		rec, cancel, done := startSSE(t, server, fullIP)
		recs = append(recs, rec)
		cancels = append(cancels, cancel)
		doned = append(doned, done)
	}
	// 所有连接都应成功（第一组 4 个不同 IP + 第二组同一 IP 的 8 条）
	for _, rec := range recs {
		if code := waitStatus(t, rec); code != http.StatusOK {
			t.Fatalf("SSE status = %d, want 200", code)
		}
	}
	// 确认满配额 IP 的第 9 条被拒
	rec, cancel, done := startSSE(t, server, fullIP)
	if code := waitStatus(t, rec); code != http.StatusTooManyRequests {
		t.Fatalf("over-quota IP status = %d, want 429", code)
	}
	cancel()
	<-done
	// 释放所有连接
	for _, cancel := range cancels {
		cancel()
	}
	for _, done := range doned {
		<-done
	}
}

// ---- /api/health TTL 缓存 ----

func TestHealthTTL(t *testing.T) {
	var h healthTTL
	now := time.Unix(2000, 0)
	// 空缓存不新鲜
	if _, fresh := h.get(now); fresh {
		t.Fatal("empty cache must not be fresh")
	}
	h.set(now, true)
	if healthy, fresh := h.get(now.Add(2 * time.Second)); !fresh || !healthy {
		t.Fatalf("cache within TTL: healthy=%v fresh=%v", healthy, fresh)
	}
	if _, fresh := h.get(now.Add(4 * time.Second)); fresh {
		t.Fatal("cache must expire after TTL")
	}
}

// ---- HSTS ----

func TestHSTSBehindTLSProxy(t *testing.T) {
	server := newTestServer(t)
	// 反代（Caddy）终止 TLS 后以 X-Forwarded-Proto: https 转发
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	server.Handler().ServeHTTP(rec, req)
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("HSTS behind proxy = %q", got)
	}
	// 纯 HTTP 直连不发送 HSTS
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS on plaintext = %q, want empty", got)
	}
}

// ---- 404 状态码 ----

func TestUnknownPathReturnsReal404Status(t *testing.T) {
	server := newTestServer(t)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !stringsHasPrefix(ct, "text/html") {
		t.Fatalf("404 content type = %q", ct)
	}

	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown api status = %d, want 404", rec.Code)
	}

	// 非监控宿舍的 /room/ 也返回 404 状态码
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/room/N/E/X", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown room status = %d, want 404", rec.Code)
	}
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
