package web

import (
	"log"
	"net/http"
	"time"
)

// HistoryItem is one confirmed movement for the panel. The kanban fields are
// null when the tag is not linked to a kanban.
type HistoryItem struct {
	UII         string    `json:"uii"`
	KanbanCode  *string   `json:"kanban_code"`
	Description *string   `json:"description"`
	Quantity    *float64  `json:"quantity"`
	Unit        *string   `json:"unit"`
	Action      string    `json:"action"`
	AntennaID   string    `json:"antenna_id"`
	Time        time.Time `json:"time"`
	Synced      bool      `json:"synced"`
}

// handleHistory returns today's confirmed movements (gateway local day), newest first.
// GET /api/history
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	items := []HistoryItem{}
	if s.store == nil {
		s.jsonResponse(w, items)
		return
	}

	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	entries, err := s.store.ListConfirmationsBetween(start, start.AddDate(0, 0, 1))
	if err != nil {
		log.Printf("[Web] Failed to list history: %v", err)
		s.jsonError(w, "Failed to retrieve history", http.StatusInternalServerError)
		return
	}

	for _, e := range entries {
		items = append(items, HistoryItem{
			UII:         e.UII,
			KanbanCode:  e.KanbanCode,
			Description: e.Description,
			Quantity:    e.StandardPack,
			Unit:        e.Unit,
			Action:      e.Action,
			AntennaID:   e.AntennaID,
			Time:        e.Timestamp,
			Synced:      e.Synced,
		})
	}
	s.jsonResponse(w, items)
}
