package config

import (
	"os"
	"testing"
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