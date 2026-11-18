package localstore

import (
	"testing"
	"time"
)

func tagLocation(t *testing.T, store *LocalStore, uii string) string {
	t.Helper()
	tool, err := store.GetToolByUII(uii)
	if err != nil || tool == nil {
		t.Fatalf("GetToolByUII(%s) = %+v, %v", uii, tool, err)
	}
	return tool.Location
}

func TestApplyMovementLocation_EntradaMovesToWarehouseAndSalidaToKanbanZone(t *testing.T) {
	store := setupKanbanStore(t)
	seedToolTag(t, store, 1, "A0000001")
	k := fordKanban() // zones C, A
	if err := store.AssignKanban("A0000001", k); err != nil {
		t.Fatalf("assign: %v", err)
	}

	if err := store.ApplyMovementLocation("A0000001", "entrada"); err != nil {
		t.Fatalf("entrada: %v", err)
	}
	if got := tagLocation(t, store, "A0000001"); got != WarehouseLocation {
		t.Errorf("after entrada location = %q, want %q", got, WarehouseLocation)
	}

	if err := store.ApplyMovementLocation("A0000001", "salida"); err != nil {
		t.Fatalf("salida: %v", err)
	}
	if got := tagLocation(t, store, "A0000001"); got != "C" {
		t.Errorf("after salida location = %q, want the kanban's first zone C", got)
	}
}

func TestApplyMovementLocation_SalidaWithoutKanbanZoneLeavesLocationEmpty(t *testing.T) {
	store := setupKanbanStore(t)
	seedToolTag(t, store, 1, "A0000001")
	if err := store.ApplyMovementLocation("A0000001", "entrada"); err != nil {
		t.Fatalf("entrada: %v", err)
	}

	if err := store.ApplyMovementLocation("A0000001", "salida"); err != nil {
		t.Fatalf("salida: %v", err)
	}
	if got := tagLocation(t, store, "A0000001"); got != "" {
		t.Errorf("after salida without kanban location = %q, want empty", got)
	}
}

// A sync carrying the VPS's older location must not roll back a movement the
// gateway already accepted but has not sent yet.
func TestReapplyPendingMovements_RestoresLatestPendingAction(t *testing.T) {
	store := setupKanbanStore(t)
	seedToolTag(t, store, 1, "A0000001")
	seedToolTag(t, store, 2, "B0000002")
	now := time.Now()
	for _, c := range []struct{ uii, action string }{
		{"A0000001", "entrada"}, {"A0000001", "salida"}, // latest pending: salida
		{"B0000002", "entrada"},
	} {
		if _, err := store.CreateConfirmation(c.uii, c.action, "ant-1", now, 0); err != nil {
			t.Fatalf("queue %s: %v", c.uii, err)
		}
		now = now.Add(time.Second)
	}
	// The sync rewrote both tags with the VPS's stale view.
	if _, err := store.db.Exec(`UPDATE tool_tags SET location = ?`, WarehouseLocation); err != nil {
		t.Fatalf("stale sync: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE tool_tags SET location = 'E' WHERE uii = 'B0000002'`); err != nil {
		t.Fatalf("stale sync: %v", err)
	}

	if err := store.ReapplyPendingMovements(); err != nil {
		t.Fatalf("reapply: %v", err)
	}
	if got := tagLocation(t, store, "A0000001"); got != "" {
		t.Errorf("A location = %q, want empty (pending salida, no kanban)", got)
	}
	if got := tagLocation(t, store, "B0000002"); got != WarehouseLocation {
		t.Errorf("B location = %q, want %q (pending entrada)", got, WarehouseLocation)
	}
}

// A newer movement that already reached the VPS wins over an older pending
// one for the same tag: re-applying must not roll the tag back.
func TestReapplyPendingMovements_NewerSentMovementWins(t *testing.T) {
	store := setupKanbanStore(t)
	seedToolTag(t, store, 1, "A0000001")
	if err := store.AssignKanban("A0000001", fordKanban()); err != nil { // zones C, A
		t.Fatalf("assign: %v", err)
	}
	if _, err := store.CreateConfirmation("A0000001", "entrada", "ant-1", time.Now(), 0); err != nil {
		t.Fatalf("queue entrada: %v", err)
	}
	if err := store.RecordSyncedConfirmation("A0000001", "salida", "ant-1", time.Now()); err != nil {
		t.Fatalf("record online salida: %v", err)
	}
	if err := store.ApplyMovementLocation("A0000001", "salida"); err != nil {
		t.Fatalf("apply salida: %v", err)
	}

	if err := store.ReapplyPendingMovements(); err != nil {
		t.Fatalf("reapply: %v", err)
	}
	if got := tagLocation(t, store, "A0000001"); got != "C" {
		t.Errorf("location = %q, want C from the newer sent salida", got)
	}
}

// Retention must not purge a tag's newer sent movement while an older one of
// the same tag is still pending: the pending one would become its "latest"
// and the next sync would roll the tag back to it.
func TestPurgeSyncedConfirmations_KeepsSentMovementsNewerThanAPendingOne(t *testing.T) {
	store := setupKanbanStore(t)
	seedToolTag(t, store, 1, "A0000001")
	if err := store.AssignKanban("A0000001", fordKanban()); err != nil { // zones C, A
		t.Fatalf("assign: %v", err)
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	if _, err := store.CreateConfirmation("A0000001", "entrada", "ant-1", old, 0); err != nil {
		t.Fatalf("queue entrada: %v", err)
	}
	for _, uii := range []string{"A0000001", "C0000003"} { // C has no pending movement
		if err := store.RecordSyncedConfirmation(uii, "salida", "ant-1", time.Now().Add(-35*24*time.Hour)); err != nil {
			t.Fatalf("record %s: %v", uii, err)
		}
	}
	if err := store.RecordSyncedConfirmation("B0000002", "entrada", "ant-1", time.Now()); err != nil {
		t.Fatalf("record B: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE pending_confirmations SET retry_count = 10, created_at = datetime('now', '-40 days') WHERE synced = 0`); err != nil {
		t.Fatalf("age pending: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE pending_confirmations SET created_at = synced_at WHERE synced = 1`); err != nil {
		t.Fatalf("age synced: %v", err)
	}

	removed, err := store.PurgeSyncedConfirmations(time.Now().Add(-ConfirmationRetention))
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1 (only C's old sent movement)", removed)
	}

	if err := store.ApplyMovementLocation("A0000001", "salida"); err != nil {
		t.Fatalf("apply salida: %v", err)
	}
	if err := store.ReapplyPendingMovements(); err != nil {
		t.Fatalf("reapply: %v", err)
	}
	if got := tagLocation(t, store, "A0000001"); got != "C" {
		t.Errorf("location = %q, want C: the newer sent salida must still win after retention", got)
	}
}
