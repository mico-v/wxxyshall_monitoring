// Package config —— Hub: config.json / token.json 的线程安全热重载。
package config

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// Hub 持有当前生效的不可变配置快照与 token 快照。
type Hub struct {
	mu       sync.RWMutex
	writeMu  sync.Mutex
	cfg      *Config
	tok      *Token
	cfgPath  string
	tokPath  string
	cfgStamp [32]byte
	tokStamp [32]byte
	tokSet   bool
}

// NewHub 创建 Hub 并加载当前 config.json / token.json。
func NewHub() (*Hub, error) {
	h := &Hub{cfgPath: ConfigPath(), tokPath: TokenPath()}
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	tok, err := LoadToken()
	if err != nil {
		return nil, err
	}
	h.cfg = cfg.Clone()
	h.tok = cloneToken(tok)
	cfgData, err := os.ReadFile(h.cfgPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	h.cfgStamp = contentStamp(cfgData)
	if tokData, readErr := os.ReadFile(h.tokPath); readErr == nil {
		h.tokStamp = contentStamp(tokData)
		h.tokSet = true
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("读取 token 文件失败: %w", readErr)
	}
	return h, nil
}

// Config 返回配置的深拷贝，调用方可安全修改。
func (h *Hub) Config() *Config {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cfg.Clone()
}

// Token 返回 token 的副本。
func (h *Hub) Token() *Token {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return cloneToken(h.tok)
}

// UpdateConfig 在独占写锁下基于最新配置生成并保存新快照。
func (h *Hub) UpdateConfig(fn func(*Config) error) (*Config, error) {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	// API 更新可能恰好发生在外部编辑 config.json 后、后台 watcher 发现前。
	// 先读取磁盘最新内容，避免用旧内存快照覆盖刚完成的手工修改。
	data, err := os.ReadFile(h.cfgPath)
	if err != nil {
		return nil, fmt.Errorf("读取最新配置文件失败: %w", err)
	}
	diskStamp := contentStamp(data)
	h.mu.RLock()
	knownStamp := h.cfgStamp
	h.mu.RUnlock()

	var cfg *Config
	if diskStamp != knownStamp {
		cfg, err = parseConfig(data, h.cfgPath)
		if err != nil {
			return nil, fmt.Errorf("配置文件已在外部修改但内容无效，拒绝覆盖: %w", err)
		}
	} else {
		cfg = h.Config()
		if cfg == nil {
			return nil, fmt.Errorf("当前配置不可用")
		}
	}
	if err := fn(cfg); err != nil {
		return nil, err
	}
	normalizeConfig(cfg)
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	if err := SaveConfig(cfg); err != nil {
		return nil, err
	}
	data, err = os.ReadFile(h.cfgPath)
	if err != nil {
		return nil, fmt.Errorf("重新读取配置文件失败: %w", err)
	}
	h.mu.Lock()
	h.cfg = cfg.Clone()
	h.cfgStamp = contentStamp(data)
	h.mu.Unlock()
	return cfg.Clone(), nil
}

// SetToken 保存 token 并立即生效。
func (h *Hub) SetToken(tok *Token) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if tok == nil {
		return fmt.Errorf("token 不能为空")
	}
	cp := *tok
	if err := normalizeAndValidateToken(&cp); err != nil {
		return err
	}
	if err := SaveToken(&cp); err != nil {
		return err
	}
	data, err := os.ReadFile(h.tokPath)
	if err != nil {
		return fmt.Errorf("重新读取 token 文件失败: %w", err)
	}
	h.mu.Lock()
	h.tok = cloneToken(&cp)
	h.tokStamp = contentStamp(data)
	h.tokSet = true
	h.mu.Unlock()
	return nil
}

// Reload 比较文件内容摘要，有变更时先完整校验，再原子替换内存快照。
// 使用内容摘要而非 mtime，避免快速原子替换或时间戳精度导致漏掉热更新。
func (h *Hub) Reload() (cfgChanged, tokChanged bool, err error) {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()

	h.mu.RLock()
	knownCfgStamp, knownTokStamp, knownTokSet := h.cfgStamp, h.tokStamp, h.tokSet
	h.mu.RUnlock()

	var reloadErr error
	cfgData, readErr := os.ReadFile(h.cfgPath)
	if readErr != nil {
		reloadErr = fmt.Errorf("读取配置文件失败: %w", readErr)
	} else {
		cfgStamp := contentStamp(cfgData)
		if cfgStamp != knownCfgStamp {
			cfg, loadErr := parseConfig(cfgData, h.cfgPath)
			if loadErr != nil {
				reloadErr = loadErr
			} else {
				h.mu.Lock()
				h.cfg = cfg.Clone()
				h.cfgStamp = cfgStamp
				h.mu.Unlock()
				cfgChanged = true
			}
		}
	}

	tokData, readErr := os.ReadFile(h.tokPath)
	if os.IsNotExist(readErr) {
		if knownTokSet || h.Token() != nil {
			h.mu.Lock()
			h.tok = nil
			h.tokStamp = [32]byte{}
			h.tokSet = false
			h.mu.Unlock()
			tokChanged = true
		}
		return cfgChanged, tokChanged, reloadErr
	}
	if readErr != nil {
		return cfgChanged, false, errors.Join(reloadErr, fmt.Errorf("读取 token 文件失败: %w", readErr))
	}
	tokStamp := contentStamp(tokData)
	if !knownTokSet || tokStamp != knownTokStamp {
		tok, loadErr := parseToken(tokData)
		if loadErr != nil {
			return cfgChanged, false, errors.Join(reloadErr, loadErr)
		}
		h.mu.Lock()
		h.tok = cloneToken(tok)
		h.tokStamp = tokStamp
		h.tokSet = true
		h.mu.Unlock()
		tokChanged = true
	}
	return cfgChanged, tokChanged, reloadErr
}

func cloneToken(tok *Token) *Token {
	if tok == nil {
		return nil
	}
	cp := *tok
	return &cp
}
