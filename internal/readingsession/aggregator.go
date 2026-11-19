// Package readingsession merges repeated reads of the same tag into one
// reading session before they reach the upload queue.
//
// A reader reports a tag tens of times per second while it stays in range.
// The Aggregator keeps one open session per (gateway, antenna, EPC): the first
// read sets Timestamp, later reads move LastSeen, raise ReadCount and keep the
// highest RSSI. A session is closed once the tag has not been read for the
// idle window, once it has been open for the max window, or on shutdown, and
// is then handed to the queue.
package readingsession

import (
	"context"
	"errors"
	"log"
	"math"
	"sync"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// maxReadCount is the highest read_count the VPS accepts; a session closes
// before exceeding it. A variable so tests can lower it.
var maxReadCount = math.MaxInt32

// maxClosing caps the sessions waiting for a queue retry; beyond it the
// oldest are dropped (logged). A variable so tests can lower it.
var maxClosing = 10000

// Store is the queue that receives closed sessions (antenna.Cache).
type Store interface {
	Store(r models.Reading) error
}

// Flusher is the queue's write buffer (cache.SQLite.Flush).
type Flusher interface {
	Flush() error
}

type key struct {
	gateway, antenna, epc string
}

// session is one open session. reading carries wall-clock times only, as
// stored and sent; firstAt and lastAt keep the original read times, whose
// monotonic readings (time.Now) make the idle and max ages immune to wall
// clock steps.
type session struct {
	reading models.Reading
	firstAt time.Time
	lastAt  time.Time
}

// Aggregator implements antenna.Cache in front of the upload queue.
type Aggregator struct {
	next Store
	idle time.Duration
	max  time.Duration

	// flushMu serializes flushes, so a final flush waits for an in-flight
	// tick and retries keep their order. Store never takes it.
	flushMu sync.Mutex

	mu      sync.Mutex
	open    map[key]*session
	closing []models.Reading // closed, waiting to be queued (or queue retry)
	stopped bool
}

// New returns an Aggregator that closes a session after idle without reads or
// after max since its first read, whichever comes first.
func New(next Store, idle, max time.Duration) *Aggregator {
	return &Aggregator{next: next, idle: idle, max: max, open: map[key]*session{}}
}

// Store adds one raw read to its session. It only fails for an invalid
// reading, or after shutdown when the read goes straight to the queue:
// closing a session never queues inline, so a failing queue cannot reject the
// read that triggered the close.
func (a *Aggregator) Store(r models.Reading) error {
	if err := r.Validate(); err != nil {
		return err
	}
	at := r.Timestamp
	r.Timestamp = r.Timestamp.Round(0)
	k := key{r.GatewayID, r.AntennaID, r.EPC}

	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return a.next.Store(r)
	}
	defer a.mu.Unlock()
	if s, ok := a.open[k]; ok {
		if at.Sub(s.firstAt) < a.max && at.Sub(s.lastAt) < a.idle && s.reading.ReadCount < maxReadCount {
			if at.After(s.lastAt) {
				s.lastAt = at
			}
			// The wire needs last_seen >= timestamp even if the wall clock
			// stepped back, so LastSeen only moves forward.
			if r.Timestamp.After(s.reading.LastSeen) {
				s.reading.LastSeen = r.Timestamp
			}
			s.reading.ReadCount++
			// RSSI is signed dBm with 0 = unknown: keep the strongest known
			// read, and never let an unknown read replace a known one.
			if r.RSSI != 0 && (s.reading.RSSI == 0 || r.RSSI > s.reading.RSSI) {
				s.reading.RSSI = r.RSSI
			}
			return nil
		}
		a.addClosing(s.reading)
		delete(a.open, k)
	}
	r.LastSeen = r.Timestamp
	r.ReadCount = 1
	a.open[k] = &session{reading: r, firstAt: at, lastAt: at}
	return nil
}

// addClosing appends a closed session to the retry list, dropping the oldest
// beyond maxClosing. Callers hold mu.
func (a *Aggregator) addClosing(rs ...models.Reading) {
	a.closing = append(a.closing, rs...)
	if over := len(a.closing) - maxClosing; over > 0 {
		log.Printf("[Sessions] retry list full: dropping %d oldest sessions", over)
		a.closing = append([]models.Reading(nil), a.closing[over:]...)
	}
}

// FlushIdle queues every closed session plus every open session whose tag has
// not been read for idle, or that has been open for max, as of now. Sessions
// the queue rejects are kept and retried on the next call. After
// ShutdownQueue it does nothing: only the final flush may touch the queue.
func (a *Aggregator) FlushIdle(now time.Time) error {
	return a.flush(false, func(s *session) bool {
		return now.Sub(s.lastAt) >= a.idle || now.Sub(s.firstAt) >= a.max
	})
}

// FlushAll queues every session (shutdown).
func (a *Aggregator) FlushAll() error {
	return a.flush(true, func(*session) bool { return true })
}

func (a *Aggregator) flush(final bool, closed func(*session) bool) error {
	a.flushMu.Lock()
	defer a.flushMu.Unlock()

	a.mu.Lock()
	if a.stopped && !final {
		a.mu.Unlock()
		return nil
	}
	batch := a.closing
	a.closing = nil
	for k, s := range a.open {
		if closed(s) {
			batch = append(batch, s.reading)
			delete(a.open, k)
		}
	}
	a.mu.Unlock()

	// Queue outside mu: a slow queue must not block Store on every antenna.
	var errs []error
	var failed []models.Reading
	for _, s := range batch {
		if err := a.next.Store(s); err != nil {
			errs = append(errs, err)
			failed = append(failed, s)
		}
	}
	if len(failed) > 0 {
		a.mu.Lock()
		rest := a.closing
		a.closing = nil
		a.addClosing(failed...)
		a.addClosing(rest...)
		a.mu.Unlock()
	}
	return errors.Join(errs...)
}

// Run closes idle sessions every tick until ctx ends.
func (a *Aggregator) Run(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if err := a.FlushIdle(now); err != nil {
				log.Printf("[Sessions] failed to queue closed sessions: %v", err)
			}
		}
	}
}

// ShutdownQueue stops session tracking (later reads go straight to the
// queue), queues every session after any in-flight tick, and always flushes
// the queue's write buffer, so a following Close persists what was queued.
// Call it after the antennas stopped and before closing the queue.
func ShutdownQueue(a *Aggregator, queue Flusher) error {
	a.mu.Lock()
	a.stopped = true
	a.mu.Unlock()
	return errors.Join(a.FlushAll(), queue.Flush())
}
