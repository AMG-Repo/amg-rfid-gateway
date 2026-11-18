package sync

import (
	"errors"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"testing"
	"time"
)

func movementLocOK(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func movementLocLocation(t *testing.T, s *localstore.LocalStore) string {
	t.Helper()
	x, e := s.GetToolByUII("A")
	movementLocOK(t, e)
	if x == nil {
		t.Fatal("tag missing")
	}
	return x.Location
}
func TestMovementLocationOfflineRoundTripThroughSyncAndFlush(t *testing.T) {
	for _, zone := range []string{"C", ""} {
		t.Run("zone="+zone, func(t *testing.T) {
			s, _ := tagIdentityStore(t)
			k := localstore.Kanban{Code: "OLD-A"}
			if zone != "" {
				k.Zones = []string{zone}
			}
			movementLocOK(t, s.AssignKanban("A", k))
			for _, action := range []string{"entrada", "salida"} {
				_, e := s.CreateConfirmation("A", action, "ant", time.Now(), 0)
				movementLocOK(t, e)
				movementLocOK(t, s.ApplyMovementLocation("A", action))
				t.Logf("queued %s location=%q", action, movementLocLocation(t, s))
			}
			stale := localstore.WarehouseLocation
			item := tagIdentityItem(1, "A", &k)
			item.Location = &stale
			tagIdentityRun(t, s, []localstore.SyncDataItem{item}, []localstore.Kanban{k})
			if got := movementLocLocation(t, s); got != zone {
				t.Fatalf("pending sync location=%q want %q", got, zone)
			}
			worker := NewToolsSync(&mockVPSClient{}, s, "test", time.Hour, time.Second)
			worker.performFlush()
			n, e := s.GetPendingConfirmationsCount()
			movementLocOK(t, e)
			if n != 0 {
				t.Fatalf("pending after flush %d", n)
			}
			vps := "VPS-Z"
			item.Location = &vps
			tagIdentityRun(t, s, []localstore.SyncDataItem{item}, []localstore.Kanban{k})
			got := movementLocLocation(t, s)
			t.Logf("all synced; next sync location=%q", got)
			if got != vps {
				t.Fatal("did not take VPS location")
			}
		})
	}
}
func TestMovementLocationPartialFlushMustNotRestoreOlderPending(t *testing.T) {
	s, db := tagIdentityStore(t)
	k := localstore.Kanban{Code: "OLD-A", Zones: []string{"C"}}
	movementLocOK(t, s.AssignKanban("A", k))
	for i, action := range []string{"entrada", "salida"} {
		c, e := s.CreateConfirmation("A", action, "ant", time.Now().Add(time.Duration(i)*time.Second), 0)
		movementLocOK(t, e)
		movementLocOK(t, s.ApplyMovementLocation("A", action))
		if i == 0 {
			_, e = db.Exec(`UPDATE pending_confirmations SET created_at=datetime('now','-1 minute') WHERE id=?`, c.ID)
			movementLocOK(t, e)
		}
	}
	worker := NewToolsSync(&mockVPSClient{sendConfirmV2Func: func(_, _, action, _ string) error {
		if action == "entrada" {
			return errors.New("temporary failure")
		}
		return nil
	}}, s, "test", time.Hour, time.Second)
	worker.performFlush()
	pending, e := s.GetUnsyncedConfirmations(10)
	movementLocOK(t, e)
	if len(pending) != 1 || pending[0].Action != "entrada" {
		t.Fatalf("unexpected pending=%+v", pending)
	}
	stale := "C"
	item := tagIdentityItem(1, "A", &k)
	item.Location = &stale
	tagIdentityRun(t, s, []localstore.SyncDataItem{item}, []localstore.Kanban{k})
	got := movementLocLocation(t, s)
	t.Logf("newer salida synced; old entrada pending retry=%d; after sync=%q", pending[0].RetryCount, got)
	if got != "C" {
		t.Fatalf("S13 rollback: newer accepted salida lost to old pending entrada: %q", got)
	}
}
