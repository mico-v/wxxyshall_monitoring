// Package web 提供 HTTP 服务器实现。
package web

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// SSEEvent 代表一个 SSE 事件。
type SSEEvent struct {
	Event string      // 事件类型: "reading", "collect-progress", "heartbeat"
	Data  interface{} // JSON 可序列化的数据
}

// SSEHub 管理 SSE 客户端连接。
type SSEHub struct {
	mu      sync.RWMutex
	clients map[string]chan SSEEvent
	nextID  int
}

// NewSSEHub 创建一个新的 SSE Hub。
func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients: make(map[string]chan SSEEvent),
	}
}

// Subscribe 注册一个新的 SSE 客户端。
// 返回客户端 ID 和接收事件的 channel。
func (h *SSEHub) Subscribe() (string, <-chan SSEEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	id := fmt.Sprintf("sse-%d", h.nextID)
	ch := make(chan SSEEvent, 16)
	h.clients[id] = ch
	return id, ch
}

// Unsubscribe 注销一个 SSE 客户端。
// 安全地处理 channel 已被 Close() 关闭的情况。
func (h *SSEHub) Unsubscribe(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ch, ok := h.clients[id]; ok {
		close(ch)
		delete(h.clients, id)
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

// ClientCount 返回当前连接的客户端数量。
func (h *SSEHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Close 关闭所有客户端连接并释放资源。
// 在服务器优雅关闭时调用。
func (h *SSEHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, ch := range h.clients {
		close(ch)
		delete(h.clients, id)
	}
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
	TS            string  `json:"ts"`
	Epoch         int64   `json:"epoch"`
	Campus        string  `json:"campus"`
	Building      string  `json:"building"`
	Room          string  `json:"room"`
	RoomLabel     string  `json:"room_label"`
	SurplusCharge float64 `json:"surplus_charge"`
	TotalUsage    float64 `json:"total_usage"`
}

// JobProgressEvent 是 SSE 推送的采集进度事件。
type JobProgressEvent struct {
	JobID    string `json:"job_id"`
	State    string `json:"state"`
	Total    int    `json:"total"`
	Done     int    `json:"done"`
	Percent  int    `json:"percent"`
	Current  string `json:"current,omitempty"`
}

// ---- 采集任务管理 ----

// JobState 表示采集任务的状态。
type JobState string

const (
	JobStateQueued  JobState = "queued"
	JobStateRunning JobState = "running"
	JobStateDone    JobState = "done"
	JobStateFailed  JobState = "failed"
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
	RoomLabel     string  `json:"room_label"`
	DisplayLabel  string  `json:"display_label"`
	Campus        string  `json:"campus"`
	Building      string  `json:"building"`
	Room          string  `json:"room"`
	SurplusCharge *float64 `json:"surplus_charge"`
	TotalUsage    *float64 `json:"total_usage"`
	Error         string  `json:"error,omitempty"`
}

// CollectJob 代表一个批量采集任务。
type CollectJob struct {
	ID        string       `json:"job_id"`
	State     JobState     `json:"state"`
	Requested int          `json:"requested"`
	Completed int          `json:"completed"`
	Success   int          `json:"success"`
	Failed    int          `json:"failed"`
	Skipped   int          `json:"skipped"`
	Percent   int          `json:"percent"`
	Current   *JobCurrent  `json:"current,omitempty"`
	Results   []JobResult  `json:"results"`
	Error     string       `json:"error,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// JobManager 管理后台采集任务。
type JobManager struct {
	mu       sync.Mutex
	jobs     map[string]*CollectJob
	activeID string
	hub      *SSEHub
}

// NewJobManager 创建一个新的任务管理器。
func NewJobManager(hub *SSEHub) *JobManager {
	return &JobManager{
		jobs: make(map[string]*CollectJob),
		hub:  hub,
	}
}

// Start 创建一个新的采集任务。
// 如果已有运行中的任务，返回 false。
func (jm *JobManager) Start(targets int) (*CollectJob, bool) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// 清理过期任务
	jm.cleanup()

	// 检查是否有正在运行的任务
	if jm.activeID != "" {
		if job, ok := jm.jobs[jm.activeID]; ok && (job.State == JobStateQueued || job.State == JobStateRunning) {
			return nil, false
		}
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
	return job, true
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
	cp := *job
	cp.Results = make([]JobResult, len(job.Results))
	copy(cp.Results, job.Results)
	return &cp
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

	// 广播进度
	jm.hub.Broadcast("collect-progress", JobProgressEvent{
		JobID:   id,
		State:   string(job.State),
		Total:   job.Requested,
		Done:    job.Completed,
		Percent: job.Percent,
		Current: func() string {
			if job.Current != nil {
				return job.Current.Label
			}
			return ""
		}(),
	})
}

// FinishActive 清除当前活跃任务标记。
func (jm *JobManager) FinishActive(id string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if jm.activeID == id {
		jm.activeID = ""
	}
}

// cleanup 清理过期任务（超过 15 分钟）。
func (jm *JobManager) cleanup() {
	cutoff := time.Now().Add(-15 * time.Minute)
	for id, job := range jm.jobs {
		if job.State == JobStateDone || job.State == JobStateFailed {
			if job.UpdatedAt.Before(cutoff) {
				delete(jm.jobs, id)
			}
		}
	}
}