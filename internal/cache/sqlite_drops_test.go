package cache

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

func newDropsTestCache(t *testing.T) *SQLite {
	t.Helper()
	c, err := NewSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func dropsTestReading(i int) models.Reading {
	return models.Reading{
		AntennaID: "ant-1",
		GatewayID: "gw-1",
		EPC:       fmt.Sprintf("E2000000%08d", i),
		RSSI:      -50,
		Timestamp: time.Now(),
	}
}

// failInserts makes every insert into pending_readings fail until the
// returned restore func runs.
func failInserts(t *testing.T, c *SQLite, when string) func() {
	t.Helper()
	if _, err := c.GetDB().Exec(`CREATE TRIGGER fail_insert BEFORE INSERT ON pending_readings
		WHEN ` + when + ` BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	return func() {
		if _, err := c.GetDB().Exec(`DROP TRIGGER fail_insert`); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	}
}

func TestSQLite_Store_BufferFullIncrementsRejections(t *testing.T) {
	c := newDropsTestCache(t)

	// Hold the write lock so the writer blocks inside its first flush.
	lock, err := c.GetDB().Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := lock.Exec(`INSERT INTO settings (key, value) VALUES ('lock', 'x')`); err != nil {
		lock.Rollback()
		t.Fatalf("take write lock: %v", err)
	}

	rejected := 0
	for i := 0; i < BufferSize+110; i++ {
		if err := c.Store(dropsTestReading(i)); err != nil {
			if !strings.Contains(err.Error(), "buffer full") {
				t.Fatalf("unexpected error: %v", err)
			}
			rejected++
		}
	}
	lock.Rollback()

	if rejected == 0 {
		t.Fatal("expected some readings to be rejected with a full buffer")
	}
	if got := c.BufferFullRejections(); got != int64(rejected) {
		t.Errorf("BufferFullRejections = %d, want %d", got, rejected)
	}
}

func TestSQLite_TransientWriteFailureKeepsBatch(t *testing.T) {
	c := newDropsTestCache(t)
	restore := failInserts(t, c, "1")

	const n = 20
	for i := 0; i < n; i++ {
		if err := c.Store(dropsTestReading(i)); err != nil {
			t.Fatalf("store: %v", err)
		}
	}
	c.Flush()
	if got, _ := c.Count(); got != 0 {
		t.Fatalf("Count during failure = %d, want 0", got)
	}

	restore()
	if err := c.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got, _ := c.Count(); got != n {
		t.Errorf("Count after recovery = %d, want %d (no loss, no duplicates)", got, n)
	}
	if got := c.DroppedReadings(); got != 0 {
		t.Errorf("DroppedReadings = %d, want 0", got)
	}
}

func TestSQLite_FlushOnce_AllOrNothing(t *testing.T) {
	c := newDropsTestCache(t)
	restore := failInserts(t, c, "NEW.epc = 'BAD'")
	defer restore()

	batch := []models.Reading{dropsTestReading(1), dropsTestReading(2), dropsTestReading(3)}
	batch[1].EPC = "BAD"
	if err := c.flushOnce(batch); err == nil {
		t.Fatal("expected flushOnce to fail")
	}
	if got, _ := c.Count(); got != 0 {
		t.Errorf("Count = %d, want 0 (failed batch must not commit partially)", got)
	}
}

func TestSQLite_CarryCapDropsOldest(t *testing.T) {
	c := newDropsTestCache(t)
	c.maxCarried = 10
	restore := failInserts(t, c, "1")

	const n = 15
	for i := 0; i < n; i++ {
		if err := c.Store(dropsTestReading(i)); err != nil {
			t.Fatalf("store: %v", err)
		}
	}
	c.Flush()
	if got := c.DroppedReadings(); got != n-10 {
		t.Fatalf("DroppedReadings = %d, want %d", got, n-10)
	}

	restore()
	c.Flush()
	pending, err := c.GetUnsynced(100)
	if err != nil {
		t.Fatalf("GetUnsynced: %v", err)
	}
	if len(pending) != 10 {
		t.Fatalf("pending = %d, want 10", len(pending))
	}
	for _, p := range pending {
		if p.EPC == dropsTestReading(0).EPC || p.EPC == dropsTestReading(4).EPC {
			t.Errorf("oldest reading %s should have been dropped", p.EPC)
		}
	}
}

func TestSQLite_CloseCountsUnwrittenReadings(t *testing.T) {
	c, err := NewSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	failInserts(t, c, "1")
	for i := 0; i < 5; i++ {
		c.Store(dropsTestReading(i))
	}
	c.Close()
	if got := c.DroppedReadings(); got != 5 {
		t.Errorf("DroppedReadings after failed close = %d, want 5", got)
	}
}
