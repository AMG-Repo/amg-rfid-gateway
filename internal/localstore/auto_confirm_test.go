package localstore

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
)

func TestAutoConfirmEnabled_DefaultsToManual(t *testing.T) {
	store, _ := newProductionStore(t)
	enabled, err := store.AutoConfirmEnabled()
	if err != nil || enabled {
		t.Errorf("AutoConfirmEnabled() = %v, %v; want false (manual), nil", enabled, err)
	}
}

func TestAutoConfirmEnabled_SurvivesReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	open := func() (*LocalStore, *cache.SQLite) {
		c, err := cache.NewSQLite(dbPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return New(c.GetDB()), c
	}

	store, c := open()
	if err := store.SetAutoConfirmEnabled(true); err != nil {
		t.Fatalf("set: %v", err)
	}
	c.Close()

	store, c = open()
	defer c.Close()
	if enabled, err := store.AutoConfirmEnabled(); err != nil || !enabled {
		t.Errorf("after reopen AutoConfirmEnabled() = %v, %v; want true", enabled, err)
	}
	if err := store.SetAutoConfirmEnabled(false); err != nil {
		t.Fatalf("set back: %v", err)
	}
	if enabled, _ := store.AutoConfirmEnabled(); enabled {
		t.Errorf("after switching back AutoConfirmEnabled() = true; want false")
	}
}

func TestAutoFlag_StoredAndListedInHistory(t *testing.T) {
	store, _ := newProductionStore(t)
	at := time.Now().UTC().Add(-time.Minute)

	if _, err := store.CreateConfirmationWithIDAuto("EPC-Q", "entrada", "ant-1", "id-q", at, 0, true); err != nil {
		t.Fatalf("queue auto: %v", err)
	}
	if err := store.RecordSyncedConfirmationWithIDAuto("EPC-S", "salida", "ant-1", "id-s", at.Add(time.Second), true); err != nil {
		t.Fatalf("record auto: %v", err)
	}
	if err := store.RecordSyncedConfirmationWithID("EPC-M", "entrada", "manual", "id-m", at.Add(2*time.Second)); err != nil {
		t.Fatalf("record manual: %v", err)
	}

	entries, err := store.ListConfirmationsBetween(at.Add(-time.Hour), at.Add(time.Hour))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.UII] = e.Auto
	}
	want := map[string]bool{"EPC-Q": true, "EPC-S": true, "EPC-M": false}
	for uii, auto := range want {
		if a, ok := got[uii]; !ok || a != auto {
			t.Errorf("%s auto = %v (listed %v), want %v", uii, a, ok, auto)
		}
	}
}

func TestHasPendingConfirmation(t *testing.T) {
	store, _ := newProductionStore(t)
	now := time.Now()
	if pending, err := store.HasPendingConfirmation("E1"); err != nil || pending {
		t.Fatalf("empty queue: %v, %v; want false, nil", pending, err)
	}
	if err := store.RecordSyncedConfirmationWithIDAuto("E1", "salida", "ant-1", "id-1", now, false); err != nil {
		t.Fatalf("record synced: %v", err)
	}
	if pending, err := store.HasPendingConfirmation("E1"); err != nil || pending {
		t.Errorf("synced only: %v, %v; want false, nil", pending, err)
	}
	c, err := store.CreateConfirmationWithIDAuto("E1", "salida", "ant-1", "id-2", now, 0, false)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if pending, err := store.HasPendingConfirmation("E1"); err != nil || !pending {
		t.Errorf("queued: %v, %v; want true, nil", pending, err)
	}
	if pending, _ := store.HasPendingConfirmation("E2"); pending {
		t.Error("another tag reported pending")
	}
	if err := store.MarkConfirmationSynced(c.ID); err != nil {
		t.Fatalf("mark synced: %v", err)
	}
	if pending, err := store.HasPendingConfirmation("E1"); err != nil || pending {
		t.Errorf("after sync: %v, %v; want false, nil", pending, err)
	}
}

func TestHasConfirmationSince(t *testing.T) {
	store, _ := newProductionStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	since := now.Add(-10 * time.Minute)
	if found, err := store.HasConfirmationSince("E1", since); err != nil || found {
		t.Fatalf("no rows: %v, %v; want false, nil", found, err)
	}
	// Old confirmations of the tag and a recent one of another tag.
	if err := store.RecordSyncedConfirmationWithIDAuto("E1", "salida", "manual", "old", now.Add(-11*time.Minute), false); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSyncedConfirmation("E1", "entrada", "manual", now.Add(-20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateConfirmationWithIDAuto("E2", "salida", "manual", "other", now, 0, false); err != nil {
		t.Fatal(err)
	}
	if found, err := store.HasConfirmationSince("E1", since); err != nil || found {
		t.Fatalf("only old rows: %v, %v; want false, nil", found, err)
	}

	tests := []struct {
		name   string
		record func(uii string) error
	}{
		{"synced manual", func(uii string) error {
			return store.RecordSyncedConfirmationWithIDAuto(uii, "salida", "manual", uii, now.Add(-3*time.Minute), false)
		}},
		{"pending auto", func(uii string) error {
			_, err := store.CreateConfirmationWithIDAuto(uii, "salida", "ant-1", uii, now.Add(-3*time.Minute), 0, true)
			return err
		}},
		{"legacy without occurred_at", func(uii string) error {
			return store.RecordSyncedConfirmation(uii, "salida", "manual", now.Add(-3*time.Minute))
		}},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uii := "R" + string(rune('0'+i))
			if err := tc.record(uii); err != nil {
				t.Fatalf("record: %v", err)
			}
			if found, err := store.HasConfirmationSince(uii, since); err != nil || !found {
				t.Errorf("recent: %v, %v; want true, nil", found, err)
			}
			if found, _ := store.HasConfirmationSince(uii, now.Add(-time.Minute)); found {
				t.Error("found a confirmation older than since")
			}
		})
	}
}

func TestLastConfirmationAction(t *testing.T) {
	store, _ := newProductionStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	if _, ok, err := store.LastConfirmationAction("E1"); err != nil || ok {
		t.Fatalf("no rows: %v, %v; want false, nil", ok, err)
	}
	// Inserted out of order: the latest occurred time wins, whatever its state.
	if _, err := store.CreateConfirmationWithIDAuto("E1", "salida", "ant-1", "p", now.Add(-time.Minute), 0, true); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSyncedConfirmationWithIDAuto("E1", "entrada", "manual", "s", now.Add(-time.Hour), false); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSyncedConfirmation("E1", "entrada", "manual", now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSyncedConfirmation("E2", "entrada", "manual", now); err != nil {
		t.Fatal(err)
	}
	if a, ok, err := store.LastConfirmationAction("E1"); err != nil || !ok || a != "salida" {
		t.Errorf("E1 = %q, %v, %v; want salida", a, ok, err)
	}
	// Legacy row without occurred_at: ordered by its timestamp.
	if err := store.RecordSyncedConfirmation("E1", "entrada", "manual", now); err != nil {
		t.Fatal(err)
	}
	if a, ok, err := store.LastConfirmationAction("E1"); err != nil || !ok || a != "entrada" {
		t.Errorf("E1 after legacy = %q, %v, %v; want entrada", a, ok, err)
	}
}
