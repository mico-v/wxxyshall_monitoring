// Package auth 提供 token 过期检查功能。
//
// 登录接口 POST /berserker-auth/oauth/token 是标准 Spring OAuth2，
// access_token 服务端声明有效期约 70 天 (expires_in≈6047999s)。
//
// ⚠️ 实测(2026-08)：本平台 refresh_token 续期不可用——
// 带 logintype=snoNew 时服务端返回 HTTP 500，不带则 8016"越权操作"，
// 且主站前端本就无续期逻辑。因此本项目不做续期：token 过期后重新
// 运行 login.py 即可。
package auth

import (
	"math"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

// ExpiryEpoch 返回 token 的过期时间戳（秒）。
// 如果缺少 login_time 或 expires_in 元数据，返回 nil。
func ExpiryEpoch(tok *config.Token) *int64 {
	if tok == nil || tok.LoginTime <= 0 || tok.ExpiresIn <= 0 {
		return nil
	}
	v := tok.LoginTime + int64(tok.ExpiresIn)
	return &v
}

// SecondsLeft 返回 token 剩余有效秒数。
// 元数据缺失时返回 nil（表示未知）。
func SecondsLeft(tok *config.Token) *float64 {
	exp := ExpiryEpoch(tok)
	if exp == nil {
		return nil
	}
	left := float64(*exp) - float64(time.Now().Unix())
	return &left
}

// IsExpired 判断 token 是否已过期（或临近过期）。
// skewSeconds 为提前过期的时间窗口（默认 600 秒 = 10 分钟）。
// 元数据缺失时返回 false（让请求层遇到 401 再提示重新登录）。
func IsExpired(tok *config.Token, skewSeconds int) bool {
	if skewSeconds <= 0 {
		skewSeconds = 600
	}
	left := SecondsLeft(tok)
	if left == nil {
		return false
	}
	return *left <= float64(skewSeconds)
}

// DaysLeft 返回 token 剩余天数（向下取整）。
// 元数据缺失时返回 math.MaxInt64（表示未知）。
func DaysLeft(tok *config.Token) int {
	left := SecondsLeft(tok)
	if left == nil {
		return math.MaxInt64
	}
	days := int(*left / 86400)
	if days < 0 {
		return 0
	}
	return days
}
