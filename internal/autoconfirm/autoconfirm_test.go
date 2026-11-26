package autoconfirm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/confirm"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/verify"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

var t0 = time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC)

// fakeVPS is a reachable VPS recording every confirmation it accepts.
type fakeVPS struct {
	*httptest.Server
	confirms atomic.Int32
	last     atomic.Value // map[string]any
	// hold, when set, is called inside each confirmation POST before it is
	// answered.
	hold func()
}

func newFakeVPS(t *testing.T) *fakeVPS {
	t.Helper()
	f := &fakeVPS{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if f.hold != nil {
			f.hold()
		}
		var sent map[string]any
		json.NewDecoder(r.Body).Decode(&sent)
		f.last.Store(sent)
		f.confirms.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"OK"}`))
	}))
	t.Cleanup(f.Close)
	return f
}

// sink records every reading the tracker forwards.
type sink struct {
	mu       sync.Mutex
	readings []models.Reading
}

func (s *sink) Store(r models.Reading) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readings = append(s.readings, r)
	return nil
}

type env struct {
	store   *localstore.LocalStore
	bus     *events.EventBus
	mode    *Mode
	tracker *Tracker
	sink    *sink
	svc     *confirm.Service
	ac      *AutoConfirmer
	vps     *fakeVPS
	auto    chan events.AutoConfirmed
	clock   atomic.Int64
	links   int64
}

// newEnv wires a tracker and an auto-confirmer in automatic mode, idle 1m,
// min reads 3, to a real store and (when online) a fake VPS. The clock is
// explicit: set it with at.
func newEnv(t *testing.T, online bool) *env {
	t.Helper()
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	store := localstore.New(c.GetDB())
	bus := events.NewEventBus(100)
	t.Cleanup(bus.Close)

	e := &env{store: store, bus: bus, mode: &Mode{}, sink: &sink{}}
	e.at(t0)
	e.mode.Set(true)
	e.tracker = NewTracker(Config{Idle: time.Minute, MinReads: 3}, e.sink, e.mode)
	e.tracker.Now = func() time.Time { return time.Unix(0, e.clock.Load()).UTC() }
	e.svc = &confirm.Service{Store: store, CompanyID: "c", Claimer: e.tracker, Now: e.tracker.Now}
	if online {
		e.vps = newFakeVPS(t)
		e.svc.VPS = httpclient.NewVPSClient(e.vps.URL, 2*time.Second, httpclient.StaticToken("tok"))
	}
	e.ac = New(e.tracker, bus, store, storeTools{store}, e.svc)
	e.auto = bus.SubscribeAutoConfirmed()
	return e
}

// at sets the clock.
func (e *env) at(now time.Time) { e.clock.Store(now.UnixNano()) }

// storeTools adapts the local store to the verifier's ToolStore.
type storeTools struct{ s *localstore.LocalStore }

func (a storeTools) GetToolByUII(uii string) (*verify.Tool, error) {
	tool, err := a.s.GetToolByUII(uii)
	if err != nil || tool == nil {
		return nil, err
	}
	return &verify.Tool{UII: tool.UII, Location: tool.Location, Status: tool.Status}, nil
}

// fakeTools is a ToolStore returning a fixed tool or error.
type fakeTools struct {
	tool *verify.Tool
	err  error
}

func (f fakeTools) GetToolByUII(string) (*verify.Tool, error) { return f.tool, f.err }

// link assigns uii to a kanban with the given cached location (nil = NULL).
func (e *env) link(t *testing.T, uii string, location *string) {
	t.Helper()
	// A salida moves the tag to the kanban's first zone, a known location.
	k := localstore.Kanban{Code: "K1", Description: "Burlete", StandardPack: 10, Unit: "PZ", Zones: []string{"Línea 3"}}
	e.links++
	item := localstore.SyncDataItem{ID: e.links, ToolID: e.links, UII: uii, SKU: "S", Name: "N", Active: true, Location: location, Kanban: &k}
	if err := e.store.UpsertToolsFromSync([]localstore.SyncDataItem{item}); err != nil {
		t.Fatalf("upsert tool: %v", err)
	}
	if err := e.store.ApplyKanbanLinks([]localstore.SyncDataItem{item}); err != nil {
		t.Fatalf("link: %v", err)
	}
}

func loc(s string) *string { return &s }

// reads stores n reads of uii one second apart starting at start through the
// tracker; it returns the last read time.
func (e *env) reads(uii, antenna string, start time.Time, n int) time.Time {
	last := start
	for i := 0; i < n; i++ {
		last = start.Add(time.Duration(i) * time.Second)
		e.tracker.Store(models.Reading{EPC: uii, AntennaID: antenna, GatewayID: "gw", Timestamp: last})
	}
	return last
}

// tick runs a tick at now and then the queued work, as the worker would.
func (e *env) tick(now time.Time) {
	e.at(now)
	e.ac.Tick(now)
	e.work()
}

// work runs every queued pass synchronously.
func (e *env) work() {
	for {
		select {
		case j := <-e.ac.work:
			e.ac.confirmPass(context.Background(), j)
		default:
			return
		}
	}
}

func (e *env) manual(uii, action string) error {
	_, err := e.svc.Confirm(context.Background(), confirm.Request{UII: uii, Action: action})
	return err
}

func (e *env) history(t *testing.T) []localstore.HistoryEntry {
	t.Helper()
	entries, err := e.store.ListConfirmationsBetween(t0.Add(-time.Hour), t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	return entries
}

func TestTick_AutomaticPassConfirmsOnceOnline(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-2", t0, 3)

	e.tick(last.Add(ended))
	e.tick(last.Add(2 * time.Minute))

	if n := e.vps.confirms.Load(); n != 1 {
		t.Fatalf("VPS confirms = %d, want 1", n)
	}
	sent, _ := e.vps.last.Load().(map[string]any)
	h := e.history(t)
	if len(h) != 1 || !h[0].Auto || !h[0].Synced || h[0].Action != "salida" || h[0].AntennaID != "ant-2" {
		t.Fatalf("history = %+v, want one synced auto salida on ant-2", h)
	}
	if !h[0].Timestamp.Equal(last) || sent["occurred_at"] != last.Format(time.RFC3339Nano) || sent["action"] != "salida" {
		t.Errorf("occurred_at recorded %v sent %v, want last read %v", h[0].Timestamp, sent["occurred_at"], last)
	}
	select {
	case ev := <-e.auto:
		if ev.UII != "E1" || ev.Action != "salida" || ev.ConfirmationID != sent["confirmation_id"] {
			t.Errorf("event %+v, sent id %v", ev, sent["confirmation_id"])
		}
	default:
		t.Error("no AutoConfirmed event published")
	}
}

func TestTick_OutsideTagConfirmsEntrada(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Línea 3"))
	last := e.reads("E1", "ant-1", t0, 4)

	e.tick(last.Add(ended))

	if h := e.history(t); len(h) != 1 || h[0].Action != "entrada" || !h[0].Auto {
		t.Errorf("history = %+v, want one auto entrada", h)
	}
}

func TestTick_NothingConfirmed(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, e *env)
		reads int
	}{
		{"fewer reads than the minimum", func(t *testing.T, e *env) { e.link(t, "E1", loc("Almacén General")) }, 2},
		{"manual mode", func(t *testing.T, e *env) { e.link(t, "E1", loc("Almacén General")); e.mode.Set(false) }, 5},
		{"tag without kanban", func(t *testing.T, e *env) {}, 5},
		{"user badge", func(t *testing.T, e *env) {
			e.link(t, "E1", loc("Almacén General"))
			if err := e.store.UpsertUsers([]localstore.User{{ID: 1, CompanyID: "c", Name: "Ana", RFIDTag: "E1", Active: true}}); err != nil {
				t.Fatalf("user: %v", err)
			}
		}, 5},
		{"empty location", func(t *testing.T, e *env) { e.link(t, "E1", loc("")) }, 5},
		{"blank location", func(t *testing.T, e *env) { e.link(t, "E1", loc("   ")) }, 5},
		{"NULL location", func(t *testing.T, e *env) { e.link(t, "E1", nil) }, 5},
		{"missing tool row", func(t *testing.T, e *env) {
			e.link(t, "E1", loc("Almacén General"))
			e.ac.tools = fakeTools{}
		}, 5},
		{"tool lookup error", func(t *testing.T, e *env) {
			e.link(t, "E1", loc("Almacén General"))
			e.ac.tools = fakeTools{err: errors.New("db down")}
		}, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true)
			tc.setup(t, e)
			last := e.reads("E1", "ant-1", t0, tc.reads)

			e.tick(last.Add(ended))

			if n := e.vps.confirms.Load(); n != 0 {
				t.Errorf("VPS confirms = %d, want 0", n)
			}
			if h := e.history(t); len(h) != 0 {
				t.Errorf("history = %+v, want none", h)
			}
			// The pass is discarded either way: it is not retried later.
			e.mode.Set(true)
			e.ac.tools = storeTools{e.store}
			e.tick(last.Add(2 * time.Minute))
			if h := e.history(t); len(h) != 0 {
				t.Errorf("pass survived its end: history = %+v", h)
			}
		})
	}
}

func TestTick_ReadsKeepThePassAlive(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	e.reads("E1", "ant-1", t0, 3)

	e.tick(t0.Add(50 * time.Second))
	last := e.reads("E1", "ant-1", t0.Add(55*time.Second), 1)
	e.tick(t0.Add(70 * time.Second))

	if n := e.vps.confirms.Load(); n != 0 {
		t.Fatalf("confirmed before idle: %d", n)
	}
	e.tick(last.Add(ended))
	if h := e.history(t); len(h) != 1 || !h[0].Timestamp.Equal(last) {
		t.Errorf("history = %+v, want one confirm at the last read %v", h, last)
	}
}

func TestTick_NewReadAfterConfirmStartsNewPass(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)
	e.tick(last.Add(ended))

	// The confirm moved the tag out of the warehouse; one read is not a pass.
	start := last.Add(2 * time.Minute)
	e.reads("E1", "ant-1", start, 1)
	e.tick(start.Add(ended))
	if n := e.vps.confirms.Load(); n != 1 {
		t.Fatalf("confirms after short pass = %d, want 1", n)
	}

	// After the Cooldown since the first confirm.
	last = e.reads("E1", "ant-1", start.Add(Cooldown), 3)
	e.tick(last.Add(ended))
	if n := e.vps.confirms.Load(); n != 2 {
		t.Fatalf("confirms = %d, want 2", n)
	}
	sent, _ := e.vps.last.Load().(map[string]any)
	if sent["action"] != "entrada" {
		t.Errorf("second pass action = %v, want entrada", sent["action"])
	}
}

func TestTick_OfflineQueuesAutoConfirmation(t *testing.T) {
	e := newEnv(t, false)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)

	e.tick(last.Add(ended))

	queue, _ := e.store.GetUnsyncedConfirmations(10)
	if len(queue) != 1 || queue[0].Action != "salida" || !queue[0].OccurredAt.Equal(last) {
		t.Fatalf("queue = %+v, want one salida at %v", queue, last)
	}
	if h := e.history(t); len(h) != 1 || !h[0].Auto || h[0].Synced {
		t.Errorf("history = %+v, want one pending auto row", h)
	}
	select {
	case ev := <-e.auto:
		if ev.ConfirmationID != queue[0].ConfirmationID {
			t.Errorf("event id %s, queued %s", ev.ConfirmationID, queue[0].ConfirmationID)
		}
	default:
		t.Error("no AutoConfirmed event for a queued confirmation")
	}
}

func TestObserve_GapOfIdleStartsNewPass(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 2)
	last = e.reads("E1", "ant-1", last.Add(time.Minute), 1)

	e.tick(last.Add(ended))

	if h := e.history(t); len(h) != 0 {
		t.Errorf("history = %+v, want none: passes joined across an idle gap", h)
	}
}

func TestStore_ForwardsEveryReadingUnchanged(t *testing.T) {
	e := newEnv(t, true)
	in := []models.Reading{
		{EPC: "E1", AntennaID: "ant-1", GatewayID: "gw", RSSI: -40, Timestamp: t0},
		{EPC: "E1", AntennaID: "ant-2", GatewayID: "gw", RSSI: -50, Timestamp: t0.Add(time.Second)},
		{EPC: "", AntennaID: "ant-1", GatewayID: "gw", Timestamp: t0}, // invalid: still forwarded
	}
	for _, r := range in {
		if err := e.tracker.Store(r); err != nil {
			t.Fatalf("store: %v", err)
		}
	}
	if len(e.sink.readings) != len(in) {
		t.Fatalf("forwarded %d readings, want %d", len(e.sink.readings), len(in))
	}
	for i := range in {
		if e.sink.readings[i] != in[i] {
			t.Errorf("reading %d forwarded as %+v, want %+v", i, e.sink.readings[i], in[i])
		}
	}
}

func TestClaim_ReadAfterEnqueueRejectsTheQueuedPass(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)
	now := last.Add(ended)
	e.at(now)
	e.ac.Tick(now)

	e.reads("E1", "ant-1", now, 1) // the tag is read again before the worker claims
	e.work()

	if n := e.vps.confirms.Load(); n != 0 {
		t.Errorf("VPS confirms = %d, want 0: the tag is being read again", n)
	}
	if h := e.history(t); len(h) != 0 {
		t.Errorf("history = %+v, want none", h)
	}
}

func TestClaim_ManualAfterEnqueueWins(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)
	now := last.Add(ended)
	e.at(now)
	e.ac.Tick(now)

	if err := e.manual("E1", "salida"); err != nil {
		t.Fatalf("manual: %v", err)
	}
	e.work()

	if h := e.history(t); len(h) != 1 || h[0].Auto {
		t.Errorf("history = %+v, want exactly the manual movement", h)
	}
	if n := e.vps.confirms.Load(); n != 1 {
		t.Errorf("VPS confirms = %d, want 1", n)
	}
}

func TestClaim_ModeSwitchedToManualAfterEnqueue(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)
	now := last.Add(ended)
	e.at(now)
	e.ac.Tick(now)

	e.mode.Set(false)
	e.work()

	if n := e.vps.confirms.Load(); n != 0 {
		t.Errorf("VPS confirms = %d, want 0 after switching to manual", n)
	}
	if h := e.history(t); len(h) != 0 {
		t.Errorf("history = %+v, want none", h)
	}
}

// holdVPS makes the next confirmation POST wait: entered is closed when it
// arrives, and it is answered once release is closed.
func holdVPS(e *env) (entered, release chan struct{}) {
	entered, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.vps.hold = func() {
		once.Do(func() {
			close(entered)
			<-release
		})
	}
	return entered, release
}

func TestClaim_ManualWhileAutoInFlightIsRefused(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)
	entered, release := holdVPS(e)
	e.at(last.Add(ended))
	e.ac.Tick(last.Add(ended))
	done := make(chan struct{})
	go func() {
		e.work()
		close(done)
	}()
	<-entered

	err := e.manual("E1", "salida")
	close(release)
	<-done

	if !errors.Is(err, confirm.ErrAutoInProgress) {
		t.Fatalf("manual err = %v, want ErrAutoInProgress", err)
	}
	if h := e.history(t); len(h) != 1 || !h[0].Auto {
		t.Errorf("history = %+v, want only the auto movement", h)
	}
	if n := e.vps.confirms.Load(); n != 1 {
		t.Errorf("VPS confirms = %d, want 1", n)
	}
	// Released: once the tag is read again, a manual confirm is accepted.
	e.reads("E1", "ant-1", last.Add(3*time.Minute), 1)
	if err := e.manual("E1", "entrada"); err != nil {
		t.Errorf("manual after release: %v", err)
	}
}

func TestClaim_AutoSuccessKeepsNewerPass(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)
	now := last.Add(ended)
	entered, release := holdVPS(e)
	e.at(now)
	e.ac.Tick(now)
	done := make(chan struct{})
	go func() {
		e.work()
		close(done)
	}()
	<-entered
	// A new pass starts while the first one is being confirmed.
	newLast := e.reads("E1", "ant-1", now.Add(time.Second), 3)
	close(release)
	<-done

	// The newer pass is kept unconsumed (the Cooldown, not the claim, then
	// keeps it from a second automatic confirm).
	e.tracker.mu.Lock()
	p := e.tracker.passes["E1"]
	e.tracker.mu.Unlock()
	if p == nil || p.consumed || p.reads != 3 || !p.lastSeen.Equal(newLast) {
		t.Errorf("newer pass = %+v, want it live and unconsumed", p)
	}
}

func TestTick_FullQueueRetriesOnLaterTick(t *testing.T) {
	old := confirmQueue
	confirmQueue = 1
	t.Cleanup(func() { confirmQueue = old })
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	e.link(t, "E2", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)
	e.reads("E2", "ant-1", t0, 3)
	now := last.Add(ended)

	e.at(now)
	e.ac.Tick(now)
	e.ac.Tick(now) // queue full: nothing blocks
	before := len(e.sink.readings)
	e.reads("E3", "ant-1", now, 2) // reads are still observed and forwarded
	if got := len(e.sink.readings) - before; got != 2 {
		t.Fatalf("forwarded %d reads while the queue was full, want 2", got)
	}
	if got := len(e.ac.work); got != 1 {
		t.Fatalf("queued %d passes, want 1", got)
	}
	e.work()
	e.tick(now.Add(time.Second))

	if n := e.vps.confirms.Load(); n != 2 {
		t.Errorf("VPS confirms = %d, want 2 (second pass on a later tick)", n)
	}
}

func TestClaim_ManualWhileReadingThenIdle(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	e.reads("E1", "ant-1", t0, 3)
	e.at(t0.Add(5 * time.Second))
	if err := e.manual("E1", "salida"); err != nil {
		t.Fatalf("manual: %v", err)
	}
	last := e.reads("E1", "ant-1", t0.Add(10*time.Second), 3)

	e.tick(last.Add(ended))
	if h := e.history(t); len(h) != 1 || h[0].Auto {
		t.Fatalf("history = %+v, want only the manual movement", h)
	}

	last = e.reads("E1", "ant-1", last.Add(Cooldown), 3)
	e.tick(last.Add(ended))
	h := e.history(t)
	auto := 0
	for _, m := range h {
		if m.Auto {
			auto++
		}
	}
	if len(h) != 2 || auto != 1 {
		t.Errorf("history = %+v, want the manual one plus one auto", h)
	}
}

func TestClaim_ManualWithoutPassConsumesTheNextReads(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	if err := e.manual("E1", "salida"); err != nil { // no pass yet: one is created
		t.Fatalf("manual: %v", err)
	}
	last := e.reads("E1", "ant-1", t0.Add(10*time.Second), 3) // still the consumed pass
	e.tick(last.Add(ended))
	if n := e.vps.confirms.Load(); n != 1 {
		t.Fatalf("VPS confirms = %d, want only the manual one", n)
	}
}

func TestClaim_FailedAutoConfirmIsNotRetried(t *testing.T) {
	e := newEnv(t, false)
	e.svc.MaxPending = 1
	e.link(t, "E2", loc("Almacén General"))
	if _, err := e.svc.Confirm(context.Background(), confirm.Request{UII: "E2", Action: "salida"}); err != nil {
		t.Fatalf("fill queue: %v", err)
	}
	last := e.reads("E2", "ant-1", t0, 3)
	e.tick(last.Add(ended)) // queue full: the auto confirm fails
	e.svc.MaxPending = 0
	e.tick(last.Add(2 * time.Minute))

	if q, _ := e.store.GetUnsyncedConfirmations(10); len(q) != 1 {
		t.Errorf("queue = %+v, want only the first movement", q)
	}
	// The failed claim was released: a manual confirm is accepted.
	if err := e.manual("E2", "salida"); err != nil {
		t.Errorf("manual after failed auto: %v", err)
	}
}

func TestRun_ConfirmsOnTickAndStopsOnCancel(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)
	e.at(last.Add(ended))
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		e.ac.run(ctx, ticks)
		close(done)
	}()
	ticks <- time.Time{}
	select {
	case <-e.auto:
	case <-time.After(5 * time.Second):
		t.Fatal("no auto confirm after a tick")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop on cancel")
	}
}

func TestRun_StopsOnCancel(t *testing.T) {
	e := newEnv(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.ac.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop on cancel")
	}
}

func TestWorker_PendingConfirmationBlocksAuto(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	// An offline salida still queued; the cached location says warehouse.
	if _, err := e.store.CreateConfirmationWithIDAuto("E1", "salida", "manual", "pending-1", t0.Add(-2*time.Hour), 0, false); err != nil {
		t.Fatalf("queue: %v", err)
	}
	last := e.reads("E1", "ant-1", t0, 3)

	e.tick(last.Add(ended))
	e.tick(last.Add(2 * time.Minute))

	if n := e.vps.confirms.Load(); n != 0 {
		t.Errorf("VPS confirms = %d, want 0 while a confirmation is pending", n)
	}
	if q, _ := e.store.GetUnsyncedConfirmations(10); len(q) != 1 {
		t.Errorf("queue = %+v, want only the pending one", q)
	}
}

// changeAtClaim runs change on the first clock read after it is armed: the
// claim is the first thing the worker does that reads the clock.
func (e *env) changeAtClaim(change func()) (arm func()) {
	var armed, done atomic.Bool
	e.tracker.Now = func() time.Time {
		if armed.Load() && done.CompareAndSwap(false, true) {
			change()
		}
		return time.Unix(0, e.clock.Load()).UTC()
	}
	e.svc.Now = e.tracker.Now
	return func() { armed.Store(true) }
}

func TestWorker_DecisionUsesPostClaimValues(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, e *env)
		want   string // action sent, "" = nothing
	}{
		{"location changed", func(t *testing.T, e *env) { e.link(t, "E1", loc("Línea 3")) }, "entrada"},
		{"user badge added", func(t *testing.T, e *env) {
			if err := e.store.UpsertUsers([]localstore.User{{ID: 1, CompanyID: "c", Name: "Ana", RFIDTag: "E1", Active: true}}); err != nil {
				t.Errorf("user: %v", err)
			}
		}, ""},
		{"kanban unlinked", func(t *testing.T, e *env) {
			if err := e.store.ApplyKanbanLinks([]localstore.SyncDataItem{{UII: "E1", Active: true}}); err != nil {
				t.Errorf("unlink: %v", err)
			}
		}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true)
			e.link(t, "E1", loc("Almacén General"))
			last := e.reads("E1", "ant-1", t0, 3)
			arm := e.changeAtClaim(func() { tc.change(t, e) })
			now := last.Add(ended)
			e.at(now)
			e.ac.Tick(now)
			arm()
			e.work()

			sent, _ := e.vps.last.Load().(map[string]any)
			if tc.want == "" {
				if n := e.vps.confirms.Load(); n != 0 {
					t.Errorf("VPS confirms = %d (%v), want 0", n, sent)
				}
				return
			}
			if n := e.vps.confirms.Load(); n != 1 || sent["action"] != tc.want {
				t.Errorf("VPS confirms = %d action %v, want one %s", n, sent["action"], tc.want)
			}
		})
	}
}

func TestClaim_RejectedClaimIsReconsideredLater(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)
	now := last.Add(ended)
	e.at(now)
	e.ac.Tick(now)
	e.reads("E1", "ant-1", last.Add(59*time.Second), 1) // the pass extends before the claim
	e.work()
	if n := e.vps.confirms.Load(); n != 0 {
		t.Fatalf("VPS confirms = %d, want 0 after the rejected claim", n)
	}

	e.tick(now.Add(ended))
	e.tick(now.Add(2 * time.Minute))

	if n := e.vps.confirms.Load(); n != 1 {
		t.Errorf("VPS confirms = %d, want exactly 1 on a later tick", n)
	}
}

func TestClaim_StaleManualAfterAutoIsRefused(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)
	e.tick(last.Add(ended))

	err := e.manual("E1", "salida")
	if !errors.Is(err, confirm.ErrAlreadyAutoConfirmed) {
		t.Fatalf("manual err = %v, want ErrAlreadyAutoConfirmed", err)
	}
	if h := e.history(t); len(h) != 1 || !h[0].Auto {
		t.Fatalf("history = %+v, want only the auto movement", h)
	}

	// A new read: manual confirms are normal again.
	e.reads("E1", "ant-1", last.Add(3*time.Minute), 1)
	if err := e.manual("E1", "entrada"); err != nil {
		t.Errorf("manual after a new read: %v", err)
	}
	if n := e.vps.confirms.Load(); n != 2 {
		t.Errorf("VPS confirms = %d, want 2", n)
	}
}

// recordAt records a synced confirmation of uii at occurredAt.
func (e *env) recordAt(t *testing.T, uii string, occurredAt time.Time) {
	t.Helper()
	if err := e.store.RecordSyncedConfirmationWithIDAuto(uii, "salida", "manual", "seed-"+occurredAt.String(), occurredAt, false); err != nil {
		t.Fatalf("record: %v", err)
	}
}

func TestSeed_RecentConfirmationConsumesFirstPass(t *testing.T) {
	// Real time: the seed reads the store's recent confirmations.
	start := time.Now().UTC().Truncate(time.Second)
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	e.recordAt(t, "E1", start.Add(-5*time.Minute))
	e.at(start)
	if err := e.tracker.SeedRecent(e.store, start); err != nil {
		t.Fatalf("seed: %v", err)
	}

	last := e.reads("E1", "ant-1", start, 3)
	e.tick(last.Add(ended))
	if n := e.vps.confirms.Load(); n != 0 {
		t.Fatalf("VPS confirms = %d, want 0 for the first pass after start", n)
	}

	// A later pass after the Cooldown since the seeded confirmation.
	last = e.reads("E1", "ant-1", start.Add(Cooldown), 3)
	e.tick(last.Add(ended))
	if n := e.vps.confirms.Load(); n != 1 {
		t.Errorf("VPS confirms = %d, want 1 for a later pass", n)
	}
}

func TestSeed_OldConfirmationIsIgnored(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Second)
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	e.recordAt(t, "E1", start.Add(-11*time.Minute))
	e.at(start)
	if err := e.tracker.SeedRecent(e.store, start); err != nil {
		t.Fatalf("seed: %v", err)
	}

	last := e.reads("E1", "ant-1", start, 3)
	e.tick(last.Add(ended))
	if n := e.vps.confirms.Load(); n != 1 {
		t.Errorf("VPS confirms = %d, want 1: an old confirmation is not seeded", n)
	}
}

func TestWorker_CooldownAfterRecentConfirmation(t *testing.T) {
	tests := []struct {
		name string
		ago  time.Duration
		want int32
	}{
		{"3 minutes ago", 3 * time.Minute, 0},
		{"11 minutes ago", 11 * time.Minute, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true)
			// The salida reached the VPS, but the cached location is still
			// the warehouse (a stale sync snapshot).
			e.link(t, "E1", loc("Almacén General"))
			last := e.reads("E1", "ant-1", t0, 3)
			e.recordAt(t, "E1", last.Add(time.Minute-tc.ago))
			e.tick(last.Add(time.Minute + 2*time.Second))
			if n := e.vps.confirms.Load(); n != tc.want {
				t.Errorf("VPS confirms = %d, want %d", n, tc.want)
			}
		})
	}
}

func TestWarmup_PassStartingRightAfterStartIsConsumed(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	e.tracker.Start(t0)
	last := e.reads("E1", "ant-1", t0.Add(10*time.Second), 3)
	e.tick(last.Add(time.Minute + LateReadMargin))
	e.tick(last.Add(2 * time.Minute))
	if n := e.vps.confirms.Load(); n != 0 {
		t.Fatalf("VPS confirms = %d, want 0 for a pass starting in the warm-up", n)
	}

	last = e.reads("E1", "ant-1", last.Add(3*time.Minute), 3)
	e.tick(last.Add(time.Minute + LateReadMargin))
	if n := e.vps.confirms.Load(); n != 1 {
		t.Errorf("VPS confirms = %d, want 1 for a pass after the warm-up", n)
	}
}

func TestWarmup_FailedSeedKeepsTheWarmup(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	e.tracker.Start(t0)
	if err := e.tracker.SeedRecent(failingLister{}, t0); err == nil {
		t.Fatal("seed: want an error")
	}
	last := e.reads("E1", "ant-1", t0.Add(10*time.Second), 3)
	e.tick(last.Add(time.Minute + LateReadMargin))
	if n := e.vps.confirms.Load(); n != 0 {
		t.Errorf("VPS confirms = %d, want 0", n)
	}
}

type failingLister struct{}

func (failingLister) ListConfirmationsBetween(time.Time, time.Time) ([]localstore.HistoryEntry, error) {
	return nil, errors.New("db down")
}

func TestClaim_NeedsLateReadMarginPastIdle(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	last := e.reads("E1", "ant-1", t0, 3)
	now := last.Add(time.Minute)
	e.at(now)
	e.ac.Tick(now)
	// A read timestamped just before the idle deadline arrives late.
	e.reads("E1", "ant-1", last.Add(59*time.Second), 1)
	e.work()
	e.tick(now.Add(time.Second))
	if n := e.vps.confirms.Load(); n != 0 {
		t.Fatalf("VPS confirms = %d, want 0 within the margin", n)
	}
	e.tick(last.Add(59*time.Second + time.Minute + LateReadMargin))
	if n := e.vps.confirms.Load(); n != 1 {
		t.Errorf("VPS confirms = %d, want 1 after the margin", n)
	}
}

func TestObserve_ReadOlderThanThePassDoesNotCount(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", loc("Almacén General"))
	// New pass on ant-2 with 2 reads; late reads of an older pass on ant-1.
	e.reads("E1", "ant-1", t0, 3)
	start := t0.Add(5 * time.Minute)
	last := e.reads("E1", "ant-2", start, 2)
	e.reads("E1", "ant-1", t0.Add(3*time.Second), 2)
	e.tick(last.Add(time.Minute + LateReadMargin))
	if n := e.vps.confirms.Load(); n != 0 {
		t.Fatalf("VPS confirms = %d, want 0: old reads must not reach min reads", n)
	}
	e.tracker.mu.Lock()
	p := e.tracker.passes["E1"]
	e.tracker.mu.Unlock()
	if p != nil && p.lastAntenna != "ant-2" {
		t.Errorf("pass antenna = %s, want ant-2", p.lastAntenna)
	}
}

// ended is how long after its last read a pass of newEnv ends.
const ended = time.Minute + LateReadMargin

// The tag's last local confirmation decides the direction; the cached
// location only when the tag has none.
func TestWorker_DirectionFromLastLocalConfirmation(t *testing.T) {
	tests := []struct {
		name     string
		location *string
		last     string // "" = no local confirmation
		want     string // "" = nothing sent
	}{
		{"last salida, stale warehouse location", loc("Almacén General"), "salida", "entrada"},
		{"last entrada, outside location", loc("Línea 3"), "entrada", "salida"},
		{"none, warehouse location", loc("Almacén General"), "", "salida"},
		{"none, blank location", loc("  "), "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true)
			e.link(t, "E1", tc.location)
			if tc.last != "" {
				// Older than the cooldown; an older opposite one must not win.
				other := map[string]string{"salida": "entrada", "entrada": "salida"}[tc.last]
				if err := e.store.RecordSyncedConfirmationWithIDAuto("E1", other, "manual", "old", t0.Add(-time.Hour), false); err != nil {
					t.Fatal(err)
				}
				if err := e.store.RecordSyncedConfirmationWithIDAuto("E1", tc.last, "manual", "last", t0.Add(-30*time.Minute), true); err != nil {
					t.Fatal(err)
				}
			}
			last := e.reads("E1", "ant-1", t0, 3)
			e.tick(last.Add(ended))

			sent, _ := e.vps.last.Load().(map[string]any)
			if tc.want == "" {
				if n := e.vps.confirms.Load(); n != 0 {
					t.Errorf("VPS confirms = %d, want 0", n)
				}
				return
			}
			if n := e.vps.confirms.Load(); n != 1 || sent["action"] != tc.want {
				t.Errorf("VPS confirms = %d, sent %v; want one %s", n, sent["action"], tc.want)
			}
		})
	}
}
