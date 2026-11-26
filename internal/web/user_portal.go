package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	"github.com/mico-v/wxxyshall-monitoring/internal/schoollogin"
)

const (
	userTokenHeader       = "X-School-Token"
	userLoginSessionTTL   = 3 * time.Minute
	userChargeClientTTL   = 15 * time.Minute
	userPaymentOrderTTL   = 30 * time.Minute
	maxUserLoginSessions  = 256
	maxUserPaymentRecords = 512
	maxUserChargeClients  = 256
)

type userLoginSession struct {
	session  *schoollogin.Session
	expireAt time.Time
}

type userChargeClient struct {
	client   *charge.Client
	baseURL  string
	lastUsed time.Time
}

type userPaymentOrder struct {
	orderID     string
	roomKey     string
	roomLabel   string
	amount      float64
	method      charge.PaymentMethod
	passwordMap *charge.PasswordMap
	expireAt    time.Time
}

func (s *Server) limitUserLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.userLoginLimiter.Allow(clientIP(r)) {
			w.Header().Set("Retry-After", "10")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "登录尝试过于频繁，请稍后再试"})
			return
		}
		next(w, r)
	}
}

func (s *Server) limitUserRead(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.userReadLimiter.Allow(clientIP(r)) {
			w.Header().Set("Retry-After", "10")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "查询过于频繁，请稍后再试"})
			return
		}
		next(w, r)
	}
}

func (s *Server) limitUserPayment(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.userPaymentLimiter.Allow(clientIP(r)) {
			w.Header().Set("Retry-After", "10")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "充值操作过于频繁，请稍后再试"})
			return
		}
		next(w, r)
	}
}

func (s *Server) handleUserLoginKeyboard(w http.ResponseWriter, r *http.Request) {
	baseURL := s.cfgHub.Config().BaseURL
	session, err := schoollogin.NewClient().StartSession(r.Context(), baseURL)
	if err != nil {
		s.writeUserLoginError(w, err)
		return
	}
	keyboard, err := session.Keyboard(r.Context())
	if err != nil {
		s.writeUserLoginError(w, err)
		return
	}
	sessionID, err := randomID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "创建登录会话失败"})
		return
	}
	now := time.Now()
	s.userPortalMu.Lock()
	pruneUserLoginSessions(s.userLoginSessions, now)
	if len(s.userLoginSessions) >= maxUserLoginSessions {
		s.userPortalMu.Unlock()
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "登录请求过多，请稍后再试"})
		return
	}
	s.userLoginSessions[sessionID] = &userLoginSession{
		session:  session,
		expireAt: now.Add(userLoginSessionTTL),
	}
	s.userPortalMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"expires_in": int(userLoginSessionTTL.Seconds()),
		"keyboard":   keyboard,
	})
}

func (s *Server) handleUserLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID             string `json:"session_id"`
		Username              string `json:"username"`
		MappedPassword        string `json:"mapped_password"`
		ConfirmMonitorSession bool   `json:"confirm_monitor_session"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, jsonDecodeStatus(err), map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	body.SessionID = strings.TrimSpace(body.SessionID)
	body.Username = strings.TrimSpace(body.Username)
	if body.SessionID == "" || len(body.SessionID) > 128 ||
		body.Username == "" || body.MappedPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "登录参数不完整"})
		return
	}
	if len(body.Username) > 128 || len(body.MappedPassword) > 512 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "账号或映射密码过长"})
		return
	}
	monitoredAccount := s.isMonitoredSchoolAccount(body.Username, "")
	now := time.Now()
	s.userPortalMu.Lock()
	login := s.userLoginSessions[body.SessionID]
	if login == nil || now.After(login.expireAt) {
		delete(s.userLoginSessions, body.SessionID)
		s.userPortalMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "登录会话已失效，请重新打开登录页"})
		return
	}
	if monitoredAccount && !body.ConfirmMonitorSession {
		s.userPortalMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":                         "该账号同时用于服务器监控。学校对同一账号只保留一个设备会话，继续登录会使监控 token 失效。",
			"requires_monitor_confirmation": true,
		})
		return
	}
	delete(s.userLoginSessions, body.SessionID)
	s.userPortalMu.Unlock()
	token, err := login.session.LoginMapped(r.Context(), body.Username, body.MappedPassword)
	if err != nil {
		s.writeUserLoginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":    token.AccessToken,
		"expires_in":      token.ExpiresIn,
		"login_time":      token.LoginTime,
		"sno":             token.Sno,
		"source":          "browser",
		"monitor_account": s.isMonitoredSchoolAccount(body.Username, token.Sno),
	})
}

func (s *Server) isMonitoredSchoolAccount(username, sno string) bool {
	username = strings.TrimSpace(username)
	sno = strings.TrimSpace(sno)
	cfg := s.cfgHub.Config()
	tok := s.cfgHub.Token()
	monitoredSno := ""
	if tok != nil {
		monitoredSno = strings.TrimSpace(tok.Sno)
	}
	return (username != "" && strings.EqualFold(username, strings.TrimSpace(cfg.Username))) ||
		(sno != "" && ((monitoredSno != "" && sno == monitoredSno) ||
			(username != "" && strings.EqualFold(username, monitoredSno))))
}

func (s *Server) writeUserLoginError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, schoollogin.ErrCredentialsRejected),
		errors.Is(err, schoollogin.ErrLoginRejected):
		status = http.StatusUnauthorized
	case errors.Is(err, schoollogin.ErrLoginChallenge):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (s *Server) handleUserCampuses(w http.ResponseWriter, r *http.Request) {
	client, cleanup, err := s.userChargeClient(r)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	defer cleanup()
	options, err := client.ListCampusesContext(r.Context(), config.DefaultFeeItemID)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, options)
}

func (s *Server) handleUserBuildings(w http.ResponseWriter, r *http.Request) {
	campus := strings.TrimSpace(r.URL.Query().Get("campus"))
	if campus == "" || len(campus) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campus 参数无效"})
		return
	}
	client, cleanup, err := s.userChargeClient(r)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	defer cleanup()
	options, err := client.ListBuildingsContext(r.Context(), config.DefaultFeeItemID, campus)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, options)
}

func (s *Server) handleUserRooms(w http.ResponseWriter, r *http.Request) {
	campus := strings.TrimSpace(r.URL.Query().Get("campus"))
	building := strings.TrimSpace(r.URL.Query().Get("building"))
	if campus == "" || building == "" || len(campus) > 128 || len(building) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campus/building 参数无效"})
		return
	}
	client, cleanup, err := s.userChargeClient(r)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	defer cleanup()
	options, err := client.ListRoomsContext(r.Context(), config.DefaultFeeItemID, campus, building)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, options)
}

func (s *Server) handleUserRoom(w http.ResponseWriter, r *http.Request) {
	campus, building, room, ok := userRoomQuery(w, r)
	if !ok {
		return
	}
	client, cleanup, err := s.userChargeClient(r)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	defer cleanup()
	snapshot, err := client.QueryRoomSnapshotContext(
		r.Context(), config.DefaultFeeItemID, campus, building, room,
	)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, userRoomResponse(campus, building, room, snapshot))
}

func (s *Server) handleUserOrders(w http.ResponseWriter, r *http.Request) {
	status, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil || (status != 0 && status != 1) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status 必须是 0 或 1"})
		return
	}
	client, cleanup, err := s.userChargeClient(r)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	defer cleanup()
	orders, err := client.ListRechargeOrdersContext(r.Context(), status)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": orders})
}

func (s *Server) handleUserCreateOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Campus   string  `json:"campus"`
		Building string  `json:"building"`
		Room     string  `json:"room"`
		Amount   float64 `json:"amount"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, jsonDecodeStatus(err), map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	body.Campus = strings.TrimSpace(body.Campus)
	body.Building = strings.TrimSpace(body.Building)
	body.Room = strings.TrimSpace(body.Room)
	if body.Campus == "" || body.Building == "" || body.Room == "" ||
		len(body.Campus) > 128 || len(body.Building) > 128 || len(body.Room) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "房间参数不完整"})
		return
	}
	if math.IsNaN(body.Amount) || math.IsInf(body.Amount, 0) ||
		body.Amount < minUserRechargeAmount || body.Amount > maxUserRechargeAmount {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "充值金额必须在 1..500 元之间"})
		return
	}
	body.Amount = math.Round(body.Amount*100) / 100

	client, cleanup, err := s.userChargeClient(r)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	defer cleanup()
	snapshot, err := client.QueryRoomSnapshotContext(
		r.Context(), config.DefaultFeeItemID, body.Campus, body.Building, body.Room,
	)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	orderID, err := client.CreateElectricityOrderContext(
		r.Context(), config.DefaultFeeItemID, body.Amount, snapshot.Map,
	)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}

	order, methods, err := client.GetOrderContext(r.Context(), orderID)
	if err != nil {
		_ = client.CancelOrderContext(context.Background(), orderID)
		s.writeUserChargeError(w, r, err)
		return
	}
	roomKey := body.Campus + "|" + body.Building + "|" + body.Room
	if !orderMatchesRoom(order, body.Room, roomKey) || math.Abs(order.TranAmount-body.Amount) > 0.001 || order.Status != 0 {
		_ = client.CancelOrderContext(context.Background(), orderID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "学校返回的订单房间或金额不一致，订单已取消"})
		return
	}
	method, ok := cardPaymentMethod(methods)
	if !ok {
		_ = client.CancelOrderContext(context.Background(), orderID)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "当前订单没有校园卡余额支付方式，订单已取消"})
		return
	}

	paymentKey, err := s.userPaymentKey(r, orderID)
	if err != nil {
		_ = client.CancelOrderContext(context.Background(), orderID)
		s.writeUserChargeError(w, r, err)
		return
	}
	now := time.Now()
	s.userPortalMu.Lock()
	pruneUserPaymentOrders(s.userPaymentOrders, now)
	if len(s.userPaymentOrders) >= maxUserPaymentRecords {
		s.userPortalMu.Unlock()
		_ = client.CancelOrderContext(context.Background(), orderID)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "待支付订单过多，请稍后再试"})
		return
	}
	s.userPaymentOrders[paymentKey] = &userPaymentOrder{
		orderID:   orderID,
		roomKey:   roomKey,
		roomLabel: userRoomLabel(snapshot.Map),
		amount:    body.Amount,
		method:    method,
		expireAt:  now.Add(userPaymentOrderTTL),
	}
	s.userPortalMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"order":          order,
		"payment_method": method,
		"amount_limits": map[string]float64{
			"min": minUserRechargeAmount,
			"max": maxUserRechargeAmount,
		},
	})
}

func (s *Server) handleUserPreparePayment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrderID string `json:"order_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, jsonDecodeStatus(err), map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	body.OrderID = strings.TrimSpace(body.OrderID)
	if !validUserOrderID(body.OrderID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "order_id 参数无效"})
		return
	}
	payment, ok := s.userPayment(r, body.OrderID)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "支付状态已失效，请返回房间重新发起充值"})
		return
	}
	client, cleanup, err := s.userChargeClient(r)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	defer cleanup()
	order, methods, err := client.GetOrderContext(r.Context(), payment.orderID)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	method, ok := cardPaymentMethod(methods)
	if !ok || method.PayID != payment.method.PayID || order.Status != 0 ||
		math.Abs(order.TranAmount-payment.amount) > 0.001 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "订单状态或支付方式已变化，请重新发起充值"})
		return
	}
	passwordMap, err := client.GetPasswordMapContext(r.Context(), payment.orderID, method)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	payment.method = method
	payment.passwordMap = passwordMap
	payment.expireAt = time.Now().Add(userPaymentOrderTTL)
	key, err := s.userPaymentKey(r, body.OrderID)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	s.userPortalMu.Lock()
	s.userPaymentOrders[key] = &payment
	s.userPortalMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"order":          order,
		"payment_method": method,
		"password_map":   passwordMap,
	})
}

func (s *Server) handleUserPay(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrderID         string `json:"order_id"`
		UUID            string `json:"uuid"`
		PasswordIndexes string `json:"password_indexes"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, jsonDecodeStatus(err), map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	body.OrderID = strings.TrimSpace(body.OrderID)
	body.UUID = strings.TrimSpace(body.UUID)
	if !validUserOrderID(body.OrderID) ||
		body.UUID == "" || len(body.UUID) > 256 || len(body.PasswordIndexes) != 6 ||
		strings.IndexFunc(body.PasswordIndexes, func(char rune) bool {
			return char < '0' || char > '9'
		}) >= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "支付密码映射无效"})
		return
	}
	payment, ok := s.claimUserPayment(r, body.OrderID, body.UUID)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "支付验证已失效，请重新获取安全键盘"})
		return
	}
	client, cleanup, err := s.userChargeClient(r)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	defer cleanup()
	order, _, err := client.GetOrderContext(r.Context(), payment.orderID)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	if order.Status != 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "订单已不是待支付状态"})
		return
	}
	result, err := client.SubmitPasswordContext(
		r.Context(),
		payment.orderID,
		payment.method,
		body.PasswordIndexes,
		body.UUID,
	)
	if err != nil {
		s.writeUserPaymentError(w, r, err)
		return
	}
	slog.Info("用户充值支付已提交", "amount", payment.amount, "room", payment.roomLabel, "status", order.Status)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"order_id":  payment.orderID,
		"result":    result,
		"poll_hint": "支付结果可能异步更新，请查询订单状态和宿舍余额",
	})
}

func (s *Server) handleUserCancelOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrderID string `json:"order_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, jsonDecodeStatus(err), map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	body.OrderID = strings.TrimSpace(body.OrderID)
	if !validUserOrderID(body.OrderID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "order_id 参数无效"})
		return
	}
	client, cleanup, err := s.userChargeClient(r)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	defer cleanup()
	order, _, err := client.GetOrderContext(r.Context(), body.OrderID)
	if err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	if order.Status != 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "只能取消待支付订单"})
		return
	}
	if err := client.CancelOrderContext(r.Context(), body.OrderID); err != nil {
		s.writeUserChargeError(w, r, err)
		return
	}
	if key, err := s.userPaymentKey(r, body.OrderID); err == nil {
		s.userPortalMu.Lock()
		delete(s.userPaymentOrders, key)
		s.userPortalMu.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) userChargeClient(r *http.Request) (*charge.Client, func(), error) {
	token := strings.TrimSpace(r.Header.Get(userTokenHeader))
	if len(token) < 16 || len(token) > 64<<10 {
		return nil, func() {}, &charge.ChargeAuthError{Msg: "学校登录状态无效，请重新登录"}
	}
	sum := sha256.Sum256([]byte(token))
	key := hex.EncodeToString(sum[:])
	cfg := s.cfgHub.Config()
	now := time.Now()
	s.userPortalMu.Lock()
	for cacheKey, cached := range s.userChargeClients {
		if now.Sub(cached.lastUsed) > userChargeClientTTL {
			delete(s.userChargeClients, cacheKey)
		}
	}
	cached := s.userChargeClients[key]
	if cached == nil || cached.baseURL != cfg.BaseURL {
		if len(s.userChargeClients) >= maxUserChargeClients {
			s.userPortalMu.Unlock()
			return nil, func() {}, &charge.ChargeError{Msg: "用户会话过多，请稍后再试"}
		}
		cached = &userChargeClient{
			client:  charge.NewClientOnce(cfg.BaseURL, token),
			baseURL: cfg.BaseURL,
		}
		s.userChargeClients[key] = cached
	}
	cached.lastUsed = now
	s.userPortalMu.Unlock()
	if err := cached.client.EstablishContext(
		r.Context(), config.DefaultFeeItemID, config.DefaultAppID,
	); err != nil {
		s.userPortalMu.Lock()
		if s.userChargeClients[key] == cached {
			delete(s.userChargeClients, key)
		}
		s.userPortalMu.Unlock()
		return nil, func() {}, err
	}
	return cached.client, func() {
		s.userPortalMu.Lock()
		cached.lastUsed = time.Now()
		s.userPortalMu.Unlock()
	}, nil
}

func (s *Server) userPayment(r *http.Request, orderID string) (userPaymentOrder, bool) {
	if orderID == "" {
		return userPaymentOrder{}, false
	}
	key, err := s.userPaymentKey(r, orderID)
	if err != nil {
		return userPaymentOrder{}, false
	}
	now := time.Now()
	s.userPortalMu.Lock()
	defer s.userPortalMu.Unlock()
	pruneUserPaymentOrders(s.userPaymentOrders, now)
	payment := s.userPaymentOrders[key]
	if payment == nil || now.After(payment.expireAt) {
		delete(s.userPaymentOrders, key)
		return userPaymentOrder{}, false
	}
	return *payment, true
}

func (s *Server) claimUserPayment(r *http.Request, orderID, uuid string) (userPaymentOrder, bool) {
	if orderID == "" || uuid == "" {
		return userPaymentOrder{}, false
	}
	key, err := s.userPaymentKey(r, orderID)
	if err != nil {
		return userPaymentOrder{}, false
	}
	now := time.Now()
	s.userPortalMu.Lock()
	defer s.userPortalMu.Unlock()
	pruneUserPaymentOrders(s.userPaymentOrders, now)
	payment := s.userPaymentOrders[key]
	if payment == nil || now.After(payment.expireAt) ||
		payment.passwordMap == nil || payment.passwordMap.UUID != uuid {
		delete(s.userPaymentOrders, key)
		return userPaymentOrder{}, false
	}
	// 领取即从缓存删除。即使学校响应超时，也不允许第二次提交同一密码映射。
	delete(s.userPaymentOrders, key)
	return *payment, true
}

func (s *Server) userPaymentKey(r *http.Request, orderID string) (string, error) {
	token := strings.TrimSpace(r.Header.Get(userTokenHeader))
	if token == "" {
		return "", &charge.ChargeAuthError{Msg: "学校登录状态无效"}
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:]) + "|" + orderID, nil
}

func (s *Server) writeUserChargeError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器错误"})
		return
	}
	if charge.IsAuthError(err) {
		s.forgetUserChargeClient(r)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "学校登录已失效，请重新登录"})
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		writeJSON(w, http.StatusRequestTimeout, map[string]string{"error": "学校请求已取消或超时"})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
}

func (s *Server) forgetUserChargeClient(r *http.Request) {
	token := strings.TrimSpace(r.Header.Get(userTokenHeader))
	if token == "" {
		return
	}
	sum := sha256.Sum256([]byte(token))
	key := hex.EncodeToString(sum[:])
	s.userPortalMu.Lock()
	delete(s.userChargeClients, key)
	s.userPortalMu.Unlock()
}

func (s *Server) writeUserPaymentError(w http.ResponseWriter, r *http.Request, err error) {
	if charge.IsAuthError(err) {
		s.writeUserChargeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func userRoomQuery(w http.ResponseWriter, r *http.Request) (string, string, string, bool) {
	campus := strings.TrimSpace(r.URL.Query().Get("campus"))
	building := strings.TrimSpace(r.URL.Query().Get("building"))
	room := strings.TrimSpace(r.URL.Query().Get("room"))
	if campus == "" || building == "" || room == "" ||
		len(campus) > 128 || len(building) > 128 || len(room) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campus/building/room 参数无效"})
		return "", "", "", false
	}
	return campus, building, room, true
}

func validUserOrderID(orderID string) bool {
	return orderID != "" && len(orderID) <= 256
}

func userRoomResponse(campus, building, room string, snapshot *charge.RoomSnapshot) map[string]any {
	rawRoom, _ := snapshot.Map["data"].(map[string]any)
	reading := map[string]any{
		"surplus_charge": snapshot.Reading.SurplusCharge,
		"show":           snapshot.Reading.Show,
	}
	return map[string]any{
		"campus":         campus,
		"building":       building,
		"room":           room,
		"room_name":      displayValue(rawRoom["room"]),
		"building_name":  displayValue(rawRoom["building"]),
		"campus_name":    displayValue(rawRoom["campus"]),
		"reading":        reading,
		"amount_options": []float64{10, 50, 100},
		"amount_limits": map[string]float64{
			"min": minUserRechargeAmount,
			"max": maxUserRechargeAmount,
		},
	}
}

func userRoomLabel(roomMap map[string]any) string {
	rawRoom, _ := roomMap["data"].(map[string]any)
	parts := []string{
		displayValue(rawRoom["campus"]),
		displayValue(rawRoom["building"]),
		displayValue(rawRoom["room"]),
	}
	return strings.Join(parts, " ")
}

func displayValue(value any) string {
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func cardPaymentMethod(methods []charge.PaymentMethod) (charge.PaymentMethod, bool) {
	for _, method := range methods {
		if method.Code == "CARDTSM" && method.PayID > 0 {
			return method, true
		}
	}
	return charge.PaymentMethod{}, false
}

func orderMatchesRoom(order *charge.RechargeOrder, roomID, roomKey string) bool {
	if order == nil || order.Room == nil {
		return false
	}
	if roomID != "" && (order.Room.RoomID == roomID || order.Room.Room == roomID) {
		return true
	}
	return strings.TrimSpace(order.Room.CampusID+"|"+order.Room.BuildingID+"|"+order.Room.RoomID) == roomKey
}

func randomID() (string, error) {
	data := make([]byte, 18)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func pruneUserLoginSessions(values map[string]*userLoginSession, now time.Time) {
	for key, value := range values {
		if value == nil || now.After(value.expireAt) {
			delete(values, key)
		}
	}
}

func pruneUserPaymentOrders(values map[string]*userPaymentOrder, now time.Time) {
	for key, value := range values {
		if value == nil || now.After(value.expireAt) {
			delete(values, key)
		}
	}
}
