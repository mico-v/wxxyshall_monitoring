package config

// Token 代表从登录接口获取的 OAuth2 token。
type Token struct {
	AccessToken string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn   int    `json:"expires_in"`
	LoginTime   int64  `json:"login_time"`
	Sno         string `json:"sno"`
	Source      string `json:"source"`
}