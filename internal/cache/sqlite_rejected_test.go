package cache

import (
	"testing"
	"time"
)

// A reading the VPS rejected is kept apart: it is not pending, never resent,
// and the purge keeps it whatever its age.
func TestSQLite_RejectedReadingsAreKeptOutOfQueueAndPurge(t *testing.T) {
	c, ids := newRetentionCache(t, 3)
	old := time.Now().Add(-30 * 24 * time.Hour)

	// ids[0]: rejected, old -> kept, not pending
	// ids[1]: synced, old -> purged
	// ids[2]: unsynced -> pending
	if err := c.MarkRejected(ids[:1]); err != nil {
		t.Fatalf("failed to mark rejected: %v", err)
	}
	if err := c.MarkSynced(ids[1:2]); err != nil {
		t.Fatalf("failed to mark synced: %v", err)
	}
	setCreatedAt(t, c, ids[0], old)
	setCreatedAt(t, c, ids[1], old)

	pending, err := c.GetUnsynced(10)
	if err != nil {
		t.Fatalf("GetUnsynced: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != ids[2] {
		t.Errorf("GetUnsynced = %v, want only id %d", pending, ids[2])
	}
	if count, _ := c.Count(); count != 1 {
		t.Errorf("Count = %d, want 1", count)
	}

	removed, err := c.PurgeSyncedReadings(time.Now().Add(-ReadingRetention))
	if err != nil {
		t.Fatalf("purge failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 purged reading, got %d", removed)
	}
	var synced int
	if err := c.GetDB().QueryRow(`SELECT synced FROM pending_readings WHERE id = ?`, ids[0]).Scan(&synced); err != nil {
		t.Fatalf("rejected reading was purged: %v", err)
	}
	if synced != 2 {
		t.Errorf("rejected reading synced = %d, want 2", synced)
	}
}
