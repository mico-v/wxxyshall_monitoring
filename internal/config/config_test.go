package config

import (
	"os"
	"testing"
	"time"
)

func TestTargetKey(t *testing.T) {
	target := Target{
		Campus:   "江枫",
		Building: "1号楼",
		Room:     "101",
	}
	key := target.Key()
	expected := "江枫|1号楼|101"
	if key != expected {
		t.Fatalf("expected %q, got %q", expected, key)
	}
}

func boolPtr(value bool) *bool { return &value }
func intPtr(value int) *int    { return &value }

func TestTargetWebVisibilityAndPollInterval(t *testing.T) {
	global := 60
	visible := Target{}
	if !visible.IsShownInWeb() {
		t.Fatal("omitted show_in_web should default to visible")
	}
	hidden := Target{ShowInWeb: boolPtr(false), PollIntervalMin: intPtr(15)}
	if hidden.IsShownInWeb() {
		t.Fatal("explicit show_in_web=false should be hidden")
	}
	if got := hidden.PollInterval(global); got != 15*time.Minute {
		t.Fatalf("target interval = %s, want 15m", got)
	}
	if got := visible.PollInterval(global); got != time.Hour {
		t.Fatalf("global fallback interval = %s, want 1h", got)
	}
}

func TestConfigGetWebTargets(t *testing.T) {
	cfg := &Config{Targets: []Target{
		{Campus: "A", Building: "B", Room: "1"},
		{Campus: "A", Building: "B", Room: "2", ShowInWeb: boolPtr(false)},
	}}
	targets := cfg.GetWebTargets()
	if len(targets) != 1 || targets[0].Room != "1" {
		t.Fatalf("web targets = %+v", targets)
	}
}

func TestTargetPollIntervalValidation(t *testing.T) {
	cfg := &Config{
		Username: "u", Port: 8080, BaseURL: DefaultBaseURL,
		PollIntervalMin: 60, RateLimitPerMinute: 30,
		Targets: []Target{{FeeItemID: 409, AppID: 34, Campus: "A", Building: "B", Room: "C", PollIntervalMin: intPtr(0)}},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("zero target poll interval should be rejected")
	}
}

func TestTargetDisplayLabel(t *testing.T) {
	t.Run("with label", func(t *testing.T) {
		target := Target{
			Campus:   "江枫",
			Building: "1号楼",
			Room:     "101",
			Label:    "我的宿舍",
		}
		if got := target.DisplayLabel(); got != "我的宿舍" {
			t.Fatalf("expected '我的宿舍', got %q", got)
		}
	})

	t.Run("without label uses key", func(t *testing.T) {
		target := Target{
			Campus:   "江枫",
			Building: "1号楼",
			Room:     "101",
		}
		got := target.DisplayLabel()
		expected := "江枫|1号楼|101"
		if got != expected {
			t.Fatalf("expected %q, got %q", expected, got)
		}
	})
}

func TestConfigGetTargets(t *testing.T) {
	t.Run("returns targets", func(t *testing.T) {
		cfg := &Config{
			Targets: []Target{
				{Campus: "A", Building: "B", Room: "C"},
			},
		}
		targets := cfg.GetTargets()
		if len(targets) != 1 {
			t.Fatalf("expected 1 target, got %d", len(targets))
		}
	})

	t.Run("empty targets", func(t *testing.T) {
		cfg := &Config{}
		targets := cfg.GetTargets()
		if targets != nil {
			t.Fatal("expected nil for empty targets")
		}
	})
}

func TestConfigDefaults(t *testing.T) {
	// Save and restore env
	old := os.Getenv("ELEc_DIR")
	defer os.Setenv("ELEc_DIR", old)

	os.Setenv("ELEc_DIR", "/tmp/elec-test")
	got := DataDir()
	if got != "/tmp/elec-test/data" {
		t.Fatalf("expected /tmp/elec-test/data, got %q", got)
	}
}

func TestConfigNewDefaultsAndLegacyHomepageCompatibility(t *testing.T) {
	cfg, err := parseConfig([]byte(`{
  "username":"u","base_url":"https://example.com","targets":[],
  "poll_interval_minutes":60,"rate_limit_per_minute":30
}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 5009 {
		t.Fatalf("default port = %d, want 5009", cfg.Port)
	}
	if cfg.AdminAuthEnabled {
		t.Fatal("admin auth should default to disabled")
	}
	if !cfg.IsHomepageShown() {
		t.Fatal("omitted show_homepage should default to true")
	}
}
