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

// Aggregator implements antenna.Cache in front of the upload queue.
type Aggregator struct {
	next Store
	idle time.Duration
	max  time.Duration

	mu      sync.Mutex
	open    map[key]*models.Reading
	closing []models.Reading // closed, waiting to be queued (or queue retry)
}

// New returns an Aggregator that closes a session after idle without reads or
// after max since its first read, whichever comes first.
func New(next Store, idle, max time.Duration) *Aggregator {
	return &Aggregator{next: next, idle: idle, max: max, open: map[key]*models.Reading{}}
}

// Store adds one raw read to its session. It only fails for an invalid
// reading: closing a session never queues inline, so a failing queue cannot
// reject the read that triggered the close.
func (a *Aggregator) Store(r models.Reading) error {
	if err := r.Validate(); err != nil {
		return err
	}
	// Compare and persist wall-clock times only: SQLite and JSON drop the
	// monotonic reading, so comparisons must match what is stored and sent.
	r.Timestamp = r.Timestamp.Round(0)
	k := key{r.GatewayID, r.AntennaID, r.EPC}

	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.open[k]; ok {
		if r.Timestamp.Sub(s.Timestamp) < a.max && r.Timestamp.Sub(s.LastSeen) < a.idle && s.ReadCount < maxReadCount {
			if r.Timestamp.After(s.LastSeen) {
				s.LastSeen = r.Timestamp
			}
			s.ReadCount++
			if r.RSSI > s.RSSI {
				s.RSSI = r.RSSI
			}
			return nil
		}
		a.closing = append(a.closing, *s)
		delete(a.open, k)
	}
	session := r
	session.LastSeen = r.Timestamp
	session.ReadCount = 1
	a.open[k] = &session
	return nil
}

// FlushIdle queues every closed session plus every open session whose tag has
// not been read for idle, or that has been open for max, as of now. Sessions
// the queue rejects are kept and retried on the next call.
func (a *Aggregator) FlushIdle(now time.Time) error {
	now = now.Round(0)
	return a.flush(func(s *models.Reading) bool {
		return now.Sub(s.LastSeen) >= a.idle || now.Sub(s.Timestamp) >= a.max
	})
}

// FlushAll queues every session (shutdown).
func (a *Aggregator) FlushAll() error {
	return a.flush(func(*models.Reading) bool { return true })
}

func (a *Aggregator) flush(closed func(*models.Reading) bool) error {
	a.mu.Lock()
	batch := a.closing
	a.closing = nil
	for k, s := range a.open {
		if closed(s) {
			batch = append(batch, *s)
			delete(a.open, k)
		}
	}
	a.mu.Unlock()

	// Queue outside the lock: a slow queue must not block Store on every antenna.
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
		a.closing = append(failed, a.closing...)
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

// ShutdownQueue queues every session and always flushes the queue's write
// buffer, so a following Close persists what was queued even if some
// sessions failed. Call it after the antennas stopped and before closing the
// queue.
func ShutdownQueue(a *Aggregator, queue Flusher) error {
	return errors.Join(a.FlushAll(), queue.Flush())
}
