package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

// SyncTrigger asks the tools sync to refresh right away (implemented by sync.ToolsSync).
type SyncTrigger interface {
	RequestSync()
}

// SetSyncTrigger wires the sync that is refreshed after a successful assignment.
func (s *Server) SetSyncTrigger(trigger SyncTrigger) {
	s.syncTrigger = trigger
}

// AssignRequest is the body of POST /api/tags/assign.
type AssignRequest struct {
	UII        string `json:"uii"`
	KanbanCode string `json:"kanban_code"`
	AntennaID  string `json:"antenna_id,omitempty"`
}

// AssignResponse is the success body of POST /api/tags/assign.
type AssignResponse struct {
	Success bool               `json:"success"`
	Message string             `json:"message"`
	UII     string             `json:"uii"`
	Kanban  *localstore.Kanban `json:"kanban"`
}

// AssignErrorResponse is the error body of POST /api/tags/assign.
type AssignErrorResponse struct {
	Error         string `json:"error"`
	Code          string `json:"code"`
	CurrentKanban string `json:"current_kanban,omitempty"`
}

const (
	msgAssignOffline   = "Sin conexión con el VPS: la asignación requiere estar en línea."
	msgAssignNoStore   = "Almacenamiento local no disponible."
	msgAssignNotFound  = "El kanban no existe en el VPS."
	msgAssignRejected  = "El VPS rechazó la solicitud de asignación."
	msgAssignVPSFailed = "El VPS no pudo procesar la asignación. Intenta de nuevo."
)

func (s *Server) assignError(w http.ResponseWriter, status int, resp AssignErrorResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

// handleAssignTag assigns an unassigned tag to a kanban.
// POST /api/tags/assign
//
// Assignment registers the tag on the VPS, so it is synchronous and online-only:
// there is no offline queue and nothing is stored locally unless the VPS accepts.
// The kanban code is forwarded as-is (the VPS is authoritative; the local
// catalog may be stale) and a VPS 404 is mapped to a clear response.
func (s *Server) handleAssignTag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AssignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.assignError(w, http.StatusBadRequest, AssignErrorResponse{Error: "Solicitud inválida.", Code: "invalid_request"})
		return
	}
	req.UII = strings.TrimSpace(req.UII)
	req.KanbanCode = strings.TrimSpace(req.KanbanCode)
	if req.UII == "" || req.KanbanCode == "" {
		s.assignError(w, http.StatusBadRequest, AssignErrorResponse{Error: "Se requieren uii y kanban_code.", Code: "invalid_request"})
		return
	}

	// Nothing can be recorded without the local store: fail before touching the VPS.
	if s.store == nil {
		s.assignError(w, http.StatusInternalServerError, AssignErrorResponse{Error: msgAssignNoStore, Code: "no_local_store"})
		return
	}
	if s.vpsClient == nil {
		s.assignError(w, http.StatusServiceUnavailable, AssignErrorResponse{Error: msgAssignOffline, Code: "vps_offline"})
		return
	}

	kanban, err := s.vpsClient.AssignTag(r.Context(), s.companyID, req.UII, req.KanbanCode, s.assignAntenna(req))
	if err != nil {
		s.writeAssignFailure(w, err)
		return
	}

	// The VPS may omit the kanban body; fall back to the code that was requested.
	if kanban == nil {
		kanban = &localstore.Kanban{Code: req.KanbanCode}
	}
	if strings.TrimSpace(kanban.Code) == "" {
		kanban.Code = req.KanbanCode
	}

	// The assignment is done on the VPS; a local cache failure heals on the next sync.
	if err := s.store.AssignKanban(req.UII, *kanban); err != nil {
		log.Printf("[Web] Assignment accepted by VPS but local cache update failed for %s: %v", req.UII, err)
	}
	if s.syncTrigger != nil {
		s.syncTrigger.RequestSync()
	}

	s.jsonResponse(w, AssignResponse{
		Success: true,
		Message: fmt.Sprintf("Tag asignada al kanban %s.", kanban.Code),
		UII:     req.UII,
		Kanban:  kanban,
	})
}

// assignAntenna picks the antenna recorded for audit: the request value, else
// the antenna that last read the tag (unassigned inbox), else "manual".
func (s *Server) assignAntenna(req AssignRequest) string {
	if req.AntennaID != "" {
		return req.AntennaID
	}
	if tags, err := s.store.ListUnassignedTags(); err == nil {
		for _, tag := range tags {
			if tag.EPC == req.UII && tag.AntennaID != "" {
				return tag.AntennaID
			}
		}
	}
	return "manual"
}

// writeAssignFailure maps a VPS assignment failure to a clear HTTP response.
func (s *Server) writeAssignFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, httpclient.ErrVPSUnavailable) {
		log.Printf("[Web] Tag assignment failed, VPS unreachable: %v", err)
		s.assignError(w, http.StatusServiceUnavailable, AssignErrorResponse{Error: msgAssignOffline, Code: "vps_offline"})
		return
	}

	var assignErr *httpclient.AssignError
	if !errors.As(err, &assignErr) {
		log.Printf("[Web] Tag assignment failed: %v", err)
		s.assignError(w, http.StatusBadGateway, AssignErrorResponse{Error: msgAssignVPSFailed, Code: "vps_error"})
		return
	}

	switch assignErr.StatusCode {
	case http.StatusNotFound:
		s.assignError(w, http.StatusNotFound, AssignErrorResponse{Error: msgAssignNotFound, Code: "kanban_not_found"})
	case http.StatusConflict:
		msg := "La tag ya está asignada a otro kanban."
		if assignErr.CurrentKanban != "" {
			msg = fmt.Sprintf("La tag ya está asignada al kanban %s.", assignErr.CurrentKanban)
		}
		s.assignError(w, http.StatusConflict, AssignErrorResponse{Error: msg, Code: "uii_assigned", CurrentKanban: assignErr.CurrentKanban})
	case http.StatusBadRequest:
		s.assignError(w, http.StatusBadRequest, AssignErrorResponse{Error: msgAssignRejected, Code: "invalid_request"})
	default:
		log.Printf("[Web] Tag assignment failed: %v", assignErr)
		s.assignError(w, http.StatusBadGateway, AssignErrorResponse{Error: msgAssignVPSFailed, Code: "vps_error"})
	}
}

// handleKanbans returns the cached kanban catalog (read-only copy of the VPS data)
// used by the assignment picker.
// GET /api/kanbans
func (s *Server) handleKanbans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.store == nil {
		s.jsonResponse(w, []struct{}{})
		return
	}

	kanbans, err := s.store.ListKanbans()
	if err != nil {
		log.Printf("[Web] Failed to list kanbans: %v", err)
		s.jsonError(w, "Failed to retrieve kanbans", http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, kanbans)
}
