package db

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteFileURLWindowsPath(t *testing.T) {
	u := sqliteFileURL(`C:\Users\M\AppData\Local\WxxyshallMonitoring\data\electricity.db`, true)
	if got, want := u.String(), "file:///C:/Users/M/AppData/Local/WxxyshallMonitoring/data/electricity.db"; got != want {
		t.Fatalf("Windows SQLite URL = %q, want %q", got, want)
	}
}

func TestQueryReadingsRetainsAllAndReturnsNewestTenThousand(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	tx, err := database.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO readings
		(ts, epoch, room_label, surplus_charge, show_json, raw_json, campus, building, room)
		VALUES (?, ?, ?, ?, '{}', '{}', 'A', 'B', 'C')`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10001; i++ {
		if _, err := stmt.Exec(fmt.Sprintf("ts-%d", i), i, "room", float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if count, err := database.Count(); err != nil || count != 10001 {
		t.Fatalf("Count() = %d, %v; want 10001", count, err)
	}
	rows, err := database.QueryReadings(0, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 10000 || rows[0].Epoch != 2 || rows[len(rows)-1].Epoch != 10001 {
		t.Fatalf("unexpected query window: len=%d first=%d last=%d", len(rows), rows[0].Epoch, rows[len(rows)-1].Epoch)
	}
}

func TestQueryDerivedConsumptionRechargeAndPower(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	base := int64(1700000000)
	type sample struct {
		ts      string
		epoch   int64
		surplus float64
		total   *float64
	}
	f := func(v float64) *float64 { return &v }
	samples := []sample{
		{"2026-09-20 10:00:00", base, 100, f(0)},
		{"2026-09-20 11:00:00", base + 3600, 98, f(0)},
		{"2026-09-20 12:00:00", base + 7200, 90, f(10)},           // 充值 10
		{"2026-09-21 13:00:00", base + 7200 + 25*3600, 88, f(10)}, // >24h 缺口
	}
	for _, s := range samples {
		if _, err := database.db.Exec(`INSERT INTO readings
			(ts, epoch, room_label, surplus_charge, total_usage, show_json, raw_json, campus, building, room)
			VALUES (?, ?, 'room', ?, ?, '{}', '{}', 'A', 'B', 'C')`,
			s.ts, s.epoch, s.surplus, s.total); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := database.QueryReadings(0, "A", "B", "C")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	// 首行无前值
	if rows[0].Consumption != nil || rows[0].Power != nil {
		t.Fatalf("first row should have nil derived values: %+v", rows[0])
	}
	// 正常用电: 消耗 2, 功率 2kW
	if rows[1].Consumption == nil || *rows[1].Consumption != 2 {
		t.Fatalf("row1 consumption = %v, want 2", rows[1].Consumption)
	}
	if rows[1].Power == nil || *rows[1].Power != 2 {
		t.Fatalf("row1 power = %v, want 2", rows[1].Power)
	}
	// 充值: 消耗 = 10 - (90-98) = 18, 充电 10
	if rows[2].Consumption == nil || *rows[2].Consumption != 18 {
		t.Fatalf("row2 consumption = %v, want 18", rows[2].Consumption)
	}
	if rows[2].Recharge == nil || *rows[2].Recharge != 10 {
		t.Fatalf("row2 recharge = %v, want 10", rows[2].Recharge)
	}
	// 缺口 >24h: 功率置空
	if rows[3].Power != nil || rows[3].Consumption != nil {
		t.Fatalf("row3 (gap) should be nil: cons=%v power=%v", rows[3].Consumption, rows[3].Power)
	}

	daily, err := database.QueryDailyStats(0, "A", "B", "C")
	if err != nil {
		t.Fatal(err)
	}
	if len(daily) != 2 {
		t.Fatalf("daily = %d, want 2", len(daily))
	}
	if daily[0].Day != "2026-09-20" || daily[0].Consumption != 20 || daily[0].AvgPower == nil || *daily[0].AvgPower != 10 {
		t.Fatalf("daily[0] = %+v, want day=2026-09-20 cons=20 avgPower=10", daily[0])
	}
	if daily[0].Recharge != 10 {
		t.Fatalf("daily[0].Recharge = %v, want 10", daily[0].Recharge)
	}

	events, err := database.QueryRechargeEvents(0, "A", "B", "C")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("recharge events = %d, want 1", len(events))
	}
	if events[0].Recharge != 10 || events[0].SurplusAfter == nil || *events[0].SurplusAfter != 90 {
		t.Fatalf("event = %+v, want recharge=10 surplusAfter=90", events[0])
	}
}

func TestQueryReadingsRequiresCompleteRoomFilter(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.QueryReadings(0, "A", "", ""); err == nil {
		t.Fatal("partial room filter should fail")
	}
}

func TestOpenCreatesGlobalAndPerRoomTimeIndexes(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, name := range []string{"idx_readings_epoch", "idx_readings_room_epoch", "idx_readings_legacy_room_label"} {
		var count int
		if err := database.db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", name,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("index %s count = %d, want 1", name, count)
		}
	}
}

func TestInitMigratesOnlyMismatchedRoomIndex(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.db.Exec("DROP INDEX idx_readings_room_epoch"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec("CREATE INDEX idx_readings_room_epoch ON readings(room_label, epoch)"); err != nil {
		t.Fatal(err)
	}
	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	columns, err := database.indexColumns("idx_readings_room_epoch")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"campus", "building", "room", "epoch"}
	if !equalStrings(columns, want) {
		t.Fatalf("index columns = %v, want %v", columns, want)
	}
}

func TestOpenEscapesSpecialCharactersInDatabasePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history?name#1.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database was not created at exact path %q: %v", path, err)
	}
}
