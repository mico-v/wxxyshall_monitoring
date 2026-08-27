// Package web 提供 HTTP 服务器实现。
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const (
	maxSSEClients = 128 // 全局并发上限，防止单个进程被连接数耗尽
	maxSSEPerIP   = 8   // 单 IP 并发上限，防止单机占用全部槽位
)

// subscribeResult 表示 Subscribe 的结果。
type subscribeResult int

const (
	subscribeOK        subscribeResult = iota // 注册成功
	subscribeClosed                           // Hub 已关闭
	subscribeFull                             // 全局并发已达上限
	subscribeIPLimited                        // 当前 IP 并发已达上限
)

// SSEEvent 代表一个 SSE 事件。
type SSEEvent struct {
	Event string      // 事件类型: "reading", "heartbeat"
	Data  interface{} // JSON 可序列化的数据
}

// SSEHub 管理 SSE 客户端连接。
type SSEHub struct {
	mu       sync.RWMutex
	clients  map[string]chan SSEEvent
	ipCounts map[string]int
	nextID   int
	closed   bool
}

// NewSSEHub 创建一个新的 SSE Hub。
func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients:  make(map[string]chan SSEEvent),
		ipCounts: make(map[string]int),
	}
}

// Subscribe 注册一个新的 SSE 客户端，并计入该 IP 的并发数。
// 返回客户端 ID、接收事件的 channel 与注册结果。
func (h *SSEHub) Subscribe(ip string) (string, <-chan SSEEvent, subscribeResult) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	id := fmt.Sprintf("sse-%d", h.nextID)
	ch := make(chan SSEEvent, 16)
	if h.closed {
		close(ch)
		return id, ch, subscribeClosed
	}
	if len(h.clients) >= maxSSEClients {
		close(ch)
		return id, ch, subscribeFull
	}
	if h.ipCounts[ip] >= maxSSEPerIP {
		close(ch)
		return id, ch, subscribeIPLimited
	}
	h.clients[id] = ch
	h.ipCounts[ip]++
	return id, ch, subscribeOK
}

// Unsubscribe 注销一个 SSE 客户端并释放该 IP 的并发额度。
// 安全地处理 channel 已被 Close() 关闭的情况。
func (h *SSEHub) Unsubscribe(id, ip string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ch, ok := h.clients[id]; ok {
		close(ch)
		delete(h.clients, id)
		h.ipCounts[ip]--
		if h.ipCounts[ip] <= 0 {
			delete(h.ipCounts, ip)
		}
	}
}

// Broadcast 向所有已连接的客户端广播事件。
func (h *SSEHub) Broadcast(event string, data interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	e := SSEEvent{Event: event, Data: data}
	for _, ch := range h.clients {
		select {
		case ch <- e:
		default:
			// 客户端 channel 已满，跳过（防止阻塞）
		}
	}
}

// Close 关闭所有客户端连接并释放资源。
// 在服务器优雅关闭时调用。
func (h *SSEHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, ch := range h.clients {
		close(ch)
		delete(h.clients, id)
	}
	h.ipCounts = make(map[string]int)
}

// MarshalEvent 将 SSE 事件序列化为文本格式。
func MarshalEvent(e SSEEvent) ([]byte, error) {
	data, err := json.Marshal(e.Data)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", e.Event, string(data))), nil
}

// MarshalHeartbeat 生成心跳事件文本。
func MarshalHeartbeat() []byte {
	return []byte("event: heartbeat\ndata: {}\n\n")
}

// ---- 消息类型定义 ----

// ReadingEvent 是 SSE 推送的读数事件。
type ReadingEvent struct {
	TS            string   `json:"ts"`
	Epoch         int64    `json:"epoch"`
	Campus        string   `json:"campus"`
	Building      string   `json:"building"`
	Room          string   `json:"room"`
	RoomLabel     string   `json:"room_label"`
	SurplusCharge *float64 `json:"surplus_charge"`
	TotalUsage    *float64 `json:"total_usage"`
}

// ---- 采集任务管理 ----

// JobState 表示采集任务的状态。
type JobState string

const (
	JobStateQueued     JobState = "queued"
	JobStateRunning    JobState = "running"
	JobStateCancelling JobState = "cancelling"
	JobStateDone       JobState = "done"
	JobStateFailed     JobState = "failed"
	JobStateCancelled  JobState = "cancelled"
)

// JobCurrent 表示当前正在采集的宿舍。
type JobCurrent struct {
	Campus   string `json:"campus"`
	Building string `json:"building"`
	Room     string `json:"room"`
	Label    string `json:"label"`
}

// JobResult 表示单个宿舍的采集结果。
type JobResult struct {
	RoomLabel     string   `json:"room_label"`
	DisplayLabel  string   `json:"display_label"`
	Campus        string   `json:"campus"`
	Building      string   `json:"building"`
	Room          string   `json:"room"`
	SurplusCharge *float64 `json:"surplus_charge"`
	TotalUsage    *float64 `json:"total_usage"`
	Error         string   `json:"error,omitempty"`
}

// CollectJob 代表一个批量采集任务。
type CollectJob struct {
	ID        string      `json:"job_id"`
	State     JobState    `json:"state"`
	Requested int         `json:"requested"`
	Completed int         `json:"completed"`
	Success   int         `json:"success"`
	Failed    int         `json:"failed"`
	Percent   int         `json:"percent"`
	Current   *JobCurrent `json:"current,omitempty"`
	Results   []JobResult `json:"results,omitempty"`
	Error     string      `json:"error,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// JobManager 管理后台采集任务。
type JobManager struct {
	mu       sync.Mutex
	wg       sync.WaitGroup
	jobs     map[string]*CollectJob
	activeID string
	cancels  map[string]context.CancelFunc
}

// NewJobManager 创建一个新的任务管理器。
func NewJobManager() *JobManager {
	return &JobManager{
		jobs:    make(map[string]*CollectJob),
		cancels: make(map[string]context.CancelFunc),
	}
}

// Start 创建一个新的采集任务。
// 如果已有运行中的任务，返回 false。
func (jm *JobManager) Start(parent context.Context, targets int) (*CollectJob, context.Context, bool) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// 清理过期任务
	jm.cleanup()

	// 检查是否有正在运行的任务
	if jm.activeID != "" {
		if _, ok := jm.jobs[jm.activeID]; ok {
			return nil, nil, false
		}
		jm.activeID = ""
	}

	// 创建新任务
	id := fmt.Sprintf("job-%d", time.Now().UnixNano())
	now := time.Now()
	job := &CollectJob{
		ID:        id,
		State:     JobStateQueued,
		Requested: targets,
		CreatedAt: now,
		UpdatedAt: now,
	}
	jm.jobs[id] = job
	jm.activeID = id
	jm.wg.Add(1)
	ctx, cancel := context.WithCancel(parent)
	jm.cancels[id] = cancel
	return cloneJob(job), ctx, true
}

// Get 获取任务快照。
func (jm *JobManager) Get(id string) *CollectJob {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	jm.cleanup()
	job, ok := jm.jobs[id]
	if !ok {
		return nil
	}
	// 返回副本
	return cloneJob(job)
}

// Update 更新任务状态。
func (jm *JobManager) Update(id string, fn func(job *CollectJob)) {
	jm.mu.Lock()
	job, ok := jm.jobs[id]
	if !ok {
		jm.mu.Unlock()
		return
	}
	fn(job)
	job.UpdatedAt = time.Now()
	jm.mu.Unlock()
}

// FinishActive 清除当前活跃任务标记。
func (jm *JobManager) FinishActive(id string) {
	jm.mu.Lock()
	if jm.activeID == id {
		jm.activeID = ""
	}
	delete(jm.cancels, id)
	jm.mu.Unlock()
	jm.wg.Done()
}

func (jm *JobManager) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		jm.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Cancel 仅取消 ID 精确匹配的当前任务，避免旧客户端误取消后续任务。
func (jm *JobManager) Cancel(id string) bool {
	if id == "" {
		return false
	}
	return jm.cancel(id)
}

// CancelActive 用于服务关闭时取消当前任务。
func (jm *JobManager) CancelActive() bool { return jm.cancel("") }

func (jm *JobManager) cancel(expectedID string) bool {
	jm.mu.Lock()
	if jm.activeID == "" {
		jm.mu.Unlock()
		return false
	}
	id := jm.activeID
	if expectedID != "" && id != expectedID {
		jm.mu.Unlock()
		return false
	}
	job, ok := jm.jobs[id]
	if !ok {
		jm.activeID = ""
		jm.mu.Unlock()
		return false
	}
	if job.State != JobStateQueued && job.State != JobStateRunning {
		jm.mu.Unlock()
		return false
	}
	job.State = JobStateCancelling
	job.Current = nil
	job.UpdatedAt = time.Now()
	cancel := jm.cancels[id]
	jm.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

// cleanup 清理过期任务（超过 15 分钟）。
func (jm *JobManager) cleanup() {
	cutoff := time.Now().Add(-15 * time.Minute)
	for id, job := range jm.jobs {
		if job.State == JobStateDone || job.State == JobStateFailed || job.State == JobStateCancelled {
			if job.UpdatedAt.Before(cutoff) {
				delete(jm.jobs, id)
				delete(jm.cancels, id)
			}
		}
	}
}

func cloneJob(job *CollectJob) *CollectJob {
	if job == nil {
		return nil
	}
	cp := *job
	cp.Results = append([]JobResult(nil), job.Results...)
	if job.Current != nil {
		current := *job.Current
		cp.Current = &current
	}
	return &cp
}
