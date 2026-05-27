package web

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
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
}

// StatusResponse represents the system status.
type StatusResponse struct {
	VPSOnline      bool `json:"vps_online"`
	PendingCount   int  `json:"pending_count"`
	ToolsCount     int  `json:"tools_count"`
	PendingWarning bool `json:"pending_warning"`
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
		s.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate action
	if err := s.verifier.ValidateAction(req.Action); err != nil {
		s.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

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
		err := s.vpsClient.SendConfirmationV2(s.companyID, req.UII, req.Action, antennaID)
		if err == nil {
			s.jsonResponse(w, ConfirmResponse{
				Success: true,
				Message: "Confirmation sent to VPS",
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
			Message: "No local store available",
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
	maxPending := 0
	if s.config != nil && s.config.MaxPendingConfirmations != nil {
		maxPending = *s.config.MaxPendingConfirmations
	}

	_, err := s.store.CreateConfirmation(req.UII, req.Action, antennaID, time.Now(), maxPending)
	if err != nil {
		log.Printf("[Web] Failed to create local confirmation: %v", err)
		s.jsonResponse(w, ConfirmResponse{
			Success: false,
			Message: "Failed to queue confirmation: " + err.Error(),
			Mode:    "offline",
		})
		return
	}

	s.jsonResponse(w, ConfirmResponse{
		Success: true,
		Message: "Confirmation queued locally (offline mode)",
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

	// Calculate pending warning based on threshold
	pendingWarning := false
	if s.config != nil && s.config.PendingWarningThreshold != nil {
		threshold := *s.config.PendingWarningThreshold
		if threshold > 0 && pendingCount >= threshold {
			pendingWarning = true
		}
	} else {
		// Default threshold is 1000
		if pendingCount >= 1000 {
			pendingWarning = true
		}
	}

	s.jsonResponse(w, StatusResponse{
		VPSOnline:      vpsOnline,
		PendingCount:   pendingCount,
		ToolsCount:     toolsCount,
		PendingWarning: pendingWarning,
	})
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
