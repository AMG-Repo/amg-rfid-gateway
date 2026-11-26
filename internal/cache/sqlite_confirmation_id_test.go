package cache

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// seedLegacyConfirmations creates a pre-#146 pending_confirmations table with
// one unsynced and one synced row.
func seedLegacyConfirmations(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE pending_confirmations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uii TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('entrada', 'salida')),
			antenna_id TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced BOOLEAN DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced_at DATETIME,
			next_attempt_at DATETIME
		);
		INSERT INTO pending_confirmations (uii, action, synced) VALUES ('EPC-PENDING', 'entrada', 0);
		INSERT INTO pending_confirmations (uii, action, synced) VALUES ('EPC-SENT', 'salida', 1);
	`)
	if err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
}

func readConfirmationID(t *testing.T, db *sql.DB, uii string) sql.NullString {
	t.Helper()
	var id sql.NullString
	if err := db.QueryRow(`SELECT confirmation_id FROM pending_confirmations WHERE uii = ?`, uii).Scan(&id); err != nil {
		t.Fatalf("read confirmation_id of %s: %v", uii, err)
	}
	return id
}

func TestNewSQLite_PendingConfirmationsHasIdempotencyColumns(t *testing.T) {
	c, err := NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer c.Close()
	for _, col := range []string{"confirmation_id", "occurred_at"} {
		var n int
		if err := c.GetDB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('pending_confirmations') WHERE name = ?`, col).Scan(&n); err != nil || n != 1 {
			t.Errorf("column %s count = %d (err %v), want 1", col, n, err)
		}
	}
}

// Rows queued before the upgrade get an id once; reopening keeps that id.
func TestMigrate_AssignsStableConfirmationIDToUnsyncedRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	seedLegacyConfirmations(t, dbPath)

	c1, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	first := readConfirmationID(t, c1.GetDB(), "EPC-PENDING")
	c1.Close()
	if !first.Valid || len(first.String) != 36 {
		t.Fatalf("unsynced row confirmation_id = %+v, want a UUID", first)
	}

	c2, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer c2.Close()
	if second := readConfirmationID(t, c2.GetDB(), "EPC-PENDING"); second != first {
		t.Errorf("confirmation_id changed on second migration: %q -> %q", first.String, second.String)
	}
}

func TestMigrate_LeavesSyncedRowsAndOccurredAtAlone(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	seedLegacyConfirmations(t, dbPath)

	c, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer c.Close()
	if id := readConfirmationID(t, c.GetDB(), "EPC-SENT"); id.Valid {
		t.Errorf("synced row got confirmation_id %q, want NULL", id.String)
	}
	var withOccurredAt int
	if err := c.GetDB().QueryRow(`SELECT COUNT(*) FROM pending_confirmations WHERE occurred_at IS NOT NULL`).Scan(&withOccurredAt); err != nil || withOccurredAt != 0 {
		t.Errorf("rows with occurred_at = %d (err %v), want 0", withOccurredAt, err)
	}
}

func TestNewSQLite_ConfirmationIDIsUnique(t *testing.T) {
	c, err := NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer c.Close()
	db := c.GetDB()
	if _, err := db.Exec(`INSERT INTO pending_confirmations (uii, action, confirmation_id) VALUES ('A', 'entrada', 'x'), ('B', 'entrada', NULL), ('C', 'entrada', NULL)`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO pending_confirmations (uii, action, confirmation_id) VALUES ('D', 'entrada', 'x')`); err == nil {
		t.Errorf("duplicate confirmation_id accepted, want a unique constraint error")
	}
}
