// Package db 提供 SQLite 数据库操作。
//
// 数据文件默认位于 ELEc_DIR/data/electricity.db。
// WAL 模式允许仪表盘读取与采集写入并发。
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	_ "modernc.org/sqlite"
)

// ReadingRow 代表 readings 表中的一行记录。
type ReadingRow struct {
	TS            string   `json:"ts"`
	Epoch         int64    `json:"epoch"`
	RoomLabel     string   `json:"room_label"`
	SurplusCharge *float64 `json:"surplus_charge"`
	ShowJSON      string   `json:"-"`
	RawJSON       string   `json:"-"`
	Campus        string   `json:"campus"`
	Building      string   `json:"building"`
	Room          string   `json:"room"`
	// 反序列化后的字段
	Show       map[string]string `json:"show,omitempty"`
	TotalUsage *float64          `json:"total_usage,omitempty"`

	// 由相邻两条读数用窗口函数推算的派生量:
	// 消耗 = Δ总用电量 - Δ剩余电量(总用电量只在充值时跳升)
	Consumption *float64 `json:"consumption_kwh,omitempty"`
	Recharge    *float64 `json:"recharge_kwh,omitempty"`
	Power       *float64 `json:"power_kw,omitempty"`
}

// DailyStat 是按自然日聚合的用电统计(日期取区间结束读数所在日)。
type DailyStat struct {
	Day         string   `json:"day"`
	Consumption float64  `json:"consumption_kwh"`
	Recharge    float64  `json:"recharge_kwh"`
	AvgPower    *float64 `json:"avg_power_kw"`
	Samples     int      `json:"samples"`
	SurplusEnd  *float64 `json:"surplus_end"`
}

// RechargeEvent 是一次充值事件(总用电量发生跳升)。
type RechargeEvent struct {
	TS           string   `json:"ts"`
	Epoch        int64    `json:"epoch"`
	Recharge     float64  `json:"recharge_kwh"`
	SurplusAfter *float64 `json:"surplus_after"`
	Consumption  *float64 `json:"interval_consumption_kwh"`
}

// DB 封装 SQLite 数据库操作。
type DB struct {
	db *sql.DB
}

// Open 打开并初始化数据库。创建表、索引、执行迁移。
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("解析数据库绝对路径失败: %w", err)
	}
	dsnURL := sqliteFileURL(absPath, runtime.GOOS == "windows")
	query := dsnURL.Query()
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(NORMAL)")
	dsnURL.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", dsnURL.String())
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	database.SetMaxOpenConns(3)
	database.SetMaxIdleConns(1)

	d := &DB{db: database}
	if err := d.init(); err != nil {
		database.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0640); err != nil {
		database.Close()
		return nil, fmt.Errorf("设置数据库权限失败: %w", err)
	}
	return d, nil
}

func sqliteFileURL(path string, windowsPath bool) *url.URL {
	if windowsPath {
		path = strings.Map(func(r rune) rune {
			if r == rune(92) {
				return '/'
			}
			return r
		}, path)
		for strings.Contains(path, "//") {
			path = strings.ReplaceAll(path, "//", "/")
		}
	}
	normalized := filepath.ToSlash(path)
	if windowsPath {
		normalized = strings.ReplaceAll(path, `\`, "/")
		if len(normalized) >= 2 && normalized[1] == ':' && !strings.HasPrefix(normalized, "/") {
			normalized = "/" + normalized
		}
	}
	return &url.URL{Scheme: "file", Path: normalized}
}

// init 初始化数据库 schema。
func (d *DB) init() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS readings (
			ts            TEXT NOT NULL,
			epoch         INTEGER NOT NULL,
			room_label    TEXT,
			surplus_charge REAL,
			total_usage   REAL,
			show_json     TEXT,
			raw_json      TEXT,
			campus        TEXT,
			building      TEXT,
			room          TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("创建表失败: %w", err)
	}

	cols, err := d.tableColumns("readings")
	if err != nil {
		return err
	}
	for _, col := range []string{"campus", "building", "room"} {
		if !cols[col] {
			if _, err := d.db.Exec(fmt.Sprintf("ALTER TABLE readings ADD COLUMN %s TEXT", col)); err != nil {
				return fmt.Errorf("添加列 %s 失败: %w", col, err)
			}
		}
	}
	indexColumns, err := d.indexColumns("idx_readings_room_epoch")
	if err != nil {
		return err
	}
	wantedColumns := []string{"campus", "building", "room", "epoch"}
	if len(indexColumns) > 0 && !equalStrings(indexColumns, wantedColumns) {
		if _, err := d.db.Exec("DROP INDEX idx_readings_room_epoch"); err != nil {
			return fmt.Errorf("迁移旧索引失败: %w", err)
		}
	}
	_, err = d.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_readings_room_epoch
		ON readings(campus, building, room, epoch)
	`)
	if err != nil {
		return fmt.Errorf("创建索引失败: %w", err)
	}
	if _, err := d.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_readings_epoch
		ON readings(epoch)
	`); err != nil {
		return fmt.Errorf("创建全局时间索引失败: %w", err)
	}
	if _, err := d.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_readings_legacy_room_label
		ON readings(room_label) WHERE campus IS NULL
	`); err != nil {
		return fmt.Errorf("创建旧数据迁移索引失败: %w", err)
	}
	return nil
}

func (d *DB) indexColumns(index string) ([]string, error) {
	rows, err := d.db.Query(fmt.Sprintf("PRAGMA index_info(%s)", index))
	if err != nil {
		return nil, fmt.Errorf("读取索引 %s 失败: %w", index, err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var sequence, columnID int
		var name sql.NullString
		if err := rows.Scan(&sequence, &columnID, &name); err != nil {
			return nil, fmt.Errorf("读取索引 %s 字段失败: %w", index, err)
		}
		columns = append(columns, name.String)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历索引 %s 失败: %w", index, err)
	}
	return columns, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (d *DB) tableColumns(table string) (map[string]bool, error) {
	rows, err := d.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var name, ctype string
		var cid, notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("读取表结构失败: %w", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历表结构失败: %w", err)
	}
	return cols, nil
}

// totalUsageFromShow 从 show 字段解析"电表总用电量"。
func totalUsageFromShow(show map[string]string) *float64 {
	if show == nil {
		return nil
	}
	raw, ok := show["电表总用电量"]
	if !ok {
		return nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}

// BackfillRoomIDs 回填旧数据的 campus/building/room 字段。
func (d *DB) BackfillRoomIDs(cfg *config.Config) error {
	targets := cfg.GetTargets()
	if len(targets) == 0 {
		return nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("开始旧数据回填事务失败: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare("UPDATE readings SET campus=?, building=?, room=? WHERE campus IS NULL AND room_label=?")
	if err != nil {
		return fmt.Errorf("准备旧数据回填失败: %w", err)
	}
	for _, t := range targets {
		if t.Label == "" {
			continue
		}
		_, err := stmt.Exec(t.Campus, t.Building, t.Room, t.Label)
		if err != nil {
			return fmt.Errorf("回填宿舍 ID 失败: %w", err)
		}
	}
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("关闭旧数据回填语句失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交旧数据回填失败: %w", err)
	}
	return nil
}

// InsertReading 插入一条读数记录。
func (d *DB) InsertReading(t config.Target, reading struct {
	SurplusCharge *float64
	Show          map[string]string
	Raw           map[string]any
}) error {
	now := time.Now()
	showJSON, err := json.Marshal(reading.Show)
	if err != nil {
		return fmt.Errorf("序列化 show 数据失败: %w", err)
	}
	rawJSON, err := json.Marshal(reading.Raw)
	if err != nil {
		return fmt.Errorf("序列化 raw 数据失败: %w", err)
	}
	prev, prevErr := d.GetLatestReading(t.Campus, t.Building, t.Room)
	if prevErr != nil {
		slog.Warn("读取上一条记录失败", "room", t.DisplayLabel(), "err", prevErr)
	}

	_, err = d.db.Exec(
		`INSERT INTO readings (ts, epoch, room_label, surplus_charge, total_usage, show_json, raw_json, campus, building, room)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		now.Format("2006-01-02 15:04:05"),
		now.Unix(),
		t.DisplayLabel(),
		reading.SurplusCharge,
		totalUsageFromShow(reading.Show),
		string(showJSON),
		string(rawJSON),
		t.Campus, t.Building, t.Room,
	)
	if err != nil {
		return fmt.Errorf("插入读数失败: %w", err)
	}

	if prev != nil && prev.SurplusCharge != nil && reading.SurplusCharge != nil {
		delta := *reading.SurplusCharge - *prev.SurplusCharge
		if delta > 0.01 || delta < -0.01 {
			slog.Info("入库", "room", t.DisplayLabel(), "surplus_charge", *reading.SurplusCharge, "delta", delta)
		} else {
			slog.Info("入库", "room", t.DisplayLabel(), "surplus_charge", *reading.SurplusCharge)
		}
	} else {
		slog.Info("入库", "room", t.DisplayLabel(), "surplus_charge", reading.SurplusCharge)
	}
	return nil
}

// GetLatestReading 获取某个宿舍的最新读数。
func (d *DB) GetLatestReading(campus, building, room string) (*ReadingRow, error) {
	row := d.db.QueryRow(
		`SELECT ts, epoch, COALESCE(room_label,''), surplus_charge, COALESCE(show_json,''),
		        COALESCE(campus,''), COALESCE(building,''), COALESCE(room,'')
		 FROM readings WHERE campus=? AND building=? AND room=?
		 ORDER BY epoch DESC, rowid DESC LIMIT 1`,
		campus, building, room,
	)

	var r ReadingRow
	var ts, showJSON string
	err := row.Scan(&ts, &r.Epoch, &r.RoomLabel, &r.SurplusCharge, &showJSON, &r.Campus, &r.Building, &r.Room)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.TS = ts
	return &r, nil
}

// 由相邻读数推算区间消耗/充电的 SQL 片段。
// 总用电量只在充值时跳升,故 区间消耗 = Δ总用电量 - Δ剩余电量:
//   - Δ总用电量 = 0(没充值): 消耗 = -Δ剩余电量(剩余减少量)
//   - Δ总用电量 = R(充值 R): 消耗 = R - Δ剩余电量
//
// 间隔 <300s(重启/手动采集)或 >24h(停机缺口)、总用电量回退时置 NULL。
const (
	consumptionSQL = `CASE
		WHEN prev_epoch IS NULL OR epoch - prev_epoch < 300 OR epoch - prev_epoch > 86400
		     OR total_usage IS NULL OR prev_total IS NULL OR total_usage < prev_total
		     OR surplus_charge IS NULL OR prev_surplus IS NULL
		THEN NULL
		ELSE MAX(0.0, (total_usage - prev_total) - (surplus_charge - prev_surplus))
	END`
	rechargeSQL = `CASE
		WHEN prev_total IS NULL OR total_usage IS NULL OR total_usage <= prev_total
		THEN NULL ELSE total_usage - prev_total END`
)

// buildReadingFilter 生成 readings 查询的 WHERE 条件与参数。
func buildReadingFilter(days int, campus, building, room string) ([]string, []any, error) {
	var conds []string
	var args []any
	if days > 0 {
		cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
		conds = append(conds, "epoch >= ?")
		args = append(args, cutoff)
	}
	filters := 0
	for _, value := range []string{campus, building, room} {
		if value != "" {
			filters++
		}
	}
	if filters != 0 && filters != 3 {
		return nil, nil, fmt.Errorf("campus/building/room 必须同时提供")
	}
	if filters == 3 {
		conds = append(conds, "campus = ? AND building = ? AND room = ?")
		args = append(args, campus, building, room)
	}
	return conds, args, nil
}

func whereClause(conds []string) string {
	if len(conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conds, " AND ")
}

// QueryReadings 查询读数记录,并附带由相邻读数推算的消耗/充电/功率。
// 最多返回 10000 条记录以防止内存溢出。
func (d *DB) QueryReadings(days int, campus, building, room string) ([]ReadingRow, error) {
	conds, args, err := buildReadingFilter(days, campus, building, room)
	if err != nil {
		return nil, err
	}
	query := `WITH base AS (
			SELECT ts, epoch, COALESCE(room_label,'') AS room_label, surplus_charge, total_usage,
			       COALESCE(show_json,'') AS show_json, COALESCE(campus,'') AS campus,
			       COALESCE(building,'') AS building, COALESCE(room,'') AS room, rowid
			FROM readings` + whereClause(conds) + `
			ORDER BY epoch DESC, rowid DESC LIMIT 10000
		),
		lagged AS (
			SELECT *, LAG(total_usage) OVER w AS prev_total,
			       LAG(surplus_charge) OVER w AS prev_surplus,
			       LAG(epoch) OVER w AS prev_epoch
			FROM base
			WINDOW w AS (PARTITION BY campus, building, room ORDER BY epoch, rowid)
		),
		calc AS (
			SELECT *, ` + consumptionSQL + ` AS cons
			FROM lagged
		)
		SELECT ts, epoch, room_label, surplus_charge, total_usage, show_json, campus, building, room,
		       cons AS consumption_kwh,
		       ` + rechargeSQL + ` AS recharge_kwh,
		       CASE WHEN cons IS NULL THEN NULL
		            ELSE cons * 3600.0 / (epoch - prev_epoch) END AS power_kw
		FROM calc
		ORDER BY epoch ASC, rowid ASC`

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询读数失败: %w", err)
	}
	defer rows.Close()

	var result []ReadingRow
	for rows.Next() {
		var r ReadingRow
		var showJSON string
		if err := rows.Scan(&r.TS, &r.Epoch, &r.RoomLabel, &r.SurplusCharge, &r.TotalUsage, &showJSON,
			&r.Campus, &r.Building, &r.Room, &r.Consumption, &r.Recharge, &r.Power); err != nil {
			return nil, fmt.Errorf("扫描行失败: %w", err)
		}
		if showJSON != "" {
			var show map[string]string
			if err := json.Unmarshal([]byte(showJSON), &show); err == nil {
				r.Show = show
			}
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历查询结果失败: %w", err)
	}
	return result, nil
}

// QueryDailyStats 按自然日聚合消耗/充电/平均功率(日期取区间结束读数所在日)。
func (d *DB) QueryDailyStats(days int, campus, building, room string) ([]DailyStat, error) {
	conds, args, err := buildReadingFilter(days, campus, building, room)
	if err != nil {
		return nil, err
	}
	query := `WITH base AS (
			SELECT ts, epoch, surplus_charge, total_usage, campus, building, room, rowid
			FROM readings` + whereClause(conds) + `
			ORDER BY epoch DESC, rowid DESC LIMIT 10000
		),
		lagged AS (
			SELECT substr(ts,1,10) AS day, epoch, surplus_charge, total_usage,
			       LAG(total_usage) OVER w AS prev_total,
			       LAG(surplus_charge) OVER w AS prev_surplus,
			       LAG(epoch) OVER w AS prev_epoch
			FROM base
			WINDOW w AS (PARTITION BY campus, building, room ORDER BY epoch, rowid)
		),
		iv AS (
			SELECT day, epoch, prev_epoch, ` + consumptionSQL + ` AS cons, ` + rechargeSQL + ` AS recharge
			FROM lagged
		)
		SELECT day,
		       COALESCE(SUM(cons), 0) AS consumption,
		       COALESCE(SUM(recharge), 0) AS recharge,
		       CASE WHEN SUM(CASE WHEN cons IS NOT NULL THEN epoch - prev_epoch ELSE 0 END) > 0
		            THEN SUM(cons) * 3600.0 / SUM(CASE WHEN cons IS NOT NULL THEN epoch - prev_epoch ELSE 0 END)
		            ELSE NULL END AS avg_power,
		       COUNT(*) AS samples,
		       (SELECT l3.surplus_charge FROM lagged l3 WHERE l3.day = iv.day
		        ORDER BY l3.epoch DESC LIMIT 1) AS surplus_end
		FROM iv
		GROUP BY day
		ORDER BY day ASC`

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询每日统计失败: %w", err)
	}
	defer rows.Close()

	var result []DailyStat
	for rows.Next() {
		var st DailyStat
		if err := rows.Scan(&st.Day, &st.Consumption, &st.Recharge, &st.AvgPower, &st.Samples, &st.SurplusEnd); err != nil {
			return nil, fmt.Errorf("扫描每日统计失败: %w", err)
		}
		result = append(result, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历每日统计失败: %w", err)
	}
	return result, nil
}

// QueryRechargeEvents 返回充值事件(总用电量跳升),一次充值一条。
func (d *DB) QueryRechargeEvents(days int, campus, building, room string) ([]RechargeEvent, error) {
	conds, args, err := buildReadingFilter(days, campus, building, room)
	if err != nil {
		return nil, err
	}
	query := `WITH base AS (
			SELECT ts, epoch, surplus_charge, total_usage, campus, building, room, rowid
			FROM readings` + whereClause(conds) + `
			ORDER BY epoch DESC, rowid DESC LIMIT 10000
		),
		lagged AS (
			SELECT ts, epoch, surplus_charge, total_usage,
			       LAG(total_usage) OVER w AS prev_total,
			       LAG(surplus_charge) OVER w AS prev_surplus
			FROM base
			WINDOW w AS (PARTITION BY campus, building, room ORDER BY epoch, rowid)
		)
		SELECT ts, epoch, total_usage - prev_total AS recharge, surplus_charge,
		       CASE WHEN surplus_charge IS NULL OR prev_surplus IS NULL THEN NULL
		            ELSE MAX(0.0, (total_usage - prev_total) - (surplus_charge - prev_surplus))
		       END AS consumption
		FROM lagged
		WHERE prev_total IS NOT NULL AND total_usage IS NOT NULL AND total_usage - prev_total > 0.01
		ORDER BY epoch DESC
		LIMIT 1000`

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询充值记录失败: %w", err)
	}
	defer rows.Close()

	var result []RechargeEvent
	for rows.Next() {
		var ev RechargeEvent
		if err := rows.Scan(&ev.TS, &ev.Epoch, &ev.Recharge, &ev.SurplusAfter, &ev.Consumption); err != nil {
			return nil, fmt.Errorf("扫描充值记录失败: %w", err)
		}
		result = append(result, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历充值记录失败: %w", err)
	}
	return result, nil
}

// PowerPoint 是重采样后的功率点: 每个时间桶一点, 功率为「至少 bucketMinutes 的滚动窗口」
// 上的平均功率, 从而避免临近读数的短间隔把成块下降放大成伪尖峰。
type PowerPoint struct {
	TS          string   `json:"ts"`
	Epoch       int64    `json:"epoch"`
	WindowSecs  *int64   `json:"window_seconds"`
	Consumption *float64 `json:"consumption_kwh"`
	Power       *float64 `json:"power_kw"`
}

// QueryPowerSeries 返回按固定时间桶重采样的功率序列。
// bucketMinutes 是最小滚动窗口(同时作为分桶宽度, 默认 60, 限制 5..1440)。
// 每个桶取该桶内最后一条读数作为窗口终点, 起点为其之前至少 bucketMinutes 的最近读数;
// 这样窗口时长 >= bucketMinutes, 余额的成块跳变会被均摊到足够长的窗口上。
func (d *DB) QueryPowerSeries(days int, campus, building, room string, bucketMinutes int) ([]PowerPoint, error) {
	conds, args, err := buildReadingFilter(days, campus, building, room)
	if err != nil {
		return nil, err
	}
	if bucketMinutes <= 0 {
		bucketMinutes = 60
	}
	if bucketMinutes < 5 {
		bucketMinutes = 5
	}
	if bucketMinutes > 1440 {
		bucketMinutes = 1440
	}
	bucketSecs := int64(bucketMinutes) * 60
	args = append(args, bucketSecs, bucketSecs)

	query := `WITH base AS (
			SELECT ts, epoch, surplus_charge, total_usage, campus, building, room, rowid
			FROM readings` + whereClause(conds) + `
			ORDER BY epoch DESC, rowid DESC LIMIT 10000
		),
		paired AS (
			SELECT b.rowid, b.ts, b.epoch, b.surplus_charge, b.total_usage, b.campus, b.building, b.room,
			       (SELECT s.rowid FROM readings s
			         WHERE s.campus = b.campus AND s.building = b.building AND s.room = b.room
			           AND s.epoch <= b.epoch - ?
			         ORDER BY s.epoch DESC, s.rowid DESC LIMIT 1) AS start_rowid
			FROM base b
		),
		calc AS (
			SELECT p.rowid, p.ts, p.epoch, p.campus, p.building, p.room,
			       (p.epoch - s.epoch) AS win_secs,
			       CASE WHEN s.epoch IS NULL OR p.surplus_charge IS NULL OR s.surplus_charge IS NULL
			                 OR p.total_usage IS NULL OR s.total_usage IS NULL OR p.total_usage < s.total_usage
			            THEN NULL
			            ELSE MAX(0.0, (p.total_usage - s.total_usage) - (p.surplus_charge - s.surplus_charge))
			       END AS cons
			FROM paired p LEFT JOIN readings s ON s.rowid = p.start_rowid
		),
		ranked AS (
			SELECT *, ROW_NUMBER() OVER (
			    PARTITION BY campus, building, room, (epoch / ?)
			    ORDER BY epoch DESC, rowid DESC) AS rn
			FROM calc
		)
		SELECT ts, epoch, win_secs, cons,
		       CASE WHEN cons IS NULL THEN NULL ELSE cons * 3600.0 / win_secs END AS power_kw
		FROM ranked WHERE rn = 1
		ORDER BY epoch ASC`

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询功率序列失败: %w", err)
	}
	defer rows.Close()

	var result []PowerPoint
	for rows.Next() {
		var p PowerPoint
		if err := rows.Scan(&p.TS, &p.Epoch, &p.WindowSecs, &p.Consumption, &p.Power); err != nil {
			return nil, fmt.Errorf("扫描功率序列失败: %w", err)
		}
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历功率序列失败: %w", err)
	}
	return result, nil
}

// Close 关闭数据库连接。
func (d *DB) Close() error {
	return d.db.Close()
}

// Count 返回记录总数。
func (d *DB) Count() (int, error) {
	var n int
	err := d.db.QueryRow("SELECT COUNT(*) FROM readings").Scan(&n)
	return n, err
}

func (d *DB) Ping(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var one int
	return d.db.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}
