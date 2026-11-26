// Package schoollogin 通过学校移动服务平台的账号登录接口获取 access token。
//
// 学校前端的安全键盘会先请求 berserker-secure/keyboard 获取本次键位图片和
// 随机替换表。界面按固定 QWERTY/数字/符号布局显示字符，提交的是可见键位在
// 替换表中对应的隐藏字符，并附上本次 uuid。这里复用同一协议，不需要浏览器，
// 也不会保存用户密码。
package schoollogin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

const (
	maxResponseBody = 256 << 10
	oauthBasicAuth  = "Basic bW9iaWxlX3NlcnZpY2VfcGxhdGZvcm06bW9iaWxlX3NlcnZpY2VfcGxhdGZvcm1fc2VjcmV0"
	userAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/158.0.0.0 Safari/537.36 Edg/158.0.0.0"
	acceptLanguage = "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6,zh-TW;q=0.5"
	secCHUA        = `"Chromium";v="158", "Not.A/Brand";v="24", "Microsoft Edge";v="158"`
)

var (
	numberLayout = "0123456789"
	lowerLayout  = "qwertyuiopasdfghjklzxcvbnm"
	upperLayout  = "QWERTYUIOPASDFGHJKLZXCVBNM"
	symbolKeys   = []rune{
		'*', '\\', '-', '[', ']', '{', '}', '/', '!', ',', '<', '>', '?', '~', '&',
		'@', '#', '.', ':', '+', '|', '`', '%', '\'', '$', ';', '^', '"', '_',
	}
)

var (
	// ErrCredentialsRejected 表示学校明确拒绝了账号或密码。
	ErrCredentialsRejected = errors.New("学校账号或密码错误")
	// ErrLoginRejected 表示学校拒绝了本次登录请求。
	ErrLoginRejected = errors.New("学校拒绝了登录请求")
	// ErrLoginChallenge 表示学校要求额外验证（验证码或选择学号）。
	ErrLoginChallenge = errors.New("学校要求额外验证")
	// ErrUnavailable 表示学校接口不可用或返回了无法识别的数据。
	ErrUnavailable = errors.New("学校登录服务不可用")
)

// Client 调用学校登录接口。HTTPClient 可替换以便测试；普通调用使用 NewClient。
type Client struct {
	HTTPClient *http.Client
	Now        func() time.Time
}

// Session 保存一次登录流程所需的 Cookie 和本次安全键盘 uuid。
// 浏览器只上传映射后的密码，不会把明文密码发送给本站服务端。
type Session struct {
	client       *http.Client
	baseURL      string
	keyboardUUID string
	now          func() time.Time
}

// NewClient 创建登录客户端。
func NewClient() *Client {
	return &Client{
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:          4,
				MaxIdleConnsPerHost:   2,
				MaxConnsPerHost:       4,
				IdleConnTimeout:       30 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 20 * time.Second,
				ExpectContinueTimeout: time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("%w: 重定向次数过多", ErrUnavailable)
				}
				if len(via) > 0 && !sameOrigin(req.URL, via[0].URL) {
					return fmt.Errorf("%w: 拒绝跨源重定向", ErrUnavailable)
				}
				return nil
			},
		},
		Now: time.Now,
	}
}

// Login 完成安全键盘握手并提交账号密码，返回可写入 token.json 的登录结果。
func (c *Client) Login(ctx context.Context, baseURL, username, password string) (*config.Token, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, fmt.Errorf("%w: 账号和密码不能为空", ErrLoginRejected)
	}
	session, err := c.StartSession(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	keyboard, err := session.Keyboard(ctx)
	if err != nil {
		return nil, err
	}
	mappedPassword, err := encodePasswordForKeyboard(password, *keyboard)
	if err != nil {
		return nil, err
	}
	return session.LoginMapped(ctx, username, mappedPassword)
}

// StartSession 建立学校登录所需的 Cookie 会话。
func (c *Client) StartSession(ctx context.Context, baseURL string) (*Session, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("%w: 未配置学校地址", ErrUnavailable)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	httpClient, err := c.sessionClient()
	if err != nil {
		return nil, err
	}
	c.primeSession(ctx, httpClient, baseURL)
	now := c.Now
	if now == nil {
		now = time.Now
	}
	return &Session{client: httpClient, baseURL: baseURL, now: now}, nil
}

// Keyboard 获取本次登录的随机映射表。
func (s *Session) Keyboard(ctx context.Context) (*KeyboardData, error) {
	keyboard, err := (&Client{}).fetchKeyboard(ctx, s.client, s.baseURL)
	if err != nil {
		return nil, err
	}
	s.keyboardUUID = strings.TrimSpace(keyboard.Data.UUID)
	if s.keyboardUUID == "" {
		return nil, fmt.Errorf("%w: 安全键盘接口未返回 uuid", ErrUnavailable)
	}
	return &keyboard.Data, nil
}

// LoginMapped 提交浏览器已经按安全键盘转换后的密码。
func (s *Session) LoginMapped(ctx context.Context, username, mappedPassword string) (*config.Token, error) {
	username = strings.TrimSpace(username)
	if username == "" || mappedPassword == "" {
		return nil, fmt.Errorf("%w: 账号和映射密码不能为空", ErrLoginRejected)
	}
	if s.keyboardUUID == "" {
		return nil, fmt.Errorf("%w: 登录会话缺少安全键盘 uuid", ErrUnavailable)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := (&Client{}).requestToken(
		ctx,
		s.client,
		s.baseURL,
		username,
		mappedPassword+"$1$"+s.keyboardUUID,
	)
	if err != nil {
		return nil, err
	}

	expiresIn, err := parseInteger(payload.ExpiresIn)
	if err != nil || expiresIn < 0 {
		return nil, fmt.Errorf("%w: token 有效期字段无效", ErrUnavailable)
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	sno := rawText(payload.Sno)
	if sno == "" {
		sno = username
	}
	return &config.Token{
		AccessToken:  strings.TrimSpace(payload.AccessToken),
		RefreshToken: strings.TrimSpace(payload.RefreshToken),
		ExpiresIn:    expiresIn,
		LoginTime:    now().Unix(),
		Sno:          sno,
		Source:       "web",
	}, nil
}

func (c *Client) sessionClient() (*http.Client, error) {
	base := c.HTTPClient
	if base == nil {
		base = NewClient().HTTPClient
	}
	jar := base.Jar
	if jar == nil {
		var err error
		jar, err = cookiejar.New(nil)
		if err != nil {
			return nil, fmt.Errorf("%w: 创建登录会话失败", ErrUnavailable)
		}
	}
	client := *base
	client.Jar = jar
	return &client, nil
}

func (c *Client) primeSession(ctx context.Context, client *http.Client, baseURL string) {
	query := url.Values{"synAccessSource": {"h5"}}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		baseURL+"/berserker-app/frontInfo?"+query.Encode(),
		nil,
	)
	if err != nil {
		return
	}
	setBrowserHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

type keyboardResponse struct {
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Msg     string       `json:"msg"`
	Data    KeyboardData `json:"data"`
}

// KeyboardData 是学校本次随机键位映射。
type KeyboardData struct {
	UUID                string `json:"uuid"`
	NumberKeyboard      string `json:"numberKeyboard"`
	LowerLetterKeyboard string `json:"lowerLetterKeyboard"`
	UpperLetterKeyboard string `json:"upperLetterKeyboard"`
	SymbolKeyboard      string `json:"symbolKeyboard"`
}

func (c *Client) fetchKeyboard(ctx context.Context, client *http.Client, baseURL string) (*keyboardResponse, error) {
	u := baseURL + "/berserker-secure/keyboard"
	query := url.Values{
		"type":            {"Standard"},
		"order":           {"0"},
		"synAccessSource": {"h5"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: 创建安全键盘请求失败", ErrUnavailable)
	}
	setBrowserHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		return nil, requestError(err)
	}
	body, err := readResponseBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: 安全键盘接口 HTTP %d", ErrUnavailable, resp.StatusCode)
	}

	var result keyboardResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("%w: 安全键盘接口返回非 JSON", ErrUnavailable)
	}
	if result.Code != http.StatusOK {
		message := firstNonEmpty(result.Message, result.Msg)
		if message == "" {
			message = fmt.Sprintf("安全键盘接口返回 code=%d", result.Code)
		}
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, message)
	}
	return &result, nil
}

func encodePasswordForKeyboard(password string, keyboard KeyboardData) (string, error) {
	numbers := []rune(keyboard.NumberKeyboard)
	lower := []rune(keyboard.LowerLetterKeyboard)
	upper := []rune(keyboard.UpperLetterKeyboard)
	symbols := []rune(keyboard.SymbolKeyboard)
	if len(numbers) < 10 || len(lower) < 26 || len(upper) < 26 || len(symbols) < len(symbolKeys) {
		return "", fmt.Errorf("%w: 安全键盘映射不完整", ErrUnavailable)
	}

	encoded := make([]rune, 0, len([]rune(password)))
	for _, char := range password {
		var mapping []rune
		index := -1
		switch {
		case char >= '0' && char <= '9':
			index = indexRune([]rune(numberLayout), char)
			mapping = numbers
		case char >= 'a' && char <= 'z':
			index = indexRune([]rune(lowerLayout), char)
			mapping = lower
		case char >= 'A' && char <= 'Z':
			index = indexRune([]rune(upperLayout), char)
			mapping = upper
		default:
			index = indexRune(symbolKeys, char)
			mapping = symbols
		}
		if index < 0 || index >= len(mapping) {
			return "", fmt.Errorf("%w: 密码包含安全键盘不支持的字符", ErrLoginRejected)
		}
		encoded = append(encoded, mapping[index])
	}
	return string(encoded), nil
}

func indexRune(runes []rune, target rune) int {
	for index, value := range runes {
		if value == target {
			return index
		}
	}
	return -1
}

type tokenResponse struct {
	AccessToken      string          `json:"access_token"`
	RefreshToken     string          `json:"refresh_token"`
	ExpiresIn        json.RawMessage `json:"expires_in"`
	Sno              json.RawMessage `json:"sno"`
	Code             int             `json:"code"`
	Status           int             `json:"status"`
	Message          string          `json:"message"`
	Msg              string          `json:"msg"`
	ErrorDescription string          `json:"error_description"`
	Data             json.RawMessage `json:"data"`
}

func (c *Client) requestToken(ctx context.Context, client *http.Client, baseURL, username, password string) (*tokenResponse, error) {
	form := url.Values{
		"username":        {username},
		"password":        {password},
		"logintype":       {"snoNew"},
		"grant_type":      {"password"},
		"scope":           {"all"},
		"loginFrom":       {"h5"},
		"device_token":    {"h5"},
		"synAccessSource": {"h5"},
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/berserker-auth/oauth/token",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: 创建登录请求失败", ErrUnavailable)
	}
	setBrowserHeaders(req)
	req.Header.Set("Authorization", oauthBasicAuth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", baseURL)

	resp, err := client.Do(req)
	if err != nil {
		return nil, requestError(err)
	}
	body, err := readResponseBody(resp)
	if err != nil {
		return nil, err
	}

	var result tokenResponse
	_ = json.Unmarshal(body, &result)
	if result.AccessToken == "" && len(result.Data) > 0 {
		var nested tokenResponse
		if json.Unmarshal(result.Data, &nested) == nil {
			nested.Code = firstNonZero(nested.Code, result.Code)
			nested.Status = firstNonZero(nested.Status, result.Status)
			nested.Message = firstNonEmpty(nested.Message, result.Message)
			nested.Msg = firstNonEmpty(nested.Msg, result.Msg)
			nested.ErrorDescription = firstNonEmpty(nested.ErrorDescription, result.ErrorDescription)
			result = nested
		}
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices &&
		strings.TrimSpace(result.AccessToken) != "" {
		return &result, nil
	}

	message := firstNonEmpty(result.ErrorDescription, result.Message, result.Msg)
	code := firstNonZero(result.Code, result.Status)
	switch code {
	case 8000:
		return nil, fmt.Errorf("%w", ErrCredentialsRejected)
	case 8001:
		return nil, fmt.Errorf("%w: 该账号匹配到多个学号，暂不支持自动选择", ErrLoginChallenge)
	case 8002, 8003:
		return nil, fmt.Errorf("%w: 学校要求输入验证码，请稍后重试", ErrLoginChallenge)
	}
	if message == "" {
		message = fmt.Sprintf("登录接口 HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%w: %s", ErrLoginRejected, message)
	}
	return nil, fmt.Errorf("%w: %s", ErrUnavailable, message)
}

func setBrowserHeaders(req *http.Request) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", acceptLanguage)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("synaccesssource", "h5")
	req.Header.Set("Sec-CH-UA", secCHUA)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("%w: 读取学校响应失败", ErrUnavailable)
	}
	if len(body) > maxResponseBody {
		return nil, fmt.Errorf("%w: 学校响应过大", ErrUnavailable)
	}
	return body, nil
}

func requestError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

func parseInteger(raw json.RawMessage) (int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		value, err := strconv.ParseInt(number.String(), 10, 64)
		if err != nil || value > int64(^uint(0)>>1) {
			return 0, fmt.Errorf("invalid integer")
		}
		return int(value), nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil || value > int64(^uint(0)>>1) {
			return 0, fmt.Errorf("invalid integer")
		}
		return int(value), nil
	}
	return 0, fmt.Errorf("invalid integer")
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}
