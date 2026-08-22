// Package charge 实现与学校电费查询 API 的通信。
//
// 鉴权来自 charge-app 逆向：
//   Authorization: Basic Y2hhcmdlOmNoYXJnZV9zZWNyZXQ=   (charge:charge_secret, 固定)
//   synjones-auth: "bearer " + <berserker access_token>
// 调用链：
//   GET  /charge/feeitem/toAppitem?feeitemid&appId&loginFrom=h5&synjones-auth=<token>  建立会话
//   GET  /charge/user/caslogin_userxx                                                  建立 charge 用户态
//   POST /charge/feeitem/getThirdData  feeitemid&type=IEC&level=3&campus&building&room  查余额
package charge

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
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

// ChargeError 表示电费接口通用错误。
type ChargeError struct {
	Msg string
}

func (e *ChargeError) Error() string { return e.Msg }

// ChargeAuthError 表示 access_token 失效。
type ChargeAuthError struct {
	Msg string
}

func (e *ChargeAuthError) Error() string { return e.Msg }

// IsAuthError 判断错误是否为登录态失效错误。
func IsAuthError(err error) bool {
	_, ok := err.(*ChargeAuthError)
	return ok
}

// Reading 是归一化后的电费读数。
type Reading struct {
	SurplusCharge *float64           `json:"surplus_charge"`
	Show          map[string]string  `json:"show"`
	Raw           map[string]any     `json:"raw"`
}

// Option 代表发现接口中的选项（校区/楼栋/房间）。
type Option struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

// Client 是与学校电费 API 通信的 HTTP 客户端。
type Client struct {
	baseURL     string
	token       string
	httpClient  *http.Client
	mu          sync.Mutex
	established bool
}

// NewClient 创建一个新的电费查询客户端。
func NewClient(baseURL, accessToken string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   accessToken,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        5,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  false,
			},
			// 自动跟随重定向（allow_redirects=True）
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("重定向次数过多")
				}
				return nil
			},
		},
	}
}

// do 执行 HTTP 请求，带瞬时错误重试。
func (c *Client) do(method, path string, params url.Values, body io.Reader) (*http.Response, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, &ChargeError{Msg: fmt.Sprintf("创建请求失败(%s): %v", path, err)}
	}

	req.Header.Set("User-Agent", ua)
	req.Header.Set("Authorization", chargeBasic)
	req.Header.Set("synjones-auth", "bearer "+c.token)
	if method == "POST" && body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}

	var lastErr error
	for attempt := 0; attempt <= 2; attempt++ {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt < 2 {
				slog.Warn("请求失败，重试", "method", method, "path", path, "attempt", attempt+1, "err", err)
				time.Sleep(time.Duration(1.5*float64(attempt+1)) * time.Second)
				continue
			}
			return nil, &ChargeError{Msg: fmt.Sprintf("网络错误(%s): %v", path, err)}
		}

		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			resp.Body.Close()
			return nil, &ChargeAuthError{Msg: fmt.Sprintf("charge 鉴权失败 HTTP %d(%s)", resp.StatusCode, path)}
		}

		return resp, nil
	}
	return nil, &ChargeError{Msg: fmt.Sprintf("请求失败(%s): %v", path, lastErr)}
}

// Establish 走一遍进入 charge 子系统的跳转，拿到会话 cookie（幂等，仅首次真正执行）。
func (c *Client) Establish(feeitemid, appID int) error {
	c.mu.Lock()
	if c.established {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	slog.Info("建立 charge 会话", "feeitemid", feeitemid, "appId", appID)

	params := url.Values{
		"feeitemid":      {fmt.Sprintf("%d", feeitemid)},
		"appId":          {fmt.Sprintf("%d", appID)},
		"loginFrom":      {"h5"},
		"synjones-auth":  {c.token},
	}
	resp, err := c.do("GET", "/charge/feeitem/toAppitem", params, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()

	resp, err = c.do("GET", "/charge/user/caslogin_userxx", nil, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()

	c.mu.Lock()
	c.established = true
	c.mu.Unlock()
	return nil
}

// third 调用 getThirdData 接口并解析响应。
func (c *Client) third(feeitemid int, data url.Values) (map[string]any, error) {
	body := io.NopCloser(strings.NewReader(data.Encode()))
	resp, err := c.do("POST", "/charge/feeitem/getThirdData", nil, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ChargeError{Msg: fmt.Sprintf("读取响应失败: %v", err)}
	}

	var j map[string]any
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, &ChargeAuthError{Msg: "getThirdData 返回非 JSON，疑似登录态失效"}
	}

	code := int(j["code"].(float64))
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
	params := url.Values{
		"feeitemid": {fmt.Sprintf("%d", feeitemid)},
		"type":      {"IEC"},
		"level":     {"3"},
		"campus":    {campus},
		"building":  {building},
		"room":      {room},
	}
	m, err := c.third(feeitemid, params)
	if err != nil {
		return nil, err
	}

	return ParseReading(m), nil
}

// ListCampuses 获取校区列表。
func (c *Client) ListCampuses(feeitemid int) ([]Option, error) {
	params := url.Values{
		"feeitemid": {fmt.Sprintf("%d", feeitemid)},
		"type":      {"select"},
		"level":     {"0"},
	}
	m, err := c.third(feeitemid, params)
	if err != nil {
		return nil, err
	}
	return extractOptions(m)
}

// ListBuildings 获取某个校区下的楼栋列表。
func (c *Client) ListBuildings(feeitemid int, campus string) ([]Option, error) {
	params := url.Values{
		"feeitemid": {fmt.Sprintf("%d", feeitemid)},
		"type":      {"select"},
		"level":     {"1"},
		"campus":    {campus},
	}
	m, err := c.third(feeitemid, params)
	if err != nil {
		return nil, err
	}
	return extractOptions(m)
}

// ListRooms 获取某个楼栋下的房间列表。
func (c *Client) ListRooms(feeitemid int, campus, building string) ([]Option, error) {
	params := url.Values{
		"feeitemid": {fmt.Sprintf("%d", feeitemid)},
		"type":      {"select"},
		"level":     {"2"},
		"campus":    {campus},
		"building":  {building},
	}
	m, err := c.third(feeitemid, params)
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
		opts = append(opts, Option{
			Value: fmt.Sprintf("%v", it["value"]),
			Name:  fmt.Sprintf("%v", it["name"]),
		})
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
		return &val
	case string:
		var f float64
		if _, err := fmt.Sscanf(val, "%f", &f); err == nil {
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
	var f float64
	if _, err := fmt.Sscanf(v, "%f", &f); err != nil {
		return nil
	}
	return &f
}

// Round2 将浮点数四舍五入到小数点后两位。
func Round2(v float64) float64 {
	return math.Round(v*100) / 100
}