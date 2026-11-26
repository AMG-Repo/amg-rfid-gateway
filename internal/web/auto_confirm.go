package web

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/amg-rfid/amg-rfid-gateway/internal/autoconfirm"
	"github.com/amg-rfid/amg-rfid-gateway/internal/confirm"
)

// AutoConfirmRequest switches the confirmation mode.
type AutoConfirmRequest struct {
	Enabled *bool `json:"enabled"`
}

// AutoConfirmResponse reports the active confirmation mode.
type AutoConfirmResponse struct {
	AutoConfirm bool `json:"auto_confirm"`
}

// SetAutoConfirmMode shares the auto-confirmer's mode with the panel.
func (s *Server) SetAutoConfirmMode(mode *autoconfirm.Mode) {
	if mode != nil {
		s.autoMode = mode
	}
}

// SetConfirmClaimer makes manual confirms claim the tag's pass, so they
// never duplicate an automatic one.
func (s *Server) SetConfirmClaimer(c confirm.Claimer) {
	s.claimer = c
}

// AutoConfirmMode returns the confirmation mode the panel switches.
func (s *Server) AutoConfirmMode() *autoconfirm.Mode {
	return s.autoMode
}

// handleAutoConfirm persists the confirmation mode, then applies it.
// POST /api/auto-confirm {"enabled": bool}
func (s *Server) handleAutoConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req AutoConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		s.jsonError(w, msgConfirmBadBody, http.StatusBadRequest)
		return
	}
	s.autoModeMu.Lock()
	defer s.autoModeMu.Unlock()
	if s.store != nil {
		if err := s.store.SetAutoConfirmEnabled(*req.Enabled); err != nil {
			log.Printf("[Web] Failed to persist auto-confirm mode: %v", err)
			s.jsonError(w, "No se pudo guardar el modo de confirmación.", http.StatusInternalServerError)
			return
		}
	}
	s.autoMode.Set(*req.Enabled)
	log.Printf("[Web] Auto-confirm mode set to %v", *req.Enabled)
	s.jsonResponse(w, AutoConfirmResponse{AutoConfirm: *req.Enabled})
}
