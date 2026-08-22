package auth

import (
	"math"
	"testing"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

func TestExpiryEpoch(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		tok := &config.Token{LoginTime: 1000, ExpiresIn: 86400}
		got := ExpiryEpoch(tok)
		if got == nil {
			t.Fatal("expected non-nil")
		}
		if *got != 1000+86400 {
			t.Fatalf("expected %d, got %d", 1000+86400, *got)
		}
	})

	t.Run("nil token", func(t *testing.T) {
		if got := ExpiryEpoch(nil); got != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("zero login_time", func(t *testing.T) {
		tok := &config.Token{LoginTime: 0, ExpiresIn: 86400}
		if got := ExpiryEpoch(tok); got != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("zero expires_in", func(t *testing.T) {
		tok := &config.Token{LoginTime: 1000, ExpiresIn: 0}
		if got := ExpiryEpoch(tok); got != nil {
			t.Fatal("expected nil")
		}
	})
}

func TestSecondsLeft(t *testing.T) {
	t.Run("future expiry", func(t *testing.T) {
		now := time.Now().Unix()
		tok := &config.Token{LoginTime: now, ExpiresIn: 86400}
		left := SecondsLeft(tok)
		if left == nil {
			t.Fatal("expected non-nil")
		}
		if *left <= 0 || *left > 86401 {
			t.Fatalf("expected ~86400, got %f", *left)
		}
	})

	t.Run("expired", func(t *testing.T) {
		tok := &config.Token{LoginTime: 100, ExpiresIn: 1}
		left := SecondsLeft(tok)
		if left == nil {
			t.Fatal("expected non-nil")
		}
		if *left >= 0 {
			t.Fatalf("expected negative, got %f", *left)
		}
	})

	t.Run("nil token", func(t *testing.T) {
		if got := SecondsLeft(nil); got != nil {
			t.Fatal("expected nil")
		}
	})
}

func TestIsExpired(t *testing.T) {
	t.Run("not expired", func(t *testing.T) {
		now := time.Now().Unix()
		tok := &config.Token{LoginTime: now, ExpiresIn: 86400}
		if IsExpired(tok, 600) {
			t.Fatal("expected not expired")
		}
	})

	t.Run("expired", func(t *testing.T) {
		tok := &config.Token{LoginTime: 100, ExpiresIn: 1}
		if !IsExpired(tok, 600) {
			t.Fatal("expected expired")
		}
	})

	t.Run("nil token", func(t *testing.T) {
		if IsExpired(nil, 600) {
			t.Fatal("expected false for nil token")
		}
	})

	t.Run("default skew", func(t *testing.T) {
		now := time.Now().Unix()
		// 500s left, default skew is 600s -> expired
		tok := &config.Token{LoginTime: now, ExpiresIn: 500}
		if !IsExpired(tok, 0) {
			t.Fatal("expected expired with default skew")
		}
	})
}

func TestDaysLeft(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		now := time.Now().Unix()
		tok := &config.Token{LoginTime: now, ExpiresIn: 86400 * 2}
		days := DaysLeft(tok)
		if days != 2 {
			t.Fatalf("expected 2 days, got %d", days)
		}
	})

	t.Run("nil token", func(t *testing.T) {
		days := DaysLeft(nil)
		if days != math.MaxInt64 {
			t.Fatalf("expected MaxInt64, got %d", days)
		}
	})

	t.Run("expired returns 0", func(t *testing.T) {
		tok := &config.Token{LoginTime: 100, ExpiresIn: 1}
		days := DaysLeft(tok)
		if days != 0 {
			t.Fatalf("expected 0, got %d", days)
		}
	})
}