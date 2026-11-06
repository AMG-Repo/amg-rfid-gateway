package sync

import (
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

// A sync carrying the VPS's older location must not roll back a movement the
// gateway accepted offline and has not sent yet.
func TestToolsSync_SyncKeepsLocationOfPendingMovement(t *testing.T) {
	s, _ := tagIdentityStore(t)
	if _, err := s.CreateConfirmation("A", "salida", "ant-1", time.Now(), 0); err != nil {
		t.Fatalf("queue: %v", err)
	}
	if err := s.ApplyMovementLocation("A", "salida"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	stale := localstore.WarehouseLocation
	ka, kb := localstore.Kanban{Code: "OLD-A"}, localstore.Kanban{Code: "OLD-B"}
	a, b := tagIdentityItem(1, "A", &ka), tagIdentityItem(2, "B", &kb)
	a.Location, b.Location = &stale, &stale
	tagIdentityRun(t, s, []localstore.SyncDataItem{a, b}, []localstore.Kanban{ka, kb})

	tool, err := s.GetToolByUII("A")
	if err != nil || tool == nil {
		t.Fatalf("GetToolByUII = %+v, %v", tool, err)
	}
	if tool.Location == localstore.WarehouseLocation {
		t.Errorf("sync rolled back the pending salida: location = %q", tool.Location)
	}
	other, _ := s.GetToolByUII("B")
	if other == nil || other.Location != localstore.WarehouseLocation {
		t.Errorf("tag without pending movements must follow the VPS, got %+v", other)
	}
}
