package sync

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// With a real cache, a batch with one rejected index marks exactly that
// reading rejected and the rest synced.
func TestEngine_SyncOnce_MarksRejectedReadingsApart(t *testing.T) {
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "engine.db"))
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	for i := 0; i < 3; i++ {
		if err := c.Store(models.Reading{AntennaID: "ant-1", EPC: fmt.Sprintf("EPC-REJ-%d", i), RSSI: -55, Timestamp: time.Now()}); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}
	if err := c.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	batch, err := c.GetUnsynced(10)
	if err != nil || len(batch) != 3 {
		t.Fatalf("expected 3 pending readings, got %d (err %v)", len(batch), err)
	}

	sender := &senderOnly{resp: &models.SyncResponse{Success: true, Accepted: 2, Errors: []string{"reading[1]: epc is required", "reading[1]: duplicate message"}}}
	engine := NewEngine(c, sender, EngineConfig{GatewayID: "gw-001", CompanyID: "comp-123", BatchSize: 100})
	if err := engine.syncOnce(context.Background()); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}

	for i, r := range batch {
		var synced int
		if err := c.GetDB().QueryRow(`SELECT synced FROM pending_readings WHERE id = ?`, r.ID).Scan(&synced); err != nil {
			t.Fatalf("lookup %d: %v", r.ID, err)
		}
		want := 1
		if i == 1 {
			want = 2
		}
		if synced != want {
			t.Errorf("reading[%d] synced = %d, want %d", i, synced, want)
		}
	}
	if n, _ := c.Count(); n != 0 {
		t.Errorf("Count = %d, want 0", n)
	}
	stats := engine.GetStats()
	if stats.TotalSynced != 2 || stats.TotalFailed != 1 {
		t.Errorf("TotalSynced=%d TotalFailed=%d, want 2 and 1", stats.TotalSynced, stats.TotalFailed)
	}
}
