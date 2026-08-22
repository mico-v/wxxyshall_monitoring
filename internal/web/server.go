package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/auth"
	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	"github.com/mico-v/wxxyshall-monitoring/internal/db"
	"github.com/mico-v/wxxyshall-monitoring/internal/rate"
)

const (
	defaultFeeitemID = 409
	defaultAppID     = 34
	totalKey         = "电表总用电量"
	rateWindow       = 60 // 秒
	defaultPort      = 8080
)

// Server 是 HTTP 服务器的主结构体。
type Server struct {
	cfg     *config.Config
	tok     *config.Token
	database *db.DB
	limiter *rate.Limiter
	hub     *SSEHub
	jobMgr  *JobManager
	mu      sync.RWMutex
	cfgPath string
	tokPath string
	dbPath  string
	adminKey string
	rootDir  string
}

// NewServer 创建一个新的 HTTP 服务器。
func NewServer(cfgPath, tokPath, dbPath, adminKey, rootDir string) (*Server, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("加载配置失败: %w", err)
	}

	tok, err := config.LoadToken()
	if err != nil {
		return nil, fmt.Errorf("加载 token 失败: %w", err)
	}

	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 回填旧数据
	if err := database.BackfillRoomIDs(cfg); err != nil {
		log.Printf("回填宿舍 ID 失败: %v", err)
	}

	hub := NewSSEHub()

	s := &Server{
		cfg:      cfg,
		tok:      tok,
		database: database,
		limiter:  rate.NewLimiter(cfg.RateLimitPerMinute),
		hub:      hub,
		jobMgr:   NewJobManager(hub),
		cfgPath:  cfgPath,
		tokPath:  tokPath,
		dbPath:   dbPath,
		adminKey: adminKey,
		rootDir:  rootDir,
	}

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
	mux.HandleFunc("POST /api/config", s.handleSaveConfig)
	mux.HandleFunc("POST /api/collect", s.rateLimit(s.handleCollect))
	mux.HandleFunc("POST /api/collect-all", s.handleCollectAll)
	mux.HandleFunc("GET /api/collect-all/status", s.handleCollectStatus)
	mux.HandleFunc("GET /api/campuses", s.rateLimit(s.handleDiscover))
	mux.HandleFunc("GET /api/buildings", s.rateLimit(s.handleDiscover))
	mux.HandleFunc("GET /api/rooms", s.rateLimit(s.handleDiscover))
	mux.HandleFunc("POST /api/token", s.handlePushToken)

	// 静态文件
	mux.HandleFunc("GET /static/", s.handleStatic)
	mux.HandleFunc("GET /sw.js", s.serveFile("sw.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /manifest.json", s.serveFile("manifest.json", "application/manifest+json"))

	// 404 兜底
	mux.HandleFunc("/", s.handle404)

	return mux
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
	s.mu.RLock()
	targets := s.cfg.GetTargets()
	s.mu.RUnlock()
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
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
	w.Header().Set("Access-Control-Allow-Origin", "*")

	id, ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(id)

	// 发送初始心跳
	w.Write(MarshalHeartbeat())
	flusher.Flush()

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
			w.Write(data)
			flusher.Flush()
		case <-ticker.C:
			w.Write(MarshalHeartbeat())
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleReadings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days := 0
	if d := q.Get("days"); d != "" {
		fmt.Sscanf(d, "%d", &days)
	}
	campus := q.Get("campus")
	building := q.Get("building")
	room := q.Get("room")

	rows, err := s.database.QueryReadings(days, campus, building, room)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("查询失败: %v", err)})
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
	s.mu.RLock()
	targets := s.cfg.GetTargets()
	s.mu.RUnlock()
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
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"username": cfg.Username,
		"base_url": cfg.BaseURL,
		"targets":  cfg.GetTargets(),
	})
}

func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target struct {
			FeeItemID int    `json:"feeitemid"`
			AppID     int    `json:"appId"`
			Campus    string `json:"campus"`
			Building  string `json:"building"`
			Room      string `json:"room"`
			Label     string `json:"label"`
		} `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 解析失败: " + err.Error()})
		return
	}

	t := body.Target
	if t.Campus == "" || t.Building == "" || t.Room == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target 需要 campus/building/room"})
		return
	}

	// 规范化
	if t.FeeItemID == 0 {
		t.FeeItemID = defaultFeeitemID
	}
	if t.AppID == 0 {
		t.AppID = defaultAppID
	}
	if t.Label == "" {
		t.Label = t.Campus + "/" + t.Building + "/" + t.Room
	}

	s.mu.Lock()
	targets := s.cfg.GetTargets()
	key := t.Campus + "|" + t.Building + "|" + t.Room
	replaced := false
	for i, old := range targets {
		if old.Key() == key {
			targets[i] = config.Target{
				FeeItemID: t.FeeItemID,
				AppID:     t.AppID,
				Campus:    t.Campus,
				Building:  t.Building,
				Room:      t.Room,
				Label:     t.Label,
			}
			replaced = true
			break
		}
	}
	if !replaced {
		targets = append(targets, config.Target{
			FeeItemID: t.FeeItemID,
			AppID:     t.AppID,
			Campus:    t.Campus,
			Building:  t.Building,
			Room:      t.Room,
			Label:     t.Label,
		})
	}
	s.cfg.Targets = targets
	if err := config.SaveConfig(s.cfg); err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存配置失败: " + err.Error()})
		return
	}
	s.mu.Unlock()

	action := "添加"
	if replaced {
		action = "更新"
	}
	log.Printf("设置宿舍: %s %s/%s/%s (%s)", action, t.Campus, t.Building, t.Room, t.Label)

	s.handleGetConfig(w, r)
}

func (s *Server) handleCollect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Campus   string `json:"campus"`
		Building string `json:"building"`
		Room     string `json:"room"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	s.mu.RLock()
	tok := s.tok
	cfg := s.cfg
	s.mu.RUnlock()

	if tok == nil || tok.AccessToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录，请先运行 python login.py"})
		return
	}
	if auth.IsExpired(tok, 600) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token 已过期或临近过期（续期不可用），请重新运行 login.py"})
		return
	}

	// 解析目标宿舍
	var target config.Target
	targets := cfg.GetTargets()
	if body.Campus != "" && body.Building != "" && body.Room != "" {
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

	// 采集
	client := charge.NewClient(cfg.BaseURL, tok.AccessToken)
	if err := client.Establish(target.FeeItemID, target.AppID); err != nil {
		writeError(w, err)
		return
	}
	reading, err := client.QueryBalance(target.FeeItemID, target.Campus, target.Building, target.Room)
	if err != nil {
		writeError(w, err)
		return
	}

	// 入库
	insertData := struct {
		SurplusCharge *float64
		Show          map[string]string
		Raw           map[string]any
	}{
		SurplusCharge: reading.SurplusCharge,
		Show:          reading.Show,
		Raw:           reading.Raw,
	}
	if err := s.database.InsertReading(target, insertData); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "入库失败: " + err.Error()})
		return
	}

	// 广播 SSE 事件
	s.hub.Broadcast("reading", ReadingEvent{
		TS:            time.Now().Format("2006-01-02 15:04:05"),
		Epoch:         time.Now().Unix(),
		Campus:        target.Campus,
		Building:      target.Building,
		Room:          target.Room,
		RoomLabel:     target.DisplayLabel(),
		SurplusCharge: func() float64 {
			if reading.SurplusCharge != nil {
				return *reading.SurplusCharge
			}
			return 0
		}(),
		TotalUsage: func() float64 {
			if v := reading.TotalUsage(); v != nil {
				return *v
			}
			return 0
		}(),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"room_label":     target.DisplayLabel(),
		"display_label":  target.DisplayLabel(),
		"campus":         target.Campus,
		"building":       target.Building,
		"room":           target.Room,
		"ts":             time.Now().Format("2006-01-02 15:04:05"),
		"epoch":          time.Now().Unix(),
		"surplus_charge": reading.SurplusCharge,
		"total_usage":    reading.TotalUsage(),
	})
}

func (s *Server) handleCollectAll(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	tok := s.tok
	cfg := s.cfg
	s.mu.RUnlock()

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

	job, ok := s.jobMgr.Start(len(targets))
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "已有采集任务正在运行"})
		return
	}

	// 后台 goroutine 执行采集
	go s.runCollectJob(job.ID, cfg, tok, targets)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":         true,
		"job_id":     job.ID,
		"state":      "queued",
		"requested":  len(targets),
		"status_url": fmt.Sprintf("/api/collect-all/status?job_id=%s", job.ID),
	})
}

func (s *Server) handleCollectStatus(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 job_id 参数"})
		return
	}

	job := s.jobMgr.Get(jobID)
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "采集任务不存在或已过期"})
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	tok := s.tok
	cfg := s.cfg
	s.mu.RUnlock()

	if tok == nil || tok.AccessToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录，请先运行 python login.py"})
		return
	}

	q := r.URL.Query()
	feeitemID := defaultFeeitemID
	if fid := q.Get("feeitemid"); fid != "" {
		fmt.Sscanf(fid, "%d", &feeitemID)
	}

	client := charge.NewClient(cfg.BaseURL, tok.AccessToken)
	if err := client.Establish(feeitemID, defaultAppID); err != nil {
		writeError(w, err)
		return
	}

	var opts []charge.Option
	var err error

	switch {
	case r.URL.Path == "/api/campases":
		opts, err = client.ListCampuses(feeitemID)
	case r.URL.Path == "/api/buildings":
		campus := q.Get("campus")
		if campus == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 campus 参数"})
			return
		}
		opts, err = client.ListBuildings(feeitemID, campus)
	case r.URL.Path == "/api/rooms":
		campus := q.Get("campus")
		building := q.Get("building")
		if campus == "" || building == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 campus/building 参数"})
			return
		}
		opts, err = client.ListRooms(feeitemID, campus, building)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) handlePushToken(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminKey(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "admin key 无效或未配置（需设置 ADMIN_KEY）"})
		return
	}

	var tok config.Token
	if err := json.NewDecoder(r.Body).Decode(&tok); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 解析失败: " + err.Error()})
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

	if err := config.SaveToken(&tok); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存 token 失败: " + err.Error()})
		return
	}

	days := tok.ExpiresIn / 86400
	log.Printf("收到 token 推送（有效期约 %d 天）", days)

	s.mu.Lock()
	s.tok = &tok
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"expires_in_days": days,
	})
}

// ---- 静态文件 ----

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/static/")
	rel = strings.TrimLeft(rel, "/")

	// 路径穿越防护
	root, _ := filepath.Abs(filepath.Join(s.rootDir, "static"))
	fullPath, _ := filepath.Abs(filepath.Join(root, rel))
	if !strings.HasPrefix(fullPath, root) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	// 扩展名白名单
	ext := strings.TrimPrefix(filepath.Ext(fullPath), ".")
	contentTypes := map[string]string{
		"js":    "application/javascript; charset=utf-8",
		"css":   "text/css; charset=utf-8",
		"json":  "application/json; charset=utf-8",
		"svg":   "image/svg+xml",
		"png":   "image/png",
		"woff2": "font/woff2",
		"html":  "text/html; charset=utf-8",
		"ico":   "image/x-icon",
	}
	ctype, ok := contentTypes[ext]
	if !ok {
		ctype = "application/octet-stream"
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(data)
}

func (s *Server) serveFile(filename, ctype string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fullPath := filepath.Join(s.rootDir, filename)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			s.handle404(w, r)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Write(data)
	}
}

func (s *Server) serveHTML(w http.ResponseWriter, filename string) {
	fullPath := filepath.Join(s.rootDir, filename)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
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

func (s *Server) rateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.Allow() {
			s.mu.RLock()
			limit := s.cfg.RateLimitPerMinute
			s.mu.RUnlock()
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": fmt.Sprintf("请求过于频繁:每分钟最多 %d 次，请稍后再试", limit),
			})
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
	return provided == s.adminKey
}

// CloseSSE 关闭 SSE Hub，断开所有 SSE 客户端连接。
// 在服务器优雅关闭时调用，确保 Shutdown 不会因 SSE 长连接而超时。
func (s *Server) CloseSSE() {
	s.hub.Close()
}

// ---- 批量采集任务 ----

func (s *Server) runCollectJob(jobID string, cfg *config.Config, tok *config.Token, targets []config.Target) {
	s.jobMgr.Update(jobID, func(job *CollectJob) {
		job.State = JobStateRunning
	})

	limit := cfg.RateLimitPerMinute
	pitch := 0.0
	if limit > 0 {
		pitch = float64(rateWindow) / float64(limit)
	}

	success, failed, skipped := 0, 0, 0
	results := make([]JobResult, 0, len(targets))

	base := cfg.BaseURL
	clients := make(map[int]*charge.Client) // key: feeitemid

	for i, t := range targets {
		// 更新当前进度
		target := t
		s.jobMgr.Update(jobID, func(job *CollectJob) {
			job.Current = &JobCurrent{
				Campus:   target.Campus,
				Building: target.Building,
				Room:     target.Room,
				Label:    target.DisplayLabel(),
			}
		})

		// 限流
		if limit > 0 && !s.limiter.Allow() {
			skipped = len(targets) - i
			log.Printf("批量采集限流，跳过剩余 %d 间", skipped)
			s.jobMgr.Update(jobID, func(job *CollectJob) {
				job.Completed = i
				job.Skipped = skipped
				job.Current = nil
				job.Percent = i * 100 / len(targets)
			})
			break
		}

		// 获取/创建客户端
		client, ok := clients[t.FeeItemID]
		if !ok {
			client = charge.NewClient(base, tok.AccessToken)
			if err := client.Establish(t.FeeItemID, t.AppID); err != nil {
				if charge.IsAuthError(err) {
					log.Printf("批量采集中止: 登录态失效")
					s.jobMgr.Update(jobID, func(job *CollectJob) {
						job.State = JobStateFailed
						job.Error = err.Error()
						job.Current = nil
						job.Completed = i
						job.Percent = i * 100 / len(targets)
					})
					s.jobMgr.FinishActive(jobID)
					return
				}
				log.Printf("批量采集 %s 建立会话失败: %v", t.DisplayLabel(), err)
				failed++
				results = append(results, JobResult{
					RoomLabel:    t.DisplayLabel(),
					DisplayLabel: t.DisplayLabel(),
					Campus:       t.Campus,
					Building:     t.Building,
					Room:         t.Room,
					Error:        fmt.Sprintf("建立会话失败: %v", err),
				})
				continue
			}
			clients[t.FeeItemID] = client
		}

		// 查询
		reading, err := client.QueryBalance(t.FeeItemID, t.Campus, t.Building, t.Room)
		if err != nil {
			if charge.IsAuthError(err) {
				log.Printf("批量采集中止: 登录态失效")
				s.jobMgr.Update(jobID, func(job *CollectJob) {
					job.State = JobStateFailed
					job.Error = err.Error()
					job.Current = nil
					job.Completed = i
					job.Percent = i * 100 / len(targets)
				})
				s.jobMgr.FinishActive(jobID)
				return
			}
			log.Printf("批量采集 %s 失败: %v", t.DisplayLabel(), err)
			failed++
			results = append(results, JobResult{
				RoomLabel:    t.DisplayLabel(),
				DisplayLabel: t.DisplayLabel(),
				Campus:       t.Campus,
				Building:     t.Building,
				Room:         t.Room,
				Error:        fmt.Sprintf("学校接口异常: %v", err),
			})
			continue
		}

		// 入库
		insertData := struct {
			SurplusCharge *float64
			Show          map[string]string
			Raw           map[string]any
		}{
			SurplusCharge: reading.SurplusCharge,
			Show:          reading.Show,
			Raw:           reading.Raw,
		}
		if err := s.database.InsertReading(t, insertData); err != nil {
			log.Printf("批量采集 %s 入库失败: %v", t.DisplayLabel(), err)
			failed++
			results = append(results, JobResult{
				RoomLabel:    t.DisplayLabel(),
				DisplayLabel: t.DisplayLabel(),
				Campus:       t.Campus,
				Building:     t.Building,
				Room:         t.Room,
				Error:        fmt.Sprintf("入库失败: %v", err),
			})
			continue
		}

		success++
		results = append(results, JobResult{
			RoomLabel:     t.DisplayLabel(),
			DisplayLabel:  t.DisplayLabel(),
			Campus:        t.Campus,
			Building:      t.Building,
			Room:          t.Room,
			SurplusCharge: reading.SurplusCharge,
			TotalUsage:    reading.TotalUsage(),
		})

		// 广播 SSE 事件
		s.hub.Broadcast("reading", ReadingEvent{
			TS:       time.Now().Format("2006-01-02 15:04:05"),
			Epoch:    time.Now().Unix(),
			Campus:   t.Campus,
			Building: t.Building,
			Room:     t.Room,
			RoomLabel: t.DisplayLabel(),
			SurplusCharge: func() float64 {
				if reading.SurplusCharge != nil {
					return *reading.SurplusCharge
				}
				return 0
			}(),
			TotalUsage: func() float64 {
				if v := reading.TotalUsage(); v != nil {
					return *v
				}
				return 0
			}(),
		})

		completed := i + 1
		s.jobMgr.Update(jobID, func(job *CollectJob) {
			job.Completed = completed
			job.Success = success
			job.Failed = failed
			job.Skipped = skipped
			job.Results = results
			job.Current = nil
			job.Percent = completed * 100 / len(targets)
		})

		// 限速等待
		if pitch > 0 && i < len(targets)-1 {
			time.Sleep(time.Duration(pitch * float64(time.Second)))
		}
	}

	log.Printf("批量采集完成: 成功 %d, 失败 %d, 跳过 %d", success, failed, skipped)
	s.jobMgr.Update(jobID, func(job *CollectJob) {
		job.State = JobStateDone
		job.Success = success
		job.Failed = failed
		job.Skipped = skipped
		job.Results = results
		job.Current = nil
		job.Percent = 100
	})
	s.jobMgr.FinishActive(jobID)
}

// ---- 工具函数 ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	switch e := err.(type) {
	case *charge.ChargeAuthError:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "登录态失效: " + e.Error()})
	case *charge.ChargeError:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "学校接口异常: " + e.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务器错误: " + err.Error()})
	}
}