// Package readingsession merges repeated reads of the same tag into one
// reading session before they reach the upload queue.
//
// A reader reports a tag tens of times per second while it stays in range.
// The Aggregator keeps one open session per (gateway, antenna, EPC): the first
// read sets Timestamp, later reads move LastSeen, raise ReadCount and keep the
// highest RSSI. A session is handed to the queue once the tag has not been
// read for the idle window, once it has been open for the max window, or on
// shutdown.
package readingsession

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// Store is the queue that receives closed sessions (antenna.Cache).
type Store interface {
	Store(r models.Reading) error
}

type key struct {
	gateway, antenna, epc string
}

// Aggregator implements antenna.Cache in front of the upload queue.
type Aggregator struct {
	next Store
	idle time.Duration
	max  time.Duration

	mu   sync.Mutex
	open map[key]*models.Reading
}

// New returns an Aggregator that closes a session after idle without reads or
// after max since its first read, whichever comes first.
func New(next Store, idle, max time.Duration) *Aggregator {
	return &Aggregator{next: next, idle: idle, max: max, open: map[key]*models.Reading{}}
}

// Store adds one raw read to its session. A read that falls max or more after
// the session's first read closes that session and starts a new one.
func (a *Aggregator) Store(r models.Reading) error {
	if err := r.Validate(); err != nil {
		return err
	}
	k := key{r.GatewayID, r.AntennaID, r.EPC}

	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.open[k]; ok {
		if r.Timestamp.Sub(s.Timestamp) < a.max {
			if r.Timestamp.After(s.LastSeen) {
				s.LastSeen = r.Timestamp
			}
			s.ReadCount++
			if r.RSSI > s.RSSI {
				s.RSSI = r.RSSI
			}
			return nil
		}
		if err := a.next.Store(*s); err != nil {
			return err
		}
		delete(a.open, k)
	}
	session := r
	session.LastSeen = r.Timestamp
	session.ReadCount = 1
	a.open[k] = &session
	return nil
}

// FlushIdle queues every session whose tag has not been read for idle, or
// that has been open for max, as of now. A session the queue rejects stays
// open and is retried on the next call.
func (a *Aggregator) FlushIdle(now time.Time) error {
	return a.flush(func(s *models.Reading) bool {
		return now.Sub(s.LastSeen) >= a.idle || now.Sub(s.Timestamp) >= a.max
	})
}

// FlushAll queues every open session (shutdown).
func (a *Aggregator) FlushAll() error {
	return a.flush(func(*models.Reading) bool { return true })
}

func (a *Aggregator) flush(closed func(*models.Reading) bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var errs []error
	for k, s := range a.open {
		if !closed(s) {
			continue
		}
		if err := a.next.Store(*s); err != nil {
			errs = append(errs, err)
			continue
		}
		delete(a.open, k)
	}
	return errors.Join(errs...)
}

// Run closes idle sessions every tick until ctx ends, then queues the
// sessions still open.
func (a *Aggregator) Run(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := a.FlushAll(); err != nil {
				log.Printf("[Sessions] failed to queue open sessions on stop: %v", err)
			}
			return
		case now := <-t.C:
			if err := a.FlushIdle(now); err != nil {
				log.Printf("[Sessions] failed to queue closed sessions: %v", err)
			}
		}
	}
}

// Flusher is the queue's write buffer (cache.SQLite.Flush).
type Flusher interface {
	Flush() error
}

// ShutdownQueue queues every open session and flushes the queue's write
// buffer, so a following Close persists them. Call it after the antennas
// stopped and before closing the queue.
func ShutdownQueue(a *Aggregator, queue Flusher) error {
	if err := a.FlushAll(); err != nil {
		return err
	}
	return queue.Flush()
}
