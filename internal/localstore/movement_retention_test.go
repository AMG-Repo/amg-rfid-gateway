package localstore

import (
	"testing"
	"time"
)

func s13OK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestMovementRetentionLatestOverallMatrix(t *testing.T) {
	s := setupKanbanStore(t)
	// Interleave several tags; deliberately reverse event timestamps to verify MAX(id).
	for i, u := range []string{"A", "B", "C", "D", "E", "F"} {
		seedToolTag(t, s, int64(i+1), u)
		s13OK(t, s.AssignKanban(u, fordKanban()))
	}
	add := func(u, a string, synced bool, age time.Duration) {
		t.Helper()
		if synced {
			s13OK(t, s.RecordSyncedConfirmation(u, a, "ant", time.Now().Add(age)))
			return
		}
		_, err := s.CreateConfirmation(u, a, "ant", time.Now().Add(age), 0)
		s13OK(t, err)
	}
	add("A", "entrada", false, 0)
	add("B", "entrada", false, 0)
	add("C", "salida", true, 0)
	add("D", "salida", false, 0)
	add("E", "entrada", true, 0)
	add("A", "salida", false, -time.Hour)  // latest pending wins, despite older timestamp
	add("B", "salida", true, -time.Hour)   // latest synced wins over older pending
	add("C", "entrada", false, -time.Hour) // pending after synced wins
	add("D", "entrada", true, -time.Hour)  // latest synced wins over older pending
	_, err := s.db.Exec(`UPDATE tool_tags SET location='VPS'`)
	s13OK(t, err)
	s13OK(t, s.ReapplyPendingMovements())
	for u, want := range map[string]string{"A": "C", "B": "VPS", "C": WarehouseLocation, "D": "VPS", "E": "VPS", "F": "VPS"} {
		if got := tagLocation(t, s, u); got != want {
			t.Errorf("%s location=%q want=%q", u, got, want)
		} else {
			t.Logf("%s location=%q", u, got)
		}
	}
}

func TestMovementRetentionRetentionMustNotResurrectSupersededPending(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sentAge time.Duration
		other   bool
		removed int64
	}{
		{"old_latest_for_A_but_newer_synced_B", -35 * 24 * time.Hour, true, 0}, // kept: newer than A's pending entrada
		{"global_latest_guard_control", -35 * 24 * time.Hour, false, 0},
		{"within_retention_control", -29 * 24 * time.Hour, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := setupKanbanStore(t)
			seedToolTag(t, s, 1, "A")
			s13OK(t, s.AssignKanban("A", fordKanban()))
			now := time.Now().UTC()
			c, err := s.CreateConfirmation("A", "entrada", "ant", now.Add(-40*24*time.Hour), 0)
			s13OK(t, err)
			_, err = s.db.Exec(`UPDATE pending_confirmations SET created_at=?,retry_count=10 WHERE id=?`, now.Add(-40*24*time.Hour).Format(sqliteTimestamp), c.ID)
			s13OK(t, err)
			s13OK(t, s.RecordSyncedConfirmation("A", "salida", "ant", now.Add(tc.sentAge)))
			if tc.other {
				s13OK(t, s.RecordSyncedConfirmation("B", "entrada", "ant", now))
			}
			_, err = s.db.Exec(`UPDATE tool_tags SET location='C' WHERE uii='A'`)
			s13OK(t, err)
			s13OK(t, s.ReapplyPendingMovements())
			if got := tagLocation(t, s, "A"); got != "C" {
				t.Fatalf("before retention=%q want C", got)
			}
			removed, err := s.PurgeSyncedConfirmations(now.Add(-ConfirmationRetention))
			s13OK(t, err)
			if removed != tc.removed {
				t.Fatalf("purged=%d want=%d", removed, tc.removed)
			}
			n, err := s.GetPendingConfirmationsCount()
			s13OK(t, err)
			if n != 1 {
				t.Fatalf("pending=%d want=1", n)
			}
			s13OK(t, s.ReapplyPendingMovements())
			got := tagLocation(t, s, "A")
			t.Logf("purged=%d old pending retained=%d before=C after=%q", removed, n, got)
			if got != "C" {
				t.Errorf("S13 rollback: purging latest synced A resurrects superseded stuck entrada; got %q want C", got)
			}
		})
	}
}
