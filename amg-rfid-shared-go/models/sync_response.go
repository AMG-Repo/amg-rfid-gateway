package models

import (
	"errors"
	"time"
)

// SyncResponse represents the cloud's acknowledgment of a sync request.
type SyncResponse struct {
	Success    bool      `json:"success"`
	Accepted   int       `json:"accepted"`
	Duplicates int       `json:"duplicates"`
	Errors     []string  `json:"errors"`
	NextSyncIn int       `json:"next_sync_in"` // Seconds until next sync
	ServerTime time.Time `json:"server_time"`
}

// Validate checks that the sync response has valid data.
// Uses NEGATIVE PROGRAMMING: check what should NOT be, early returns.
func (r SyncResponse) Validate() error {
	// NEGATIVE: Accepted cannot be negative
	if r.Accepted < 0 {
		return errors.New("accepted cannot be negative")
	}

	// NEGATIVE: Duplicates cannot be negative
	if r.Duplicates < 0 {
		return errors.New("duplicates cannot be negative")
	}

	// NEGATIVE: ServerTime cannot be zero
	if r.ServerTime.IsZero() {
		return errors.New("server_time cannot be zero")
	}

	// HAPPY PATH: All validations passed
	return nil
}
