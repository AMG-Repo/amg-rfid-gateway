package web

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

// defaultUnassignedFlushInterval is how often coalesced reads are written to the
// unassigned_tags inbox. Antennas report the same tag many times per second, so
// reads are counted in memory and flushed in one upsert per EPC.
const defaultUnassignedFlushInterval = time.Second

// unassignedBatch accumulates the reads of one EPC between two flushes.
type unassignedBatch struct {
	antennaID string
	lastSeen  time.Time
	reads     int
}

// handleUnassignedTags returns the tags read but not assigned to any kanban.
// GET /api/unassigned-tags
func (s *Server) handleUnassignedTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.store == nil {
		s.jsonResponse(w, []struct{}{})
		return
	}

	now := time.Now
	if s.now != nil {
		now = s.now
	}
	if _, err := s.store.PurgeUnassignedTagsBefore(now().Add(-localstore.UnassignedTagTTL)); err != nil {
		log.Printf("[Web] Failed to expire unassigned tags: %v", err)
	}

	tags, err := s.store.ListUnassignedTags()
	if err != nil {
		log.Printf("[Web] Failed to list unassigned tags: %v", err)
		s.jsonError(w, "Failed to retrieve unassigned tags", http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, tags)
}

// startUnassignedRecorder subscribes to tag reads and keeps the unassigned_tags
// inbox up to date, independently of any SSE client being connected.
// It is a no-op without a store or event bus. It stops when ctx is cancelled.
func (s *Server) startUnassignedRecorder(ctx context.Context) {
	if s.store == nil || s.eventBus == nil {
		return
	}

	interval := s.unassignedFlushInterval
	if interval <= 0 {
		interval = defaultUnassignedFlushInterval
	}

	ch := s.eventBus.Subscribe()
	go func() {
		pending := make(map[string]*unassignedBatch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case event, ok := <-ch:
				if !ok {
					s.flushUnassigned(pending)
					return
				}
				addUnassignedRead(pending, event)
			case <-ticker.C:
				s.flushUnassigned(pending)
			case <-ctx.Done():
				s.flushUnassigned(pending)
				s.eventBus.Unsubscribe(ch)
				return
			}
		}
	}()
}

func addUnassignedRead(pending map[string]*unassignedBatch, event events.TagDetected) {
	seen := event.Timestamp
	if seen.IsZero() {
		seen = time.Now()
	}
	batch, ok := pending[event.EPC]
	if !ok {
		batch = &unassignedBatch{}
		pending[event.EPC] = batch
	}
	batch.reads++
	batch.antennaID = event.AntennaID
	if seen.After(batch.lastSeen) {
		batch.lastSeen = seen
	}
}

// flushUnassigned writes the coalesced reads of EPCs that have no active kanban link.
// User badges are skipped: they are read by the same antennas but are not tools.
func (s *Server) flushUnassigned(pending map[string]*unassignedBatch) {
	for epc, batch := range pending {
		delete(pending, epc)

		kanban, err := s.store.GetKanbanByUII(epc)
		if err != nil {
			log.Printf("[Web] Failed to check kanban for %s: %v", epc, err)
			continue
		}
		if kanban != nil {
			continue
		}

		user, err := s.store.GetUserByRFIDTag(epc)
		if err != nil {
			log.Printf("[Web] Failed to check user badge for %s: %v", epc, err)
			continue
		}
		if user != nil {
			continue
		}

		if err := s.store.UpsertUnassignedTag(epc, batch.antennaID, batch.lastSeen, batch.reads); err != nil {
			log.Printf("[Web] Failed to record unassigned tag %s: %v", epc, err)
		}
	}
}
