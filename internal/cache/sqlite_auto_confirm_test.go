package cache

import (
	"path/filepath"
	"testing"
)

func TestNewSQLite_HasSettingsTableAndAutoColumn(t *testing.T) {
	c, err := NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer c.Close()
	if _, err := c.GetDB().Exec(`INSERT INTO settings (key, value) VALUES ('k', 'v')`); err != nil {
		t.Errorf("insert into settings: %v", err)
	}
	if _, err := c.GetDB().Exec(`INSERT INTO pending_confirmations (uii, action) VALUES ('E1', 'entrada')`); err != nil {
		t.Fatalf("insert confirmation: %v", err)
	}
	var auto int
	if err := c.GetDB().QueryRow(`SELECT auto FROM pending_confirmations WHERE uii = 'E1'`).Scan(&auto); err != nil || auto != 0 {
		t.Errorf("auto = %d (err %v), want 0 by default", auto, err)
	}
}

// An existing database without the auto column or settings table is migrated,
// keeps its rows (auto = 0), and a second open is a no-op.
func TestNewSQLite_AutoConfirmMigrationIsIdempotentOnExistingDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	seedLegacyConfirmations(t, dbPath)
	for i := 0; i < 2; i++ {
		c, err := NewSQLite(dbPath)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		var n, autoSum int
		if err := c.GetDB().QueryRow(`SELECT COUNT(*), COALESCE(SUM(auto), 0) FROM pending_confirmations`).Scan(&n, &autoSum); err != nil {
			t.Fatalf("open %d: read auto: %v", i, err)
		}
		if n != 2 || autoSum != 0 {
			t.Errorf("open %d: rows %d auto sum %d, want 2 and 0", i, n, autoSum)
		}
		if _, err := c.GetDB().Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('k', 'v')`); err != nil {
			t.Errorf("open %d: settings: %v", i, err)
		}
		c.Close()
	}
}
