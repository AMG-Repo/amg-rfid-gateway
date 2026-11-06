package web

import (
	"log"
	"net/http"

	syncpkg "github.com/amg-rfid/amg-rfid-gateway/internal/sync"
)

// handleRequeue resets the retry count of every pending confirmation that
// reached the sync worker's max retries so it is sent again.
// POST /api/confirmations/requeue -> {"requeued": N}
func (s *Server) handleRequeue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		s.jsonError(w, "No hay almacén local disponible.", http.StatusServiceUnavailable)
		return
	}

	requeued, err := s.store.RequeueStuckConfirmations(syncpkg.MaxRetries)
	if err != nil {
		log.Printf("[Web] Failed to requeue stuck confirmations: %v", err)
		s.jsonError(w, "No se pudieron reencolar las confirmaciones.", http.StatusInternalServerError)
		return
	}
	if requeued > 0 {
		log.Printf("[Web] Requeued %d stuck confirmations", requeued)
	}
	s.jsonResponse(w, map[string]int64{"requeued": requeued})
}
