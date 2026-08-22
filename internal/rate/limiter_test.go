package rate

import (
	"sync"
	"testing"
)

func TestNewLimiter(t *testing.T) {
	l := NewLimiter(10)
	if l == nil {
		t.Fatal("expected non-nil")
	}
	if l.rate != 10 {
		t.Fatalf("expected rate 10, got %d", l.rate)
	}
}

func TestLimiterAllow(t *testing.T) {
	t.Run("zero rate means unlimited", func(t *testing.T) {
		l := NewLimiter(0)
		for i := 0; i < 100; i++ {
			if !l.Allow() {
				t.Fatal("zero rate should always allow")
			}
		}
	})

	t.Run("negative rate means unlimited", func(t *testing.T) {
		l := NewLimiter(-1)
		if !l.Allow() {
			t.Fatal("negative rate should always allow")
		}
	})

	t.Run("honors rate limit", func(t *testing.T) {
		l := NewLimiter(5)
		// 5 requests should be allowed
		for i := 0; i < 5; i++ {
			if !l.Allow() {
				t.Fatalf("request %d should be allowed", i)
			}
		}
		// 6th should be denied
		if l.Allow() {
			t.Fatal("6th request should be denied")
		}
	})

	t.Run("refills after window", func(t *testing.T) {
		l := NewLimiter(1)
		if !l.Allow() {
			t.Fatal("first request should be allowed")
		}
		if l.Allow() {
			t.Fatal("second request should be denied")
		}
		// Set times to be empty (simulate time passing)
		l.mu.Lock()
		l.times = l.times[:0]
		l.mu.Unlock()
		if !l.Allow() {
			t.Fatal("after reset, request should be allowed")
		}
	})
}

func TestLimiterConcurrent(t *testing.T) {
	l := NewLimiter(50)
	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	if allowed > 50 {
		t.Fatalf("expected at most 50 allowed, got %d", allowed)
	}
	mu.Unlock()
}

func TestSetRate(t *testing.T) {
	l := NewLimiter(5)
	for i := 0; i < 5; i++ {
		l.Allow()
	}
	l.SetRate(10)
	// After raising rate, more requests should be allowed
	for i := 0; i < 5; i++ {
		if !l.Allow() {
			t.Fatalf("after raising rate, request %d should be allowed", i)
		}
	}
}

func TestReset(t *testing.T) {
	l := NewLimiter(3)
	for i := 0; i < 3; i++ {
		l.Allow()
	}
	if l.Allow() {
		t.Fatal("should be denied")
	}
	l.Reset()
	for i := 0; i < 3; i++ {
		if !l.Allow() {
			t.Fatalf("after reset, request %d should be allowed", i)
		}
	}
}

func TestRate(t *testing.T) {
	l := NewLimiter(42)
	if l.Rate() != 42 {
		t.Fatalf("expected rate 42, got %d", l.Rate())
	}
	l.SetRate(10)
	if l.Rate() != 10 {
		t.Fatalf("expected rate 10, got %d", l.Rate())
	}
}