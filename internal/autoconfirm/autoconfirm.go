// Package autoconfirm confirms tag passes by itself when the gateway is in
// automatic mode: a pass ends once the tag goes unread for the idle time, and
// a pass with enough reads of a kanban-linked tag (not a user badge) with a
// known location is confirmed through the shared confirm path.
//
// Reads are observed synchronously by the Tracker, which sits in front of the
// reading-session aggregator. Every confirm, manual or automatic, claims the
// tag's pass on the Tracker before anything is written, so a pass yields at
// most one movement.
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
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// TickInterval is how often Run checks for ended passes.
const TickInterval = time.Second

// SeedWindow is how far back SeedRecent looks for confirmations made before
// a restart.
const SeedWindow = 10 * time.Minute

// confirmQueue bounds the ended passes waiting for the confirm worker; a
// variable so tests can lower it.
var confirmQueue = 64

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

// Sink receives every reading after the Tracker observed it (the reading
// session aggregator).
type Sink interface {
	Store(r models.Reading) error
}

type pass struct {
	id          uint64
	reads       int
	lastSeen    time.Time
	lastAntenna string
	// consumed marks a pass already confirmed (or claimed) manually or
	// automatically: it never yields another movement.
	consumed bool
	// queued marks a pass handed to the confirm worker.
	queued bool
}

// Tracker tracks tag passes (keyed by EPC, which is the UII) from the reads
// the protocol handlers store, and arbitrates every confirm of a pass. It
// implements confirm.Claimer.
type Tracker struct {
	cfg  Config
	next Sink
	mode *Mode
	// Now is the clock for reads without a timestamp and for claims.
	Now func() time.Time

	mu     sync.Mutex
	passes map[string]*pass
	// inFlight maps a tag to the pass of its automatic confirm in progress.
	inFlight map[string]uint64
	// autoDone maps a tag to the pass of its last automatic confirm while the
	// tag is not read again: a manual confirm then is stale.
	autoDone map[string]uint64
	// seeded marks tags confirmed shortly before start: their first pass is
	// consumed.
	seeded map[string]bool
	lastID uint64
}

var _ confirm.Claimer = (*Tracker)(nil)

// NewTracker creates a tracker forwarding every reading to next.
func NewTracker(cfg Config, next Sink, mode *Mode) *Tracker {
	return &Tracker{
		cfg:      cfg,
		next:     next,
		mode:     mode,
		Now:      time.Now,
		passes:   make(map[string]*pass),
		inFlight: make(map[string]uint64),
		autoDone: make(map[string]uint64),
		seeded:   make(map[string]bool),
	}
}

// Store counts the read in its tag's pass, then forwards it unchanged. A read
// at least the idle time after the previous one starts a new pass. It never
// blocks on the auto-confirmer.
func (t *Tracker) Store(r models.Reading) error {
	t.observe(r.EPC, r.AntennaID, r.Timestamp)
	return t.next.Store(r)
}

func (t *Tracker) observe(uii, antenna string, seen time.Time) {
	if uii == "" {
		return
	}
	if seen.IsZero() {
		seen = t.Now()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.autoDone, uii)
	p := t.passes[uii]
	if p == nil || seen.Sub(p.lastSeen) >= t.cfg.Idle {
		p = t.newPass(uii, seen)
	}
	p.reads++
	if seen.After(p.lastSeen) {
		p.lastSeen = seen
	}
	p.lastAntenna = antenna
}

// newPass replaces the tag's pass; the first pass of a seeded tag is
// consumed. Callers hold mu.
func (t *Tracker) newPass(uii string, seen time.Time) *pass {
	t.lastID++
	p := &pass{id: t.lastID, lastSeen: seen, consumed: t.seeded[uii]}
	delete(t.seeded, uii)
	t.passes[uii] = p
	return p
}

// RecentLister lists the confirmations recorded locally.
type RecentLister interface {
	ListConfirmationsBetween(start, end time.Time) ([]localstore.HistoryEntry, error)
}

// SeedRecent marks every tag confirmed (manually or automatically) in the
// SeedWindow before now: its first pass after start is consumed, so a tag
// still being read across a restart is not confirmed again. Call it before
// reads arrive.
func (t *Tracker) SeedRecent(store RecentLister, now time.Time) error {
	entries, err := store.ListConfirmationsBetween(now.Add(-SeedWindow), now.Add(SeedWindow))
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, e := range entries {
		t.seeded[e.UII] = true
	}
	return nil
}

// ClaimAuto claims pass passID of uii for an automatic confirm: it must still
// be the tag's current pass, not consumed, unread for the idle time now, with
// enough reads, in automatic mode. A claimed pass is consumed and in flight
// until the auto-confirmer finishes it.
func (t *Tracker) ClaimAuto(uii string, passID uint64) bool {
	_, ok := t.claimAuto(uii, passID)
	return ok
}

// claimAuto is ClaimAuto returning the claimed pass as it is at the claim. A
// rejected claim of the tag's current pass unqueues it, so a later tick
// reconsiders it.
func (t *Tracker) claimAuto(uii string, passID uint64) (job, bool) {
	now := t.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.passes[uii]
	if p == nil || p.id != passID {
		return job{}, false
	}
	if p.consumed || now.Sub(p.lastSeen) < t.cfg.Idle || p.reads < t.cfg.MinReads || !t.mode.Enabled() {
		p.queued = false
		return job{}, false
	}
	p.consumed = true
	t.inFlight[uii] = passID
	t.autoDone[uii] = passID
	return job{uii, p.id, p.reads, p.lastSeen, p.lastAntenna}, true
}

// ClaimManual is ClaimManualErr reporting only whether the claim succeeded.
func (t *Tracker) ClaimManual(uii string) bool { return t.ClaimManualErr(uii) == nil }

// ClaimManualErr consumes the tag's current pass (a new one when it has none
// live). It is refused while an automatic confirm of the tag is in flight
// (confirm.ErrAutoInProgress) and after one, until the tag is read again
// (confirm.ErrAlreadyAutoConfirmed).
func (t *Tracker) ClaimManualErr(uii string) error {
	now := t.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, busy := t.inFlight[uii]; busy {
		return confirm.ErrAutoInProgress
	}
	if _, done := t.autoDone[uii]; done {
		return confirm.ErrAlreadyAutoConfirmed
	}
	p := t.passes[uii]
	if p == nil || now.Sub(p.lastSeen) >= t.cfg.Idle {
		p = t.newPass(uii, now)
	}
	p.consumed = true
	return nil
}

// finish ends the in-flight state of the automatic claim of pass passID. The
// pass stays consumed whatever the outcome; when nothing was confirmed,
// manual confirms of the tag are accepted again.
func (t *Tracker) finish(uii string, passID uint64, confirmed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inFlight[uii] == passID {
		delete(t.inFlight, uii)
	}
	if !confirmed && t.autoDone[uii] == passID {
		delete(t.autoDone, uii)
	}
}

// drop forgets pass passID of uii when it is still the tag's current pass.
func (t *Tracker) drop(uii string, passID uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p := t.passes[uii]; p != nil && p.id == passID {
		delete(t.passes, uii)
	}
}

// job is an ended pass waiting for the confirm worker.
type job struct {
	uii         string
	passID      uint64
	reads       int
	lastSeen    time.Time
	lastAntenna string
}

// ended returns the passes unread for the idle time at now that may be
// confirmed and are not queued yet; every other ended pass is forgotten.
func (t *Tracker) ended(now time.Time) []job {
	auto := t.mode.Enabled()
	var jobs []job
	t.mu.Lock()
	defer t.mu.Unlock()
	for uii, p := range t.passes {
		if now.Sub(p.lastSeen) < t.cfg.Idle {
			continue
		}
		if _, busy := t.inFlight[uii]; busy {
			continue
		}
		if p.consumed || !auto || p.reads < t.cfg.MinReads {
			delete(t.passes, uii)
			continue
		}
		if !p.queued {
			jobs = append(jobs, job{uii, p.id, p.reads, p.lastSeen, p.lastAntenna})
		}
	}
	return jobs
}

func (t *Tracker) setQueued(uii string, passID uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p := t.passes[uii]; p != nil && p.id == passID {
		p.queued = true
	}
}

// Store is the lookup surface the auto-confirmer needs.
type Store interface {
	GetKanbanByUII(uii string) (*localstore.Kanban, error)
	GetUserByRFIDTag(rfidTag string) (*localstore.User, error)
	HasPendingConfirmation(uii string) (bool, error)
}

// Confirmer registers a movement. Automatic requests are already claimed on
// the Tracker by the auto-confirmer.
type Confirmer interface {
	Confirm(ctx context.Context, req confirm.Request) (confirm.Result, error)
}

// AutoConfirmer confirms the tracked passes that qualify when they end.
type AutoConfirmer struct {
	tracker   *Tracker
	bus       *events.EventBus
	store     Store
	tools     verify.ToolStore
	confirmer Confirmer
	work      chan job
}

// New creates an auto-confirmer for the tracker's passes. The confirmer must
// claim on the same tracker.
func New(tracker *Tracker, bus *events.EventBus, store Store, tools verify.ToolStore, confirmer Confirmer) *AutoConfirmer {
	return &AutoConfirmer{
		tracker:   tracker,
		bus:       bus,
		store:     store,
		tools:     tools,
		confirmer: confirmer,
		work:      make(chan job, confirmQueue),
	}
}

// Tick queues every pass unread for the idle time at now that may be
// confirmed. It never blocks: with a full queue the pass is retried on the
// next tick.
func (a *AutoConfirmer) Tick(now time.Time) {
	for _, j := range a.tracker.ended(now) {
		select {
		case a.work <- j:
			a.tracker.setQueued(j.uii, j.passID)
		default:
			return
		}
	}
}

// confirmPass claims a queued pass first (rejected when it changed since it
// was queued), then decides and confirms it from the values after the claim.
func (a *AutoConfirmer) confirmPass(ctx context.Context, queued job) {
	j, ok := a.tracker.claimAuto(queued.uii, queued.passID)
	if !ok {
		return
	}
	confirmed := false
	defer func() { a.tracker.finish(j.uii, j.passID, confirmed) }()
	action, ok := a.direction(j.uii)
	if !ok {
		// Left for a manual confirm.
		a.tracker.drop(j.uii, j.passID)
		return
	}
	res, err := a.confirmer.Confirm(ctx, confirm.Request{
		UII:        j.uii,
		Action:     string(action),
		AntennaID:  j.lastAntenna,
		OccurredAt: j.lastSeen.UTC(),
		Auto:       true,
		PassID:     j.passID,
	})
	if err != nil {
		log.Printf("[AutoConfirm] Failed to confirm %s %s: %v", action, j.uii, err)
		return
	}
	confirmed = true
	log.Printf("[AutoConfirm] Confirmed %s %s (%s, %d reads)", action, j.uii, res.Mode, j.reads)
	if a.bus != nil {
		a.bus.PublishAutoConfirmed(events.AutoConfirmed{UII: j.uii, Action: string(action), ConfirmationID: res.ConfirmationID})
	}
}

// direction is the movement of a kanban-linked tag (not a user badge) with no
// unsynced confirmation queued and a known, non-blank location; ok is false
// otherwise. A cached location is only trusted when nothing is pending.
func (a *AutoConfirmer) direction(uii string) (verify.Action, bool) {
	kanban, err := a.store.GetKanbanByUII(uii)
	if err != nil {
		log.Printf("[AutoConfirm] Failed to look up kanban for %s: %v", uii, err)
		return "", false
	}
	if kanban == nil {
		return "", false
	}
	user, err := a.store.GetUserByRFIDTag(uii)
	if err != nil {
		log.Printf("[AutoConfirm] Failed to look up user badge %s: %v", uii, err)
		return "", false
	}
	if user != nil {
		return "", false
	}
	pending, err := a.store.HasPendingConfirmation(uii)
	if err != nil {
		log.Printf("[AutoConfirm] Failed to look up pending confirmations of %s: %v", uii, err)
		return "", false
	}
	if pending {
		log.Printf("[AutoConfirm] Skipped %s: a confirmation is pending sync", uii)
		return "", false
	}
	tool, err := a.tools.GetToolByUII(uii)
	if err != nil {
		log.Printf("[AutoConfirm] Failed to look up location of %s: %v", uii, err)
		return "", false
	}
	action, ok := verify.KnownLocationAction(tool)
	if !ok {
		log.Printf("[AutoConfirm] Skipped %s: no known location", uii)
	}
	return action, ok
}

// Run ticks every TickInterval and confirms queued passes in a worker until
// ctx is done; confirms run with a ctx derived from ctx. Run returns after the
// worker stops.
func (a *AutoConfirmer) Run(ctx context.Context) {
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()
	a.run(ctx, ticker.C)
}

func (a *AutoConfirmer) run(ctx context.Context, ticks <-chan time.Time) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-a.work:
				if ctx.Err() != nil {
					return
				}
				a.confirmPass(ctx, j)
			}
		}
	}()
	defer wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			a.Tick(a.tracker.Now())
		}
	}
}
