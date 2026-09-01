package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

func TestSendReadingUsesConfiguredJSONBody(t *testing.T) {
	var got map[string]any
	var gotAuth, gotContentType string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})}

	surplus := 8.5
	err := sendReading(context.Background(), config.WebhookConfig{
		Enabled:             true,
		URL:                 "http://webhook.test/send",
		Token:               "secret-token",
		NotifyMode:          "low_balance",
		LowBalanceThreshold: 10,
		Body: map[string]any{
			"content":      "{{label}} {{surplus_charge}} {{low_balance_threshold}} {{ts}}",
			"umo":          "爱丽丝:FriendMessage:2265044253",
			"custom_field": "固定值",
			"nested": map[string]any{
				"room":  "{{room}}",
				"items": []any{"{{campus}}", true, 3.5},
			},
		},
	}, config.Target{Campus: "校区", Building: "1号楼", Room: "101", Label: "我的宿舍"},
		&charge.Reading{SurplusCharge: &surplus}, time.Date(2026, 8, 30, 12, 34, 56, 0, time.FixedZone("CST", 8*60*60)), client)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Fatalf("content type = %q", gotContentType)
	}
	want := map[string]any{
		"content":      "我的宿舍 8.50 10.00 2026-08-30 12:34:56",
		"umo":          "爱丽丝:FriendMessage:2265044253",
		"custom_field": "固定值",
		"nested": map[string]any{
			"room":  "101",
			"items": []any{"校区", true, 3.5},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request body = %#v, want %#v", got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestShouldNotifyLowBalanceOnlyOnThresholdEntry(t *testing.T) {
	cfg := config.WebhookConfig{Enabled: true, NotifyMode: "low_balance", LowBalanceThreshold: 10}
	reading := func(value float64) *charge.Reading { return &charge.Reading{SurplusCharge: &value} }
	previous := func(value float64) *float64 { return &value }

	if !shouldNotify(cfg, reading(8), nil) {
		t.Fatal("first low reading should notify")
	}
	if shouldNotify(cfg, reading(7), previous(8)) {
		t.Fatal("continued low readings should not notify")
	}
	if !shouldNotify(cfg, reading(9), previous(12)) {
		t.Fatal("crossing into low range should notify")
	}
	if shouldNotify(cfg, reading(12), previous(8)) {
		t.Fatal("recovery above threshold should not notify")
	}
	if !shouldNotify(cfg, reading(9), previous(12)) {
		t.Fatal("a later drop after recovery should notify again")
	}
}

func TestShouldNotifyModes(t *testing.T) {
	previous := 12.0
	current := 11.0
	reading := &charge.Reading{SurplusCharge: &current}
	if !shouldNotify(config.WebhookConfig{Enabled: true, NotifyMode: "balance_decrease"}, reading, &previous) {
		t.Fatal("balance decrease mode should notify on a decrease")
	}
	if !shouldNotify(config.WebhookConfig{Enabled: true, NotifyMode: "every_collection"}, reading, nil) {
		t.Fatal("every collection mode should notify without previous reading")
	}
}

func TestNotifierPerTargetNotificationModes(t *testing.T) {
	hub := testWebhookHub(t, config.WebhookConfig{
		Enabled: true, URL: "http://global.test/send", Token: "global-token",
		NotifyMode: "low_balance", LowBalanceThreshold: 10,
		Body: map[string]any{"content": "global"},
	})
	notifier := New(hub)
	sent := make(chan config.WebhookConfig, 4)
	notifier.send = func(_ context.Context, cfg config.WebhookConfig, _ config.Target, _ *charge.Reading, _ time.Time) error {
		sent <- cfg
		return nil
	}
	surplus := 20.0
	target := config.Target{Campus: "A", Building: "B", Room: "C", NotifyMode: "daily", NotifyTime: "08:00"}
	zone := time.FixedZone("CST", 8*60*60)
	notifier.Notify(target, &charge.Reading{SurplusCharge: &surplus}, nil, time.Date(2026, 9, 1, 7, 59, 0, 0, zone))
	notifier.Notify(target, &charge.Reading{SurplusCharge: &surplus}, nil, time.Date(2026, 9, 1, 8, 0, 0, 0, zone))
	notifier.Notify(target, &charge.Reading{SurplusCharge: &surplus}, nil, time.Date(2026, 9, 1, 9, 0, 0, 0, zone))
	notifier.Notify(target, &charge.Reading{SurplusCharge: &surplus}, nil, time.Date(2026, 9, 2, 8, 0, 0, 0, zone))
	if err := notifier.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(sent); got != 2 {
		t.Fatalf("daily notifications = %d, want 2", got)
	}

	none := target
	none.NotifyMode = "none"
	notifier = New(hub)
	noneSent := false
	notifier.send = func(_ context.Context, _ config.WebhookConfig, _ config.Target, _ *charge.Reading, _ time.Time) error {
		noneSent = true
		return nil
	}
	notifier.Notify(none, &charge.Reading{SurplusCharge: &surplus}, nil, time.Date(2026, 9, 1, 12, 0, 0, 0, zone))
	if err := notifier.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if noneSent {
		t.Fatal("none mode should not send")
	}
}

func TestNotifierUsesTargetWebhookOverride(t *testing.T) {
	hub := testWebhookHub(t, config.WebhookConfig{Enabled: false})
	notifier := New(hub)
	sent := make(chan config.WebhookConfig, 1)
	notifier.send = func(_ context.Context, cfg config.WebhookConfig, _ config.Target, _ *charge.Reading, _ time.Time) error {
		sent <- cfg
		return nil
	}
	surplus := 3.0
	target := config.Target{
		Campus: "A", Building: "B", Room: "C", NotifyMode: "alert",
		Webhook: &config.WebhookConfig{
			Enabled: true, URL: "http://room.test/send", Token: "room-token",
			LowBalanceThreshold: 5, Body: map[string]any{"custom": "room"},
		},
	}
	notifier.Notify(target, &charge.Reading{SurplusCharge: &surplus}, nil, time.Now())
	if err := notifier.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case cfg := <-sent:
		if cfg.URL != "http://room.test/send" || cfg.Token != "room-token" {
			t.Fatalf("target webhook override not used: %+v", cfg)
		}
	case <-time.After(time.Second):
		t.Fatal("target webhook override was not sent")
	}
}

func TestDailyNotificationRetriesAfterFailedDelivery(t *testing.T) {
	hub := testWebhookHub(t, config.WebhookConfig{
		Enabled: true, URL: "http://global.test/send", Token: "global-token",
		Body: map[string]any{"content": "daily"},
	})
	notifier := New(hub)
	attempts := 0
	firstDone := make(chan struct{})
	notifier.send = func(_ context.Context, _ config.WebhookConfig, _ config.Target, _ *charge.Reading, _ time.Time) error {
		attempts++
		if attempts == 1 {
			close(firstDone)
			return context.DeadlineExceeded
		}
		return nil
	}
	surplus := 20.0
	target := config.Target{Campus: "A", Building: "B", Room: "C", NotifyMode: "daily", NotifyTime: "08:00"}
	at := time.Date(2026, 9, 1, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	reading := &charge.Reading{SurplusCharge: &surplus}
	notifier.Notify(target, reading, nil, at)
	<-firstDone
	notifier.Notify(target, reading, nil, at)
	if err := notifier.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("daily delivery attempts = %d, want 2 after retry", attempts)
	}
}

func TestNotifierLogsSuccessfulDelivery(t *testing.T) {
	hub := testWebhookHub(t, config.WebhookConfig{
		Enabled: true, URL: "http://webhook.test/send", Token: "secret",
		NotifyMode: "low_balance", LowBalanceThreshold: 10,
		Body: map[string]any{"custom": "{{room}}"},
	})

	oldLogger := slog.Default()
	defer slog.SetDefault(oldLogger)
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))

	notifier := New(hub)
	notifier.send = func(context.Context, config.WebhookConfig, config.Target, *charge.Reading, time.Time) error {
		return nil
	}
	surplus := 8.0
	notifier.Notify(config.Target{Campus: "校区", Building: "1号楼", Room: "101", Label: "我的宿舍"},
		&charge.Reading{SurplusCharge: &surplus}, nil, time.Now())
	if err := notifier.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "webhook 通知发送成功") || !strings.Contains(logs.String(), "我的宿舍") {
		t.Fatalf("success log = %q", logs.String())
	}
}

func testWebhookHub(t *testing.T, webhook config.WebhookConfig) *config.Hub {
	t.Helper()
	t.Setenv("ELEc_DIR", t.TempDir())
	if err := config.SaveConfig(&config.Config{
		Username: "u", Port: 8080, BaseURL: config.DefaultBaseURL,
		PollIntervalMin: 60, RateLimitPerMinute: 30,
		Webhook: webhook,
	}); err != nil {
		t.Fatal(err)
	}
	hub, err := config.NewHub()
	if err != nil {
		t.Fatal(err)
	}
	return hub
}
