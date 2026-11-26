// Package autoconfirm confirms tag passes by itself when the gateway is in
// automatic mode: a pass ends once the tag goes unread for the idle time, and
// a pass with enough reads of a kanban-linked tag (not a user badge) is
// confirmed through the shared confirm path, with the direction taken from the
// tag's cached state.
package autoconfirm

import (
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/confirm"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/verify"
)

// TickInterval is how often Run checks for ended passes.
const TickInterval = time.Second

// confirmQueue bounds the ended passes waiting for the confirm worker.
const confirmQueue = 64

// Mode is the in-memory confirmation mode shared by the auto-confirmer and the
// web panel; the zero value is manual.
type Mode struct{ enabled atomic.Bool }

// Enabled reports whether automatic mode is on.
func (m *Mode) Enabled() bool { return m.enabled.Load() }

// Set switches automatic mode on or off.
func (m *Mode) Set(enabled bool) { m.enabled.Store(enabled) }

// Config holds the pass thresholds.
type Config struct {
	// Idle is how long a tag must go unread for its pass to end.
	Idle time.Duration
	// MinReads is the minimum reads in a pass to confirm it.
	MinReads int
}

// Store is the lookup surface the auto-confirmer needs.
type Store interface {
	GetKanbanByUII(uii string) (*localstore.Kanban, error)
	GetUserByRFIDTag(rfidTag string) (*localstore.User, error)
}

// Suggester decides the movement direction of a tag.
type Suggester interface {
	SuggestAction(uii, antennaID string) verify.SuggestedAction
}

// Confirmer registers a movement.
type Confirmer interface {
	Confirm(ctx context.Context, req confirm.Request) (confirm.Result, error)
}

type pass struct {
	reads       int
	lastSeen    time.Time
	lastAntenna string
	// consumed marks a pass already confirmed (manually or automatically):
	// it is dropped without confirming when it ends.
	consumed bool
}

type ended struct {
	uii string
	pass
	now time.Time
}

// AutoConfirmer tracks tag passes (keyed by EPC, which is the UII) and
// confirms the ones that qualify when they end.
type AutoConfirmer struct {
	cfg       Config
	bus       *events.EventBus
	store     Store
	suggester Suggester
	confirmer Confirmer
	mode      *Mode
	// Now is the clock for reads without a timestamp, for confirms and for
	// Run's ticks.
	Now func() time.Time

	// Test hooks: afterTick runs after each Run tick, beforeConfirm right
	// before the worker re-checks the mode.
	afterTick     func()
	beforeConfirm func()

	mu     sync.Mutex
	passes map[string]*pass
}

// New creates an auto-confirmer.
func New(cfg Config, bus *events.EventBus, store Store, suggester Suggester, confirmer Confirmer, mode *Mode) *AutoConfirmer {
	return &AutoConfirmer{
		cfg:       cfg,
		bus:       bus,
		store:     store,
		suggester: suggester,
		confirmer: confirmer,
		mode:      mode,
		Now:       time.Now,
		passes:    make(map[string]*pass),
	}
}

// Observe counts one read in the tag's current pass; a read at least the idle
// time after the previous one starts a new pass.
func (a *AutoConfirmer) Observe(ev events.TagDetected) {
	if ev.EPC == "" {
		return
	}
	seen := ev.Timestamp
	if seen.IsZero() {
		seen = a.Now()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.passes[ev.EPC]
	if p == nil || seen.Sub(p.lastSeen) >= a.cfg.Idle {
		p = &pass{}
		a.passes[ev.EPC] = p
	}
	p.reads++
	if seen.After(p.lastSeen) {
		p.lastSeen = seen
	}
	p.lastAntenna = ev.AntennaID
}

// Consumed marks the tag's current pass as confirmed (creating one when the
// tag has no live pass), so it ends without another movement.
func (a *AutoConfirmer) Consumed(ev events.MovementConfirmed) {
	if ev.UII == "" {
		return
	}
	a.markConsumed(ev.UII, a.Now())
}

func (a *AutoConfirmer) markConsumed(uii string, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.passes[uii]
	if p == nil || at.Sub(p.lastSeen) >= a.cfg.Idle {
		p = &pass{}
		a.passes[uii] = p
	}
	p.consumed = true
	if at.After(p.lastSeen) {
		p.lastSeen = at
	}
}

// endPasses removes every pass unread for at least the idle time at now and
// returns the ones that may be confirmed (automatic mode, not consumed).
func (a *AutoConfirmer) endPasses(now time.Time) []ended {
	var done []ended
	a.mu.Lock()
	for uii, p := range a.passes {
		if now.Sub(p.lastSeen) >= a.cfg.Idle {
			if !p.consumed {
				done = append(done, ended{uii, *p, now})
			}
			delete(a.passes, uii)
		}
	}
	a.mu.Unlock()
	if !a.mode.Enabled() {
		return nil
	}
	return done
}

// Tick ends every pass unread for at least the idle time at now. In automatic
// mode a qualifying pass is confirmed; every ended pass is discarded, so the
// next read starts a new one.
func (a *AutoConfirmer) Tick(ctx context.Context, now time.Time) {
	for _, e := range a.endPasses(now) {
		a.confirmPass(ctx, e)
	}
}

func (a *AutoConfirmer) confirmPass(ctx context.Context, e ended) {
	uii, p := e.uii, e.pass
	if p.reads < a.cfg.MinReads {
		return
	}
	kanban, err := a.store.GetKanbanByUII(uii)
	if err != nil {
		log.Printf("[AutoConfirm] Failed to look up kanban for %s: %v", uii, err)
		return
	}
	if kanban == nil {
		return
	}
	user, err := a.store.GetUserByRFIDTag(uii)
	if err != nil {
		log.Printf("[AutoConfirm] Failed to look up user badge %s: %v", uii, err)
		return
	}
	if user != nil {
		return
	}

	// Direction from the tag's known location only, never from an antenna
	// zone; an unknown location (missing row, lookup error) is left for a
	// manual confirm.
	suggested := a.suggester.SuggestAction(uii, "")
	if !strings.HasPrefix(suggested.Reason, "location:") {
		log.Printf("[AutoConfirm] Skipped %s: no known location (%s)", uii, suggested.Reason)
		return
	}
	action := string(suggested.Action)

	if a.beforeConfirm != nil {
		a.beforeConfirm()
	}
	if !a.mode.Enabled() {
		return
	}
	res, err := a.confirmer.Confirm(ctx, confirm.Request{
		UII:        uii,
		Action:     action,
		AntennaID:  p.lastAntenna,
		OccurredAt: p.lastSeen.UTC(),
		Auto:       true,
	})
	if err != nil {
		log.Printf("[AutoConfirm] Failed to confirm %s %s: %v", action, uii, err)
		return
	}
	a.markConsumed(uii, e.now)
	log.Printf("[AutoConfirm] Confirmed %s %s (%s, %d reads)", action, uii, res.Mode, p.reads)
	if a.bus != nil {
		a.bus.PublishAutoConfirmed(events.AutoConfirmed{UII: uii, Action: action, ConfirmationID: res.ConfirmationID})
	}
}

// Run observes bus reads and confirmed movements and ticks every TickInterval
// until ctx is done. Ended passes are confirmed by a separate worker so reads
// keep being observed during VPS I/O; Run returns after the worker stops.
func (a *AutoConfirmer) Run(ctx context.Context) {
	reads := a.bus.Subscribe()
	defer a.bus.Unsubscribe(reads)
	moves := a.bus.SubscribeMovementConfirmed()
	defer a.bus.UnsubscribeMovementConfirmed(moves)
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()
	a.run(ctx, reads, moves, ticker.C)
}

func (a *AutoConfirmer) run(ctx context.Context, reads <-chan events.TagDetected, moves <-chan events.MovementConfirmed, ticks <-chan time.Time) {
	ctx, cancel := context.WithCancel(ctx)
	work := make(chan ended, confirmQueue)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-work:
				a.confirmPass(ctx, e)
			}
		}
	}()
	defer wg.Wait()
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-reads:
			if !ok {
				return
			}
			a.Observe(ev)
		case ev, ok := <-moves:
			if !ok {
				return
			}
			a.Consumed(ev)
		case <-ticks:
			// Every queued event counts before passes are ended.
			if !a.drain(reads, moves) {
				return
			}
			for _, e := range a.endPasses(a.Now()) {
				select {
				case work <- e:
				case <-ctx.Done():
					return
				}
			}
			if a.afterTick != nil {
				a.afterTick()
			}
		}
	}
}

// drain observes every queued read and movement without blocking; it reports
// false when a channel was closed.
func (a *AutoConfirmer) drain(reads <-chan events.TagDetected, moves <-chan events.MovementConfirmed) bool {
	for {
		select {
		case ev, ok := <-reads:
			if !ok {
				return false
			}
			a.Observe(ev)
		case ev, ok := <-moves:
			if !ok {
				return false
			}
			a.Consumed(ev)
		default:
			return true
		}
	}
}
