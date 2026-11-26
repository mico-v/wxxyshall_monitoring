package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

func TestUserLoginUsesMappedPasswordAndReturnsBrowserSession(t *testing.T) {
	server := newTestServer(t)
	var tokenPassword string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/berserker-app/frontInfo":
			_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
		case "/berserker-secure/keyboard":
			_, _ = w.Write(schoolKeyboardJSON("browser-keyboard"))
		case "/berserker-auth/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			tokenPassword = r.Form.Get("password")
			_, _ = w.Write([]byte(`{"access_token":"browser-access","expires_in":3600,"sno":"20260001"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	if _, err := server.cfgHub.UpdateConfig(func(cfg *config.Config) error {
		cfg.BaseURL = upstream.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	keyboardRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(keyboardRec, httptest.NewRequest(
		http.MethodPost, "/api/user/login/keyboard", nil,
	))
	if keyboardRec.Code != http.StatusOK {
		t.Fatalf("keyboard status = %d body=%s", keyboardRec.Code, keyboardRec.Body.String())
	}
	var keyboard struct {
		SessionID string              `json:"session_id"`
		Keyboard  schoolloginKeyboard `json:"keyboard"`
	}
	if err := json.Unmarshal(keyboardRec.Body.Bytes(), &keyboard); err != nil {
		t.Fatal(err)
	}
	if keyboard.SessionID == "" || keyboard.Keyboard.UUID != "browser-keyboard" {
		t.Fatalf("unexpected keyboard response: %+v", keyboard)
	}

	loginRec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(
		`{"session_id":"`+keyboard.SessionID+`","username":"20260001","mapped_password":"mapped-987"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(loginRec, req)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", loginRec.Code, loginRec.Body.String())
	}
	if tokenPassword != "mapped-987$1$browser-keyboard" {
		t.Fatalf("token password = %q, want mapped password and keyboard uuid", tokenPassword)
	}
	if strings.Contains(loginRec.Body.String(), "mapped-987") {
		t.Fatal("browser login response must not echo the mapped password")
	}
	var session map[string]any
	if err := json.Unmarshal(loginRec.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session["access_token"] != "browser-access" || session["sno"] != "20260001" {
		t.Fatalf("unexpected browser session: %#v", session)
	}
	if _, ok := session["refresh_token"]; ok {
		t.Fatal("browser session must not expose an unused refresh token")
	}
}

type schoolloginKeyboard struct {
	UUID string `json:"uuid"`
}

func TestUserAPIRejectsMissingSchoolToken(t *testing.T) {
	server := newTestServer(t)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/user/campuses", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s, want 401", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "重新登录") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestUserPortalRoutesServeSPA(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{
		"/user",
		"/user/room/campus/building/room",
	} {
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Fatalf("%s content type = %q", path, got)
		}
	}
}

func TestUserOrderValidationRunsBeforeUpstreamRequest(t *testing.T) {
	server := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/recharge/order", strings.NewReader(
		`{"campus":"A","building":"B","room":"C","amount":0}`,
	))
	req.Header.Set(userTokenHeader, "0123456789abcdef")
	req.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestUserPaymentClaimIsOneUse(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set(userTokenHeader, "0123456789abcdef")
	key, err := server.userPaymentKey(req, "order-1")
	if err != nil {
		t.Fatal(err)
	}
	server.userPaymentOrders[key] = &userPaymentOrder{
		orderID: "order-1",
		amount:  10,
		method: charge.PaymentMethod{
			PayID: 65,
			Code:  "CARDTSM",
			Name:  "卡账户",
		},
		passwordMap: &charge.PasswordMap{
			UUID: "uuid-1",
			Keys: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 0},
		},
		expireAt: time.Now().Add(time.Minute),
	}
	if _, ok := server.claimUserPayment(req, "order-1", "uuid-1"); !ok {
		t.Fatal("first claim should succeed")
	}
	if _, ok := server.claimUserPayment(req, "order-1", "uuid-1"); ok {
		t.Fatal("second claim must fail")
	}
}

func TestUserRoomResponseExcludesReadingRaw(t *testing.T) {
	snapshot := &charge.RoomSnapshot{
		Reading: &charge.Reading{
			Show: map[string]string{"当前剩余电量": "10.5"},
			Raw:  map[string]any{"private": "must-not-leak"},
		},
		Map: map[string]any{
			"data": map[string]any{
				"campus":   "校区",
				"building": "楼栋",
				"room":     "房间",
			},
		},
	}
	body, err := json.Marshal(userRoomResponse("campus", "building", "room", snapshot))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "must-not-leak") {
		t.Fatalf("room response leaked raw reading: %s", body)
	}
	if !strings.Contains(string(body), "当前剩余电量") {
		t.Fatalf("room response omitted public reading fields: %s", body)
	}
}
