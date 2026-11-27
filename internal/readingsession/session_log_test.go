package readingsession

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

type sliceStore struct{ got []models.Reading }

func (s *sliceStore) Store(r models.Reading) error { s.got = append(s.got, r); return nil }

// A closed session is logged once at info with its tag, antenna and read
// count, instead of one line per raw read.
func TestFlush_LogsOneSummaryPerClosedSession(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	store := &sliceStore{}
	a := New(store, time.Second, time.Minute)
	now := time.Now()
	for i := 0; i < 3; i++ {
		if err := a.Store(models.Reading{GatewayID: "gw", AntennaID: "entrada", EPC: "EPC-1", RSSI: -50 - i, Timestamp: now}); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}
	if err := a.FlushAll(); err != nil {
		t.Fatalf("FlushAll: %v", err)
	}

	if len(store.got) != 1 {
		t.Fatalf("queued %d sessions, want 1", len(store.got))
	}
	lines := strings.Count(buf.String(), "[Sessions] closed")
	if lines != 1 || !strings.Contains(buf.String(), "EPC=EPC-1") || !strings.Contains(buf.String(), "antenna=entrada") || !strings.Contains(buf.String(), "reads=3") {
		t.Errorf("log = %q, want one summary with EPC=EPC-1 antenna=entrada reads=3", buf.String())
	}
}
