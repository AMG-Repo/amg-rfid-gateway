package sync

import (
	stdsync "sync"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// purgingCache adds the optional synced-reading purge to the base mock.
type purgingCache struct {
	mockCache
	pmu     stdsync.Mutex
	cutoffs []time.Time
}

func (m *purgingCache) PurgeSyncedReadings(cutoff time.Time) (int64, error) {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	m.cutoffs = append(m.cutoffs, cutoff)
	return 0, nil
}

func (m *purgingCache) purgeCutoffs() []time.Time {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	return append([]time.Time(nil), m.cutoffs...)
}

// The sync loop purges synced readings older than the retention, even when
// nothing is pending, and does not repeat the purge on every tick.
func TestEngine_PurgesSyncedReadingsOncePerInterval(t *testing.T) {
	c := &purgingCache{}
	client := &mockWSClient{connected: true, syncResponse: &models.SyncResponse{}}
	engine := NewEngine(c, client, EngineConfig{
		GatewayID:    "gw-001",
		CompanyID:    "comp-123",
		SyncInterval: 20 * time.Millisecond,
		BatchSize:    100,
		MaxRetries:   5,
	})
	engine.Start()
	t.Cleanup(engine.Stop)

	waitFor(t, "first reading purge", func() bool { return len(c.purgeCutoffs()) >= 1 })
	time.Sleep(150 * time.Millisecond) // several more ticks
	cutoffs := c.purgeCutoffs()
	if len(cutoffs) != 1 {
		t.Fatalf("expected exactly one purge within the interval, got %d", len(cutoffs))
	}
	want := time.Now().Add(-cache.ReadingRetention)
	if d := cutoffs[0].Sub(want); d < -time.Minute || d > time.Minute {
		t.Errorf("purge cutoff %v is not now minus the retention (%v)", cutoffs[0], want)
	}
}
