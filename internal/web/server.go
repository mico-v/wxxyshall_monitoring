package web

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/auth"
	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/collector"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	"github.com/mico-v/wxxyshall-monitoring/internal/db"
)

const (
	maxJSONBody = 1 << 20
)

var errDiscoveryTokenMissing = errors.New("discovery token missing")

// Server 是 HTTP 服务器的主结构体。
type Server struct {
	cfgHub    *config.Hub
	database  *db.DB
	collector *collector.Service
	discovery *discoveryCache
	hub       *SSEHub
	jobMgr    *JobManager
	adminKey  string
	rootDir   string
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewServer 创建一个新的 HTTP 服务器。
func NewServer(hub *config.Hub, dbPath, adminKey, rootDir string) (*Server, error) {
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 回填旧数据
	if err := database.BackfillRoomIDs(hub.Config()); err != nil {
		slog.Warn("回填宿舍 ID 失败", "err", err)
	}

	sseHub := NewSSEHub()
	serverCtx, cancel := context.WithCancel(context.Background())

	s := &Server{
		cfgHub:    hub,
		database:  database,
		collector: collector.New(hub, database),
		discovery: newDiscoveryCache(maxDiscoveryCacheEntries),
		hub:       sseHub,
		jobMgr:    NewJobManager(),
		adminKey:  adminKey,
		rootDir:   rootDir,
		ctx:       serverCtx,
		cancel:    cancel,
	}
	s.collector.SetReadingHandler(func(event collector.ReadingEvent) {
		s.hub.Broadcast("reading", readingEvent(event))
	})

	return s, nil
}

// Handler 返回注册了所有路由的 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 前端页面
	mux.HandleFunc("GET /", s.handleRoot)
	mux.HandleFunc("GET /room/{campus}/{building}/{room}", s.handleRoom)

	// API 路由
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/events", s.handleSSE)
	mux.HandleFunc("GET /api/readings", s.handleReadings)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("POST /api/config", s.requireAdmin(s.handleSaveConfig))
	mux.HandleFunc("POST /api/collect", s.requireAdmin(s.handleCollect))
	mux.HandleFunc("POST /api/collect-all", s.requireAdmin(s.handleCollectAll))
	mux.HandleFunc("GET /api/collect-all/status", s.requireAdmin(s.handleCollectStatus))
	mux.HandleFunc("POST /api/collect-all/cancel", s.requireAdmin(s.handleCollectCancel))
	mux.HandleFunc("GET /api/campuses", s.requireAdmin(s.handleDiscover))
	mux.HandleFunc("GET /api/buildings", s.requireAdmin(s.handleDiscover))
	mux.HandleFunc("GET /api/rooms", s.requireAdmin(s.handleDiscover))
	mux.HandleFunc("POST /api/token", s.requireAdmin(s.handlePushToken))
	mux.HandleFunc("POST /api/admin/verify", s.requireAdmin(s.handleAdminVerify))

	// 静态文件
	mux.HandleFunc("GET /static/", s.handleStatic)
	mux.HandleFunc("GET /sw.js", s.serveFile("sw.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /manifest.json", s.serveFile("manifest.json", "application/manifest+json"))
	mux.HandleFunc("GET /offline.html", s.serveFile("offline.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /404.html", s.serveFile("404.html", "text/html; charset=utf-8"))

	// 404 兜底
	mux.HandleFunc("/", s.handle404)

	return securityHeaders(gzipHandler(mux))
}

// ---- 前端页面 ----

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.handle404(w, r)
		return
	}
	s.serveHTML(w, "webapp.html")
}

func (s *Server) handleRoom(w http.ResponseWriter, r *http.Request) {
	campus := r.PathValue("campus")
	building := r.PathValue("building")
	room := r.PathValue("room")

	// 验证该宿舍是否在监控列表中
	targets := s.cfgHub.Config().GetTargets()
	monitored := false
	for _, t := range targets {
		if t.Campus == campus && t.Building == building && t.Room == room {
			monitored = true
			break
		}
	}
	if !monitored {
		s.serveHTML(w, "404.html")
		return
	}
	s.serveHTML(w, "webapp.html")
}

// ---- API 处理器 ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.database.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "database": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "database": true})
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Streaming not supported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	id, ch, ok := s.hub.Subscribe()
	if !ok {
		w.Header().Set("Retry-After", "30")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "SSE 连接数已达上限或服务正在关闭"})
		return
	}
	defer s.hub.Unsubscribe(id)
	controller := http.NewResponseController(w)
	writeEvent := func(data []byte) bool {
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, err := w.Write(data)
		flusher.Flush()
		_ = controller.SetWriteDeadline(time.Time{})
		return err == nil
	}

	// 发送初始心跳
	if !writeEvent(MarshalHeartbeat()) {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				// SSE Hub 已关闭（服务器优雅关闭）
				return
			}
			data, err := MarshalEvent(evt)
			if err != nil {
				continue
			}
			if !writeEvent(data) {
				return
			}
		case <-ticker.C:
			if !writeEvent(MarshalHeartbeat()) {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleReadings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days := 0
	if d := q.Get("days"); d != "" {
		parsed, err := strconv.Atoi(d)
		if err != nil || parsed < 0 || parsed > 36500 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "days 必须是 0..36500 的整数"})
			return
		}
		days = parsed
	}
	campus := q.Get("campus")
	building := q.Get("building")
	room := q.Get("room")
	if len(campus) > 128 || len(building) > 128 || len(room) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campus/building/room 参数过长"})
		return
	}
	roomFilters := 0
	for _, value := range []string{campus, building, room} {
		if value != "" {
			roomFilters++
		}
	}
	if roomFilters != 0 && roomFilters != 3 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campus/building/room 必须同时提供"})
		return
	}

	rows, err := s.database.QueryReadings(days, campus, building, room)
	if err != nil {
		slog.Error("查询历史读数失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "查询失败"})
		return
	}

	// 解析 show_json 并添加 total_usage
	type outputRow struct {
		TS            string            `json:"ts"`
		Epoch         int64             `json:"epoch"`
		RoomLabel     string            `json:"room_label"`
		DisplayLabel  string            `json:"display_label"`
		SurplusCharge *float64          `json:"surplus_charge"`
		TotalUsage    *float64          `json:"total_usage"`
		Show          map[string]string `json:"show"`
		Campus        string            `json:"campus"`
		Building      string            `json:"building"`
		Room          string            `json:"room"`
	}

	// 获取配置中的 label 映射
	targets := s.cfgHub.Config().GetTargets()
	labelMap := make(map[string]string)
	for _, t := range targets {
		labelMap[t.Key()] = t.DisplayLabel()
	}

	out := make([]outputRow, 0, len(rows))
	for _, row := range rows {
		label := row.RoomLabel
		key := row.Campus + "|" + row.Building + "|" + row.Room
		if l, ok := labelMap[key]; ok && l != "" {
			label = l
		}

		out = append(out, outputRow{
			TS:            row.TS,
			Epoch:         row.Epoch,
			RoomLabel:     label,
			DisplayLabel:  label,
			SurplusCharge: row.SurplusCharge,
			TotalUsage:    row.TotalUsage,
			Show:          row.Show,
			Campus:        row.Campus,
			Building:      row.Building,
			Room:          row.Room,
		})
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfgHub.Config()
	authorized := s.checkAdminKey(r)
	if r.Header.Get("Authorization") != "" && !authorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="elec-admin"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "管理密钥无效"})
		return
	}
	out := map[string]any{
		"targets":             cfg.GetTargets(),
		"defaults":            map[string]int{"feeitemid": config.DefaultFeeItemID, "appId": config.DefaultAppID},
		"admin_auth_required": true,
	}
	if authorized {
		out["username"] = cfg.Username
		out["port"] = cfg.Port
		out["base_url"] = cfg.BaseURL
		out["poll_interval_minutes"] = cfg.PollIntervalMin
		out["rate_limit_per_minute"] = cfg.RateLimitPerMinute
		out["port_change_requires_restart"] = true
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username           *string          `json:"username"`
		Port               *int             `json:"port"`
		BaseURL            *string          `json:"base_url"`
		Targets            *[]config.Target `json:"targets"`
		Target             *config.Target   `json:"target"`
		PollIntervalMin    *int             `json:"poll_interval_minutes"`
		RateLimitPerMinute *int             `json:"rate_limit_per_minute"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, jsonDecodeStatus(err), map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	if body.Port != nil && (*body.Port < 1024 || *body.Port > 65535) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "port 必须在 1024..65535 之间"})
		return
	}
	if body.BaseURL != nil && strings.TrimSpace(*body.BaseURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "base_url 不能为空"})
		return
	}
	if body.PollIntervalMin != nil && (*body.PollIntervalMin < 1 || *body.PollIntervalMin > 7*24*60) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "poll_interval_minutes 必须在 1..10080 之间"})
		return
	}
	if body.RateLimitPerMinute != nil && (*body.RateLimitPerMinute < 1 || *body.RateLimitPerMinute > 600) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rate_limit_per_minute 必须在 1..600 之间"})
		return
	}
	if body.Targets != nil {
		for i, target := range *body.Targets {
			if target.FeeItemID <= 0 || target.AppID <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("targets[%d] 的 feeitemid/appId 必须为正整数", i),
				})
				return
			}
		}
	}
	if body.Target != nil {
		if body.Username != nil || body.Port != nil || body.BaseURL != nil || body.Targets != nil ||
			body.PollIntervalMin != nil || body.RateLimitPerMinute != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target 不能与其他配置字段同时提交"})
			return
		}
		body.Target.Campus = strings.TrimSpace(body.Target.Campus)
		body.Target.Building = strings.TrimSpace(body.Target.Building)
		body.Target.Room = strings.TrimSpace(body.Target.Room)
		body.Target.Label = strings.TrimSpace(body.Target.Label)
		if body.Target.Campus == "" || body.Target.Building == "" || body.Target.Room == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target 需要 campus/building/room"})
			return
		}
		if body.Target.FeeItemID == 0 {
			body.Target.FeeItemID = config.DefaultFeeItemID
		}
		if body.Target.AppID == 0 {
			body.Target.AppID = config.DefaultAppID
		}
		if body.Target.FeeItemID <= 0 || body.Target.AppID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target 的 feeitemid/appId 必须为正整数"})
			return
		}
		if body.Target.Label == "" {
			body.Target.Label = body.Target.Campus + "/" + body.Target.Building + "/" + body.Target.Room
		}
	}
	oldPort := s.cfgHub.Config().Port
	cfg, err := s.cfgHub.UpdateConfig(func(cfg *config.Config) error {
		if body.Target != nil {
			target := *body.Target
			for i, old := range cfg.Targets {
				if old.Key() == target.Key() {
					cfg.Targets[i].Label = target.Label
					return nil
				}
			}
			cfg.Targets = append(cfg.Targets, target)
			return nil
		}
		if body.Username != nil {
			cfg.Username = *body.Username
		}
		if body.Port != nil {
			cfg.Port = *body.Port
		}
		if body.BaseURL != nil {
			cfg.BaseURL = *body.BaseURL
		}
		if body.Targets != nil {
			cfg.Targets = append([]config.Target(nil), (*body.Targets)...)
		}
		if body.PollIntervalMin != nil {
			cfg.PollIntervalMin = *body.PollIntervalMin
		}
		if body.RateLimitPerMinute != nil {
			cfg.RateLimitPerMinute = *body.RateLimitPerMinute
		}
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "保存配置失败: " + err.Error()})
		return
	}
	s.collector.SetRate(cfg.RateLimitPerMinute)
	restartRequired := cfg.Port != oldPort
	slog.Info("配置已更新", "targets", len(cfg.Targets), "interval_minutes", cfg.PollIntervalMin, "rate_limit", cfg.RateLimitPerMinute, "restart_required", restartRequired)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "config": cfg, "targets": cfg.GetTargets(),
		"restart_required": restartRequired,
	})
	if restartRequired {
		slog.Warn("端口配置已保存，需重启服务后生效", "old_port", oldPort, "new_port", cfg.Port)
	}
}

func (s *Server) handleCollect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Campus   string `json:"campus"`
		Building string `json:"building"`
		Room     string `json:"room"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, jsonDecodeStatus(err), map[string]string{"error": "请求体解析失败: " + err.Error()})
		return
	}

	tok := s.cfgHub.Token()
	cfg := s.cfgHub.Config()

	if tok == nil || tok.AccessToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录，请先运行 python login.py"})
		return
	}
	if auth.IsExpired(tok, 600) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token 已过期或临近过期（续期不可用），请重新运行 login.py"})
		return
	}

	// 解析目标宿舍；空对象表示配置第一项，部分字段不允许静默回退。
	var target config.Target
	targets := cfg.GetTargets()
	fields := 0
	for _, value := range []string{body.Campus, body.Building, body.Room} {
		if value != "" {
			fields++
		}
	}
	if len(body.Campus) > 128 || len(body.Building) > 128 || len(body.Room) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campus/building/room 字段过长"})
		return
	}
	if fields != 0 && fields != 3 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campus/building/room 必须同时提供"})
		return
	}
	if fields == 3 {
		found := false
		for _, t := range targets {
			if t.Campus == body.Campus && t.Building == body.Building && t.Room == body.Room {
				target = t
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "该宿舍不在监控列表中"})
			return
		}
	} else if len(targets) > 0 {
		target = targets[0]
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "还没有监控宿舍，请先在查询设置里添加"})
		return
	}

	result, err := s.collector.CollectOne(r.Context(), target)
	if errors.Is(err, collector.ErrBusy) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if result.Err != nil {
		writeError(w, result.Err)
		return
	}
	reading := result.Reading
	now := time.Now()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"room_label":     target.DisplayLabel(),
		"display_label":  target.DisplayLabel(),
		"campus":         target.Campus,
		"building":       target.Building,
		"room":           target.Room,
		"ts":             now.Format("2006-01-02 15:04:05"),
		"epoch":          now.Unix(),
		"surplus_charge": reading.SurplusCharge,
		"total_usage":    reading.TotalUsage(),
	})
}

func (s *Server) handleCollectAll(w http.ResponseWriter, r *http.Request) {
	tok := s.cfgHub.Token()
	cfg := s.cfgHub.Config()

	if tok == nil || tok.AccessToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录，请先运行 python login.py"})
		return
	}
	if auth.IsExpired(tok, 600) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token 已过期或临近过期，请先重新登录"})
		return
	}

	targets := cfg.GetTargets()
	if len(targets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "还没有监控宿舍，请先在查询设置里添加"})
		return
	}
	if !s.collector.TryReserve() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": collector.ErrBusy.Error()})
		return
	}

	job, jobCtx, ok := s.jobMgr.Start(s.ctx, len(targets))
	if !ok {
		s.collector.ReleaseReservation()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "已有采集任务正在运行"})
		return
	}

	// 后台 goroutine 执行采集
	go s.runCollectJob(jobCtx, job.ID, targets)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":         true,
		"job_id":     job.ID,
		"state":      "queued",
		"requested":  len(targets),
		"status_url": fmt.Sprintf("/api/collect-all/status?job_id=%s", job.ID),
		"cancel_url": fmt.Sprintf("/api/collect-all/cancel?job_id=%s", job.ID),
	})
}

func (s *Server) handleCollectStatus(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" || len(jobID) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "job_id 参数缺失或过长"})
		return
	}

	job := s.jobMgr.Get(jobID)
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "采集任务不存在或已过期"})
		return
	}
	if job.State == JobStateQueued || job.State == JobStateRunning || job.State == JobStateCancelling {
		job.Results = nil
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleCollectCancel(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" || len(jobID) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "job_id 参数缺失或过长"})
		return
	}
	if s.jobMgr.Cancel(jobID) {
		slog.Info("批量采集任务正在取消", "job_id", jobID)
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "state": "cancelling", "job_id": jobID})
	} else {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "指定任务不是当前可取消的采集任务"})
	}
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfgHub.Config()
	if cfg == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "配置不可用"})
		return
	}

	q := r.URL.Query()
	feeitemID := config.DefaultFeeItemID
	if fid := q.Get("feeitemid"); fid != "" {
		parsed, err := strconv.Atoi(fid)
		if err != nil || parsed <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "feeitemid 必须为正整数"})
			return
		}
		feeitemID = parsed
	}
	appID := config.DefaultAppID
	if raw := q.Get("appId"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "appId 必须为正整数"})
			return
		}
		appID = parsed
	}

	var kind, campus, building string
	switch {
	case r.URL.Path == "/api/campuses":
		kind = "campuses"
	case r.URL.Path == "/api/buildings":
		kind = "buildings"
		campus = q.Get("campus")
		if campus == "" || len(campus) > 128 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campus 参数缺失或过长"})
			return
		}
	case r.URL.Path == "/api/rooms":
		kind = "rooms"
		campus = q.Get("campus")
		building = q.Get("building")
		if campus == "" || building == "" || len(campus) > 128 || len(building) > 128 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "campus/building 参数缺失或过长"})
			return
		}
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	key := discoveryCacheKey{
		baseURL: cfg.BaseURL, feeItemID: feeitemID, appID: appID,
		kind: kind, campus: campus, building: building,
	}
	opts, cached, err := s.discovery.Get(r.Context(), key, func(ctx context.Context) ([]charge.Option, error) {
		tok := s.cfgHub.Token()
		if tok == nil || tok.AccessToken == "" {
			return nil, errDiscoveryTokenMissing
		}
		if auth.IsExpired(tok, 600) {
			return nil, &charge.ChargeAuthError{Msg: "token 已过期或临近过期，请重新运行 login.py"}
		}
		client := charge.NewClientWithLimiter(cfg.BaseURL, tok.AccessToken, s.collector.Limiter())
		if err := client.EstablishContext(ctx, feeitemID, appID); err != nil {
			return nil, err
		}
		switch kind {
		case "campuses":
			return client.ListCampusesContext(ctx, feeitemID)
		case "buildings":
			return client.ListBuildingsContext(ctx, feeitemID, campus)
		default:
			return client.ListRoomsContext(ctx, feeitemID, campus, building)
		}
	})

	if err != nil {
		if errors.Is(err, errDiscoveryTokenMissing) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录，请先运行 python login.py"})
			return
		}
		writeError(w, err)
		return
	}

	if cached {
		w.Header().Set("X-Elec-Discovery-Cache", "hit")
	} else {
		w.Header().Set("X-Elec-Discovery-Cache", "miss")
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) handlePushToken(w http.ResponseWriter, r *http.Request) {
	var tok config.Token
	if err := decodeJSON(w, r, &tok); err != nil {
		writeJSON(w, jsonDecodeStatus(err), map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}
	if tok.AccessToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 access_token"})
		return
	}

	if tok.LoginTime == 0 {
		tok.LoginTime = time.Now().Unix()
	}
	if tok.Source == "" {
		tok.Source = "upload"
	}

	if err := s.cfgHub.SetToken(&tok); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存 token 失败: " + err.Error()})
		return
	}

	days := tok.ExpiresIn / 86400
	slog.Info("收到 token 推送", "expires_in_days", days)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"expires_in_days": days,
	})
}

func (s *Server) handleAdminVerify(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- 静态文件 ----

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	// 从嵌入的文件系统提供静态文件
	s.embeddedFileServer().ServeHTTP(w, r)
}

func (s *Server) serveFile(filename, ctype string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := readEmbeddedFile(filename)
		if err != nil {
			// 回退到磁盘读取
			fullPath := filepath.Join(s.rootDir, filename)
			data, err = os.ReadFile(fullPath)
			if err != nil {
				s.handle404(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(data)
	}
}

func (s *Server) serveHTML(w http.ResponseWriter, filename string) {
	data, err := readEmbeddedFile(filename)
	if err != nil {
		// 回退到磁盘读取
		fullPath := filepath.Join(s.rootDir, filename)
		data, err = os.ReadFile(fullPath)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (s *Server) handle404(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/static/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	s.serveHTML(w, "404.html")
}

// ---- 中间件/辅助 ----

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAdminKey(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="elec-admin"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "管理密钥无效"})
			return
		}
		next(w, r)
	}
}

func (s *Server) checkAdminKey(r *http.Request) bool {
	if s.adminKey == "" {
		return false
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return false
	}
	provided := strings.TrimSpace(auth[7:])
	if len(provided) < 16 || len(provided) > 256 {
		return false
	}
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(s.adminKey))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

// CloseSSE 关闭 SSE Hub，断开所有 SSE 客户端连接。
// 在服务器优雅关闭时调用，确保 Shutdown 不会因 SSE 长连接而超时。
func (s *Server) CloseSSE() {
	s.hub.Close()
}

func (s *Server) Close() error {
	s.cancel()
	s.jobMgr.CancelActive()
	s.CloseSSE()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	waitErr := errors.Join(s.jobMgr.Wait(waitCtx), s.collector.WaitIdle(waitCtx))
	if waitErr != nil {
		return waitErr
	}
	return s.database.Close()
}

func (s *Server) Collector() *collector.Service { return s.collector }

// ---- 批量采集任务 ----

func (s *Server) runCollectJob(ctx context.Context, jobID string, targets []config.Target) {
	defer s.jobMgr.FinishActive(jobID)
	s.jobMgr.Update(jobID, func(job *CollectJob) {
		if job.State == JobStateQueued {
			job.State = JobStateRunning
		}
	})

	success, failed := 0, 0
	results := make([]JobResult, 0, len(targets))
	_, err := s.collector.CollectAllReserved(ctx, targets, func(progress collector.Progress) {
		t := progress.Result.Target
		if progress.Started {
			s.jobMgr.Update(jobID, func(job *CollectJob) {
				if job.State == JobStateCancelling || job.State == JobStateCancelled {
					return
				}
				job.Current = &JobCurrent{Campus: t.Campus, Building: t.Building, Room: t.Room, Label: t.DisplayLabel()}
			})
			return
		}
		jr := JobResult{
			RoomLabel: t.DisplayLabel(), DisplayLabel: t.DisplayLabel(),
			Campus: t.Campus, Building: t.Building, Room: t.Room,
		}
		if progress.Result.Err != nil {
			failed++
			jr.Error = progress.Result.Err.Error()
		} else {
			success++
			jr.SurplusCharge = progress.Result.Reading.SurplusCharge
			jr.TotalUsage = progress.Result.Reading.TotalUsage()
		}
		results = append(results, jr)
		completed := progress.Completed
		s.jobMgr.Update(jobID, func(job *CollectJob) {
			if job.State == JobStateCancelling || job.State == JobStateCancelled {
				return
			}
			job.Completed = completed
			job.Success = success
			job.Failed = failed
			job.Results = append(job.Results, jr)
			job.Current = nil
			job.Percent = completed * 100 / len(targets)
		})
	})
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		slog.Info("批量采集已取消", "completed", len(results))
		s.jobMgr.Update(jobID, func(job *CollectJob) {
			job.State = JobStateCancelled
			job.Current = nil
			job.Completed = len(results)
			job.Success = success
			job.Failed = failed
			job.Percent = len(results) * 100 / len(targets)
			job.Results = append([]JobResult(nil), results...)
		})
		return
	}
	if err != nil {
		slog.Warn("批量采集失败", "err", err)
		s.jobMgr.Update(jobID, func(job *CollectJob) {
			job.State = JobStateFailed
			job.Error = err.Error()
			job.Current = nil
			job.Completed = len(results)
			job.Success = success
			job.Failed = failed
			job.Percent = len(results) * 100 / len(targets)
			job.Results = append([]JobResult(nil), results...)
		})
		return
	}

	finalState := JobStateDone
	s.jobMgr.Update(jobID, func(job *CollectJob) {
		if job.State == JobStateCancelling || job.State == JobStateCancelled {
			finalState = JobStateCancelled
			job.State = JobStateCancelled
			job.Completed = len(results)
			job.Success = success
			job.Failed = failed
			job.Percent = len(results) * 100 / len(targets)
			job.Results = append([]JobResult(nil), results...)
			job.Current = nil
			return
		}
		job.State = JobStateDone
		job.Success = success
		job.Failed = failed
		job.Results = append([]JobResult(nil), results...)
		job.Current = nil
		job.Percent = 100
	})
	if finalState == JobStateCancelled {
		slog.Info("批量采集已取消", "completed", len(results))
	} else {
		slog.Info("批量采集完成", "success", success, "failed", failed)
	}
}

// ---- 工具函数 ----

// gzipHandler 包装 HTTP 处理器，对支持 gzip 的客户端压缩响应。
func gzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if r.Method == http.MethodHead || !acceptsEncoding(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		// SSE 不压缩
		if r.URL.Path == "/api/events" || strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer func() {
			if err := gw.Close(); err != nil {
				slog.Debug("关闭 gzip 响应失败", "err", err)
			}
		}()
		next.ServeHTTP(gw, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	Writer      *gzip.Writer
	wroteHeader bool
	compress    bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.compress = statusAllowsBody(status) && w.Header().Get("Content-Encoding") == "" &&
		compressibleContentType(w.Header().Get("Content-Type"))
	if w.compress {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		w.Writer = gzip.NewWriter(w.ResponseWriter)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if !w.compress {
		return w.ResponseWriter.Write(b)
	}
	return w.Writer.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.Writer != nil {
		_ = w.Writer.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *gzipResponseWriter) Close() error {
	if w.Writer == nil {
		return nil
	}
	return w.Writer.Close()
}

func (w *gzipResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("底层 ResponseWriter 不支持 Hijack")
	}
	return hijacker.Hijack()
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; worker-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func acceptsEncoding(header, encoding string) bool {
	var wildcard *float64
	for _, part := range strings.Split(header, ",") {
		segments := strings.Split(part, ";")
		name := strings.ToLower(strings.TrimSpace(segments[0]))
		if name != encoding && name != "*" {
			continue
		}
		quality := 1.0
		for _, parameter := range segments[1:] {
			key, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
			if !found || !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				quality = 0
			} else {
				quality = parsed
			}
		}
		if name == encoding {
			return quality > 0
		}
		q := quality
		wildcard = &q
	}
	return wildcard != nil && *wildcard > 0
}

func statusAllowsBody(status int) bool {
	return status >= 200 && status != http.StatusNoContent && status != http.StatusResetContent && status != http.StatusNotModified
}

func compressibleContentType(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if mediaType == "" || mediaType == "image/svg+xml" {
		return true
	}
	if strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(mediaType, "audio/") ||
		strings.HasPrefix(mediaType, "video/") || strings.HasPrefix(mediaType, "font/") {
		return false
	}
	switch mediaType {
	case "application/gzip", "application/zip", "application/pdf", "application/octet-stream":
		return false
	default:
		return true
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	mediaType = strings.ToLower(mediaType)
	if err != nil || (mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json")) {
		return fmt.Errorf("Content-Type 必须是 application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("请求体只能包含一个 JSON 对象")
		}
		return err
	}
	return nil
}

func jsonDecodeStatus(err error) int {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func readingEvent(event collector.ReadingEvent) ReadingEvent {
	reading := event.Reading
	out := ReadingEvent{
		TS: event.TS.Format("2006-01-02 15:04:05"), Epoch: event.TS.Unix(),
		Campus: event.Target.Campus, Building: event.Target.Building,
		Room: event.Target.Room, RoomLabel: event.Target.DisplayLabel(),
	}
	if reading != nil {
		out.SurplusCharge = reading.SurplusCharge
		out.TotalUsage = reading.TotalUsage()
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("JSON 序列化失败", "err", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	if err == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器错误"})
		return
	}
	var authErr *charge.ChargeAuthError
	var chargeErr *charge.ChargeError
	switch {
	case errors.Is(err, context.Canceled):
		writeJSON(w, http.StatusRequestTimeout, map[string]string{"error": "请求已取消"})
	case errors.Is(err, collector.ErrBusy):
		writeJSON(w, http.StatusConflict, map[string]string{"error": collector.ErrBusy.Error()})
	case errors.As(err, &authErr):
		e := authErr
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "登录态失效: " + e.Error()})
	case errors.As(err, &chargeErr):
		e := chargeErr
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "学校接口异常: " + e.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器错误: " + err.Error()})
	}
}
