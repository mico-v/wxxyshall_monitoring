// Package config 管理配置文件和 token 文件的读写。
// 数据目录由 ELEc_DIR 环境变量指定；Linux 默认 /opt/elec/data，
// Windows 默认 %LOCALAPPDATA%/WxxyshallMonitoring/data。
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultBaseURL            = "https://wxxyshall.usts.edu.cn"
	DefaultPort               = 5009
	DefaultPollIntervalMin    = 60
	DefaultRateLimitPerMinute = 30
	DefaultFeeItemID          = 409
	DefaultAppID              = 34
	DefaultWebhookMode        = "low_balance"
	DefaultWebhookThreshold   = 10.0
	DefaultWebhookTemplate    = "【电费监控】{{label}} 当前余额：{{surplus_charge}}，采集时间：{{ts}}"
	DefaultTargetNotifyTime   = "08:00"
)

// Target 代表一个监控宿舍目标。
type Target struct {
	FeeItemID       int            `json:"feeitemid"`
	AppID           int            `json:"appId"`
	Campus          string         `json:"campus"`
	Building        string         `json:"building"`
	Room            string         `json:"room"`
	Label           string         `json:"label"`
	ShowInWeb       *bool          `json:"show_in_web,omitempty"`
	PollIntervalMin *int           `json:"poll_interval_minutes,omitempty"`
	NotifyMode      string         `json:"notify_mode,omitempty"`
	NotifyTime      string         `json:"notify_time,omitempty"`
	Webhook         *WebhookConfig `json:"webhook,omitempty"`
}

// WebhookConfig 是采集成功通知的配置。
type WebhookConfig struct {
	Enabled             bool           `json:"enabled"`
	URL                 string         `json:"url"`
	Token               string         `json:"token,omitempty"`
	NotifyMode          string         `json:"notify_mode"`
	LowBalanceThreshold float64        `json:"low_balance_threshold"`
	Body                map[string]any `json:"body"`

	// Deprecated compatibility fields. They are migrated into Body on load.
	UMO             string `json:"umo,omitempty"`
	ContentTemplate string `json:"content_template,omitempty"`
}

// IsShownInWeb reports whether this target is included in public web views.
func (t Target) IsShownInWeb() bool { return t.ShowInWeb == nil || *t.ShowInWeb }

// PollInterval returns the target override or the global interval.
func (t Target) PollInterval(global int) time.Duration {
	minutes := global
	if t.PollIntervalMin != nil && *t.PollIntervalMin > 0 {
		minutes = *t.PollIntervalMin
	}
	return time.Duration(minutes) * time.Minute
}

// Key 返回宿舍的稳定身份标识（不含 label）。
func (t Target) Key() string {
	return t.Campus + "|" + t.Building + "|" + t.Room
}

// DisplayLabel 返回宿舍的显示名称。
func (t Target) DisplayLabel() string {
	if t.Label != "" {
		return t.Label
	}
	return t.Key()
}

// EffectiveWebhook returns this target's webhook override, or the global
// webhook configuration when no target override is present.
func (t Target) EffectiveWebhook(global WebhookConfig) WebhookConfig {
	if t.Webhook == nil {
		global.Body = cloneJSONMap(global.Body)
		return global
	}
	webhook := *t.Webhook
	webhook.Body = cloneJSONMap(webhook.Body)
	return webhook
}

// Config 是 config.json 的完整映射。
type Config struct {
	Username           string        `json:"username"`
	Port               int           `json:"port"`
	BaseURL            string        `json:"base_url"`
	Targets            []Target      `json:"targets"`
	PollIntervalMin    int           `json:"poll_interval_minutes"`
	RateLimitPerMinute int           `json:"rate_limit_per_minute"`
	AdminAuthEnabled   bool          `json:"admin_auth_enabled"`
	ShowHomepage       *bool         `json:"show_homepage"`
	Webhook            WebhookConfig `json:"webhook"`
}

// IsHomepageShown reports whether the aggregate homepage is publicly visible.
// The pointer keeps older configuration files compatible: an omitted field defaults to true.
func (c *Config) IsHomepageShown() bool {
	return c == nil || c.ShowHomepage == nil || *c.ShowHomepage
}

// Clone 返回可安全交给调用方修改的深拷贝。
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Targets = append([]Target(nil), c.Targets...)
	if c.ShowHomepage != nil {
		value := *c.ShowHomepage
		cp.ShowHomepage = &value
	}
	cp.Webhook.Body = cloneJSONMap(c.Webhook.Body)
	for i := range cp.Targets {
		if c.Targets[i].ShowInWeb != nil {
			value := *c.Targets[i].ShowInWeb
			cp.Targets[i].ShowInWeb = &value
		}
		if c.Targets[i].PollIntervalMin != nil {
			value := *c.Targets[i].PollIntervalMin
			cp.Targets[i].PollIntervalMin = &value
		}
		if c.Targets[i].Webhook != nil {
			webhook := *c.Targets[i].Webhook
			webhook.Body = cloneJSONMap(c.Targets[i].Webhook.Body)
			cp.Targets[i].Webhook = &webhook
		}
	}
	return &cp
}

// GetTargets 返回监控目标列表副本，顺序与 config.json 完全一致。
func (c *Config) GetTargets() []Target {
	if c == nil || len(c.Targets) == 0 {
		return nil
	}
	return c.Clone().Targets
}

// GetWebTargets returns only targets enabled for public web display.
func (c *Config) GetWebTargets() []Target {
	if c == nil || len(c.Targets) == 0 {
		return nil
	}
	targets := make([]Target, 0, len(c.Targets))
	for _, target := range c.Targets {
		if target.IsShownInWeb() {
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	return targets
}

// DataDir 返回数据目录路径。
// 优先使用 ELEc_DIR 环境变量下的 data 子目录，否则使用平台默认目录。
func DataDir() string {
	if d := os.Getenv("ELEc_DIR"); d != "" {
		return filepath.Join(d, "data")
	}
	return defaultDataDir()
}

// ConfigPath 返回 config.json 的完整路径。
func ConfigPath() string {
	return filepath.Join(DataDir(), "config.json")
}

// TokenPath 返回 token.json 的完整路径。
func TokenPath() string {
	return filepath.Join(DataDir(), "token.json")
}

// DBPath 返回 electricity.db 的完整路径（SQLite 数据库）。
// Go 版与 Python 版共用同一 SQLite 数据库格式，数据可互相读。
func DBPath() string {
	return filepath.Join(DataDir(), "electricity.db")
}

// LoadConfig 读取并解析 config.json。
// 如果文件不存在或缺少关键字段，返回错误。
func LoadConfig() (*Config, error) {
	p := ConfigPath()
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件 %s 失败: %w\n请复制 config.example.json 为 config.json 并填入你的学号", p, err)
	}
	return parseConfig(data, p)
}

func parseConfig(data []byte, source string) (*Config, error) {
	// 兼容旧版本曾写入但从未生效的 admin_key；加载后不再保存该字段。
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err == nil {
		if _, ok := raw["admin_key"]; ok {
			delete(raw, "admin_key")
			cleaned, marshalErr := json.Marshal(raw)
			if marshalErr != nil {
				return nil, fmt.Errorf("解析配置文件 %s 失败: %w", source, marshalErr)
			}
			data = cleaned
		}
	}

	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败: %w", source, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("解析配置文件 %s 失败: 文件只能包含一个 JSON 对象", source)
	}
	normalizeConfig(&cfg)
	if err := ValidateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("配置文件 %s 无效: %w", source, err)
	}
	return &cfg, nil
}

// SaveConfig 将配置写回 config.json。
func SaveConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("配置不能为空")
	}
	cp := cfg.Clone()
	normalizeConfig(cp)
	if err := ValidateConfig(cp); err != nil {
		return err
	}
	p := ConfigPath()
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	return writeFileAtomic(p, append(data, '\n'), 0640)
}

// LoadToken 读取 token.json，文件不存在时返回 nil。
func LoadToken() (*Token, error) {
	p := TokenPath()
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 token 文件失败: %w", err)
	}
	return parseToken(data)
}

func parseToken(data []byte) (*Token, error) {
	var tok Token
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&tok); err != nil {
		return nil, fmt.Errorf("解析 token 文件失败: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("解析 token 文件失败: 文件只能包含一个 JSON 对象")
	}
	if err := normalizeAndValidateToken(&tok); err != nil {
		return nil, fmt.Errorf("token 文件无效: %w", err)
	}
	return &tok, nil
}

// SaveToken 将 token 写入 token.json。
func SaveToken(tok *Token) error {
	if tok == nil {
		return fmt.Errorf("token 不能为空")
	}
	cp := *tok
	if err := normalizeAndValidateToken(&cp); err != nil {
		return err
	}
	p := TokenPath()
	data, err := json.MarshalIndent(&cp, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 token 失败: %w", err)
	}
	return writeFileAtomic(p, append(data, '\n'), 0600)
}

func normalizeAndValidateToken(tok *Token) error {
	tok.AccessToken = strings.TrimSpace(tok.AccessToken)
	tok.RefreshToken = strings.TrimSpace(tok.RefreshToken)
	tok.Sno = strings.TrimSpace(tok.Sno)
	tok.Source = strings.TrimSpace(tok.Source)
	if tok.AccessToken == "" {
		return fmt.Errorf("access_token 不能为空")
	}
	if len(tok.AccessToken) > 64<<10 || len(tok.RefreshToken) > 64<<10 {
		return fmt.Errorf("token 字段过长")
	}
	if tok.ExpiresIn < 0 || tok.LoginTime < 0 {
		return fmt.Errorf("expires_in/login_time 不能为负数")
	}
	if len(tok.Sno) > 128 || len(tok.Source) > 128 {
		return fmt.Errorf("sno/source 字段过长")
	}
	return nil
}

// ValidateConfig 校验所有可配置字段及目标顺序中的重复项。
func ValidateConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("配置不能为空")
	}
	if strings.TrimSpace(cfg.Username) == "" {
		return fmt.Errorf("username(学号)不能为空")
	}
	if len(cfg.Username) > 128 || len(cfg.BaseURL) > 2048 {
		return fmt.Errorf("username/base_url 字段过长")
	}
	if cfg.Port < 1024 || cfg.Port > 65535 {
		return fmt.Errorf("port 必须在 1024..65535 之间")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("base_url 必须是有效的 http/https 地址")
	}
	if cfg.PollIntervalMin < 1 || cfg.PollIntervalMin > 7*24*60 {
		return fmt.Errorf("poll_interval_minutes 必须在 1..10080 之间")
	}
	if cfg.RateLimitPerMinute < 1 || cfg.RateLimitPerMinute > 600 {
		return fmt.Errorf("rate_limit_per_minute 必须在 1..600 之间")
	}
	if err := validateWebhook(cfg.Webhook); err != nil {
		return err
	}
	if len(cfg.Targets) > 1000 {
		return fmt.Errorf("targets 最多允许 1000 项")
	}
	seen := make(map[string]struct{}, len(cfg.Targets))
	for i, target := range cfg.Targets {
		if target.FeeItemID <= 0 || target.AppID <= 0 {
			return fmt.Errorf("targets[%d] 的 feeitemid/appId 必须为正整数", i)
		}
		if strings.TrimSpace(target.Campus) == "" || strings.TrimSpace(target.Building) == "" || strings.TrimSpace(target.Room) == "" {
			return fmt.Errorf("targets[%d] 的 campus/building/room 不能为空", i)
		}
		if len(target.Campus) > 128 || len(target.Building) > 128 || len(target.Room) > 128 || len(target.Label) > 128 {
			return fmt.Errorf("targets[%d] 的字段过长", i)
		}
		if target.PollIntervalMin != nil && (*target.PollIntervalMin < 1 || *target.PollIntervalMin > 7*24*60) {
			return fmt.Errorf("targets[%d] 的 poll_interval_minutes 必须在 1..10080 之间", i)
		}
		if target.NotifyMode != "" && target.NotifyMode != "none" && target.NotifyMode != "daily" && target.NotifyMode != "alert" {
			return fmt.Errorf("targets[%d] 的 notify_mode 必须是 none、daily 或 alert", i)
		}
		if target.NotifyTime != "" {
			if len(target.NotifyTime) != len("15:04") {
				return fmt.Errorf("targets[%d] 的 notify_time 必须是 HH:MM 格式", i)
			}
			if _, err := time.Parse("15:04", target.NotifyTime); err != nil {
				return fmt.Errorf("targets[%d] 的 notify_time 必须是 HH:MM 格式", i)
			}
		}
		if target.Webhook != nil {
			if err := validateWebhook(*target.Webhook); err != nil {
				return fmt.Errorf("targets[%d].webhook 无效: %w", i, err)
			}
		}
		if _, ok := seen[target.Key()]; ok {
			return fmt.Errorf("targets[%d] 与前面的宿舍重复: %s", i, target.Key())
		}
		seen[target.Key()] = struct{}{}
	}
	return nil
}

func normalizeConfig(cfg *Config) {
	cfg.Username = strings.TrimSpace(cfg.Username)
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.PollIntervalMin == 0 {
		cfg.PollIntervalMin = DefaultPollIntervalMin
	}
	if cfg.RateLimitPerMinute == 0 {
		cfg.RateLimitPerMinute = DefaultRateLimitPerMinute
	}
	if cfg.ShowHomepage == nil {
		shown := true
		cfg.ShowHomepage = &shown
	}
	normalizeWebhook(&cfg.Webhook)
	for i := range cfg.Targets {
		t := &cfg.Targets[i]
		if t.FeeItemID == 0 {
			t.FeeItemID = DefaultFeeItemID
		}
		if t.AppID == 0 {
			t.AppID = DefaultAppID
		}
		t.Campus = strings.TrimSpace(t.Campus)
		t.Building = strings.TrimSpace(t.Building)
		t.Room = strings.TrimSpace(t.Room)
		t.Label = strings.TrimSpace(t.Label)
		t.NotifyMode = strings.TrimSpace(t.NotifyMode)
		t.NotifyTime = strings.TrimSpace(t.NotifyTime)
		if t.NotifyMode == "daily" && t.NotifyTime == "" {
			t.NotifyTime = DefaultTargetNotifyTime
		}
		if t.Webhook != nil {
			normalizeWebhook(t.Webhook)
		}
		if t.Label == "" {
			t.Label = t.Campus + "/" + t.Building + "/" + t.Room
		}
	}
}

func normalizeWebhook(webhook *WebhookConfig) {
	if webhook == nil {
		return
	}
	webhook.URL = strings.TrimSpace(webhook.URL)
	webhook.Token = strings.TrimSpace(webhook.Token)
	webhook.UMO = strings.TrimSpace(webhook.UMO)
	webhook.ContentTemplate = strings.TrimSpace(webhook.ContentTemplate)
	if webhook.Body == nil && (webhook.UMO != "" || webhook.ContentTemplate != "") {
		content := webhook.ContentTemplate
		if content == "" {
			content = DefaultWebhookTemplate
		}
		webhook.Body = map[string]any{"content": content, "umo": webhook.UMO}
	}
	if webhook.Body != nil {
		// The old fields are only migration inputs; new configurations use Body.
		webhook.UMO = ""
		webhook.ContentTemplate = ""
	}
	if webhook.NotifyMode == "" {
		webhook.NotifyMode = DefaultWebhookMode
	}
	if webhook.LowBalanceThreshold == 0 {
		webhook.LowBalanceThreshold = DefaultWebhookThreshold
	}
}

func validateWebhook(webhook WebhookConfig) error {
	if len(webhook.URL) > 2048 || len(webhook.Token) > 64<<10 || len(webhook.UMO) > 512 || len(webhook.NotifyMode) > 32 || len(webhook.ContentTemplate) > 64<<10 {
		return fmt.Errorf("webhook 字段过长")
	}
	if webhook.Body != nil {
		body, err := json.Marshal(webhook.Body)
		if err != nil {
			return fmt.Errorf("webhook.body 必须是有效 JSON 对象: %w", err)
		}
		if len(body) > 1<<20 {
			return fmt.Errorf("webhook.body 不能超过 1 MiB")
		}
	}
	if webhook.NotifyMode != "" && webhook.NotifyMode != "low_balance" && webhook.NotifyMode != "every_collection" && webhook.NotifyMode != "balance_decrease" {
		return fmt.Errorf("webhook.notify_mode 必须是 low_balance、balance_decrease 或 every_collection")
	}
	if math.IsNaN(webhook.LowBalanceThreshold) || math.IsInf(webhook.LowBalanceThreshold, 0) || webhook.LowBalanceThreshold < 0 {
		return fmt.Errorf("webhook.low_balance_threshold 必须是非负有限数")
	}
	if webhook.URL != "" {
		u, err := url.Parse(webhook.URL)
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" ||
			(u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("webhook.url 必须是有效的 http/https 地址")
		}
	}
	if webhook.Enabled {
		if webhook.URL == "" {
			return fmt.Errorf("webhook.enabled=true 时 webhook.url 不能为空")
		}
		if webhook.Token == "" {
			return fmt.Errorf("webhook.enabled=true 时 webhook.token 不能为空")
		}
		if webhook.Body == nil {
			return fmt.Errorf("webhook.enabled=true 时 webhook.body 不能为空")
		}
	}
	return nil
}

func cloneJSONMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = cloneJSONValue(value)
	}
	return output
}

func cloneJSONValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneJSONMap(value)
	case []any:
		output := make([]any, len(value))
		for i, item := range value {
			output[i] = cloneJSONValue(item)
		}
		return output
	default:
		return value
	}
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		return fmt.Errorf("设置文件权限失败: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("同步临时文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("原子替换文件失败: %w", err)
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	ok = true
	return nil
}

func contentStamp(data []byte) [sha256.Size]byte { return sha256.Sum256(data) }
