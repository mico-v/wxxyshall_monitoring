package charge

import (
	"testing"
)

func TestParseReading(t *testing.T) {
	t.Run("full reading", func(t *testing.T) {
		m := map[string]any{
			"surplusCharge": float64(123.45),
			"showData": map[string]any{
				"电表总用电量": "567.89",
				"当前余额":   "123.45",
			},
			"data": map[string]any{
				"extra": "info",
			},
		}
		r := ParseReading(m)
		if r == nil {
			t.Fatal("expected non-nil")
		}
		if r.SurplusCharge == nil || *r.SurplusCharge != 123.45 {
			t.Fatalf("expected surplus_charge 123.45, got %v", r.SurplusCharge)
		}
		if r.Show["电表总用电量"] != "567.89" {
			t.Fatalf("expected show 电表总用电量 567.89, got %s", r.Show["电表总用电量"])
		}
		if r.Show["当前余额"] != "123.45" {
			t.Fatalf("expected show 当前余额 123.45, got %s", r.Show["当前余额"])
		}
		if r.Raw["extra"] != "info" {
			t.Fatalf("expected raw extra info, got %v", r.Raw["extra"])
		}
	})

	t.Run("empty map", func(t *testing.T) {
		r := ParseReading(make(map[string]any))
		if r == nil {
			t.Fatal("expected non-nil")
		}
		if r.SurplusCharge != nil {
			t.Fatal("expected nil surplus_charge")
		}
		if len(r.Show) != 0 {
			t.Fatal("expected empty show")
		}
	})

	t.Run("nil map", func(t *testing.T) {
		r := ParseReading(nil)
		if r == nil {
			t.Fatal("expected non-nil")
		}
	})
}

func TestToFloat(t *testing.T) {
	tests := []struct {
		input    any
		expected *float64
	}{
		{float64(42.5), ptr(42.5)},
		{int(42), ptr(42.0)},
		{int64(42), ptr(42.0)},
		{"123.45", ptr(123.45)},
		{"not-a-number", nil},
		{nil, nil},
		{true, nil},
	}

	for _, tt := range tests {
		got := toFloat(tt.input)
		if tt.expected == nil {
			if got != nil {
				t.Errorf("toFloat(%v) = %v, want nil", tt.input, got)
			}
		} else {
			if got == nil {
				t.Errorf("toFloat(%v) = nil, want %v", tt.input, *tt.expected)
			} else if *got != *tt.expected {
				t.Errorf("toFloat(%v) = %v, want %v", tt.input, *got, *tt.expected)
			}
		}
	}
}

func TestTotalUsage(t *testing.T) {
	t.Run("has total usage", func(t *testing.T) {
		r := &Reading{
			Show: map[string]string{"电表总用电量": "789.01"},
		}
		got := r.TotalUsage()
		if got == nil || *got != 789.01 {
			t.Fatalf("expected 789.01, got %v", got)
		}
	})

	t.Run("nil reading", func(t *testing.T) {
		var r *Reading
		got := r.TotalUsage()
		if got != nil {
			t.Fatal("expected nil for nil reading")
		}
	})

	t.Run("missing total usage", func(t *testing.T) {
		r := &Reading{Show: map[string]string{"other": "value"}}
		got := r.TotalUsage()
		if got != nil {
			t.Fatal("expected nil when key missing")
		}
	})

	t.Run("invalid number", func(t *testing.T) {
		r := &Reading{Show: map[string]string{"电表总用电量": "abc"}}
		got := r.TotalUsage()
		if got != nil {
			t.Fatal("expected nil for invalid number")
		}
	})
}

func TestExtractOptions(t *testing.T) {
	t.Run("valid data", func(t *testing.T) {
		m := map[string]any{
			"data": []any{
				map[string]any{"value": "1", "name": "校区1"},
				map[string]any{"value": "2", "name": "校区2"},
			},
		}
		opts, err := extractOptions(m)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opts) != 2 {
			t.Fatalf("expected 2 options, got %d", len(opts))
		}
		if opts[0].Value != "1" || opts[0].Name != "校区1" {
			t.Fatalf("expected {1 校区1}, got %+v", opts[0])
		}
	})

	t.Run("missing data", func(t *testing.T) {
		_, err := extractOptions(map[string]any{})
		if err == nil {
			t.Fatal("expected error for missing data")
		}
	})

	t.Run("non-array data", func(t *testing.T) {
		_, err := extractOptions(map[string]any{"data": "string"})
		if err == nil {
			t.Fatal("expected error for non-array data")
		}
	})
}

func ptr(f float64) *float64 { return &f }
