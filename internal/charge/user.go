package charge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

var ErrNoCardPayment = errors.New("学校未提供校园卡余额支付方式")

// RoomSnapshot 是房间余额查询的完整结果，包含创建充值订单所需的原始通信信息。
type RoomSnapshot struct {
	Reading *Reading
	Map     map[string]any
}

// RechargeOrder 是脱敏后可供前端展示的学校订单。
type RechargeOrder struct {
	OrderID       string         `json:"order_id"`
	TranAmount    float64        `json:"tran_amount"`
	ActualAmount  *float64       `json:"actual_amount,omitempty"`
	Status        int            `json:"status"`
	CommitDate    string         `json:"commit_date,omitempty"`
	SuccessDate   string         `json:"success_date,omitempty"`
	PayExpireDate string         `json:"pay_expire_date,omitempty"`
	PayID         *int           `json:"pay_id,omitempty"`
	Abstracts     string         `json:"abstracts,omitempty"`
	FeeItemID     int            `json:"feeitemid,omitempty"`
	Room          *OrderRoom     `json:"room,omitempty"`
	Raw           map[string]any `json:"-"`
}

// OrderRoom 是订单里可公开给当前浏览器用户的房间信息。
type OrderRoom struct {
	Campus     string `json:"campus,omitempty"`
	Building   string `json:"building,omitempty"`
	Room       string `json:"room,omitempty"`
	CampusID   string `json:"campus_id,omitempty"`
	BuildingID string `json:"building_id,omitempty"`
	RoomID     string `json:"room_id,omitempty"`
}

// PaymentMethod 是学校允许的付款方式。
type PaymentMethod struct {
	PayID         int    `json:"pay_id"`
	Name          string `json:"name"`
	Code          string `json:"code"`
	NoPassword    int    `json:"no_password"`
	ChooseAccount bool   `json:"choose_account"`
}

// PasswordMap 是校园卡支付安全键盘。
type PasswordMap struct {
	UUID string `json:"uuid"`
	Keys []int  `json:"keys"`
}

// QueryRoomSnapshotContext 返回完整房间信息和归一化余额。
func (c *Client) QueryRoomSnapshotContext(
	ctx context.Context,
	feeitemid int,
	campus, building, room string,
) (*RoomSnapshot, error) {
	params := url.Values{
		"feeitemid": {strconv.Itoa(feeitemid)},
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
	return &RoomSnapshot{Reading: ParseReading(m), Map: m}, nil
}

// ListRechargeOrdersContext 查询学校个人充值/缴费订单。
func (c *Client) ListRechargeOrdersContext(ctx context.Context, status int) ([]RechargeOrder, error) {
	resp, err := c.do(ctx, http.MethodGet, "/charge/order/personal_data", url.Values{
		"status": {strconv.Itoa(status)},
	}, nil)
	if err != nil {
		return nil, err
	}
	body, err := c.successJSON(resp, "/charge/order/personal_data")
	if err != nil {
		return nil, err
	}
	items, _ := body["orderList"].([]any)
	orders := make([]RechargeOrder, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		orders = append(orders, summarizeOrder(raw))
	}
	return orders, nil
}

// GetOrderContext 读取单个订单及当前可用支付方式。
func (c *Client) GetOrderContext(ctx context.Context, orderID string) (*RechargeOrder, []PaymentMethod, error) {
	resp, err := c.do(ctx, http.MethodGet, "/charge/pay/getpayinfo", url.Values{
		"orderid": {orderID},
	}, nil)
	if err != nil {
		return nil, nil, err
	}
	body, err := c.successJSON(resp, "/charge/pay/getpayinfo")
	if err != nil {
		return nil, nil, err
	}
	rawOrder, _ := body["order"].(map[string]any)
	if rawOrder == nil {
		return nil, nil, &ChargeError{Msg: "支付信息缺少 order 字段"}
	}
	order := summarizeOrder(rawOrder)
	payItems, _ := body["payList"].([]any)
	methods := make([]PaymentMethod, 0, len(payItems))
	for _, item := range payItems {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		methods = append(methods, PaymentMethod{
			PayID:         toInt(raw["payid"]),
			Name:          fmt.Sprintf("%v", raw["name"]),
			Code:          fmt.Sprintf("%v", raw["code"]),
			NoPassword:    toInt(raw["nopassword"]),
			ChooseAccount: toBool(raw["chooseAccount"]),
		})
	}
	return &order, methods, nil
}

// CreateElectricityOrderContext 创建待支付订单。该请求不会自动重试。
func (c *Client) CreateElectricityOrderContext(
	ctx context.Context,
	feeitemid int,
	amount float64,
	roomMap map[string]any,
) (string, error) {
	rawRoom, _ := roomMap["data"].(map[string]any)
	if rawRoom == nil {
		return "", &ChargeError{Msg: "房间信息缺少 data 字段"}
	}
	thirdParty := cloneAnyMap(rawRoom)
	campus := strings.TrimSpace(fmt.Sprintf("%v", rawRoom["campus"]))
	building := strings.TrimSpace(fmt.Sprintf("%v", rawRoom["building"]))
	room := strings.TrimSpace(fmt.Sprintf("%v", rawRoom["room"]))
	thirdParty["myCustomInfo"] = strings.TrimSpace(
		"房间：" + strings.Join([]string{campus, building, room}, " "),
	)
	thirdPartyJSON, err := json.Marshal(thirdParty)
	if err != nil {
		return "", &ChargeError{Msg: "序列化充值房间信息失败", Err: err}
	}
	abstracts := fmt.Sprintf(
		"公寓-%s;楼层-%s;房间-%s",
		campus,
		building,
		room,
	)
	form := url.Values{
		"feeitemid":   {strconv.Itoa(feeitemid)},
		"tranamt":     {strconv.FormatFloat(amount, 'f', 2, 64)},
		"flag":        {"choose"},
		"source":      {"app"},
		"paystep":     {"0"},
		"abstracts":   {abstracts},
		"third_party": {string(thirdPartyJSON)},
	}
	resp, err := c.doOnce(ctx, http.MethodPost, "/blade-pay/pay", form)
	if err != nil {
		return "", err
	}
	body, err := c.successJSON(resp, "/blade-pay/pay")
	if err != nil {
		return "", err
	}
	data, _ := body["data"].(map[string]any)
	orderID := strings.TrimSpace(fmt.Sprintf("%v", data["orderid"]))
	if data == nil || orderID == "" || orderID == "<nil>" {
		return "", &ChargeError{Msg: "创建订单响应缺少 orderid"}
	}
	return orderID, nil
}

// GetPasswordMapContext 获取本次校园卡安全键盘，不提交密码。该请求不会自动重试。
func (c *Client) GetPasswordMapContext(
	ctx context.Context,
	orderID string,
	method PaymentMethod,
) (*PasswordMap, error) {
	form := url.Values{
		"orderid":   {orderID},
		"paytype":   {method.Code},
		"paytypeid": {strconv.Itoa(method.PayID)},
		"paystep":   {"2"},
	}
	resp, err := c.doOnce(ctx, http.MethodPost, "/blade-pay/pay", form)
	if err != nil {
		return nil, err
	}
	body, err := c.successJSON(resp, "/blade-pay/pay")
	if err != nil {
		return nil, err
	}
	data, _ := body["data"].(map[string]any)
	maps, _ := data["passwordMap"].(map[string]any)
	if len(maps) != 1 {
		return nil, &ChargeError{Msg: "支付响应缺少 passwordMap"}
	}
	uuids := make([]string, 0, len(maps))
	for uuid := range maps {
		uuids = append(uuids, uuid)
	}
	sort.Strings(uuids)
	uuid := uuids[0]
	rawKeys, _ := maps[uuid].([]any)
	keys := make([]int, 0, len(rawKeys))
	for _, raw := range rawKeys {
		keys = append(keys, toInt(raw))
	}
	if len(keys) != 10 {
		return nil, &ChargeError{Msg: "支付安全键盘映射不完整"}
	}
	seen := make(map[int]bool, len(keys))
	for _, key := range keys {
		if key < 0 || key > 9 || seen[key] {
			return nil, &ChargeError{Msg: "支付安全键盘映射无效"}
		}
		seen[key] = true
	}
	return &PasswordMap{UUID: uuid, Keys: keys}, nil
}

// SubmitPasswordContext 提交浏览器计算出的安全键盘位置索引。该请求绝不自动重试。
func (c *Client) SubmitPasswordContext(
	ctx context.Context,
	orderID string,
	method PaymentMethod,
	passwordIndexes string,
	uuid string,
) (map[string]any, error) {
	if !isSixDigits(passwordIndexes) || strings.TrimSpace(uuid) == "" {
		return nil, &ChargeError{Msg: "支付密码映射无效"}
	}
	form := url.Values{
		"orderid":   {orderID},
		"paystep":   {"2"},
		"paytype":   {method.Code},
		"paytypeid": {strconv.Itoa(method.PayID)},
		"password":  {passwordIndexes},
		"uuid":      {uuid},
		"isWX":      {"0"},
		"userAgent": {"h5"},
	}
	resp, err := c.doOnce(ctx, http.MethodPost, "/blade-pay/pay", form)
	if err != nil {
		return nil, err
	}
	return c.successJSON(resp, "/blade-pay/pay")
}

// CancelOrderContext 取消待支付订单。该请求不会自动重试。
func (c *Client) CancelOrderContext(ctx context.Context, orderID string) error {
	resp, err := c.doOnce(ctx, http.MethodPost, "/charge/order/deleteOrder", url.Values{
		"orderid": {orderID},
	})
	if err != nil {
		return err
	}
	_, err = c.successJSON(resp, "/charge/order/deleteOrder")
	return err
}

func (c *Client) doOnce(ctx context.Context, method, path string, form url.Values) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, &ChargeError{Msg: fmt.Sprintf("创建请求失败(%s): %v", path, err), Err: err}
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Authorization", chargeBasic)
	req.Header.Set("synjones-auth", "bearer "+c.token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &ChargeError{Msg: "请求已取消", Err: ctx.Err()}
		}
		return nil, &ChargeError{Msg: fmt.Sprintf("网络错误(%s): %v", path, err), Err: err}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		return nil, &ChargeAuthError{Msg: fmt.Sprintf("charge 鉴权失败 HTTP %d(%s)", resp.StatusCode, path)}
	}
	return resp, nil
}

func (c *Client) successJSON(resp *http.Response, path string) (map[string]any, error) {
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil {
		return nil, &ChargeError{Msg: fmt.Sprintf("读取响应失败(%s): %v", path, err), Err: err}
	}
	if len(data) > 8<<20 {
		return nil, &ChargeError{Msg: fmt.Sprintf("学校响应超过 8 MiB(%s)", path)}
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, &ChargeError{Msg: fmt.Sprintf("学校返回非 JSON(HTTP %d, %s)", resp.StatusCode, path)}
	}
	code, ok := responseCode(body["code"])
	if !ok {
		return nil, &ChargeError{Msg: fmt.Sprintf("学校响应缺少有效 code(%s)", path)}
	}
	if authCodes[code] {
		return nil, &ChargeAuthError{Msg: fmt.Sprintf("charge 鉴权失败 code=%d(%s)", code, path)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || code != 200 {
		message := strings.TrimSpace(fmt.Sprintf("%v", body["msg"]))
		if message == "" || message == "<nil>" {
			message = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return nil, &ChargeError{Msg: message}
	}
	return body, nil
}

func summarizeOrder(raw map[string]any) RechargeOrder {
	order := RechargeOrder{
		OrderID:       stringValue(raw["orderid"]),
		TranAmount:    floatValue(raw["tranamt"]),
		Status:        toInt(raw["status"]),
		CommitDate:    stringValue(raw["commitdate"]),
		SuccessDate:   stringValue(raw["successdate"]),
		PayExpireDate: stringValue(raw["payexpdate"]),
		Abstracts:     stringValue(raw["abstracts"]),
		Raw:           raw,
	}
	if amount, ok := optionalFloat(raw["actulamt"]); ok {
		order.ActualAmount = &amount
	}
	if payID, ok := optionalInt(raw["payid"]); ok {
		order.PayID = &payID
	}
	if items, _ := raw["feeitemlist"].([]any); len(items) > 0 {
		if item, ok := items[0].(map[string]any); ok {
			order.FeeItemID = toInt(item["feeitemid"])
		}
	}
	var third map[string]any
	switch value := raw["third_party"].(type) {
	case map[string]any:
		third = value
	case string:
		_ = json.Unmarshal([]byte(value), &third)
	}
	if third != nil {
		order.Room = &OrderRoom{
			Campus:     stringValue(third["campus"]),
			Building:   stringValue(third["building"]),
			Room:       stringValue(third["room"]),
			CampusID:   stringValue(third["campusid"]),
			BuildingID: stringValue(third["buildingid"]),
			RoomID:     stringValue(third["roomid"]),
		}
	}
	return order
}

func cloneAnyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func isSixDigits(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func floatValue(value any) float64 {
	if number, ok := optionalFloat(value); ok {
		return number
	}
	return 0
}

func optionalFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func toInt(value any) int {
	number, ok := optionalInt(value)
	if !ok {
		return 0
	}
	return number
}

func optionalInt(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case json.Number:
		number, err := typed.Int64()
		return int(number), err == nil
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(typed))
		return number, err == nil
	default:
		return 0, false
	}
}

func toBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		text := strings.TrimSpace(strings.ToLower(typed))
		return text == "true" || text == "1"
	case float64:
		return typed != 0
	default:
		return false
	}
}
