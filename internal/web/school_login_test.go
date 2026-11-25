package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

func schoolKeyboardJSON(uuid string) []byte {
	data, _ := json.Marshal(map[string]any{
		"code": 200,
		"data": map[string]any{
			"uuid":                uuid,
			"numberKeyboard":      "0123456789",
			"lowerLetterKeyboard": "qwertyuiopasdfghjklzxcvbnm",
			"upperLetterKeyboard": "QWERTYUIOPASDFGHJKLZXCVBNM",
			"symbolKeyboard": string([]rune{
				'*', '\\', '-', '[', ']', '{', '}', '/', '!', ',', '<', '>', '?', '~', '&',
				'@', '#', '.', ':', '+', '|', '`', '%', '\'', '$', ';', '^', '"', '_',
			}),
		},
	})
	return data
}

func TestSchoolLoginStoresTokenAndUpdatesUsername(t *testing.T) {
	server := newTestServer(t)
	school := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/berserker-app/frontInfo":
			_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
		case "/berserker-secure/keyboard":
			_, _ = w.Write(schoolKeyboardJSON("web-login-keyboard"))
		case "/berserker-auth/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			if got := r.Form.Get("username"); got != "20260001" {
				t.Errorf("username = %q", got)
			}
			if got := r.Form.Get("password"); got != "Test#Pass123$1$web-login-keyboard" {
				t.Errorf("password was not encoded through the keyboard challenge")
			}
			_, _ = w.Write([]byte(`{"access_token":"school-access","refresh_token":"school-refresh","expires_in":6047999,"sno":"20260001"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer school.Close()
	if _, err := server.cfgHub.UpdateConfig(func(cfg *config.Config) error {
		cfg.BaseURL = school.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/school-login",
		strings.NewReader(`{"username":"20260001","password":"Test#Pass123"}`),
	)
	req.Header.Set("Authorization", "Bearer 0123456789abcdef")
	req.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("school login status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "school-access") || strings.Contains(rec.Body.String(), "Test#Pass123") {
		t.Fatalf("school login response leaked credentials: %s", rec.Body.String())
	}
	var response struct {
		Sno         string            `json:"sno"`
		SchoolToken schoolTokenStatus `json:"school_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Sno != "20260001" || !response.SchoolToken.Configured || !response.SchoolToken.Valid {
		t.Fatalf("unexpected login response: %+v", response)
	}
	token := server.cfgHub.Token()
	if token == nil || token.AccessToken != "school-access" || token.Source != "web" {
		t.Fatalf("stored token = %+v", token)
	}
	if got := server.cfgHub.Config().Username; got != "20260001" {
		t.Fatalf("config username = %q", got)
	}

	admin := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/config?admin=1", nil)
	req.Header.Set("Authorization", "Bearer 0123456789abcdef")
	server.Handler().ServeHTTP(admin, req)
	if admin.Code != http.StatusOK || !strings.Contains(admin.Body.String(), `"school_token":{"configured":true,"valid":true`) {
		t.Fatalf("admin config token status = %d body=%s", admin.Code, admin.Body.String())
	}
}

func TestSchoolLoginKeepsAdminSessionOnCredentialRejection(t *testing.T) {
	server := newTestServer(t)
	school := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/berserker-secure/keyboard":
			_, _ = w.Write(schoolKeyboardJSON("reject-keyboard"))
		case "/berserker-auth/oauth/token":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":8000,"message":"用户名或密码错误"}`))
		default:
			_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
		}
	}))
	defer school.Close()
	if _, err := server.cfgHub.UpdateConfig(func(cfg *config.Config) error {
		cfg.BaseURL = school.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/school-login",
		strings.NewReader(`{"username":"20260001","password":"wrong"}`),
	)
	req.Header.Set("Authorization", "Bearer 0123456789abcdef")
	req.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("school login status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("school credential failure must not invalidate the admin key")
	}
	if !strings.Contains(rec.Body.String(), "账号或密码错误") {
		t.Fatalf("unexpected error body: %s", rec.Body.String())
	}
	if token := server.cfgHub.Token(); token != nil {
		t.Fatalf("rejected login stored token: %+v", token)
	}
}

func TestSchoolLoginRequiresAdminKey(t *testing.T) {
	server := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/school-login",
		strings.NewReader(`{"username":"20260001","password":"secret"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("school login status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "elec-admin") {
		t.Fatal("missing admin authentication challenge")
	}
}
