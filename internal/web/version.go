package web

import (
	"bytes"
	"strings"
)

// Version 是构建版本号，默认 "dev"。
// 发布/部署时通过构建参数注入 git 短 hash：
//
//	go build -ldflags "-X github.com/mico-v/wxxyshall-monitoring/internal/web.Version=<hash>"
//
// 它会被写进 /api/health、页面页脚，并用于 Service Worker 缓存命名——
// 每次构建版本变化都会让旧缓存自动失效，不再需要手工改 vNN。
var Version = "dev"

// versionToken 是静态文件中等待注入版本号的占位符。
const versionToken = "__ELEC_VERSION__"

// appVersion 返回可安全嵌入 HTML/JS 的版本号：只保留标识符常用字符，
// 避免构建参数里的意外字符破坏页面或脚本。
func appVersion() string {
	v := strings.TrimSpace(Version)
	if v == "" {
		return "dev"
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "dev"
	}
	return b.String()
}

// injectVersion 把静态文件里的 __ELEC_VERSION__ 占位符替换为实际版本号。
func injectVersion(data []byte) []byte {
	token := []byte(versionToken)
	if !bytes.Contains(data, token) {
		return data
	}
	return bytes.ReplaceAll(data, token, []byte(appVersion()))
}
