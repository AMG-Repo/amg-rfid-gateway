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
