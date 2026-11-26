package localstore

import (
	"testing"
	"time"
)

const testConfirmationID = "3f2b8c1e-9a4d-4e6f-8b1a-2c3d4e5f6a7b"

func TestCreateConfirmationWithID_QueuesIDAndOccurredAt(t *testing.T) {
	store, _ := newProductionStore(t)
	occurredAt := time.Date(2026, 11, 26, 18, 30, 1, 123456789, time.UTC)

	if _, err := store.CreateConfirmationWithID("EPC-1", "entrada", "ant-1", testConfirmationID, occurredAt, 0); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	list, err := store.GetUnsyncedConfirmations(10)
	if err != nil || len(list) != 1 {
		t.Fatalf("queue = %+v (err %v), want 1 row", list, err)
	}
	if list[0].ConfirmationID != testConfirmationID || !list[0].OccurredAt.Equal(occurredAt) {
		t.Errorf("queued id/occurred_at = %q/%v, want %q/%v", list[0].ConfirmationID, list[0].OccurredAt, testConfirmationID, occurredAt)
	}
}

// Callers that do not pass an id still queue one, so retries are idempotent.
func TestCreateConfirmation_AssignsConfirmationID(t *testing.T) {
	store, _ := newProductionStore(t)
	if _, err := store.CreateConfirmation("EPC-1", "entrada", "ant-1", time.Now(), 0); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	list, _ := store.GetUnsyncedConfirmations(10)
	if len(list) != 1 || len(list[0].ConfirmationID) != 36 {
		t.Fatalf("queue = %+v, want one row with a UUID confirmation_id", list)
	}
}

// A row queued before the upgrade has no occurred_at: it reads back as zero.
func TestGetRetryableConfirmations_LegacyRowHasZeroOccurredAt(t *testing.T) {
	store, c := newProductionStore(t)
	if _, err := c.GetDB().Exec(`INSERT INTO pending_confirmations (uii, action, antenna_id, timestamp, confirmation_id) VALUES ('EPC-OLD', 'entrada', 'ant-1', ?, ?)`, time.Now(), testConfirmationID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	list, err := store.GetRetryableConfirmations(10, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("queue = %+v (err %v), want 1 row", list, err)
	}
	if !list[0].OccurredAt.IsZero() || list[0].ConfirmationID != testConfirmationID {
		t.Errorf("legacy row = %+v, want zero OccurredAt and the stored id", list[0])
	}
}

func TestRecordSyncedConfirmationWithID_StoresIDAndOccurredAt(t *testing.T) {
	store, c := newProductionStore(t)
	occurredAt := time.Date(2026, 11, 26, 18, 30, 1, 5, time.UTC)
	if err := store.RecordSyncedConfirmationWithID("EPC-1", "entrada", "ant-1", testConfirmationID, occurredAt); err != nil {
		t.Fatalf("record failed: %v", err)
	}
	var id, occurred string
	if err := c.GetDB().QueryRow(`SELECT confirmation_id, occurred_at FROM pending_confirmations WHERE uii = 'EPC-1'`).Scan(&id, &occurred); err != nil {
		t.Fatalf("read: %v", err)
	}
	if id != testConfirmationID || occurred != occurredAt.Format(time.RFC3339Nano) {
		t.Errorf("stored %q/%q, want %q/%q", id, occurred, testConfirmationID, occurredAt.Format(time.RFC3339Nano))
	}
}
