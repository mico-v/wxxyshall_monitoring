// Package collector 统一定时、单间和批量采集的业务逻辑。
package collector

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/auth"
	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	"github.com/mico-v/wxxyshall-monitoring/internal/db"
	"github.com/mico-v/wxxyshall-monitoring/internal/rate"
	"github.com/mico-v/wxxyshall-monitoring/internal/webhook"
)

var ErrBusy = errors.New("已有采集任务正在运行")

type ReadingEvent struct {
	TS                    time.Time
	Target                config.Target
	Reading               *charge.Reading
	PreviousSurplusCharge *float64
}

type Result struct {
	Target  config.Target
	Reading *charge.Reading
	Err     error
}

type Progress struct {
	Completed int
	Total     int
	Started   bool
	Result    Result
}

type clientKey struct {
	feeItemID int
	appID     int
}

// Service 保证同一进程同一时间只有一个采集任务，并共享严格请求节拍器。
type Service struct {
	hub       *config.Hub
	database  *db.DB
	limiter   *rate.Limiter
	run       chan struct{}
	notifier  *webhook.Notifier
	handlerMu sync.RWMutex
	handlers  []func(ReadingEvent)
}

func New(hub *config.Hub, database *db.DB) *Service {
	ratePerMinute := config.DefaultRateLimitPerMinute
	if cfg := hub.Config(); cfg != nil {
		ratePerMinute = cfg.RateLimitPerMinute
	}
	service := &Service{
		hub:      hub,
		database: database,
		limiter:  rate.NewLimiter(ratePerMinute),
		run:      make(chan struct{}, 1),
	}
	service.notifier = webhook.New(hub)
	return service
}

func (s *Service) Limiter() *rate.Limiter { return s.limiter }

func (s *Service) SetRate(ratePerMinute int) { s.limiter.SetRate(ratePerMinute) }

func (s *Service) SetReadingHandler(fn func(ReadingEvent)) {
	s.handlerMu.Lock()
	if fn == nil {
		s.handlers = nil
	} else {
		s.handlers = []func(ReadingEvent){fn}
	}
	s.handlerMu.Unlock()
}

// AddReadingHandler adds a callback without replacing existing callbacks.
func (s *Service) AddReadingHandler(fn func(ReadingEvent)) {
	if fn == nil {
		return
	}
	s.handlerMu.Lock()
	s.handlers = append(s.handlers, fn)
	s.handlerMu.Unlock()
}

func (s *Service) CollectOne(ctx context.Context, target config.Target) (Result, error) {
	if !s.tryEnter() {
		return Result{}, ErrBusy
	}
	defer s.leave()
	cfg, tok := s.hub.Config(), s.hub.Token()
	result := s.collectTarget(ctx, target, make(map[clientKey]*charge.Client), cfg, tok)
	return result, nil
}

func (s *Service) CollectAll(ctx context.Context, targets []config.Target, progress func(Progress)) ([]Result, error) {
	if !s.tryEnter() {
		return nil, ErrBusy
	}
	defer s.leave()
	return s.collectAllReserved(ctx, targets, progress)
}

// TryReserve 为异步任务原子预留唯一采集槽位。
func (s *Service) TryReserve() bool { return s.tryEnter() }

// ReleaseReservation 释放尚未交给 CollectAllReserved 的预留槽位。
func (s *Service) ReleaseReservation() { s.leave() }

// CollectAllReserved 在调用方已成功 TryReserve 后执行采集，并保证释放槽位。
func (s *Service) CollectAllReserved(ctx context.Context, targets []config.Target, progress func(Progress)) ([]Result, error) {
	defer s.leave()
	return s.collectAllReserved(ctx, targets, progress)
}

func (s *Service) collectAllReserved(ctx context.Context, targets []config.Target, progress func(Progress)) ([]Result, error) {
	cfg, tok := s.hub.Config(), s.hub.Token()
	clients := make(map[clientKey]*charge.Client)
	results := make([]Result, 0, len(targets))
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if progress != nil {
			progress(Progress{Completed: i, Total: len(targets), Started: true, Result: Result{Target: target}})
		}
		result := s.collectTarget(ctx, target, clients, cfg, tok)
		if result.Err != nil && (errors.Is(result.Err, context.Canceled) || errors.Is(result.Err, context.DeadlineExceeded)) {
			if err := ctx.Err(); err != nil {
				return results, err
			}
			return results, result.Err
		}
		results = append(results, result)
		if progress != nil {
			progress(Progress{Completed: i + 1, Total: len(targets), Result: result})
		}
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if charge.IsAuthError(result.Err) {
			return results, result.Err
		}
	}
	return results, nil
}

func (s *Service) collectTarget(ctx context.Context, target config.Target, clients map[clientKey]*charge.Client, cfg *config.Config, tok *config.Token) Result {
	result := Result{Target: target}
	if tok == nil || tok.AccessToken == "" {
		result.Err = fmt.Errorf("未找到 token")
		return result
	}
	if auth.IsExpired(tok, 600) {
		result.Err = &charge.ChargeAuthError{Msg: "token 已过期或临近过期"}
		return result
	}
	if cfg == nil {
		result.Err = fmt.Errorf("配置不可用")
		return result
	}

	key := clientKey{feeItemID: target.FeeItemID, appID: target.AppID}
	client := clients[key]
	if client == nil {
		client = charge.NewClientWithLimiter(cfg.BaseURL, tok.AccessToken, s.limiter)
		if err := client.EstablishContext(ctx, target.FeeItemID, target.AppID); err != nil {
			result.Err = fmt.Errorf("建立会话失败: %w", err)
			return result
		}
		clients[key] = client
	}

	reading, err := client.QueryBalanceContext(ctx, target.FeeItemID, target.Campus, target.Building, target.Room)
	if err != nil {
		result.Err = fmt.Errorf("查询失败: %w", err)
		return result
	}
	if reading == nil || reading.SurplusCharge == nil {
		result.Err = fmt.Errorf("学校接口未返回有效剩余电量")
		return result
	}
	var previousSurplusCharge *float64
	if previous, previousErr := s.database.GetLatestReading(target.Campus, target.Building, target.Room); previousErr != nil {
		// 历史记录只用于通知判断；读取失败不应阻止本次成功采集。
		// InsertReading 会再次记录数据库错误，便于运维排查。
		previousSurplusCharge = nil
	} else if previous != nil && previous.SurplusCharge != nil {
		value := *previous.SurplusCharge
		previousSurplusCharge = &value
	}
	if err := s.database.InsertReading(target, struct {
		SurplusCharge *float64
		Show          map[string]string
		Raw           map[string]any
	}{SurplusCharge: reading.SurplusCharge, Show: reading.Show, Raw: reading.Raw}); err != nil {
		result.Err = fmt.Errorf("入库失败: %w", err)
		return result
	}
	result.Reading = reading
	now := time.Now()
	s.handlerMu.RLock()
	handlers := append([]func(ReadingEvent){}, s.handlers...)
	s.handlerMu.RUnlock()
	event := ReadingEvent{TS: now, Target: target, Reading: reading}
	event.PreviousSurplusCharge = previousSurplusCharge
	s.notifier.Notify(event.Target, event.Reading, event.PreviousSurplusCharge, event.TS)
	for _, handler := range handlers {
		handler(event)
	}
	return result
}

func (s *Service) tryEnter() bool {
	select {
	case s.run <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Service) leave() { <-s.run }

// WaitIdle 等待当前采集释放运行锁，不启动新的采集。
func (s *Service) WaitIdle(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case s.run <- struct{}{}:
		<-s.run
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops accepting new webhook notifications and waits for queued sends.
func (s *Service) Close(ctx context.Context) error {
	if s == nil || s.notifier == nil {
		return nil
	}
	return s.notifier.Close(ctx)
}
