package readingsession

import (
	"context"
	"errors"
	"strings"
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

	mustStore(t, a, read("E1", "almacen", 0, -60))
	mustStore(t, a, read("E1", "almacen", 1*time.Second, -52))
	mustStore(t, a, read("E1", "almacen", 2*time.Second, -65))

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
	if !s.Timestamp.Equal(t0) || !s.LastSeen.Equal(t0.Add(2*time.Second)) || s.ReadCount != 3 || s.RSSI != -52 {
		t.Errorf("session = first %v last %v count %d rssi %d; want first t0, last t0+2s, count 3, rssi -52 (strongest)",
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
	mustStore(t, a, read("E1", "almacen", 0, -60))
	mustStore(t, a, read("E1", "almacen", 3*time.Second, -60))

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
	mustStore(t, a, read("E1", "entrada", 0, -60))
	mustStore(t, a, read("E1", "salida", 0, -61))
	mustStore(t, a, read("E2", "entrada", 0, -62))
	mustStore(t, a, read("E1", "entrada", time.Second, -63))

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
		mustStore(t, a, read("E1", "almacen", time.Duration(s)*time.Second, -60))
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
	mustStore(t, a, read("E1", "almacen", 0, -60))
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
	mustStore(t, a, read("E1", "almacen", 0, -60))
	mustStore(t, a, read("E2", "almacen", 0, -60))

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
	if err := a.Store(models.Reading{AntennaID: "almacen", GatewayID: "gw", RSSI: -60, Timestamp: t0}); err == nil {
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
	mustStore(t, a, read("E1", "almacen", 0, -60))

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

func TestAggregator_RunFlushesIdleSessionsAndStopsOnCancel(t *testing.T) {
	q := &failingQueue{}
	a := New(q, 50*time.Millisecond, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx, 10*time.Millisecond)
		close(done)
	}()

	mustStore(t, a, models.Reading{AntennaID: "almacen", GatewayID: "gw", EPC: "IDLE", RSSI: -60, Timestamp: time.Now()})
	deadline := time.Now().Add(2 * time.Second)
	for len(q.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(q.all()); n != 1 {
		t.Fatalf("Run stored %d idle sessions; want 1", n)
	}

	mustStore(t, a, models.Reading{AntennaID: "almacen", GatewayID: "gw", EPC: "OPEN", RSSI: -60, Timestamp: time.Now()})
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	// Shutdown queues the still-open session through ShutdownQueue (main.go).
	if err := ShutdownQueue(a, q); err != nil {
		t.Fatal(err)
	}
	got := q.all()
	if len(got) != 2 || got[1].EPC != "OPEN" || !q.flushed {
		t.Errorf("after shutdown stored %+v (flushed %v); want the open session queued and the buffer flushed", got, q.flushed)
	}
}

func TestAggregator_StartsNewSessionWhenTheGapReachesIdle(t *testing.T) {
	next := &recordingStore{}
	a := New(next, 5*time.Second, 60*time.Second)
	mustStore(t, a, read("E1", "almacen", 0, -60))
	mustStore(t, a, read("E1", "almacen", time.Second, -60))
	// 5.1 s of silence, but no FlushIdle tick ran in between.
	mustStore(t, a, read("E1", "almacen", 6100*time.Millisecond, -60))

	if err := a.FlushAll(); err != nil {
		t.Fatal(err)
	}
	got := next.all()
	if len(got) != 2 {
		t.Fatalf("stored %d sessions; want 2 (the gap closes the first)", len(got))
	}
	if got[0].ReadCount != 2 || !got[0].LastSeen.Equal(t0.Add(time.Second)) || got[1].ReadCount != 1 {
		t.Errorf("sessions = %+v", got)
	}
}

func TestAggregator_StoreAcceptsReadsWhileTheQueueRejectsARolledOverSession(t *testing.T) {
	next := &recordingStore{err: errors.New("cache is closed")}
	a := New(next, 5*time.Second, 60*time.Second)
	mustStore(t, a, read("E1", "almacen", 0, -60))
	// At max age the old session rolls over; the queue is failing, but the new
	// raw read must still be accepted (handlers publish it only on success).
	if err := a.Store(read("E1", "almacen", 60*time.Second, -61)); err != nil {
		t.Fatalf("Store returned %v while rolling over; want nil", err)
	}

	next.mu.Lock()
	next.err = nil
	next.mu.Unlock()
	if err := a.FlushAll(); err != nil {
		t.Fatal(err)
	}
	got := next.all()
	if len(got) != 2 || !got[0].Timestamp.Equal(t0) || !got[1].Timestamp.Equal(t0.Add(60*time.Second)) {
		t.Errorf("stored %+v; want the rolled-over session and the new one", got)
	}
}

func TestAggregator_SessionsCarryWallClockTimesOnly(t *testing.T) {
	next := &recordingStore{}
	a := New(next, 5*time.Second, 60*time.Second)
	now := time.Now() // carries a monotonic reading
	mustStore(t, a, models.Reading{AntennaID: "almacen", GatewayID: "gw", EPC: "E1", RSSI: -60, Timestamp: now})
	mustStore(t, a, models.Reading{AntennaID: "almacen", GatewayID: "gw", EPC: "E1", RSSI: -60, Timestamp: now.Add(time.Second)})
	if err := a.FlushAll(); err != nil {
		t.Fatal(err)
	}
	s := next.all()[0]
	if strings.Contains(s.Timestamp.String(), "m=") || strings.Contains(s.LastSeen.String(), "m=") {
		t.Errorf("session keeps monotonic readings (comparisons would not match the stored wall clock): %v / %v", s.Timestamp, s.LastSeen)
	}
	if s.LastSeen.Before(s.Timestamp) {
		t.Errorf("last_seen %v before timestamp %v", s.LastSeen, s.Timestamp)
	}
}

func TestAggregator_ClosesSessionBeforeReadCountLimit(t *testing.T) {
	old := maxReadCount
	maxReadCount = 3
	defer func() { maxReadCount = old }()

	next := &recordingStore{}
	a := New(next, 5*time.Second, 60*time.Second)
	for i := 0; i < 4; i++ {
		mustStore(t, a, read("E1", "almacen", time.Duration(i)*time.Millisecond, -60))
	}
	if err := a.FlushAll(); err != nil {
		t.Fatal(err)
	}
	got := next.all()
	if len(got) != 2 || got[0].ReadCount != 3 || got[1].ReadCount != 1 {
		t.Errorf("sessions = %+v; want counts 3 and 1", got)
	}
}

type failingQueue struct {
	recordingStore
	failEPC string
	flushed bool
}

func (q *failingQueue) Store(r models.Reading) error {
	if r.EPC == q.failEPC {
		return errors.New("rejected")
	}
	return q.recordingStore.Store(r)
}

func (q *failingQueue) Flush() error {
	q.flushed = true
	return nil
}

func TestShutdownQueue_FlushesTheQueueEvenWhenASessionFails(t *testing.T) {
	q := &failingQueue{failEPC: "BAD"}
	a := New(q, 5*time.Second, 60*time.Second)
	mustStore(t, a, read("GOOD", "almacen", 0, -60))
	mustStore(t, a, read("BAD", "almacen", 0, -60))

	if err := ShutdownQueue(a, q); err == nil {
		t.Error("ShutdownQueue hid the failed session")
	}
	if !q.flushed {
		t.Error("queue buffer not flushed after a partial failure; queued sessions could be lost on Close")
	}
	if got := q.all(); len(got) != 1 || got[0].EPC != "GOOD" {
		t.Errorf("queued %+v; want GOOD", got)
	}
}

// orderedQueue records Store and Flush calls in order; Store blocks for the
// EPC in slowEPC until release is closed.
type orderedQueue struct {
	mu      sync.Mutex
	events  []string
	slowEPC string
	entered chan struct{}
	release chan struct{}
}

func (q *orderedQueue) Store(r models.Reading) error {
	if r.EPC == q.slowEPC {
		close(q.entered)
		<-q.release
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events = append(q.events, "store "+r.EPC)
	return nil
}

func (q *orderedQueue) Flush() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events = append(q.events, "flush")
	return nil
}

func (q *orderedQueue) log() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.events...)
}

func TestShutdownQueue_WaitsForAnInFlightTick(t *testing.T) {
	q := &orderedQueue{slowEPC: "SLOW", entered: make(chan struct{}), release: make(chan struct{})}
	a := New(q, 5*time.Second, 60*time.Second)
	mustStore(t, a, read("SLOW", "almacen", 0, -60))

	tickDone := make(chan error, 1)
	go func() { tickDone <- a.FlushIdle(t0.Add(10 * time.Second)) }()
	<-q.entered // the tick holds the SLOW session, detached from the map

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- ShutdownQueue(a, q) }()
	select {
	case err := <-shutdownDone:
		t.Fatalf("ShutdownQueue returned (%v) while a tick was still queueing a session", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(q.release)
	if err := <-tickDone; err != nil {
		t.Fatal(err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
	if got := q.log(); len(got) != 2 || got[0] != "store SLOW" || got[1] != "flush" {
		t.Errorf("events = %v; want [store SLOW flush]", got)
	}
}

func TestAggregator_ReadsAfterShutdownGoStraightToTheQueue(t *testing.T) {
	q := &failingQueue{}
	a := New(q, 5*time.Second, 60*time.Second)
	if err := ShutdownQueue(a, q); err != nil {
		t.Fatal(err)
	}
	mustStore(t, a, read("LATE", "almacen", 0, -60))
	got := q.all()
	if len(got) != 1 || got[0].EPC != "LATE" {
		t.Fatalf("late read queued as %+v; want it queued immediately", got)
	}
	if err := got[0].Validate(); err != nil {
		t.Errorf("late read invalid: %v", err)
	}
}

func TestAggregator_BackwardWallClockKeepsLastSeenAfterTimestamp(t *testing.T) {
	next := &recordingStore{}
	a := New(next, 5*time.Second, 60*time.Second)
	mustStore(t, a, read("E1", "almacen", 0, -60))
	mustStore(t, a, read("E1", "almacen", -30*time.Second, -61)) // clock stepped back
	if err := a.FlushAll(); err != nil {
		t.Fatal(err)
	}
	got := next.all()
	if len(got) != 1 {
		t.Fatalf("stored %d; want 1", len(got))
	}
	if got[0].LastSeen.Before(got[0].Timestamp) || got[0].ReadCount != 2 {
		t.Errorf("session = first %v last %v count %d; want last_seen >= timestamp and count 2",
			got[0].Timestamp, got[0].LastSeen, got[0].ReadCount)
	}
	if err := got[0].Validate(); err != nil {
		t.Errorf("session invalid: %v", err)
	}
}

func TestAggregator_RetryListIsCapped(t *testing.T) {
	old := maxClosing
	maxClosing = 2
	defer func() { maxClosing = old }()

	next := &recordingStore{err: errors.New("cache is closed")}
	a := New(next, 5*time.Second, 60*time.Second)
	for i := 0; i < 4; i++ {
		mustStore(t, a, read("E1", "almacen", time.Duration(i)*time.Minute, -60))
	}
	_ = a.FlushIdle(t0.Add(10 * time.Minute)) // fails; sessions go to the retry list

	next.mu.Lock()
	next.err = nil
	next.mu.Unlock()
	if err := a.FlushAll(); err != nil {
		t.Fatal(err)
	}
	got := next.all()
	if len(got) != 2 {
		t.Fatalf("stored %d after recovery; want the 2 newest kept by the cap", len(got))
	}
	if !got[0].Timestamp.Equal(t0.Add(2*time.Minute)) || !got[1].Timestamp.Equal(t0.Add(3*time.Minute)) {
		t.Errorf("kept %v and %v; want the two newest sessions in order", got[0].Timestamp, got[1].Timestamp)
	}
}

// On the shutdown timeout path the ticker may still be alive: once
// ShutdownQueue ran, a later tick must not queue anything (the cache may be
// draining or closed).
func TestAggregator_TickAfterShutdownQueuesNothing(t *testing.T) {
	next := &recordingStore{err: errors.New("cache is closed")}
	q := &failingQueue{}
	a := New(next, 5*time.Second, 60*time.Second)
	mustStore(t, a, read("E1", "almacen", 0, -60))

	_ = ShutdownQueue(a, q) // the final flush fails; E1 lands on the retry list

	next.mu.Lock()
	next.err = nil
	next.mu.Unlock()
	if err := a.FlushIdle(t0.Add(10 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := next.all(); len(got) != 0 {
		t.Errorf("a tick after shutdown queued %+v; want nothing", got)
	}
}
