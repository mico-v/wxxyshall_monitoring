// Package webhook 负责向外部消息服务发送采集通知。
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

const (
	requestTimeout   = 10 * time.Second
	maxResponseBytes = 64 << 10
)

// Notifier asynchronously sends successful collection events. It reads the
// current webhook settings for every event, so config hot reload applies too.
type Notifier struct {
	hub          *config.Hub
	send         func(context.Context, config.WebhookConfig, config.Target, *charge.Reading, time.Time) error
	dailySent    map[string]string
	dailyPending map[string]string
	mu           sync.Mutex
	wg           sync.WaitGroup
	closed       bool
}

// New creates a notification sender backed by the configuration hub.
func New(hub *config.Hub) *Notifier {
	return &Notifier{
		hub:          hub,
		send:         SendReading,
		dailySent:    make(map[string]string),
		dailyPending: make(map[string]string),
	}
}

// Notify queues one successful collection result for asynchronous delivery.
func (n *Notifier) Notify(target config.Target, reading *charge.Reading, previousSurplusCharge *float64, at time.Time) {
	if n == nil || n.hub == nil {
		return
	}
	globalCfg := n.hub.Config()
	if globalCfg == nil {
		return
	}
	policy := target
	for _, configuredTarget := range globalCfg.Targets {
		if configuredTarget.Key() == target.Key() {
			policy = configuredTarget
			break
		}
	}
	cfg := policy.EffectiveWebhook(globalCfg.Webhook)
	mode := policy.NotifyMode
	if mode == "" {
		if !shouldNotify(cfg, reading, previousSurplusCharge) {
			return
		}
	} else if mode == "none" {
		return
	} else if mode == "alert" {
		alertCfg := cfg
		alertCfg.NotifyMode = config.DefaultWebhookMode
		if !shouldNotify(alertCfg, reading, previousSurplusCharge) {
			return
		}
	} else if mode != "daily" {
		return
	}
	if !cfg.Enabled || reading == nil || reading.SurplusCharge == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	dailyKey, dailyDay := "", ""
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return
	}
	if mode == "daily" {
		if n.dailySent == nil {
			n.dailySent = make(map[string]string)
		}
		if n.dailyPending == nil {
			n.dailyPending = make(map[string]string)
		}
		if !dailyDue(at, policy.NotifyTime) {
			n.mu.Unlock()
			return
		}
		dailyKey = policy.Key()
		dailyDay = at.Format("2006-01-02")
		if n.dailySent[dailyKey] == dailyDay || n.dailyPending[dailyKey] == dailyDay {
			n.mu.Unlock()
			return
		}
		n.dailyPending[dailyKey] = dailyDay
	}
	send := n.send
	if send == nil {
		send = SendReading
	}
	n.wg.Add(1)
	n.mu.Unlock()
	go func() {
		defer n.wg.Done()
		if err := send(context.Background(), cfg, policy, reading, at); err != nil {
			if mode == "daily" {
				n.mu.Lock()
				if n.dailyPending[dailyKey] == dailyDay {
					delete(n.dailyPending, dailyKey)
				}
				n.mu.Unlock()
			}
			slog.Warn("webhook 通知发送失败", "room", policy.DisplayLabel(), "err", err)
			return
		}
		if mode == "daily" {
			n.mu.Lock()
			if n.dailyPending[dailyKey] == dailyDay {
				delete(n.dailyPending, dailyKey)
				n.dailySent[dailyKey] = dailyDay
			}
			n.mu.Unlock()
		}
		slog.Info("webhook 通知发送成功", "room", policy.DisplayLabel())
	}()
}

func dailyDue(at time.Time, notifyTime string) bool {
	if notifyTime == "" {
		notifyTime = config.DefaultTargetNotifyTime
	}
	scheduled, err := time.ParseInLocation("15:04", notifyTime, at.Location())
	if err != nil {
		return false
	}
	currentMinutes := at.Hour()*60 + at.Minute()
	scheduledMinutes := scheduled.Hour()*60 + scheduled.Minute()
	return currentMinutes >= scheduledMinutes
}

func shouldNotify(cfg config.WebhookConfig, reading *charge.Reading, previousSurplusCharge *float64) bool {
	if !cfg.Enabled || reading == nil || reading.SurplusCharge == nil {
		return false
	}
	if cfg.NotifyMode == "every_collection" {
		return true
	}
	if cfg.NotifyMode == "balance_decrease" {
		return previousSurplusCharge != nil && *reading.SurplusCharge < *previousSurplusCharge
	}
	threshold := cfg.LowBalanceThreshold
	if threshold == 0 {
		threshold = config.DefaultWebhookThreshold
	}
	// low_balance: notify only when the latest value enters the low range.
	// A nil previous value is treated as an entry, so a newly monitored room
	// that is already below the threshold is not silently missed.
	if *reading.SurplusCharge > threshold {
		return false
	}
	return previousSurplusCharge == nil || *previousSurplusCharge > threshold
}

// Close waits for notifications already accepted by Notify to finish.
func (n *Notifier) Close(ctx context.Context) error {
	if n == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	n.mu.Lock()
	n.closed = true
	n.mu.Unlock()
	done := make(chan struct{})
	go func() {
		n.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendReading sends one successful collection result to the configured endpoint.
// The request is compatible with POST /send endpoints expecting Bearer auth.
func SendReading(ctx context.Context, cfg config.WebhookConfig, target config.Target, reading *charge.Reading, at time.Time) error {
	return sendReading(ctx, cfg, target, reading, at, newHTTPClient())
}

func sendReading(ctx context.Context, cfg config.WebhookConfig, target config.Target, reading *charge.Reading, at time.Time, client *http.Client) error {
	if !cfg.Enabled {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if at.IsZero() {
		at = time.Now()
	}

	if cfg.Body == nil {
		return fmt.Errorf("webhook.body 不能为空")
	}
	body, err := json.Marshal(renderJSONValue(cfg.Body, templateValues(cfg.LowBalanceThreshold, target, reading, at)))
	if err != nil {
		return fmt.Errorf("序列化 webhook 消息失败: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建 webhook 请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	if client == nil {
		client = newHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送 webhook 请求失败: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("webhook 返回 HTTP %d", resp.StatusCode)
	}
	return nil
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		// Do not forward the bearer token to an unexpected redirect target.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func templateValues(threshold float64, target config.Target, reading *charge.Reading, at time.Time) map[string]string {
	if threshold == 0 {
		threshold = config.DefaultWebhookThreshold
	}
	surplus, total := "未知", "未知"
	if reading != nil && reading.SurplusCharge != nil {
		surplus = strconv.FormatFloat(*reading.SurplusCharge, 'f', 2, 64)
	}
	if reading != nil {
		if value := reading.TotalUsage(); value != nil {
			total = strconv.FormatFloat(*value, 'f', 2, 64)
		}
	}
	return map[string]string{
		"{{label}}":                 target.DisplayLabel(),
		"{{campus}}":                target.Campus,
		"{{building}}":              target.Building,
		"{{room}}":                  target.Room,
		"{{ts}}":                    at.Format("2006-01-02 15:04:05"),
		"{{surplus_charge}}":        surplus,
		"{{low_balance_threshold}}": strconv.FormatFloat(threshold, 'f', 2, 64),
		"{{total_usage}}":           total,
	}
}

func renderJSONValue(value any, replacements map[string]string) any {
	switch value := value.(type) {
	case map[string]any:
		output := make(map[string]any, len(value))
		for key, item := range value {
			output[key] = renderJSONValue(item, replacements)
		}
		return output
	case []any:
		output := make([]any, len(value))
		for i, item := range value {
			output[i] = renderJSONValue(item, replacements)
		}
		return output
	case string:
		for placeholder, replacement := range replacements {
			value = strings.ReplaceAll(value, placeholder, replacement)
		}
		return value
	default:
		return value
	}
}
