package schoollogin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testKeyboardJSON(uuid string) []byte {
	payload := map[string]any{
		"code": 200,
		"data": map[string]any{
			"uuid":                uuid,
			"numberKeyboard":      "0123456789",
			"lowerLetterKeyboard": lowerLayout,
			"upperLetterKeyboard": upperLayout,
			"symbolKeyboard":      string(symbolKeys),
		},
	}
	data, _ := json.Marshal(payload)
	return data
}

func TestLoginCompletesSecurityKeyboardChallenge(t *testing.T) {
	const uuid = "keyboard-session"
	var tokenCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/berserker-secure/keyboard":
			if r.Method != http.MethodGet {
				t.Fatalf("keyboard method = %s", r.Method)
			}
			if got := r.URL.Query().Get("type"); got != "Standard" {
				t.Fatalf("keyboard type = %q", got)
			}
			if got := r.URL.Query().Get("order"); got != "0" {
				t.Fatalf("keyboard order = %q", got)
			}
			_, _ = w.Write(testKeyboardJSON(uuid))
		case "/berserker-auth/oauth/token":
			tokenCalls++
			if r.Method != http.MethodPost {
				t.Fatalf("token method = %s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != oauthBasicAuth {
				t.Fatalf("authorization = %q", got)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			checks := map[string]string{
				"username":        "20260001",
				"password":        "Pass123$1$" + uuid,
				"logintype":       "snoNew",
				"grant_type":      "password",
				"scope":           "all",
				"loginFrom":       "h5",
				"device_token":    "h5",
				"synAccessSource": "h5",
			}
			for key, want := range checks {
				if got := r.Form.Get(key); got != want {
					t.Fatalf("form %s = %q, want %q", key, got, want)
				}
			}
			_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":"6047999","sno":"20260001"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	now := time.Unix(1_800_000_000, 0)
	client := NewClient()
	client.Now = func() time.Time { return now }
	token, err := client.Login(context.Background(), server.URL, " 20260001 ", "Pass123")
	if err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}
	if token.AccessToken != "access" || token.RefreshToken != "refresh" {
		t.Fatalf("unexpected token: %+v", token)
	}
	if token.ExpiresIn != 6_047_999 || token.LoginTime != now.Unix() {
		t.Fatalf("unexpected token metadata: %+v", token)
	}
	if token.Sno != "20260001" || token.Source != "web" {
		t.Fatalf("unexpected token identity: %+v", token)
	}
}

func TestLoginReturnsSchoolCredentialError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/berserker-secure/keyboard":
			_, _ = w.Write(testKeyboardJSON("once"))
		case "/berserker-auth/oauth/token":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"status":400,"message":"用户名或密码错误","code":8000}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := NewClient().Login(context.Background(), server.URL, "20260001", "wrong")
	if !errors.Is(err, ErrCredentialsRejected) {
		t.Fatalf("error = %v, want ErrCredentialsRejected", err)
	}
}

func TestLoginAcceptsNestedTokenResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/berserker-secure/keyboard":
			_, _ = w.Write(testKeyboardJSON("nested"))
		case "/berserker-auth/oauth/token":
			_, _ = w.Write([]byte(`{"code":200,"data":{"access_token":"nested-access","expires_in":3600,"sno":20260001}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	token, err := NewClient().Login(context.Background(), server.URL, "20260001", "password")
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "nested-access" || token.ExpiresIn != 3600 || token.Sno != "20260001" {
		t.Fatalf("unexpected token: %+v", token)
	}
}

func TestLoginRejectsMissingKeyboardUUID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
	}))
	defer server.Close()

	_, err := NewClient().Login(context.Background(), server.URL, "20260001", "password")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestLoginReportsChallengeWithoutRetrying(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/berserker-secure/keyboard":
			_, _ = w.Write(testKeyboardJSON("challenge"))
		case "/berserker-auth/oauth/token":
			calls++
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":8002,"message":"需要验证码"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := NewClient().Login(context.Background(), server.URL, "20260001", "password")
	if !errors.Is(err, ErrLoginChallenge) {
		t.Fatalf("error = %v, want ErrLoginChallenge", err)
	}
	if calls != 1 {
		t.Fatalf("token calls = %d, want 1", calls)
	}
}

func TestLoginRejectsCrossOriginRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("cross-origin target must not be called")
	}))
	defer target.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer server.Close()

	_, err := NewClient().Login(context.Background(), server.URL, "20260001", "password")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestEncodePasswordForKeyboardUsesVisibleKeyIndexes(t *testing.T) {
	keyboard := keyboardData{
		NumberKeyboard:      "9876543210",
		LowerLetterKeyboard: "zyxwvutsrqponmlkjihgfedcba",
		UpperLetterKeyboard: "ZYXWVUTSRQPONMLKJIHGFEDCBA",
		SymbolKeyboard:      string(symbolKeys),
	}
	encoded, err := encodePasswordForKeyboard("@Az9", keyboard)
	if err != nil {
		t.Fatal(err)
	}
	want := string([]rune{
		[]rune(keyboard.SymbolKeyboard)[15],
		[]rune(keyboard.UpperLetterKeyboard)[10],
		[]rune(keyboard.LowerLetterKeyboard)[19],
		[]rune(keyboard.NumberKeyboard)[9],
	})
	if encoded != want {
		t.Fatalf("encoded password = %q, want %q", encoded, want)
	}
}

func TestEncodePasswordRejectsIncompleteKeyboard(t *testing.T) {
	_, err := encodePasswordForKeyboard("abc", keyboardData{UUID: "missing"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestParseInteger(t *testing.T) {
	tests := []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{raw: `6047999`, want: 6047999},
		{raw: `"3600"`, want: 3600},
		{raw: `-1`, want: -1},
		{raw: `"invalid"`, wantErr: true},
		{raw: `null`, want: 0},
	}
	for _, test := range tests {
		got, err := parseInteger(json.RawMessage(test.raw))
		if (err != nil) != test.wantErr || got != test.want {
			t.Fatalf("parseInteger(%s) = %d, %v; want %d, err=%v", test.raw, got, err, test.want, test.wantErr)
		}
	}
}

func TestLoginFormDoesNotPutPasswordInURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/berserker-secure/keyboard":
			_, _ = w.Write(testKeyboardJSON("url-check"))
		case "/berserker-auth/oauth/token":
			if strings.Contains(r.URL.RawQuery, "password") || strings.Contains(r.URL.RawQuery, "Pass") {
				t.Fatalf("password leaked into query: %s", r.URL.RawQuery)
			}
			values, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				t.Fatal(err)
			}
			if len(values) != 0 {
				t.Fatalf("unexpected token query: %v", values)
			}
			_, _ = w.Write([]byte(`{"access_token":"access","expires_in":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if _, err := NewClient().Login(context.Background(), server.URL, "20260001", "Pass-Query-Check"); err != nil {
		t.Fatal(err)
	}
}
