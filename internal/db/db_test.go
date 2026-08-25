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
