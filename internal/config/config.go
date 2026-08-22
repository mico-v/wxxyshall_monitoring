// Package config 管理配置文件和 token 文件的读写。
// 数据目录由 USTS_DATA_DIR 环境变量指定，默认为项目根目录。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Target 代表一个监控宿舍目标。
type Target struct {
	FeeItemID int    `json:"feeitemid"`
	AppID     int    `json:"appId"`
	Campus    string `json:"campus"`
	Building  string `json:"building"`
	Room      string `json:"room"`
	Label     string `json:"label"`
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

// Config 是 config.json 的完整映射。
type Config struct {
	Username           string   `json:"username"`
	BaseURL            string   `json:"base_url"`
	Targets            []Target `json:"targets"`
	PollIntervalMin    int      `json:"poll_interval_minutes"`
	RateLimitPerMinute int      `json:"rate_limit_per_minute"`
	ChromiumPath       string   `json:"chromium_path,omitempty"`
}

// GetTargets 返回监控目标列表，优先返回 Targets 数组。
func (c *Config) GetTargets() []Target {
	if len(c.Targets) > 0 {
		return c.Targets
	}
	return nil
}

// DataDir 返回数据目录路径。
// 优先使用 USTS_DATA_DIR 环境变量，否则返回项目根目录。
func DataDir() string {
	if d := os.Getenv("USTS_DATA_DIR"); d != "" {
		return d
	}
	// 默认：寻找 go.mod 所在目录作为项目根
	wd, _ := os.Getwd()
	root := wd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			return wd
		}
		root = parent
	}
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
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败: %w", p, err)
	}
	if cfg.Username == "" {
		return nil, fmt.Errorf("请在 %s 中填写 username(学号)", p)
	}
	// 设置默认值
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://wxxyshall.usts.edu.cn"
	}
	if cfg.PollIntervalMin <= 0 {
		cfg.PollIntervalMin = 60
	}
	return &cfg, nil
}

// SaveConfig 将配置写回 config.json。
func SaveConfig(cfg *Config) error {
	p := ConfigPath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	return os.WriteFile(p, data, 0644)
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
	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("解析 token 文件失败: %w", err)
	}
	return &tok, nil
}

// SaveToken 将 token 写入 token.json。
func SaveToken(tok *Token) error {
	p := TokenPath()
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 token 失败: %w", err)
	}
	return os.WriteFile(p, data, 0644)
}