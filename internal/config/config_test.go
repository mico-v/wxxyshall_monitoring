package config

import (
	"io"
	"os"
	"strings"
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
	old := os.Getenv(EnvDir)
	defer os.Setenv(EnvDir, old)

	os.Setenv(EnvDir, "/tmp/elec-test")
	got := DataDir()
	if got != "/tmp/elec-test/data" {
		t.Fatalf("expected /tmp/elec-test/data, got %q", got)
	}
}

func TestConfigNewDefaults(t *testing.T) {
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
	if !cfg.IsPublicAddAllowed() {
		t.Fatal("omitted allow_public_add_target should default to true")
	}
}

func TestLegacyAccessConfigMigration(t *testing.T) {
	cfg, err := parseConfig([]byte(`{
  "username":"u","base_url":"https://example.com","targets":[],
  "poll_interval_minutes":60,"rate_limit_per_minute":30,
  "mode":"server","show_homepage":false,"allow_guest_add_target":true,
  "admin_auth_enabled":false,"show_collect_button":"never"
}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsPublicAddAllowed() {
		t.Fatal("legacy public homepage configuration should migrate to public additions enabled")
	}
}

func TestLegacyVisibleHomepageDoesNotOpenPublicAdds(t *testing.T) {
	cfg, err := parseConfig([]byte(`{
  "username":"u","base_url":"https://example.com","targets":[],
  "poll_interval_minutes":60,"rate_limit_per_minute":30,
  "show_homepage":true,"allow_guest_add_target":true,
  "admin_auth_enabled":false
}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsPublicAddAllowed() {
		t.Fatal("legacy public aggregate homepage should not enable public additions")
	}
}

func TestPublicAddTargetOption(t *testing.T) {
	cfg, err := parseConfig([]byte(`{
  "username":"u","base_url":"https://example.com","targets":[],
  "poll_interval_minutes":60,"rate_limit_per_minute":30,
  "allow_public_add_target":false
}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsPublicAddAllowed() {
		t.Fatal("allow_public_add_target=false should disable public additions")
	}
	clone := cfg.Clone()
	if clone.IsPublicAddAllowed() {
		t.Fatal("Clone should keep the public-add setting")
	}
	if cfg.AllowPublicAddTarget == clone.AllowPublicAddTarget {
		t.Fatal("Clone should deep-copy the public-add pointer")
	}
}

func TestWebhookConfigDefaultsAndValidation(t *testing.T) {
	cfg, err := parseConfig([]byte(`{
  "username":"u","base_url":"https://example.com","targets":[],
  "poll_interval_minutes":60,"rate_limit_per_minute":30,
  "webhook":{"enabled":true,"url":"http://127.0.0.1:9966/send","token":"secret","body":{"custom":"value"}}
}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Webhook.NotifyMode != DefaultWebhookMode {
		t.Fatalf("webhook notify mode = %q, want %q", cfg.Webhook.NotifyMode, DefaultWebhookMode)
	}
	if cfg.Webhook.LowBalanceThreshold != DefaultWebhookThreshold {
		t.Fatalf("webhook threshold = %v, want %v", cfg.Webhook.LowBalanceThreshold, DefaultWebhookThreshold)
	}
	if cfg.Webhook.Body["custom"] != "value" {
		t.Fatalf("webhook body = %#v", cfg.Webhook.Body)
	}

	cfg.Webhook.NotifyMode = "invalid"
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("invalid webhook notify mode should be rejected")
	}
}

func TestLegacyWebhookFieldsMigrateToBody(t *testing.T) {
	cfg, err := parseConfig([]byte(`{
  "username":"u","base_url":"https://example.com","targets":[],
  "poll_interval_minutes":60,"rate_limit_per_minute":30,
  "webhook":{"enabled":true,"url":"http://example.com/send","token":"secret","umo":"alice","content_template":"{{room}}"}
}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Webhook.Body["content"]; got != "{{room}}" {
		t.Fatalf("migrated content = %#v", got)
	}
	if got := cfg.Webhook.Body["umo"]; got != "alice" {
		t.Fatalf("migrated umo = %#v", got)
	}
	if cfg.Webhook.UMO != "" || cfg.Webhook.ContentTemplate != "" {
		t.Fatalf("legacy fields should be cleared after migration: %+v", cfg.Webhook)
	}
}

func TestDisabledWebhookMayOmitConnectionDetails(t *testing.T) {
	cfg := testConfigForWebhook()
	cfg.Webhook = WebhookConfig{}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("disabled empty webhook should be valid: %v", err)
	}
}

func TestNormalizeFillsTargetFieldDefaults(t *testing.T) {
	cfg, err := parseConfig([]byte(`{
  "username":"u","base_url":"https://example.com","targets":[{"campus":"A","building":"B","room":"C"}],
  "poll_interval_minutes":60,"rate_limit_per_minute":30
}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	target := cfg.Targets[0]
	if target.ShowInWeb == nil || !*target.ShowInWeb {
		t.Fatal("show_in_web should default to true")
	}
	if target.PollIntervalMin == nil || *target.PollIntervalMin != 60 {
		t.Fatalf("poll_interval_minutes default = %v, want 60", target.PollIntervalMin)
	}
	if target.FeeItemID != DefaultFeeItemID || target.AppID != DefaultAppID {
		t.Fatalf("feeitemid/appId defaults = %d/%d", target.FeeItemID, target.AppID)
	}
	if target.Label != "A/B/C" {
		t.Fatalf("label default = %q, want A/B/C", target.Label)
	}
	// 无显式通知配置时保持空串，运行时继承全局 webhook 规则。
	if target.NotifyMode != "" || target.NotifyTime != "" || target.Webhook != nil {
		t.Fatalf("notify fields should stay empty: %+v", target)
	}
}

func TestSaveConfigWritesAllTargetFields(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvDir, root)
	cfg := &Config{
		Username: "u", Port: 8080, BaseURL: "https://example.com",
		PollIntervalMin: 60, RateLimitPerMinute: 30,
		Targets: []Target{{Campus: "A", Building: "B", Room: "C"}},
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"allow_public_add_target": true`,
		`"feeitemid": 409`, `"appId": 34`,
		`"campus": "A"`, `"building": "B"`, `"room": "C"`,
		`"label": "A/B/C"`, `"show_in_web": true`, `"poll_interval_minutes": 60`,
		`"notify_mode": ""`, `"notify_time": ""`, `"webhook": null`,
	} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("config.json missing %s:\n%s", key, data)
		}
	}
	for _, removed := range []string{
		`"mode"`, `"show_homepage"`, `"allow_guest_add_target"`,
		`"admin_auth_enabled"`, `"show_collect_button"`,
	} {
		if strings.Contains(string(data), removed) {
			t.Fatalf("config.json still contains removed field %s:\n%s", removed, data)
		}
	}
}

func TestTargetNotificationSettings(t *testing.T) {
	cfg, err := parseConfig([]byte(`{
  "username":"u","base_url":"https://example.com","targets":[
    {"campus":"A","building":"B","room":"C","notify_mode":"daily"}
  ],"poll_interval_minutes":60,"rate_limit_per_minute":30
}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Targets[0].NotifyTime != DefaultTargetNotifyTime {
		t.Fatalf("target notify time = %q, want %q", cfg.Targets[0].NotifyTime, DefaultTargetNotifyTime)
	}

	cfg.Targets[0].NotifyMode = "invalid"
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("invalid target notify mode should be rejected")
	}
	cfg.Targets[0].NotifyMode = "daily"
	cfg.Targets[0].NotifyTime = "8:00"
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("non-HH:MM target notify time should be rejected")
	}
}

func TestTargetWebhookFallsBackOrOverridesGlobal(t *testing.T) {
	global := WebhookConfig{
		Enabled: true, URL: "http://global.test/send", Token: "global-token",
		Body: map[string]any{"content": "global"},
	}
	target := Target{Campus: "A", Building: "B", Room: "C"}
	if got := target.EffectiveWebhook(global); got.URL != global.URL || got.Token != global.Token || got.Body["content"] != "global" {
		t.Fatalf("target should inherit global webhook: %+v", got)
	}
	target.Webhook = &WebhookConfig{
		Enabled: true, URL: "http://room.test/send", Token: "room-token",
		Body: map[string]any{"custom": "room"},
	}
	got := target.EffectiveWebhook(global)
	if got.URL != "http://room.test/send" || got.Token != "room-token" || got.Body["custom"] != "room" {
		t.Fatalf("target webhook override not applied: %+v", got)
	}
}

func testConfigForWebhook() *Config {
	return &Config{
		Username: "u", Port: 8080, BaseURL: DefaultBaseURL,
		PollIntervalMin: 60, RateLimitPerMinute: 30,
	}
}

func TestDirFromEnvPrefersCanonicalName(t *testing.T) {
	t.Setenv(EnvDir, "/canonical")
	t.Setenv(legacyEnvDir, "/legacy")
	if got := DirFromEnv(); got != "/canonical" {
		t.Fatalf("ELEC_DIR 应优先于旧拼写，got %q", got)
	}
}

func TestDirFromEnvFallsBackToLegacyName(t *testing.T) {
	t.Setenv(EnvDir, "")
	t.Setenv(legacyEnvDir, "/legacy")

	// 旧拼写仍需生效（兼容既有部署），但要给出弃用提示。
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	got := DirFromEnv()
	os.Stderr = origStderr
	_ = w.Close()
	out, _ := io.ReadAll(r)
	_ = r.Close()

	if got != "/legacy" {
		t.Fatalf("旧拼写应继续生效，got %q", got)
	}
	if !strings.Contains(string(out), legacyEnvDir) {
		t.Fatalf("应打印弃用提示，got %q", string(out))
	}
}

func TestDirFromEnvEmpty(t *testing.T) {
	t.Setenv(EnvDir, "")
	t.Setenv(legacyEnvDir, "")
	if got := DirFromEnv(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestValidateBind(t *testing.T) {
	valid := []string{"", "127.0.0.1", "0.0.0.0", "::1", "::", "localhost", "elec.internal"}
	for _, bind := range valid {
		if err := validateBind(bind); err != nil {
			t.Errorf("bind %q 应合法，got %v", bind, err)
		}
	}
	invalid := []string{"127.0.0.1:8080", "192.168.1.1:80", "host name", "1.2.3.4/24"}
	for _, bind := range invalid {
		if err := validateBind(bind); err == nil {
			t.Errorf("bind %q 应被判为非法", bind)
		}
	}
}

func TestParseConfigBind(t *testing.T) {
	cfg, err := parseConfig([]byte(`{
  "username":"u","port":8080,"bind":" 127.0.0.1 ","base_url":"https://example.com",
  "targets":[],"poll_interval_minutes":60,"rate_limit_per_minute":30
}`), "test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bind != "127.0.0.1" {
		t.Fatalf("bind 应被 trim，got %q", cfg.Bind)
	}

	if _, err := parseConfig([]byte(`{
  "username":"u","port":8080,"bind":"127.0.0.1:8080","base_url":"https://example.com",
  "targets":[],"poll_interval_minutes":60,"rate_limit_per_minute":30
}`), "test"); err == nil {
		t.Fatal("带端口的 bind 应被拒绝")
	}
}

func TestSaveConfigOmitsEmptyBind(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvDir, root)
	cfg := &Config{
		Username: "u", Port: 8080, BaseURL: "https://example.com",
		PollIntervalMin: 60, RateLimitPerMinute: 30,
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"bind"`) {
		t.Fatalf("bind 为空时不应写出该字段: %s", raw)
	}
}
