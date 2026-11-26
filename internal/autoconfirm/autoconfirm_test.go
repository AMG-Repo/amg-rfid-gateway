package autoconfirm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/confirm"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/verify"
)

var t0 = time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC)

// fakeVPS is a reachable VPS recording every confirmation it accepts.
type fakeVPS struct {
	*httptest.Server
	confirms atomic.Int32
	last     atomic.Value // map[string]any
}

func newFakeVPS(t *testing.T) *fakeVPS {
	t.Helper()
	f := &fakeVPS{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
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

type env struct {
	store *localstore.LocalStore
	bus   *events.EventBus
	mode  *Mode
	ac    *AutoConfirmer
	vps   *fakeVPS
	auto  chan events.AutoConfirmed
}

// newEnv wires an auto-confirmer in automatic mode, idle 1m, min reads 3, to
// a real store and (when online) a fake VPS.
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

	svc := &confirm.Service{Store: store, CompanyID: "c"}
	e := &env{store: store, bus: bus, mode: &Mode{}}
	if online {
		e.vps = newFakeVPS(t)
		svc.VPS = httpclient.NewVPSClient(e.vps.URL, 2*time.Second, httpclient.StaticToken("tok"))
	}
	e.mode.Set(true)
	e.ac = New(Config{Idle: time.Minute, MinReads: 3}, bus, store, verify.NewVerifier(storeTools{store}, nil), svc, e.mode)
	e.auto = bus.SubscribeAutoConfirmed()
	return e
}

// storeTools adapts the local store to the verifier's ToolStore.
type storeTools struct{ s *localstore.LocalStore }

func (a storeTools) GetToolByUII(uii string) (*verify.Tool, error) {
	tool, err := a.s.GetToolByUII(uii)
	if err != nil || tool == nil {
		return nil, err
	}
	return &verify.Tool{UII: tool.UII, Location: tool.Location, Status: tool.Status}, nil
}

// link assigns uii to a kanban with the given cached location.
func (e *env) link(t *testing.T, uii, location string) {
	t.Helper()
	k := localstore.Kanban{Code: "K1", Description: "Burlete", StandardPack: 10, Unit: "PZ"}
	item := localstore.SyncDataItem{ID: 1, ToolID: 1, UII: uii, SKU: "S", Name: "N", Active: true, Location: &location, Kanban: &k}
	if err := e.store.UpsertToolsFromSync([]localstore.SyncDataItem{item}); err != nil {
		t.Fatalf("upsert tool: %v", err)
	}
	if err := e.store.ApplyKanbanLinks([]localstore.SyncDataItem{item}); err != nil {
		t.Fatalf("link: %v", err)
	}
}

// reads observes n reads of uii one second apart starting at start; it
// returns the last read time.
func (e *env) reads(uii, antenna string, start time.Time, n int) time.Time {
	last := start
	for i := 0; i < n; i++ {
		last = start.Add(time.Duration(i) * time.Second)
		e.ac.Observe(events.TagDetected{EPC: uii, AntennaID: antenna, Timestamp: last})
	}
	return last
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
	e.link(t, "E1", "Almacén General")
	last := e.reads("E1", "ant-2", t0, 3)

	e.ac.Tick(context.Background(), last.Add(time.Minute))
	e.ac.Tick(context.Background(), last.Add(2*time.Minute))

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
	e.link(t, "E1", "Línea 3")
	last := e.reads("E1", "ant-1", t0, 4)

	e.ac.Tick(context.Background(), last.Add(time.Minute))

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
		{"fewer reads than the minimum", func(t *testing.T, e *env) { e.link(t, "E1", "Almacén General") }, 2},
		{"manual mode", func(t *testing.T, e *env) { e.link(t, "E1", "Almacén General"); e.mode.Set(false) }, 5},
		{"tag without kanban", func(t *testing.T, e *env) {}, 5},
		{"user badge", func(t *testing.T, e *env) {
			e.link(t, "E1", "Almacén General")
			if err := e.store.UpsertUsers([]localstore.User{{ID: 1, CompanyID: "c", Name: "Ana", RFIDTag: "E1", Active: true}}); err != nil {
				t.Fatalf("user: %v", err)
			}
		}, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true)
			tc.setup(t, e)
			last := e.reads("E1", "ant-1", t0, tc.reads)

			e.ac.Tick(context.Background(), last.Add(time.Minute))

			if n := e.vps.confirms.Load(); n != 0 {
				t.Errorf("VPS confirms = %d, want 0", n)
			}
			if h := e.history(t); len(h) != 0 {
				t.Errorf("history = %+v, want none", h)
			}
			// The pass is discarded either way: the next pass starts from zero.
			e.mode.Set(true)
			e.ac.Tick(context.Background(), last.Add(2*time.Minute))
			if h := e.history(t); len(h) != 0 {
				t.Errorf("pass survived its end: history = %+v", h)
			}
		})
	}
}

func TestTick_ReadsKeepThePassAlive(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", "Almacén General")
	e.reads("E1", "ant-1", t0, 3)

	e.ac.Tick(context.Background(), t0.Add(50*time.Second))
	last := e.reads("E1", "ant-1", t0.Add(55*time.Second), 1)
	e.ac.Tick(context.Background(), t0.Add(70*time.Second))

	if n := e.vps.confirms.Load(); n != 0 {
		t.Fatalf("confirmed before idle: %d", n)
	}
	e.ac.Tick(context.Background(), last.Add(time.Minute))
	if h := e.history(t); len(h) != 1 || !h[0].Timestamp.Equal(last) {
		t.Errorf("history = %+v, want one confirm at the last read %v", h, last)
	}
}

func TestTick_NewReadAfterConfirmStartsNewPass(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", "Almacén General")
	last := e.reads("E1", "ant-1", t0, 3)
	e.ac.Tick(context.Background(), last.Add(time.Minute))

	// The confirm moved the tag out of the warehouse; one read is not a pass.
	start := last.Add(2 * time.Minute)
	e.reads("E1", "ant-1", start, 1)
	e.ac.Tick(context.Background(), start.Add(time.Minute))
	if n := e.vps.confirms.Load(); n != 1 {
		t.Fatalf("confirms after short pass = %d, want 1", n)
	}

	last = e.reads("E1", "ant-1", start.Add(2*time.Minute), 3)
	e.ac.Tick(context.Background(), last.Add(time.Minute))
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
	e.link(t, "E1", "Almacén General")
	last := e.reads("E1", "ant-1", t0, 3)

	e.ac.Tick(context.Background(), last.Add(time.Minute))

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

func TestRun_ObservesBusAndStopsOnCancel(t *testing.T) {
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

// fakeTools is a verifier ToolStore returning a fixed tool or error.
type fakeTools struct {
	tool *verify.Tool
	err  error
}

func (f fakeTools) GetToolByUII(string) (*verify.Tool, error) { return f.tool, f.err }

func TestTick_UnknownLocationIsLeftForManual(t *testing.T) {
	tests := []struct {
		name  string
		tools fakeTools
	}{
		{"missing tool row", fakeTools{}},
		{"lookup error", fakeTools{err: errors.New("db down")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true)
			e.link(t, "E1", "Almacén General")
			e.ac.suggester = verify.NewVerifier(tc.tools, nil)
			last := e.reads("E1", "ant-1", t0, 5)

			e.ac.Tick(context.Background(), last.Add(time.Minute))

			if n := e.vps.confirms.Load(); n != 0 {
				t.Errorf("VPS confirms = %d, want 0", n)
			}
			if h := e.history(t); len(h) != 0 {
				t.Errorf("history = %+v, want none", h)
			}
		})
	}
}

func TestObserve_GapOfIdleStartsNewPass(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", "Almacén General")
	last := e.reads("E1", "ant-1", t0, 2)
	last = e.reads("E1", "ant-1", last.Add(time.Minute), 1)

	e.ac.Tick(context.Background(), last.Add(time.Minute))

	if h := e.history(t); len(h) != 0 {
		t.Errorf("history = %+v, want none: passes joined across an idle gap", h)
	}
}

func TestConsumed_ManualConfirmWhileReadingConsumesPass(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", "Almacén General")
	e.reads("E1", "ant-1", t0, 3)
	e.ac.Now = func() time.Time { return t0.Add(5 * time.Second) }
	e.ac.Consumed(events.MovementConfirmed{UII: "E1", Action: "salida"})
	last := e.reads("E1", "ant-1", t0.Add(10*time.Second), 3)

	e.ac.Tick(context.Background(), last.Add(time.Minute))

	if n := e.vps.confirms.Load(); n != 0 {
		t.Errorf("VPS confirms = %d, want 0: the pass was already confirmed", n)
	}
}

func TestConsumed_ManualConfirmInManualModeThenAutomatic(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", "Almacén General")
	e.mode.Set(false)
	e.reads("E1", "ant-1", t0, 3)
	e.ac.Now = func() time.Time { return t0.Add(3 * time.Second) }
	e.ac.Consumed(events.MovementConfirmed{UII: "E1", Action: "salida"})
	e.mode.Set(true)

	e.ac.Tick(context.Background(), t0.Add(3*time.Second+time.Minute))

	if n := e.vps.confirms.Load(); n != 0 {
		t.Errorf("VPS confirms = %d, want 0", n)
	}
}

func TestConsumed_NewPassAfterIdleGapConfirmsOnce(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", "Almacén General")
	e.ac.Now = func() time.Time { return t0 }
	e.ac.Consumed(events.MovementConfirmed{UII: "E1", Action: "salida"}) // no pass yet: one is created
	e.reads("E1", "ant-1", t0.Add(10*time.Second), 3)                    // still the consumed pass
	end := t0.Add(12*time.Second + time.Minute)
	e.ac.Tick(context.Background(), end)
	if n := e.vps.confirms.Load(); n != 0 {
		t.Fatalf("consumed pass confirmed: %d", n)
	}

	last := e.reads("E1", "ant-1", end.Add(time.Minute), 3)
	e.ac.Tick(context.Background(), last.Add(time.Minute))

	if n := e.vps.confirms.Load(); n != 1 {
		t.Errorf("VPS confirms = %d, want 1", n)
	}
}

// startRun runs the loop on the given channels with a tick hook; it returns a
// stop func that cancels and waits for the loop and its worker.
func startRun(t *testing.T, e *env, reads chan events.TagDetected, ticks chan time.Time) (ticked chan struct{}, stop func()) {
	t.Helper()
	ticked = make(chan struct{}, 10)
	e.ac.afterTick = func() { ticked <- struct{}{} }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.ac.run(ctx, reads, make(chan events.MovementConfirmed), ticks)
		close(done)
	}()
	return ticked, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("run did not stop")
		}
	}
}

func TestRun_QueuedReadsAreDrainedBeforeTick(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", "Almacén General")
	e.reads("E1", "ant-1", t0, 3)
	reads := make(chan events.TagDetected, 10)
	reads <- events.TagDetected{EPC: "E1", AntennaID: "ant-1", Timestamp: t0.Add(55 * time.Second)}
	ticks := make(chan time.Time, 1)
	ticks <- t0
	e.ac.Now = func() time.Time { return t0.Add(70 * time.Second) }

	ticked, stop := startRun(t, e, reads, ticks)
	<-ticked
	stop()

	if n := e.vps.confirms.Load(); n != 0 {
		t.Errorf("VPS confirms = %d, want 0: queued read keeps the pass alive", n)
	}
}

func TestRun_SwitchToManualBeforeConfirmSkipsIt(t *testing.T) {
	e := newEnv(t, true)
	e.link(t, "E1", "Almacén General")
	last := e.reads("E1", "ant-1", t0, 3)
	e.ac.Now = func() time.Time { return last.Add(time.Minute) }
	confirming := make(chan struct{})
	e.ac.beforeConfirm = func() { e.mode.Set(false); close(confirming) }
	ticks := make(chan time.Time, 1)
	ticks <- t0

	_, stop := startRun(t, e, make(chan events.TagDetected), ticks)
	<-confirming
	stop()

	if n := e.vps.confirms.Load(); n != 0 {
		t.Errorf("VPS confirms = %d, want 0 after switching to manual", n)
	}
	if h := e.history(t); len(h) != 0 {
		t.Errorf("history = %+v, want none", h)
	}
}
