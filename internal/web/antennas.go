package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/antenna"
)

// Antenna states reported to the panel.
const (
	antennaStateReading      = "reading"
	antennaStateReconnecting = "reconnecting"
	antennaStateStopped      = "stopped"
)

// AntennaStatusProvider reports the live status of the running antenna managers.
type AntennaStatusProvider interface {
	GetAntennaStatuses() []antenna.AntennaStatus
}

// AntennaHealth is the panel view of one configured antenna.
type AntennaHealth struct {
	ID         string     `json:"id"`
	Zone       string     `json:"zone"`
	State      string     `json:"state"`
	LastReadAt *time.Time `json:"last_read_at"`
}

// SetAntennaProvider wires the live antenna statuses into /api/antennas.
func (s *Server) SetAntennaProvider(provider AntennaStatusProvider) {
	s.antennas = provider
}

// handleAntennas returns the health of every configured antenna.
// GET /api/antennas
func (s *Server) handleAntennas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	live := make(map[string]antenna.AntennaStatus)
	if s.antennas != nil {
		for _, status := range s.antennas.GetAntennaStatuses() {
			live[status.ID] = status
		}
	}

	result := []AntennaHealth{}
	if s.config != nil {
		for _, cfg := range s.config.Antennas {
			health := AntennaHealth{ID: cfg.ID, Zone: antennaZone(cfg.Zone), State: antennaStateReconnecting}
			status, running := live[cfg.ID]
			switch {
			case !cfg.Enabled:
				health.State = antennaStateStopped
			case running && status.Connected:
				health.State = antennaStateReading
			}
			if running {
				health.LastReadAt = status.LastReadAt
			}
			result = append(result, health)
		}
	}

	s.jsonResponse(w, result)
}

// antennaZone normalizes a configured zone; anything but entrada/salida is "".
func antennaZone(zone string) string {
	zone = strings.ToLower(strings.TrimSpace(zone))
	if zone == "entrada" || zone == "salida" {
		return zone
	}
	return ""
}
