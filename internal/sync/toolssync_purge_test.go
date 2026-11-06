package sync

import (
	"context"
	stdsync "sync"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

// purgingLocalStore adds the optional synced-confirmation purge to the base mock.
type purgingLocalStore struct {
	mockLocalStore
	pmu     stdsync.Mutex
	cutoffs []time.Time
}

func (m *purgingLocalStore) PurgeSyncedConfirmations(cutoff time.Time) (int64, error) {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	m.cutoffs = append(m.cutoffs, cutoff)
	return 0, nil
}

func (m *purgingLocalStore) purgeCutoffs() []time.Time {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	return append([]time.Time(nil), m.cutoffs...)
}

func startFlushLoop(t *testing.T, store LocalStore, retry time.Duration) {
	t.Helper()
	ts := NewToolsSync(&mockVPSClient{}, store, "company123", 1*time.Hour, retry)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := ts.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(ts.Stop)
}

// The flush loop purges synced confirmations older than the retention, even
// when nothing is pending, and does not repeat the purge on every tick.
func TestToolsSync_FlushLoopPurgesSyncedConfirmationsOncePerInterval(t *testing.T) {
	store := &purgingLocalStore{mockLocalStore: mockLocalStore{
		getUnsyncedFunc: func(int) ([]localstore.PendingConfirmation, error) { return nil, nil },
	}}

	startFlushLoop(t, store, 20*time.Millisecond)

	waitFor(t, "first purge", func() bool { return len(store.purgeCutoffs()) >= 1 })
	time.Sleep(150 * time.Millisecond) // several more ticks
	cutoffs := store.purgeCutoffs()
	if len(cutoffs) != 1 {
		t.Fatalf("expected exactly one purge within the interval, got %d", len(cutoffs))
	}
	want := time.Now().Add(-localstore.ConfirmationRetention)
	if d := cutoffs[0].Sub(want); d < -time.Minute || d > time.Minute {
		t.Errorf("purge cutoff %v is not now minus the retention (%v)", cutoffs[0], want)
	}
}

// Stores without the optional purge keep working unchanged.
func TestToolsSync_FlushLoopWithoutPurgeSupportStillFlushes(t *testing.T) {
	marked := make(chan int64, 1)
	store := &mockLocalStore{
		getUnsyncedFunc: func(int) ([]localstore.PendingConfirmation, error) {
			return []localstore.PendingConfirmation{{ID: 7, UII: "EPC-7", Action: "entrada", AntennaID: "a"}}, nil
		},
		markSyncedFunc: func(id int64) error {
			select {
			case marked <- id:
			default:
			}
			return nil
		},
	}

	startFlushLoop(t, store, 20*time.Millisecond)

	select {
	case id := <-marked:
		if id != 7 {
			t.Errorf("marked %d, want 7", id)
		}
	case <-time.After(time.Second):
		t.Fatal("pending confirmation was not flushed")
	}
}
