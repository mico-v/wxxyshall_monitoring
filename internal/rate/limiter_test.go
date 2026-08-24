package rate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNewLimiterNormalizesRate(t *testing.T) {
	for _, rate := range []int{0, -1} {
		l := NewLimiter(rate)
		if got := l.Rate(); got != 1 {
			t.Fatalf("NewLimiter(%d).Rate() = %d, want 1", rate, got)
		}
	}
}

func TestIntervalRoundsUp(t *testing.T) {
	l := NewLimiter(7)
	if got := l.Interval() * 7; got < time.Minute {
		t.Fatalf("7 intervals total %v, want at least one minute", got)
	}
}

func TestLimiterWaitStrictSpacing(t *testing.T) {
	l := NewLimiter(6000) // 10ms
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var times []time.Time
	for i := 0; i < 3; i++ {
		if err := l.Wait(ctx); err != nil {
			t.Fatalf("Wait(%d): %v", i, err)
		}
		times = append(times, time.Now())
	}
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < 9*time.Millisecond {
			t.Fatalf("gap %d = %v, want at least 9ms", i, gap)
		}
	}
}

func TestLimiterConcurrentWaitsAreSerialized(t *testing.T) {
	l := NewLimiter(6000) // 10ms
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	const workers = 4
	times := make([]time.Time, 0, workers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Wait(ctx); err != nil {
				t.Errorf("Wait: %v", err)
				return
			}
			mu.Lock()
			times = append(times, time.Now())
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(times) != workers {
		t.Fatalf("got %d successful waits, want %d", len(times), workers)
	}
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < 9*time.Millisecond {
			t.Fatalf("concurrent gap %d = %v, want at least 9ms", i, gap)
		}
	}
}

func TestLimiterWaitCancellationDoesNotConsumeSlot(t *testing.T) {
	l := NewLimiter(1)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait error = %v, want deadline exceeded", err)
	}

	l.Reset()
	start := time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("first wait after reset took %v", elapsed)
	}
}

func TestSetRateWakesWaiter(t *testing.T) {
	l := NewLimiter(1)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- l.Wait(context.Background()) }()
	time.Sleep(10 * time.Millisecond)
	l.SetRate(6000)

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("waiter was not woken after rate update")
	}
	if got := l.Rate(); got != 6000 {
		t.Fatalf("Rate() = %d, want 6000", got)
	}
}

func TestResetAllowsImmediateWait(t *testing.T) {
	l := NewLimiter(1)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.Reset()
	start := time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("Wait after Reset took %v", elapsed)
	}
}
