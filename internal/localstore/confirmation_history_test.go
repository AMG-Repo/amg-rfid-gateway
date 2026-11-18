package localstore

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
)

// newProductionStore opens the schema the gateway really creates.
func newProductionStore(t *testing.T) (*LocalStore, *cache.SQLite) {
	t.Helper()
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("failed to open cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return New(c.GetDB()), c
}

func TestRecordSyncedConfirmation_IsStoredAsSyncedWithSendTime(t *testing.T) {
	store, c := newProductionStore(t)
	sentAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)

	if err := store.RecordSyncedConfirmation("EPC-1", "entrada", "ant-1", sentAt); err != nil {
		t.Fatalf("record failed: %v", err)
	}

	var synced bool
	var syncedAt time.Time
	var action, antenna string
	err := c.GetDB().QueryRow(`SELECT synced, synced_at, action, antenna_id FROM pending_confirmations WHERE uii = 'EPC-1'`).Scan(&synced, &syncedAt, &action, &antenna)
	if err != nil {
		t.Fatalf("row not stored: %v", err)
	}
	if !synced || action != "entrada" || antenna != "ant-1" {
		t.Errorf("unexpected row: synced=%v action=%s antenna=%s", synced, action, antenna)
	}
	if !syncedAt.Equal(sentAt) {
		t.Errorf("synced_at = %v, want %v", syncedAt, sentAt)
	}
	if pending, _ := store.GetPendingConfirmationsCount(); pending != 0 {
		t.Errorf("a synced confirmation must not count as pending, got %d", pending)
	}
	if queue, _ := store.GetUnsyncedConfirmations(10); len(queue) != 0 {
		t.Errorf("a synced confirmation must not be re-sent, got %+v", queue)
	}
}

func TestRecordSyncedConfirmation_RejectsInvalidActionWithoutStoringAnything(t *testing.T) {
	store, c := newProductionStore(t)

	if err := store.RecordSyncedConfirmation("EPC-1", "bogus", "ant-1", time.Now()); err == nil {
		t.Fatal("expected an error for an invalid action")
	}

	var n int
	c.GetDB().QueryRow(`SELECT COUNT(*) FROM pending_confirmations`).Scan(&n)
	if n != 0 {
		t.Errorf("expected nothing stored, got %d rows", n)
	}
}

func TestMarkConfirmationSynced_StampsSyncedAt(t *testing.T) {
	store, c := newProductionStore(t)
	conf, err := store.CreateConfirmation("EPC-1", "salida", "ant-1", time.Now(), 0)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := store.MarkConfirmationSynced(conf.ID); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	var syncedAt *time.Time
	if err := c.GetDB().QueryRow(`SELECT synced_at FROM pending_confirmations WHERE id = ?`, conf.ID).Scan(&syncedAt); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if syncedAt == nil {
		t.Fatal("synced_at must be set when a pending confirmation is marked synced")
	}
	if d := time.Since(*syncedAt); d < -2*time.Second || d > time.Minute {
		t.Errorf("synced_at %v is not about now", *syncedAt)
	}
}

func TestPurgeSyncedConfirmations_DeletesOnlyOldSyncedRows(t *testing.T) {
	store, c := newProductionStore(t)
	now := time.Now().UTC()
	old := now.Add(-31 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	recent := now.Add(-29 * 24 * time.Hour).Format("2006-01-02 15:04:05")

	seed := func(uii string, synced bool, syncedAt, createdAt any) {
		t.Helper()
		_, err := c.GetDB().Exec(`INSERT INTO pending_confirmations (uii, action, antenna_id, synced, synced_at, created_at) VALUES (?, 'entrada', 'a', ?, ?, ?)`,
			uii, synced, syncedAt, createdAt)
		if err != nil {
			t.Fatalf("seed %s failed: %v", uii, err)
		}
	}
	seed("OLD-SYNCED", true, old, old)
	seed("RECENT-SYNCED", true, recent, recent)
	seed("OLD-NULL-SYNCED-AT", true, nil, old)         // legacy synced row: falls back to created_at
	seed("RECENT-NULL-SYNCED-AT", true, nil, recent)   // legacy synced row, still within retention
	seed("OLD-PENDING", false, nil, old)               // never deleted
	seed("OLD-CREATED-RECENT-SYNC", true, recent, old) // retention counts from synced_at

	removed, err := store.PurgeSyncedConfirmations(now.Add(-ConfirmationRetention))
	if err != nil {
		t.Fatalf("purge failed: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}

	rows, err := c.GetDB().Query(`SELECT uii FROM pending_confirmations ORDER BY uii`)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()
	var left []string
	for rows.Next() {
		var uii string
		rows.Scan(&uii)
		left = append(left, uii)
	}
	want := []string{"OLD-CREATED-RECENT-SYNC", "OLD-PENDING", "RECENT-NULL-SYNCED-AT", "RECENT-SYNCED"}
	if len(left) != len(want) {
		t.Fatalf("left = %v, want %v", left, want)
	}
	for i := range want {
		if left[i] != want[i] {
			t.Fatalf("left = %v, want %v", left, want)
		}
	}
}

func TestPurgeSyncedConfirmations_KeepsLatestSyncedRowForLastSyncAt(t *testing.T) {
	store, c := newProductionStore(t)
	now := time.Now().UTC()
	oldest := now.Add(-40 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	latest := now.Add(-35 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	for uii, at := range map[string]string{"OLDEST": oldest, "LATEST": latest} {
		if _, err := c.GetDB().Exec(`INSERT INTO pending_confirmations (uii, action, antenna_id, synced, synced_at, created_at) VALUES (?, 'entrada', 'a', 1, ?, ?)`, uii, at, at); err != nil {
			t.Fatalf("seed %s failed: %v", uii, err)
		}
	}
	before, err := store.GetLastSyncedAt()
	if err != nil || before == nil {
		t.Fatalf("last synced before purge = %v, %v", before, err)
	}

	removed, err := store.PurgeSyncedConfirmations(now.Add(-ConfirmationRetention))
	if err != nil {
		t.Fatalf("purge failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1 (the latest synced row is kept)", removed)
	}

	after, err := store.GetLastSyncedAt()
	if err != nil || after == nil {
		t.Fatalf("last synced after purge = %v, %v; want it kept", after, err)
	}
	if !after.Equal(*before) {
		t.Errorf("last synced changed from %v to %v", *before, *after)
	}
}

func TestPurgeSyncedConfirmations_PurgesLegacyRowsWithoutSyncedAt(t *testing.T) {
	store, c := newProductionStore(t)
	old := time.Now().UTC().Add(-31 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := c.GetDB().Exec(`INSERT INTO pending_confirmations (uii, action, antenna_id, synced, created_at) VALUES ('LEGACY', 'entrada', 'a', 1, ?)`, old); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	removed, err := store.PurgeSyncedConfirmations(time.Now().Add(-ConfirmationRetention))
	if err != nil {
		t.Fatalf("purge failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
}

func TestConfirmationRetentionIs30Days(t *testing.T) {
	if ConfirmationRetention != 30*24*time.Hour {
		t.Errorf("ConfirmationRetention = %v, want 30 days", ConfirmationRetention)
	}
}
