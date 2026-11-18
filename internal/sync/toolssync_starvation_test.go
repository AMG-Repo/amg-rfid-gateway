package sync

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

// Stuck confirmations (max retries reached) at the front of the queue must not
// starve newer ones: a flush still sends the retryable confirmation.
func TestToolsSync_FlushSendsNewConfirmationBehindStuckOnes(t *testing.T) {
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	store := localstore.New(c.GetDB())

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 150; i++ {
		conf, err := store.CreateConfirmation("STUCK", "entrada", "ant-1", base, 0)
		if err != nil {
			t.Fatalf("seed stuck: %v", err)
		}
		if _, err := c.GetDB().Exec(`UPDATE pending_confirmations SET retry_count = ?, created_at = datetime('now', '-1 hour') WHERE id = ?`, MaxRetries, conf.ID); err != nil {
			t.Fatalf("mark stuck: %v", err)
		}
	}
	fresh, err := store.CreateConfirmation("FRESH-EPC", "salida", "ant-1", time.Now(), 0)
	if err != nil {
		t.Fatalf("seed fresh: %v", err)
	}

	var sent []string
	vps := &mockVPSClient{sendConfirmV2Func: func(_, uii, _, _ string) error {
		sent = append(sent, uii)
		return nil
	}}
	ts := NewToolsSync(vps, store, "company123", time.Hour, time.Hour)
	ts.performFlush()

	if len(sent) != 1 || sent[0] != "FRESH-EPC" {
		t.Fatalf("sent = %v, want only FRESH-EPC despite 150 stuck confirmations ahead of it", sent)
	}
	pending, err := store.GetUnsyncedConfirmations(1000)
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	for _, p := range pending {
		if p.ID == fresh.ID {
			t.Errorf("FRESH-EPC must be marked synced after a successful send")
		}
	}
	if len(pending) != 150 {
		t.Errorf("stuck confirmations must stay queued, got %d pending", len(pending))
	}
}
