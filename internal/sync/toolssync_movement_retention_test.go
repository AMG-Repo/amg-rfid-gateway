package sync

import (
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"testing"
	"time"
)

func TestMovementRetentionPurgeThenSyncMustNotRollback(t *testing.T) {
	s, db := tagIdentityStore(t)
	k := localstore.Kanban{Code: "OLD-A", Zones: []string{"C"}}
	movementLocOK(t, s.AssignKanban("A", k))
	now := time.Now().UTC()
	c, err := s.CreateConfirmation("A", "entrada", "ant", now.Add(-40*24*time.Hour), 0)
	movementLocOK(t, err)
	_, err = db.Exec(`UPDATE pending_confirmations SET created_at=?,retry_count=10 WHERE id=?`, now.Add(-40*24*time.Hour).Format("2006-01-02 15:04:05"), c.ID)
	movementLocOK(t, err)
	movementLocOK(t, s.RecordSyncedConfirmation("A", "salida", "ant", now.Add(-35*24*time.Hour)))
	movementLocOK(t, s.RecordSyncedConfirmation("B", "entrada", "ant", now))
	location := "C"
	item := tagIdentityItem(1, "A", &k)
	item.Location = &location
	tagIdentityRun(t, s, []localstore.SyncDataItem{item}, []localstore.Kanban{k})
	if got := movementLocLocation(t, s); got != "C" {
		t.Fatalf("before purge=%q", got)
	}
	worker := NewToolsSync(&mockVPSClient{}, s, "test", time.Hour, time.Second)
	worker.purgeSyncedConfirmations()
	worker.performFlush() // Stuck old row is not sent, so cannot change VPS truth.
	tagIdentityRun(t, s, []localstore.SyncDataItem{item}, []localstore.Kanban{k})
	got := movementLocLocation(t, s)
	t.Logf("VPS snapshot=C; after actual purge+flush+sync location=%q", got)
	if got != "C" {
		t.Fatalf("S13: retention rolled back newer synced salida to obsolete pending entrada: %q", got)
	}
}
