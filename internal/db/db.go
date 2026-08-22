// Package db 提供 SQLite 数据库操作。
//
// 数据文件位于 USTS_DATA_DIR/electricity.db。
// WAL 模式允许 webapp 读与 monitor 写并发。
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	_ "modernc.org/sqlite"
)

// ReadingRow 代表 readings 表中的一行记录。
type ReadingRow struct {
	TS            string            `json:"ts"`
	Epoch         int64             `json:"epoch"`
	RoomLabel     string            `json:"room_label"`
	SurplusCharge *float64          `json:"surplus_charge"`
	ShowJSON      string            `json:"-"`
	RawJSON       string            `json:"-"`
	Campus        string            `json:"campus"`
	Building      string            `json:"building"`
	Room          string            `json:"room"`
	// 反序列化后的字段
	Show       map[string]string `json:"show,omitempty"`
	TotalUsage *float64          `json:"total_usage,omitempty"`
}

// DB 封装 SQLite 数据库操作。
type DB struct {
	path string
	db   *sql.DB
}

// Open 打开并初始化数据库。创建表、索引、执行迁移。
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	database.SetMaxOpenConns(3)
	database.SetMaxIdleConns(1)

	d := &DB{path: path, db: database}
	if err := d.init(); err != nil {
		database.Close()
		return nil, err
	}
	return d, nil
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

	d.db.Exec("DROP INDEX IF EXISTS idx_readings_room_epoch")
	_, err = d.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_readings_room_epoch
		ON readings(campus, building, room, epoch)
	`)
	if err != nil {
		return fmt.Errorf("创建索引失败: %w", err)
	}
	return nil
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
			continue
		}
		cols[name] = true
	}
	return cols, nil
}

// BackfillRoomIDs 回填旧数据的 campus/building/room 字段。
func (d *DB) BackfillRoomIDs(cfg *config.Config) error {
	for _, t := range cfg.GetTargets() {
		if t.Label == "" {
			continue
		}
		_, err := d.db.Exec(
			"UPDATE readings SET campus=?, building=?, room=? WHERE campus IS NULL AND room_label=?",
			t.Campus, t.Building, t.Room, t.Label,
		)
		if err != nil {
			return fmt.Errorf("回填宿舍 ID 失败: %w", err)
		}
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
	showJSON, _ := json.Marshal(reading.Show)
	rawJSON, _ := json.Marshal(reading.Raw)

	_, err := d.db.Exec(
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

	prev, _ := d.GetLatestReading(t.Campus, t.Building, t.Room)
	if prev != nil && prev.SurplusCharge != nil && reading.SurplusCharge != nil {
		delta := *reading.SurplusCharge - *prev.SurplusCharge
		if delta > 0.01 || delta < -0.01 {
			log.Printf("入库: %s  surplusCharge=%v (剩余变化 %+.2f)", t.DisplayLabel(), *reading.SurplusCharge, delta)
		} else {
			log.Printf("入库: %s  surplusCharge=%v", t.DisplayLabel(), *reading.SurplusCharge)
		}
	} else {
		log.Printf("入库: %s  surplusCharge=%v", t.DisplayLabel(), reading.SurplusCharge)
	}
	return nil
}

// GetLatestReading 获取某个宿舍的最新读数。
func (d *DB) GetLatestReading(campus, building, room string) (*ReadingRow, error) {
	row := d.db.QueryRow(
		`SELECT ts, epoch, room_label, surplus_charge, show_json, campus, building, room
		 FROM readings WHERE campus=? AND building=? AND room=?
		 ORDER BY epoch DESC LIMIT 1`,
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
func (d *DB) QueryReadings(days int, campus, building, room string) ([]ReadingRow, error) {
	query := `SELECT ts, epoch, room_label, surplus_charge, show_json, campus, building, room FROM readings`
	var args []any
	var conds []string

	if days > 0 {
		cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
		conds = append(conds, "epoch >= ?")
		args = append(args, cutoff)
	}
	if campus != "" && building != "" && room != "" {
		conds = append(conds, "campus = ? AND building = ? AND room = ?")
		args = append(args, campus, building, room)
	}
	if len(conds) > 0 {
		query += " WHERE " + conds[0]
		for _, c := range conds[1:] {
			query += " AND " + c
		}
	}
	query += " ORDER BY epoch ASC"

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
				var f float64
				if _, err := fmt.Sscanf(totalStr, "%f", &f); err == nil {
					r.TotalUsage = &f
				}
			}
		}
		result = append(result, r)
	}
	return result, nil
}

// Reset 重置数据库（删除所有记录）。
func (d *DB) Reset() error {
	_, err := d.db.Exec("DELETE FROM readings")
	return err
}

// Close 关闭数据库连接。
func (d *DB) Close() error {
	return d.db.Close()
}

// Path 返回数据库文件路径。
func (d *DB) Path() string {
	return d.path
}

// Count 返回记录总数。
func (d *DB) Count() (int, error) {
	var n int
	err := d.db.QueryRow("SELECT COUNT(*) FROM readings").Scan(&n)
	return n, err
}