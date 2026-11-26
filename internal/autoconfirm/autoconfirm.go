// Package autoconfirm confirms tag passes by itself when the gateway is in
// automatic mode: a pass ends once the tag goes unread for the idle time, and
// a pass with enough reads of a kanban-linked tag (not a user badge) is
// confirmed through the shared confirm path, with the direction taken from the
// tag's cached state.
package autoconfirm

import (
	"context"
	"log"
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
	// Now is the clock for reads without a timestamp and for Run's ticks.
	Now func() time.Time

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

// Observe counts one read in the tag's current pass.
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
	if p == nil {
		p = &pass{}
		a.passes[ev.EPC] = p
	}
	p.reads++
	if seen.After(p.lastSeen) {
		p.lastSeen = seen
	}
	p.lastAntenna = ev.AntennaID
}

// Tick ends every pass unread for at least the idle time at now. In automatic
// mode a qualifying pass is confirmed; every ended pass is discarded, so the
// next read starts a new one.
func (a *AutoConfirmer) Tick(ctx context.Context, now time.Time) {
	type ended struct {
		uii string
		pass
	}
	var done []ended
	a.mu.Lock()
	for uii, p := range a.passes {
		if now.Sub(p.lastSeen) >= a.cfg.Idle {
			done = append(done, ended{uii, *p})
			delete(a.passes, uii)
		}
	}
	a.mu.Unlock()

	if !a.mode.Enabled() {
		return
	}
	for _, e := range done {
		a.confirmPass(ctx, e.uii, e.pass)
	}
}

func (a *AutoConfirmer) confirmPass(ctx context.Context, uii string, p pass) {
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

	// Direction from the tag state only, never from an antenna zone.
	action := string(a.suggester.SuggestAction(uii, "").Action)
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
	log.Printf("[AutoConfirm] Confirmed %s %s (%s, %d reads)", action, uii, res.Mode, p.reads)
	if a.bus != nil {
		a.bus.PublishAutoConfirmed(events.AutoConfirmed{UII: uii, Action: action, ConfirmationID: res.ConfirmationID})
	}
}

// Run observes bus reads and ticks every TickInterval until ctx is done.
func (a *AutoConfirmer) Run(ctx context.Context) {
	ch := a.bus.Subscribe()
	defer a.bus.Unsubscribe(ch)
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			a.Observe(ev)
		case <-ticker.C:
			a.Tick(ctx, a.Now())
		}
	}
}
