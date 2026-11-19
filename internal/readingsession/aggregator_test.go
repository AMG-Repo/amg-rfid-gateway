package readingsession

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

type recordingStore struct {
	mu     sync.Mutex
	stored []models.Reading
	err    error
}

func (s *recordingStore) Store(r models.Reading) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.stored = append(s.stored, r)
	return nil
}

func (s *recordingStore) all() []models.Reading {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.Reading(nil), s.stored...)
}

var t0 = time.Date(2026, 11, 19, 19, 0, 0, 0, time.UTC)

func read(epc, antenna string, at time.Duration, rssi int) models.Reading {
	return models.Reading{AntennaID: antenna, GatewayID: "gw", EPC: epc, RSSI: rssi, Timestamp: t0.Add(at)}
}

func mustStore(t *testing.T, a *Aggregator, r models.Reading) {
	t.Helper()
	if err := a.Store(r); err != nil {
		t.Fatalf("Store(%+v): %v", r, err)
	}
}

func TestAggregator_MergesReadsOfOneTagIntoOneSession(t *testing.T) {
	next := &recordingStore{}
	a := New(next, 5*time.Second, 60*time.Second)

	mustStore(t, a, read("E1", "almacen", 0, 60))
	mustStore(t, a, read("E1", "almacen", 1*time.Second, 72))
	mustStore(t, a, read("E1", "almacen", 2*time.Second, 65))

	if got := next.all(); len(got) != 0 {
		t.Fatalf("stored %d readings while the tag is still being read; want 0", len(got))
	}
	if err := a.FlushIdle(t0.Add(7 * time.Second)); err != nil {
		t.Fatalf("FlushIdle: %v", err)
	}
	got := next.all()
	if len(got) != 1 {
		t.Fatalf("stored %d readings; want 1 session", len(got))
	}
	s := got[0]
	if !s.Timestamp.Equal(t0) || !s.LastSeen.Equal(t0.Add(2*time.Second)) || s.ReadCount != 3 || s.RSSI != 72 {
		t.Errorf("session = first %v last %v count %d rssi %d; want first t0, last t0+2s, count 3, rssi 72",
			s.Timestamp, s.LastSeen, s.ReadCount, s.RSSI)
	}
	if s.EPC != "E1" || s.AntennaID != "almacen" || s.GatewayID != "gw" {
		t.Errorf("session identity = %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Errorf("session fails Validate: %v", err)
	}
}

func TestAggregator_KeepsSessionOpenUntilIdleElapses(t *testing.T) {
	next := &recordingStore{}
	a := New(next, 5*time.Second, 60*time.Second)
	mustStore(t, a, read("E1", "almacen", 0, 60))
	mustStore(t, a, read("E1", "almacen", 3*time.Second, 60))

	// 4.9 s after the last read: still open.
	if err := a.FlushIdle(t0.Add(7900 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if n := len(next.all()); n != 0 {
		t.Fatalf("stored %d before 5 s of silence; want 0", n)
	}
	// Exactly 5 s after the last read: closed.
	if err := a.FlushIdle(t0.Add(8 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if n := len(next.all()); n != 1 {
		t.Fatalf("stored %d after 5 s of silence; want 1", n)
	}
}

func TestAggregator_SeparatesTagsAndAntennas(t *testing.T) {
	next := &recordingStore{}
	a := New(next, 5*time.Second, 60*time.Second)
	mustStore(t, a, read("E1", "entrada", 0, 60))
	mustStore(t, a, read("E1", "salida", 0, 61))
	mustStore(t, a, read("E2", "entrada", 0, 62))
	mustStore(t, a, read("E1", "entrada", time.Second, 63))

	if err := a.FlushIdle(t0.Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	got := next.all()
	if len(got) != 3 {
		t.Fatalf("stored %d sessions; want 3 (E1@entrada, E1@salida, E2@entrada)", len(got))
	}
	counts := map[string]int{}
	for _, s := range got {
		counts[s.EPC+"@"+s.AntennaID] = s.ReadCount
	}
	if counts["E1@entrada"] != 2 || counts["E1@salida"] != 1 || counts["E2@entrada"] != 1 {
		t.Errorf("read counts = %v", counts)
	}
}

func TestAggregator_ClosesSessionAfterMaxAndStartsANewOne(t *testing.T) {
	next := &recordingStore{}
	a := New(next, 5*time.Second, 60*time.Second)
	// One read per second for 65 s: the tag never goes idle.
	for s := 0; s <= 65; s++ {
		mustStore(t, a, read("E1", "almacen", time.Duration(s)*time.Second, 60))
		if err := a.FlushIdle(t0.Add(time.Duration(s) * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	got := next.all()
	if len(got) != 1 {
		t.Fatalf("stored %d sessions during 65 s of continuous reads; want 1 (closed at 60 s)", len(got))
	}
	first := got[0]
	if !first.Timestamp.Equal(t0) || first.ReadCount != 60 || !first.LastSeen.Equal(t0.Add(59*time.Second)) {
		t.Errorf("first session = first %v last %v count %d; want t0, t0+59s, 60",
			first.Timestamp, first.LastSeen, first.ReadCount)
	}
	if err := a.FlushAll(); err != nil {
		t.Fatal(err)
	}
	got = next.all()
	if len(got) != 2 || !got[1].Timestamp.Equal(t0.Add(60*time.Second)) || got[1].ReadCount != 6 {
		t.Errorf("second session = %+v; want first t0+60s, count 6", got[len(got)-1])
	}
}

func TestAggregator_FlushIdleClosesSessionAtMaxEvenWithoutNewReads(t *testing.T) {
	next := &recordingStore{}
	a := New(next, 120*time.Second, 60*time.Second)
	mustStore(t, a, read("E1", "almacen", 0, 60))
	if err := a.FlushIdle(t0.Add(60 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if n := len(next.all()); n != 1 {
		t.Fatalf("stored %d at max age; want 1", n)
	}
}

func TestAggregator_FlushAllStoresOpenSessions(t *testing.T) {
	next := &recordingStore{}
	a := New(next, 5*time.Second, 60*time.Second)
	mustStore(t, a, read("E1", "almacen", 0, 60))
	mustStore(t, a, read("E2", "almacen", 0, 60))

	if err := a.FlushAll(); err != nil {
		t.Fatalf("FlushAll: %v", err)
	}
	if n := len(next.all()); n != 2 {
		t.Fatalf("stored %d on shutdown; want 2", n)
	}
	if err := a.FlushAll(); err != nil {
		t.Fatal(err)
	}
	if n := len(next.all()); n != 2 {
		t.Errorf("second FlushAll stored again: %d; want 2", n)
	}
}

func TestAggregator_RejectsInvalidReadingWithoutOpeningASession(t *testing.T) {
	next := &recordingStore{}
	a := New(next, 5*time.Second, 60*time.Second)
	if err := a.Store(models.Reading{AntennaID: "almacen", GatewayID: "gw", RSSI: 60, Timestamp: t0}); err == nil {
		t.Fatal("Store accepted a reading without EPC")
	}
	if err := a.FlushAll(); err != nil {
		t.Fatal(err)
	}
	if n := len(next.all()); n != 0 {
		t.Errorf("stored %d; want 0", n)
	}
}

func TestAggregator_KeepsSessionWhenTheQueueRejectsIt(t *testing.T) {
	next := &recordingStore{err: errors.New("cache is closed")}
	a := New(next, 5*time.Second, 60*time.Second)
	mustStore(t, a, read("E1", "almacen", 0, 60))

	if err := a.FlushIdle(t0.Add(10 * time.Second)); err == nil {
		t.Fatal("FlushIdle hid the queue error")
	}
	next.mu.Lock()
	next.err = nil
	next.mu.Unlock()
	if err := a.FlushIdle(t0.Add(11 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := next.all(); len(got) != 1 || got[0].ReadCount != 1 {
		t.Errorf("stored %+v after the queue recovered; want the kept session", got)
	}
}

func TestAggregator_RunFlushesIdleSessionsAndAllOnStop(t *testing.T) {
	next := &recordingStore{}
	a := New(next, 50*time.Millisecond, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx, 10*time.Millisecond)
		close(done)
	}()

	now := time.Now()
	mustStore(t, a, models.Reading{AntennaID: "almacen", GatewayID: "gw", EPC: "IDLE", RSSI: 60, Timestamp: now})
	deadline := time.Now().Add(2 * time.Second)
	for len(next.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(next.all()); n != 1 {
		t.Fatalf("Run stored %d idle sessions; want 1", n)
	}

	mustStore(t, a, models.Reading{AntennaID: "almacen", GatewayID: "gw", EPC: "OPEN", RSSI: 60, Timestamp: time.Now()})
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	got := next.all()
	if len(got) != 2 || got[1].EPC != "OPEN" {
		t.Errorf("after stop stored %+v; want the open session flushed", got)
	}
}
