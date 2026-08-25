// Package charge 实现与学校电费查询 API 的通信。
//
// 鉴权来自 charge-app 逆向：
//
//	Authorization: Basic Y2hhcmdlOmNoYXJnZV9zZWNyZXQ=   (charge:charge_secret, 固定)
//	synjones-auth: "bearer " + <berserker access_token>
//
// 调用链：
//
//	GET  /charge/feeitem/toAppitem?feeitemid&appId&loginFrom=h5&synjones-auth=<token>  建立会话
//	GET  /charge/user/caslogin_userxx                                                  建立 charge 用户态
//	POST /charge/feeitem/getThirdData  feeitemid&type=IEC&level=3&campus&building&room  查余额
package charge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	chargeBasic = "Basic Y2hhcmdlOmNoYXJnZV9zZWNyZXQ="
	ua          = "Mozilla/5.0 (Linux; Android 13; Pixel 5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
)

// 电费系统里表示"未授权/登录失效"的业务码（HTTP 常为 200，靠 body.code 区分）。
var authCodes = map[int]bool{401: true, 403: true, 1001: true}

var errRedirectPolicy = errors.New("重定向策略拒绝")

// ChargeError 表示电费接口通用错误。
type ChargeError struct {
	Msg string
	Err error
}

func (e *ChargeError) Error() string { return e.Msg }
func (e *ChargeError) Unwrap() error { return e.Err }

// ChargeAuthError 表示 access_token 失效。
type ChargeAuthError struct {
	Msg string
	Err error
}

func (e *ChargeAuthError) Error() string { return e.Msg }
func (e *ChargeAuthError) Unwrap() error { return e.Err }

// IsAuthError 判断错误是否为登录态失效错误。
func IsAuthError(err error) bool {
	var target *ChargeAuthError
	return errors.As(err, &target)
}

// Reading 是归一化后的电费读数。
type Reading struct {
	SurplusCharge *float64          `json:"surplus_charge"`
	Show          map[string]string `json:"show"`
	Raw           map[string]any    `json:"raw"`
}

// Option 代表发现接口中的选项（校区/楼栋/房间）。
type Option struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

// Client 是与学校电费 API 通信的 HTTP 客户端。
type Client struct {
	baseURL      string
	token        string
	httpClient   *http.Client
	retryBackoff func(attempt int) time.Duration
	mu           sync.Mutex
	established  bool
	limiter      interface {
		Wait(context.Context) error
	}
}

// NewClient 创建不带请求节拍器的客户端。
// 适用于网页级联发现等由调用方自行做进程内缓存和并发合并的场景。
func NewClient(baseURL, accessToken string) *Client {
	return NewClientWithLimiter(baseURL, accessToken, nil)
}

// NewClientWithLimiter 创建共享严格节拍器的客户端；每一次真实 HTTP 尝试都会先等待。
func NewClientWithLimiter(baseURL, accessToken string, limiter interface {
	Wait(context.Context) error
}) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   accessToken,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:          5,
				MaxIdleConnsPerHost:   2,
				MaxConnsPerHost:       5,
				IdleConnTimeout:       30 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 15 * time.Second,
				ExpectContinueTimeout: time.Second,
				DisableCompression:    false,
			},
			// 自动跟随同源重定向；每次跳转仍受共享节拍器约束。
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("%w: 重定向次数过多", errRedirectPolicy)
				}
				if len(via) > 0 && !sameOrigin(req.URL, via[0].URL) {
					return fmt.Errorf("%w: 拒绝跨源重定向到 %s", errRedirectPolicy, req.URL.Redacted())
				}
				if limiter != nil {
					if err := limiter.Wait(req.Context()); err != nil {
						return err
					}
				}
				return nil
			},
		},
		limiter: limiter,
		retryBackoff: func(attempt int) time.Duration {
			return time.Duration(1500*(attempt+1)) * time.Millisecond
		},
	}
}

// do 执行 HTTP 请求，带瞬时错误重试。
func (c *Client) do(ctx context.Context, method, path string, params url.Values, body io.Reader) (*http.Response, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	var bodyData []byte
	if body != nil {
		var err error
		bodyData, err = io.ReadAll(io.LimitReader(body, 1<<20+1))
		if err != nil {
			return nil, &ChargeError{Msg: fmt.Sprintf("读取请求体失败(%s): %v", path, err), Err: err}
		}
		if len(bodyData) > 1<<20 {
			return nil, &ChargeError{Msg: fmt.Sprintf("请求体过大(%s)", path)}
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var lastErr error
	for attempt := 0; attempt <= 2; attempt++ {
		if c.limiter != nil {
			if err := c.limiter.Wait(ctx); err != nil {
				return nil, &ChargeError{Msg: "请求已取消", Err: err}
			}
		}
		var reqBody io.Reader
		if bodyData != nil {
			reqBody = bytes.NewReader(bodyData)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
		if err != nil {
			return nil, &ChargeError{Msg: fmt.Sprintf("创建请求失败(%s): %v", path, err), Err: err}
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("Authorization", chargeBasic)
		req.Header.Set("synjones-auth", "bearer "+c.token)
		if method == "POST" && bodyData != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, &ChargeError{Msg: "请求已取消", Err: ctx.Err()}
			}
			if errors.Is(err, errRedirectPolicy) {
				return nil, &ChargeError{Msg: fmt.Sprintf("请求被安全策略拒绝(%s): %v", path, err), Err: err}
			}
			if attempt < 2 {
				slog.Warn("请求失败，重试", "method", method, "path", path, "attempt", attempt+1, "err", err)
				if err := waitContext(ctx, c.retryBackoff(attempt)); err != nil {
					return nil, &ChargeError{Msg: "请求已取消", Err: err}
				}
				continue
			}
			return nil, &ChargeError{Msg: fmt.Sprintf("网络错误(%s): %v", path, err), Err: err}
		}

		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			resp.Body.Close()
			return nil, &ChargeAuthError{Msg: fmt.Sprintf("charge 鉴权失败 HTTP %d(%s)", resp.StatusCode, path)}
		}
		if isRetryableStatus(resp.StatusCode) {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			if attempt < 2 {
				slog.Warn("学校接口临时异常，重试", "method", method, "path", path, "status", resp.StatusCode, "attempt", attempt+1)
				if err := waitContext(ctx, c.retryBackoff(attempt)); err != nil {
					return nil, &ChargeError{Msg: "请求已取消", Err: err}
				}
				continue
			}
			return nil, &ChargeError{Msg: fmt.Sprintf("学校接口 HTTP %d(%s)", resp.StatusCode, path), Err: lastErr}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			return nil, &ChargeError{Msg: fmt.Sprintf("学校接口 HTTP %d(%s)", resp.StatusCode, path)}
		}

		return resp, nil
	}
	return nil, &ChargeError{Msg: fmt.Sprintf("请求失败(%s): %v", path, lastErr), Err: lastErr}
}

// Establish 走一遍进入 charge 子系统的跳转，拿到会话 cookie（幂等，仅首次真正执行）。
func (c *Client) Establish(feeitemid, appID int) error {
	return c.EstablishContext(context.Background(), feeitemid, appID)
}

func (c *Client) EstablishContext(ctx context.Context, feeitemid, appID int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.established {
		return nil
	}

	slog.Info("建立 charge 会话", "feeitemid", feeitemid, "appId", appID)

	params := url.Values{
		"feeitemid":     {fmt.Sprintf("%d", feeitemid)},
		"appId":         {fmt.Sprintf("%d", appID)},
		"loginFrom":     {"h5"},
		"synjones-auth": {c.token},
	}
	resp, err := c.do(ctx, "GET", "/charge/feeitem/toAppitem", params, nil)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()

	resp, err = c.do(ctx, "GET", "/charge/user/caslogin_userxx", nil, nil)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()

	c.established = true
	return nil
}

func (c *Client) thirdContext(ctx context.Context, feeitemid int, data url.Values) (map[string]any, error) {
	resp, err := c.do(ctx, "POST", "/charge/feeitem/getThirdData", nil, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if err != nil {
		return nil, &ChargeError{Msg: fmt.Sprintf("读取响应失败: %v", err), Err: err}
	}
	if len(b) > 4<<20 {
		return nil, &ChargeError{Msg: "学校接口响应超过 4 MiB"}
	}

	var j map[string]any
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, &ChargeAuthError{Msg: "getThirdData 返回非 JSON，疑似登录态失效"}
	}

	code, ok := responseCode(j["code"])
	if !ok {
		return nil, &ChargeError{Msg: "getThirdData 返回缺少有效 code 字段"}
	}
	if authCodes[code] {
		msg, _ := j["msg"].(string)
		return nil, &ChargeAuthError{Msg: fmt.Sprintf("getThirdData 鉴权失败 code=%d: %s", code, msg)}
	}
	if code != 200 {
		return nil, &ChargeError{Msg: fmt.Sprintf("getThirdData 返回异常 code=%d", code)}
	}

	m, ok := j["map"].(map[string]any)
	if !ok {
		return nil, &ChargeError{Msg: "getThirdData 返回缺少 map 字段"}
	}
	return m, nil
}

// QueryBalance 查询某个房间的电费余额。
// 返回的 map 中包含 showData、surplusCharge、data 等字段。
func (c *Client) QueryBalance(feeitemid int, campus, building, room string) (*Reading, error) {
	return c.QueryBalanceContext(context.Background(), feeitemid, campus, building, room)
}

func (c *Client) QueryBalanceContext(ctx context.Context, feeitemid int, campus, building, room string) (*Reading, error) {
	params := url.Values{
		"feeitemid": {fmt.Sprintf("%d", feeitemid)},
		"type":      {"IEC"},
		"level":     {"3"},
		"campus":    {campus},
		"building":  {building},
		"room":      {room},
	}
	m, err := c.thirdContext(ctx, feeitemid, params)
	if err != nil {
		return nil, err
	}

	return ParseReading(m), nil
}

// ListCampuses 获取校区列表。
func (c *Client) ListCampuses(feeitemid int) ([]Option, error) {
	return c.ListCampusesContext(context.Background(), feeitemid)
}

func (c *Client) ListCampusesContext(ctx context.Context, feeitemid int) ([]Option, error) {
	params := url.Values{
		"feeitemid": {fmt.Sprintf("%d", feeitemid)},
		"type":      {"select"},
		"level":     {"0"},
	}
	m, err := c.thirdContext(ctx, feeitemid, params)
	if err != nil {
		return nil, err
	}
	return extractOptions(m)
}

// ListBuildings 获取某个校区下的楼栋列表。
func (c *Client) ListBuildings(feeitemid int, campus string) ([]Option, error) {
	return c.ListBuildingsContext(context.Background(), feeitemid, campus)
}

func (c *Client) ListBuildingsContext(ctx context.Context, feeitemid int, campus string) ([]Option, error) {
	params := url.Values{
		"feeitemid": {fmt.Sprintf("%d", feeitemid)},
		"type":      {"select"},
		"level":     {"1"},
		"campus":    {campus},
	}
	m, err := c.thirdContext(ctx, feeitemid, params)
	if err != nil {
		return nil, err
	}
	return extractOptions(m)
}

// ListRooms 获取某个楼栋下的房间列表。
func (c *Client) ListRooms(feeitemid int, campus, building string) ([]Option, error) {
	return c.ListRoomsContext(context.Background(), feeitemid, campus, building)
}

func (c *Client) ListRoomsContext(ctx context.Context, feeitemid int, campus, building string) ([]Option, error) {
	params := url.Values{
		"feeitemid": {fmt.Sprintf("%d", feeitemid)},
		"type":      {"select"},
		"level":     {"2"},
		"campus":    {campus},
		"building":  {building},
	}
	m, err := c.thirdContext(ctx, feeitemid, params)
	if err != nil {
		return nil, err
	}
	return extractOptions(m)
}

// extractOptions 从 getThirdData 响应中提取选项列表。
func extractOptions(m map[string]any) ([]Option, error) {
	data, ok := m["data"].([]any)
	if !ok {
		return nil, &ChargeError{Msg: "发现接口返回缺少 data 数组"}
	}
	opts := make([]Option, 0, len(data))
	for _, item := range data {
		it, ok := item.(map[string]any)
		if !ok {
			continue
		}
		value, valueOK := it["value"]
		name, nameOK := it["name"]
		if !valueOK || !nameOK {
			continue
		}
		valueText := strings.TrimSpace(fmt.Sprintf("%v", value))
		nameText := strings.TrimSpace(fmt.Sprintf("%v", name))
		if valueText == "" || nameText == "" || valueText == "<nil>" || nameText == "<nil>" {
			continue
		}
		opts = append(opts, Option{Value: valueText, Name: nameText})
	}
	return opts, nil
}

// ParseReading 把 getThirdData 的 map 归一化为便于入库的 Reading。
func ParseReading(chargeMap map[string]any) *Reading {
	r := &Reading{
		Show: make(map[string]string),
		Raw:  make(map[string]any),
	}

	// surplusCharge
	if sc, ok := chargeMap["surplusCharge"]; ok {
		f := toFloat(sc)
		r.SurplusCharge = f
	}

	// showData (中文键，随表计类型而变，原样保留)
	if sd, ok := chargeMap["showData"].(map[string]any); ok {
		for k, v := range sd {
			r.Show[k] = fmt.Sprintf("%v", v)
		}
	}

	// data (原始数据)
	if d, ok := chargeMap["data"].(map[string]any); ok {
		r.Raw = d
	}

	return r
}

func toFloat(v any) *float64 {
	switch val := v.(type) {
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return nil
		}
		return &val
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return &f
		}
	case int:
		f := float64(val)
		return &f
	case int64:
		f := float64(val)
		return &f
	}
	return nil
}

// TotalUsage 从 Reading 的 Show 中提取"电表总用电量"。
func (r *Reading) TotalUsage() *float64 {
	if r == nil {
		return nil
	}
	v, ok := r.Show["电表总用电量"]
	if !ok {
		return nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}

func responseCode(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n > math.MaxInt || n < math.MinInt {
			return 0, false
		}
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil || int64(int(i)) != i {
			return 0, false
		}
		return int(i), true
	case int:
		return n, true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	default:
		return 0, false
	}
}

func isRetryableStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests ||
		status == http.StatusInternalServerError || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil || !strings.EqualFold(left.Scheme, right.Scheme) ||
		!strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	return effectivePort(left) == effectivePort(right)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}
