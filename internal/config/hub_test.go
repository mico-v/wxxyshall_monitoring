package config

import (
	"os"
	"path/filepath"
	"testing"
)

func testConfig() *Config {
	return &Config{
		Username: "20260001", Port: 8080, BaseURL: DefaultBaseURL,
		PollIntervalMin: 60, RateLimitPerMinute: 30,
		Targets: []Target{{FeeItemID: 409, AppID: 34, Campus: "A", Building: "B", Room: "C", Label: "one"}},
	}
}

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	t.Setenv("ELEc_DIR", t.TempDir())
	if err := SaveConfig(testConfig()); err != nil {
		t.Fatal(err)
	}
	hub, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	return hub
}

func TestHubSnapshotsAreDeepCopies(t *testing.T) {
	hub := newTestHub(t)
	cfg := hub.Config()
	cfg.Username = "changed"
	cfg.Targets[0].Label = "changed"

	got := hub.Config()
	if got.Username != "20260001" || got.Targets[0].Label != "one" {
		t.Fatalf("hub snapshot was mutated: %+v", got)
	}
}

func TestHubReloadDetectsContentAndTokenDeletion(t *testing.T) {
	hub := newTestHub(t)

	cfg := testConfig()
	cfg.PollIntervalMin = 17
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfgChanged, tokChanged, err := hub.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if !cfgChanged || tokChanged || hub.Config().PollIntervalMin != 17 {
		t.Fatalf("unexpected reload result: cfg=%v tok=%v config=%+v", cfgChanged, tokChanged, hub.Config())
	}

	tok := &Token{AccessToken: "secret", ExpiresIn: 100, LoginTime: 1, Sno: "20260001", Source: "test"}
	if err := SaveToken(tok); err != nil {
		t.Fatal(err)
	}
	_, tokChanged, err = hub.Reload()
	if err != nil || !tokChanged || hub.Token() == nil {
		t.Fatalf("token reload failed: changed=%v err=%v", tokChanged, err)
	}
	if err := os.Remove(TokenPath()); err != nil {
		t.Fatal(err)
	}
	_, tokChanged, err = hub.Reload()
	if err != nil || !tokChanged || hub.Token() != nil {
		t.Fatalf("token deletion reload failed: changed=%v err=%v token=%+v", tokChanged, err, hub.Token())
	}
}

func TestHubUpdateNormalizesAndWritesRestrictedFile(t *testing.T) {
	hub := newTestHub(t)
	cfg, err := hub.UpdateConfig(func(cfg *Config) error {
		cfg.BaseURL = DefaultBaseURL + "/"
		cfg.Targets[0].Label = "  label  "
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != DefaultBaseURL || cfg.Targets[0].Label != "label" {
		t.Fatalf("config not normalized: %+v", cfg)
	}
	info, err := os.Stat(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0640 {
		t.Fatalf("config mode = %o, want 640", got)
	}
	if filepath.Base(ConfigPath()) != "config.json" {
		t.Fatal("unexpected config path")
	}
}

func TestHubUpdateMergesWithLatestExternalConfig(t *testing.T) {
	hub := newTestHub(t)
	external := testConfig()
	external.PollIntervalMin = 17
	if err := SaveConfig(external); err != nil {
		t.Fatal(err)
	}

	updated, err := hub.UpdateConfig(func(cfg *Config) error {
		cfg.RateLimitPerMinute = 60
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.PollIntervalMin != 17 || updated.RateLimitPerMinute != 60 {
		t.Fatalf("external update was overwritten: %+v", updated)
	}
}

func TestLoadConfigRejectsUnknownFieldsButAcceptsLegacyAdminKey(t *testing.T) {
	t.Setenv("ELEc_DIR", t.TempDir())
	if err := os.MkdirAll(DataDir(), 0750); err != nil {
		t.Fatal(err)
	}
	legacy := `{"username":"u","port":8080,"base_url":"https://example.com","targets":[],"poll_interval_minutes":60,"rate_limit_per_minute":30,"admin_key":"old"}`
	if err := os.WriteFile(ConfigPath(), []byte(legacy), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err != nil {
		t.Fatalf("legacy admin_key should be ignored: %v", err)
	}
	unknown := `{"username":"u","port":8080,"base_url":"https://example.com","targets":[],"poll_interval_minutes":60,"rate_limit_per_minute":30,"typo":1}`
	if err := os.WriteFile(ConfigPath(), []byte(unknown), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Fatal("unknown field should be rejected")
	}
}
