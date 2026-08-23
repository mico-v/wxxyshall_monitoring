// Package config —— Hub:config.json / token.json 热重载。
//
// 采集循环与 web 服务共享同一个 Hub:
//   - 配置文件被外部修改(手动编辑、网页保存、login.py 推送)后
//     无需重启进程,轮询检测 mtime 即可自动生效;
//   - 采集间隔(poll_interval_minutes)、targets、限流等即时更新;
//   - token 更换(替换文件或 /api/token 推送)后网页与采集都会用新 token。
package config

import (
	"os"
	"sync"
	"time"
)

// Hub 持有当前生效的 Config 与 Token，并支持从磁盘热重载。
type Hub struct {
	mu      sync.RWMutex
	cfg     *Config
	tok     *Token
	cfgPath string
	tokPath string
	cfgMod  time.Time
	tokMod  time.Time
}

// NewHub 创建 Hub 并加载当前 config.json / token.json。
func NewHub() (*Hub, error) {
	h := &Hub{
		cfgPath: ConfigPath(),
		tokPath: TokenPath(),
	}
	if _, _, err := h.Reload(); err != nil {
		return nil, err
	}
	return h, nil
}

// Config 返回当前生效的配置。
func (h *Hub) Config() *Config {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cfg
}

// Token 返回当前生效的 token(可能为 nil)。
func (h *Hub) Token() *Token {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.tok
}

// SetConfig 将配置保存到磁盘并立即在内存中生效。
func (h *Hub) SetConfig(cfg *Config) error {
	if err := SaveConfig(cfg); err != nil {
		return err
	}
	h.mu.Lock()
	h.cfg = cfg
	h.cfgMod = time.Now()
	h.mu.Unlock()
	return nil
}

// SetToken 将 token 保存到磁盘并立即在内存中生效。
func (h *Hub) SetToken(tok *Token) error {
	if err := SaveToken(tok); err != nil {
		return err
	}
	h.mu.Lock()
	h.tok = tok
	h.tokMod = time.Now()
	h.mu.Unlock()
	return nil
}

// Reload 检测 config.json / token.json 是否发生变更(按 mtime)，有变更则重新加载。
// 返回 (configChanged, tokenChanged, err)。
func (h *Hub) Reload() (cfgChanged, tokChanged bool, err error) {
	if st, err := os.Stat(h.cfgPath); err == nil {
		if !st.ModTime().Equal(h.cfgMod) {
			cfg, err := LoadConfig()
			if err != nil {
				return false, false, err
			}
			h.mu.Lock()
			h.cfg = cfg
			h.cfgMod = st.ModTime()
			h.mu.Unlock()
			cfgChanged = true
		}
	}

	if st, err := os.Stat(h.tokPath); err == nil {
		if !st.ModTime().Equal(h.tokMod) {
			tok, err := LoadToken()
			if err != nil {
				return false, false, err
			}
			h.mu.Lock()
			h.tok = tok
			h.tokMod = st.ModTime()
			h.mu.Unlock()
			tokChanged = true
		}
	}
	return cfgChanged, tokChanged, nil
}