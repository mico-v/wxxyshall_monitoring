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
		`INSERT INTO readings (ts, epoch, room_label, surplus_charge, show_json, raw_json, campus, building, room)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		now.Format("2006-01-02 15:04:05"),
		now.Unix(),
		t.DisplayLabel(),
		reading.SurplusCharge,
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

// QueryReadings 查询读数记录。
// 最多返回 10000 条记录以防止内存溢出。
func (d *DB) QueryReadings(days int, campus, building, room string) ([]ReadingRow, error) {
	query := `SELECT ts, epoch, room_label, surplus_charge, show_json, campus, building, room FROM (
		SELECT ts, epoch, COALESCE(room_label,'') AS room_label, surplus_charge,
		       COALESCE(show_json,'') AS show_json, COALESCE(campus,'') AS campus,
		       COALESCE(building,'') AS building, COALESCE(room,'') AS room, rowid
		FROM readings`
	var args []any
	var conds []string

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
		return nil, fmt.Errorf("campus/building/room 必须同时提供")
	}
	if filters == 3 {
		conds = append(conds, "campus = ? AND building = ? AND room = ?")
		args = append(args, campus, building, room)
	}
	if len(conds) > 0 {
		query += " WHERE " + conds[0]
		for _, c := range conds[1:] {
			query += " AND " + c
		}
	}
	query += " ORDER BY epoch DESC, rowid DESC LIMIT 10000) ORDER BY epoch ASC, rowid ASC"

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询读数失败: %w", err)
	}
	defer rows.Close()

	var result []ReadingRow
	for rows.Next() {
		var r ReadingRow
		var showJSON string
		if err := rows.Scan(&r.TS, &r.Epoch, &r.RoomLabel, &r.SurplusCharge, &showJSON, &r.Campus, &r.Building, &r.Room); err != nil {
			return nil, fmt.Errorf("扫描行失败: %w", err)
		}
		if showJSON != "" {
			var show map[string]string
			if err := json.Unmarshal([]byte(showJSON), &show); err == nil {
				r.Show = show
			}
			if totalStr, ok := r.Show["电表总用电量"]; ok {
				if f, err := strconv.ParseFloat(strings.TrimSpace(totalStr), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
					r.TotalUsage = &f
				}
			}
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历查询结果失败: %w", err)
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
