package web

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	syncpkg "github.com/amg-rfid/amg-rfid-gateway/internal/sync"
	"github.com/amg-rfid/amg-rfid-gateway/internal/uuidutil"
)

const (
	msgConfirmNotAssigned = "No se puede registrar un movimiento hasta asignar la tag a un kanban."
	msgConfirmNoVerify    = "No se pudo verificar la asignación de la tag."
	msgConfirmSent        = "Confirmación enviada al VPS"
	msgConfirmQueued      = "Confirmación guardada localmente (modo sin conexión)"
	msgConfirmNoStore     = "No hay almacén local disponible"
	msgConfirmQueueFailed = "No se pudo guardar la confirmación en el gateway."
	msgConfirmQueueFull   = "La cola del gateway está llena: no se guardó el movimiento. Revisá la conexión con el VPS."
	msgConfirmBadBody     = "Solicitud inválida."
	msgConfirmBadAction   = "Movimiento inválido: debe ser entrada o salida."
	msgConfirmNoUII       = "Falta el código de la tag (uii)."
)

// TagInfo represents tag information for API responses.
type TagInfo struct {
	UII         string `json:"uii"`
	SKU         string `json:"sku,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Location    string `json:"location,omitempty"`
}

// ConfirmRequest represents the confirmation request body.
type ConfirmRequest struct {
	UII       string `json:"uii"`
	Action    string `json:"action"`
	AntennaID string `json:"antenna_id,omitempty"`
}

// ConfirmResponse represents the confirmation response.
type ConfirmResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Mode    string `json:"mode"` // "online" or "offline"
	// Code is set only on rejections, e.g. "tag_not_assigned".
	Code string `json:"code,omitempty"`
}

// StatusResponse represents the system status.
type StatusResponse struct {
	VPSOnline      bool `json:"vps_online"`
	PendingCount   int  `json:"pending_count"`
	ToolsCount     int  `json:"tools_count"`
	PendingWarning bool `json:"pending_warning"`
	// OldestPendingAt is when the oldest pending confirmation was queued (null when none).
	OldestPendingAt *time.Time `json:"oldest_pending_at"`
	// PendingWarningThreshold is the configured queue size that raises pending_warning.
	PendingWarningThreshold int `json:"pending_warning_threshold"`
	// LastSyncAt is when the last confirmation reached the VPS (null when none is recorded).
	LastSyncAt *time.Time `json:"last_sync_at"`
	// MaxPendingConfirmations is the enforced queue cap (0 = unlimited); at the cap
	// a new confirmation is rejected with code queue_full.
	MaxPendingConfirmations int `json:"max_pending_confirmations"`
	// StuckCount is how many pending confirmations reached the sync worker's max
	// retries: they stay queued but are no longer sent.
	StuckCount int `json:"stuck_count"`
}

// AuthModeResponse indicates whether UI write calls require a bearer token.
type AuthModeResponse struct {
	WebAccessMode  string `json:"web_access_mode"`
	RequiresBearer bool   `json:"requires_bearer"`
}

// handleTags returns all cached tools as JSON.
// GET /api/tags
func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Handle nil store
	if s.store == nil {
		s.jsonResponse(w, []TagInfo{})
		return
	}

	// Get unsynced confirmations to extract UIIs (recent tags)
	confirmations, err := s.store.GetUnsyncedConfirmations(1000)
	if err != nil {
		log.Printf("[Web] Failed to get confirmations: %v", err)
		s.jsonError(w, "Failed to retrieve tags", http.StatusInternalServerError)
		return
	}

	// Build unique UIIs
	uiiMap := make(map[string]struct{})
	for _, conf := range confirmations {
		uiiMap[conf.UII] = struct{}{}
	}

	// Get tool details for each UII
	var tags []TagInfo
	for uii := range uiiMap {
		tool, err := s.store.GetToolByUII(uii)
		if err != nil {
			log.Printf("[Web] Failed to get tool for UII %s: %v", uii, err)
			continue
		}

		if tool != nil {
			tags = append(tags, TagInfo{
				UII:         tool.UII,
				SKU:         tool.SKU,
				Name:        tool.Name,
				Description: tool.Description,
				Status:      tool.Status,
				Location:    tool.Location,
			})
		} else {
			tags = append(tags, TagInfo{
				UII:    uii,
				Status: "unknown",
			})
		}
	}

	s.jsonResponse(w, tags)
}

// handleConfirm processes a confirmation request.
// POST /api/confirm
func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req ConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, msgConfirmBadBody, http.StatusBadRequest)
		return
	}

	// Validate action
	if err := s.verifier.ValidateAction(req.Action); err != nil {
		s.jsonError(w, msgConfirmBadAction, http.StatusBadRequest)
		return
	}
	// Validation is case-insensitive; store, send and apply the canonical form.
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))

	// Movements are only registered for tags assigned to a kanban. Checked before
	// anything is sent or queued, online or offline, and fail closed if the
	// assignment cannot be verified.
	if strings.TrimSpace(req.UII) == "" {
		s.jsonError(w, msgConfirmNoUII, http.StatusBadRequest)
		return
	}
	if s.store == nil {
		s.jsonError(w, msgConfirmNoVerify, http.StatusInternalServerError)
		return
	}
	kanban, err := s.store.GetKanbanByUII(req.UII)
	if err != nil {
		log.Printf("[Web] Failed to verify kanban assignment for %s: %v", req.UII, err)
		s.jsonError(w, msgConfirmNoVerify, http.StatusInternalServerError)
		return
	}
	if kanban == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(ConfirmResponse{
			Success: false,
			Message: msgConfirmNotAssigned,
			Code:    "tag_not_assigned",
		})
		return
	}

	// Identify this confirmation once: the online send, the local record and
	// every retry of the queued row carry the same id and instant, so the VPS
	// dedupes replays.
	confirmationID := uuidutil.NewV4()
	occurredAt := time.Now().UTC()

	// Try VPS first (online mode)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Check VPS availability by attempting a lightweight operation
	vpsOnline := s.checkVPSHealth(ctx)

	if vpsOnline {
		// Try to send confirmation to VPS with antenna_id
		antennaID := req.AntennaID
		if antennaID == "" {
			antennaID = "manual"
		}
		err := s.vpsClient.SendIdempotentConfirmation(s.companyID, req.UII, req.Action, antennaID, confirmationID, occurredAt)
		if err == nil {
			// Keep a local record as already synced. The VPS has the confirmation,
			// so a failure here is logged and never fails the request.
			s.applyMovementLocation(req.UII, req.Action)
			if err := s.store.RecordSyncedConfirmationWithID(req.UII, req.Action, antennaID, confirmationID, occurredAt); err != nil {
				log.Printf("[Web] Failed to record synced confirmation for %s: %v", req.UII, err)
			}
			s.jsonResponse(w, ConfirmResponse{
				Success: true,
				Message: msgConfirmSent,
				Mode:    "online",
			})
			return
		}
		log.Printf("[Web] VPS confirmation failed: %v, falling back to local queue", err)
	}

	// Fallback to local queue (offline mode)
	if s.store == nil {
		s.jsonResponse(w, ConfirmResponse{
			Success: false,
			Message: msgConfirmNoStore,
			Mode:    "offline",
		})
		return
	}

	// Use antenna_id from request, fallback to "manual" if not provided
	antennaID := req.AntennaID
	if antennaID == "" {
		antennaID = "manual"
	}

	// Get max pending confirmations from config (0 = unlimited)
	_, err = s.store.CreateConfirmationWithID(req.UII, req.Action, antennaID, confirmationID, occurredAt, s.maxPendingConfirmations())
	if errors.Is(err, localstore.ErrQueueFull) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInsufficientStorage)
		json.NewEncoder(w).Encode(ConfirmResponse{
			Success: false,
			Message: msgConfirmQueueFull,
			Mode:    "offline",
			Code:    "queue_full",
		})
		return
	}
	if err != nil {
		log.Printf("[Web] Failed to create local confirmation: %v", err)
		s.jsonResponse(w, ConfirmResponse{
			Success: false,
			Message: msgConfirmQueueFailed,
			Mode:    "offline",
		})
		return
	}

	s.applyMovementLocation(req.UII, req.Action)
	s.jsonResponse(w, ConfirmResponse{
		Success: true,
		Message: msgConfirmQueued,
		Mode:    "offline",
	})
}

// handleStatus returns system status.
// GET /api/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check VPS health
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	vpsOnline := s.checkVPSHealth(ctx)

	// Get pending count
	pendingCount := 0
	if s.store != nil {
		var err error
		pendingCount, err = s.store.GetPendingConfirmationsCount()
		if err != nil {
			log.Printf("[Web] Failed to get pending count: %v", err)
			pendingCount = 0
		}
	}

	// Get tools count
	toolsCount := 0
	if s.store != nil {
		var err error
		toolsCount, err = s.store.GetToolsCount()
		if err != nil {
			log.Printf("[Web] Failed to get tools count: %v", err)
			toolsCount = 0
		}
	}

	// Queue detail for the panel: oldest pending and last successful send.
	var oldestPendingAt, lastSyncAt *time.Time
	if s.store != nil {
		var err error
		if oldestPendingAt, err = s.store.GetOldestPendingAt(); err != nil {
			log.Printf("[Web] Failed to get oldest pending confirmation: %v", err)
			oldestPendingAt = nil
		}
		if lastSyncAt, err = s.store.GetLastSyncedAt(); err != nil {
			log.Printf("[Web] Failed to get last sync time: %v", err)
			lastSyncAt = nil
		}
	}

	stuckCount := 0
	if s.store != nil {
		var err error
		if stuckCount, err = s.store.GetStuckConfirmationsCount(syncpkg.MaxRetries); err != nil {
			log.Printf("[Web] Failed to count stuck confirmations: %v", err)
			stuckCount = 0
		}
	}

	// Calculate pending warning based on threshold (default 1000, 0 = never warn)
	threshold := 1000
	if s.config != nil && s.config.PendingWarningThreshold != nil {
		threshold = *s.config.PendingWarningThreshold
	}
	pendingWarning := threshold > 0 && pendingCount >= threshold

	s.jsonResponse(w, StatusResponse{
		VPSOnline:               vpsOnline,
		PendingCount:            pendingCount,
		ToolsCount:              toolsCount,
		PendingWarning:          pendingWarning,
		OldestPendingAt:         oldestPendingAt,
		PendingWarningThreshold: threshold,
		LastSyncAt:              lastSyncAt,
		MaxPendingConfirmations: s.maxPendingConfirmations(),
		StuckCount:              stuckCount,
	})
}

// applyMovementLocation updates the cached tag location for an accepted
// confirmation; a failure is logged only, the movement itself is recorded.
func (s *Server) applyMovementLocation(uii, action string) {
	if err := s.store.ApplyMovementLocation(uii, action); err != nil {
		log.Printf("[Web] Failed to update cached location of %s after %s: %v", uii, action, err)
	}
}

// maxPendingConfirmations is the enforced pending queue cap (0 = unlimited).
func (s *Server) maxPendingConfirmations() int {
	if s.config != nil && s.config.MaxPendingConfirmations != nil {
		return *s.config.MaxPendingConfirmations
	}
	return 0
}

// handleAuthMode returns frontend auth requirements for write endpoints.
// GET /api/auth-mode
func (s *Server) handleAuthMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mode := "local"
	if s.config != nil && s.config.WebAccessMode != "" {
		mode = s.config.WebAccessMode
	}

	s.jsonResponse(w, AuthModeResponse{
		WebAccessMode:  mode,
		RequiresBearer: s.shouldRequireWriteToken(),
	})
}

// checkVPSHealth checks if VPS is reachable by making an actual HTTP GET request.
func (s *Server) checkVPSHealth(ctx context.Context) bool {
	if s.vpsClient == nil {
		return false
	}

	// Create a timeout context for the health check
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Make an actual HTTP GET request to the VPS health endpoint
	// Even a 401/404 response means the VPS is reachable
	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, s.vpsClient.BaseURL()+"/health", nil)
	if err != nil {
		return false
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Any HTTP response means VPS is reachable (even error status codes)
	return true
}

// GetRecentTags returns the most recent pending confirmations as tags.
// This is a helper method for the frontend to get tags currently in the queue.
func (s *Server) GetRecentTags(limit int) ([]localstore.PendingConfirmation, error) {
	return s.store.GetUnsyncedConfirmations(limit)
}
