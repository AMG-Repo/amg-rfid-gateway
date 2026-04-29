package models

import (
	"errors"
	"time"
)

// GatewayStatus represents the possible states of a gateway.
type GatewayStatus string

const (
	StatusOnline  GatewayStatus = "online"
	StatusOffline GatewayStatus = "offline"
	StatusSyncing GatewayStatus = "syncing"
	StatusError   GatewayStatus = "error"
)

// AntennaStatus represents the status of a single antenna.
type AntennaStatus struct {
	ID        string    `json:"id"`
	Connected bool      `json:"connected"`
	LastSeen  time.Time `json:"last_seen"`
}

// Validate checks that the antenna status has valid data.
func (a AntennaStatus) Validate() error {
	// NEGATIVE: ID cannot be empty
	if a.ID == "" {
		return errors.New("antenna id cannot be empty")
	}

	return nil
}

// GatewayHealthStatus represents the health status of a gateway.
type GatewayHealthStatus struct {
	GatewayID string          `json:"gateway_id"`
	CompanyID string          `json:"company_id"`
	Status    string          `json:"status"`
	Antennas  []AntennaStatus `json:"antennas"`
	CacheSize int             `json:"cache_size"`
	LastSync  time.Time       `json:"last_sync"`
	Uptime    int64           `json:"uptime"` // Seconds
	Version   string          `json:"version"`
}

// Validate checks that the health status has valid data.
// Uses NEGATIVE PROGRAMMING: check what should NOT be, early returns.
func (h GatewayHealthStatus) Validate() error {
	// NEGATIVE: GatewayID cannot be empty
	if h.GatewayID == "" {
		return errors.New("gateway_id cannot be empty")
	}

	// NEGATIVE: Status must be valid
	validStatus := false
	for _, s := range []GatewayStatus{StatusOnline, StatusOffline, StatusSyncing, StatusError} {
		if string(s) == h.Status {
			validStatus = true
			break
		}
	}
	if !validStatus {
		return errors.New("invalid status")
	}

	// NEGATIVE: CacheSize cannot be negative
	if h.CacheSize < 0 {
		return errors.New("cache_size cannot be negative")
	}

	// NEGATIVE: Uptime cannot be negative
	if h.Uptime < 0 {
		return errors.New("uptime cannot be negative")
	}

	return nil
}
