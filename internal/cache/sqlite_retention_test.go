package cache

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// newRetentionCache stores n readings with distinct EPCs and returns their IDs.
func newRetentionCache(t *testing.T, n int) (*SQLite, []int64) {
	t.Helper()
	c, err := NewSQLite(filepath.Join(t.TempDir(), "retention.db"))
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	for i := 0; i < n; i++ {
		r := models.Reading{
			AntennaID: "ant-1",
			EPC:       fmt.Sprintf("EPC-RET-%d", i),
			RSSI:      -55,
			Timestamp: time.Now(),
		}
		if err := c.Store(r); err != nil {
			t.Fatalf("failed to store reading: %v", err)
		}
	}
	if err := c.Flush(); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}
	pending, err := c.GetUnsynced(n)
	if err != nil || len(pending) != n {
		t.Fatalf("expected %d unsynced readings, got %d (err %v)", n, len(pending), err)
	}
	ids := make([]int64, n)
	for i, p := range pending {
		ids[i] = p.ID
	}
	return c, ids
}

func setCreatedAt(t *testing.T, c *SQLite, id int64, at time.Time) {
	t.Helper()
	if _, err := c.GetDB().Exec(`UPDATE pending_readings SET created_at = ? WHERE id = ?`,
		at.UTC().Format("2006-01-02 15:04:05"), id); err != nil {
		t.Fatalf("failed to set created_at: %v", err)
	}
}

func totalRows(t *testing.T, c *SQLite) int {
	t.Helper()
	var n int
	if err := c.GetDB().QueryRow(`SELECT COUNT(*) FROM pending_readings`).Scan(&n); err != nil {
		t.Fatalf("failed to count rows: %v", err)
	}
	return n
}

// Count reports the sync backlog: readings already sent to the VPS are not pending.
func TestSQLite_CountExcludesSyncedReadings(t *testing.T) {
	c, ids := newRetentionCache(t, 3)

	if err := c.MarkSynced(ids[:2]); err != nil {
		t.Fatalf("failed to mark synced: %v", err)
	}

	count, err := c.Count()
	if err != nil {
		t.Fatalf("failed to count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 pending reading, got %d", count)
	}
}

// The purge deletes only synced readings older than the cutoff; unsynced
// readings are kept whatever their age.
func TestSQLite_PurgeSyncedReadingsOlderThanCutoff(t *testing.T) {
	c, ids := newRetentionCache(t, 4)
	old := time.Now().Add(-10 * 24 * time.Hour)
	cutoff := time.Now().Add(-ReadingRetention)

	// ids[0]: synced, old -> purged
	// ids[1]: synced, recent -> kept
	// ids[2]: unsynced, old -> kept
	// ids[3]: unsynced, recent -> kept
	if err := c.MarkSynced(ids[:2]); err != nil {
		t.Fatalf("failed to mark synced: %v", err)
	}
	setCreatedAt(t, c, ids[0], old)
	setCreatedAt(t, c, ids[2], old)

	removed, err := c.PurgeSyncedReadings(cutoff)
	if err != nil {
		t.Fatalf("purge failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 purged reading, got %d", removed)
	}
	if got := totalRows(t, c); got != 3 {
		t.Errorf("expected 3 rows left, got %d", got)
	}
	var gone int
	if err := c.GetDB().QueryRow(`SELECT COUNT(*) FROM pending_readings WHERE id = ?`, ids[0]).Scan(&gone); err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if gone != 0 {
		t.Errorf("expected the old synced reading to be purged")
	}
	if count, _ := c.Count(); count != 2 {
		t.Errorf("expected 2 pending readings after purge, got %d", count)
	}
}

// A large backlog of synced readings is purged completely in one call, even
// though the delete runs in bounded batches.
func TestSQLite_PurgeSyncedReadingsLargeBacklog(t *testing.T) {
	c, _ := newRetentionCache(t, 0)
	// Seed with SQL: the async Store buffer holds fewer rows than one batch.
	const n = purgeBatchSize + 7
	if _, err := c.GetDB().Exec(`
		WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM seq WHERE i < ?)
		INSERT INTO pending_readings (antenna_id, gateway_id, epc, rssi, timestamp, synced, created_at)
		SELECT 'ant-1', 'gw-1', 'EPC-BULK-' || i, -55, ?, 1, ? FROM seq
	`, n, time.Now().UTC(), time.Now().Add(-30*24*time.Hour).UTC().Format("2006-01-02 15:04:05")); err != nil {
		t.Fatalf("failed to seed rows: %v", err)
	}

	removed, err := c.PurgeSyncedReadings(time.Now().Add(-ReadingRetention))
	if err != nil {
		t.Fatalf("purge failed: %v", err)
	}
	if removed != n {
		t.Errorf("expected %d purged readings, got %d", n, removed)
	}
	if got := totalRows(t, c); got != 0 {
		t.Errorf("expected no rows left, got %d", got)
	}
}
