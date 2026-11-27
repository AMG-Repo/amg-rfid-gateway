package sync

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// poisonSender answers status to any request that holds a reading whose EPC
// starts with "BAD" and accepts every other request whole.
type poisonSender struct {
	status int
	sizes  []int
}

func (s *poisonSender) SendSyncRequest(req models.SyncRequest) (*models.SyncResponse, error) {
	s.sizes = append(s.sizes, len(req.Readings))
	for _, r := range req.Readings {
		if strings.HasPrefix(r.EPC, "BAD") {
			return nil, statusErr(s.status)
		}
	}
	return &models.SyncResponse{Success: true, Accepted: len(req.Readings)}, nil
}

func newPoisonCache(t *testing.T, epcs ...string) *cache.SQLite {
	t.Helper()
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "engine.db"))
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	for _, epc := range epcs {
		if err := c.Store(models.Reading{AntennaID: "ant-1", EPC: epc, RSSI: -55, Timestamp: time.Now()}); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}
	if err := c.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	return c
}

func syncedByEPC(t *testing.T, c *cache.SQLite) map[string]int {
	t.Helper()
	rows, err := c.GetDB().Query(`SELECT epc, synced FROM pending_readings`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var epc string
		var synced int
		if err := rows.Scan(&epc, &synced); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[epc] = synced
	}
	return out
}

// A batch the VPS rejects whole with 400 or 413 is split until the reading
// that causes it is alone; that reading is set aside as rejected and the rest
// of the queue is delivered instead of retrying the same batch forever.
func TestEngine_SyncOnce_BisectsBatchRejectedWhole(t *testing.T) {
	for _, status := range []int{400, 413} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := newPoisonCache(t, "EPC-0", "EPC-1", "BAD-2", "EPC-3", "EPC-4")
			sender := &poisonSender{status: status}
			engine := NewEngine(c, sender, EngineConfig{GatewayID: "gw-001", CompanyID: "comp-123", BatchSize: 100})

			for i := 0; i < 10; i++ {
				_ = engine.syncOnce(context.Background())
			}

			got := syncedByEPC(t, c)
			for epc, want := range map[string]int{"EPC-0": 1, "EPC-1": 1, "BAD-2": 2, "EPC-3": 1, "EPC-4": 1} {
				if got[epc] != want {
					t.Errorf("%s synced = %d, want %d (all %v)", epc, got[epc], want, got)
				}
			}
			if n, _ := c.Count(); n != 0 {
				t.Errorf("Count = %d, want 0", n)
			}
			if sender.sizes[0] != 5 || sender.sizes[1] != 2 {
				t.Errorf("request sizes = %v, want 5 then 2", sender.sizes)
			}
			stats := engine.GetStats()
			if stats.TotalSynced != 4 {
				t.Errorf("TotalSynced = %d, want 4", stats.TotalSynced)
			}
		})
	}
}

// Statuses that say nothing about the readings never set a reading aside and
// never shrink the batch: the same readings stay pending for a later retry.
func TestEngine_SyncOnce_TransientFailureKeepsReadingsPending(t *testing.T) {
	for _, err := range []error{statusErr(401), statusErr(403), statusErr(404), statusErr(408), statusErr(429), statusErr(500), statusErr(503), errors.New("dial tcp: connection refused")} {
		t.Run(err.Error(), func(t *testing.T) {
			c := newPoisonCache(t, "EPC-0", "EPC-1")
			sender := &senderOnly{err: err}
			engine := NewEngine(c, sender, EngineConfig{GatewayID: "gw-001", CompanyID: "comp-123", BatchSize: 100})

			for i := 0; i < 3; i++ {
				if e := engine.syncOnce(context.Background()); e == nil {
					t.Fatalf("syncOnce returned nil for %v", err)
				}
			}

			for epc, synced := range syncedByEPC(t, c) {
				if synced != 0 {
					t.Errorf("%s synced = %d, want 0", epc, synced)
				}
			}
			for i, req := range sender.sent {
				if len(req.Readings) != 2 {
					t.Errorf("request %d sent %d readings, want 2", i, len(req.Readings))
				}
			}
		})
	}
}
