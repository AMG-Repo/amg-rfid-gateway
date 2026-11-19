package readingsession

import (
	"path/filepath"
	"testing"
	"time"

	cachepkg "github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// The gateway shutdown sequence: queue the open sessions, flush the cache's
// write buffer, close it. After a restart the sessions must be in the queue.
func TestShutdownSequence_PersistsOpenSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	cache, err := cachepkg.NewSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cache, 5*time.Second, time.Minute)
	now := time.Now()
	for i := 0; i < 3; i++ {
		r := models.Reading{AntennaID: "almacen", GatewayID: "gw", EPC: "E1", RSSI: -62 + i, Timestamp: now.Add(time.Duration(i) * time.Millisecond)}
		if err := a.Store(r); err != nil {
			t.Fatal(err)
		}
	}

	if err := ShutdownQueue(a, cache); err != nil {
		t.Fatalf("ShutdownQueue: %v", err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := cachepkg.NewSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetUnsynced(10)
	if err != nil || len(got) != 1 {
		t.Fatalf("after restart GetUnsynced = %v, %v; want 1 session", got, err)
	}
	if got[0].ReadCount != 3 || got[0].RSSI != -60 {
		t.Errorf("session = count %d rssi %d; want 3, -60", got[0].ReadCount, got[0].RSSI)
	}
}
