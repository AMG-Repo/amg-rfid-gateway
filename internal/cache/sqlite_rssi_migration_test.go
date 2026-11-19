package cache

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// seedLegacyRSSIQueue creates a queue as an earlier gateway left it: rssi
// stored as a positive Chainway magnitude, next to unknown and signed rows.
func seedLegacyRSSIQueue(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE pending_readings (
		id INTEGER PRIMARY KEY AUTOINCREMENT, antenna_id TEXT NOT NULL, gateway_id TEXT NOT NULL,
		epc TEXT NOT NULL, rssi INTEGER NOT NULL, timestamp DATETIME NOT NULL,
		synced BOOLEAN DEFAULT 0, retry_count INTEGER DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
		INSERT INTO pending_readings (antenna_id, gateway_id, epc, rssi, timestamp) VALUES
			('almacen', 'gw', 'POS', 66, '2026-11-19 12:00:01'),
			('almacen', 'gw', 'ZERO', 0, '2026-11-19 12:00:02'),
			('almacen', 'gw', 'NEG', -55, '2026-11-19 12:00:03');`)
	if err != nil {
		t.Fatalf("seed legacy queue: %v", err)
	}
}

func unsyncedRSSIByEPC(t *testing.T, path string) map[string]int {
	t.Helper()
	cache, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer cache.Close()
	rows, err := cache.GetUnsynced(10)
	if err != nil {
		t.Fatalf("GetUnsynced: %v", err)
	}
	got := map[string]int{}
	for _, r := range rows {
		if err := r.Reading.Validate(); err != nil {
			t.Errorf("row %s rssi %d fails Validate: %v", r.EPC, r.RSSI, err)
		}
		got[r.EPC] = r.RSSI
	}
	return got
}

// Positive rows queued before signed dBm are converted once on open; unknown
// and already signed rows are untouched, and reopening changes nothing.
func TestSQLite_MigratesPositiveRSSIToSignedDBm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	seedLegacyRSSIQueue(t, path)

	want := map[string]int{"POS": -66, "ZERO": 0, "NEG": -55}
	for open := 1; open <= 2; open++ {
		got := unsyncedRSSIByEPC(t, path)
		if len(got) != len(want) {
			t.Fatalf("open %d: rows = %v; want %v", open, got, want)
		}
		for epc, rssi := range want {
			if got[epc] != rssi {
				t.Errorf("open %d: %s rssi = %d; want %d", open, epc, got[epc], rssi)
			}
		}
	}
}
