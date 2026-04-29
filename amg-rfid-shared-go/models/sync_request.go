package models

import (
	"errors"
	"fmt"
	"time"
)

// SyncRequest represents a batch of readings sent from Gateway to Cloud.
type SyncRequest struct {
	GatewayID string    `json:"gateway_id"`
	CompanyID string    `json:"company_id"`
	Readings  []Reading `json:"readings"`
	Timestamp time.Time `json:"timestamp"`
}

// Validate checks that the sync request has valid data.
// Uses NEGATIVE PROGRAMMING: check what should NOT be, early returns.
func (r SyncRequest) Validate() error {
	// NEGATIVE: GatewayID cannot be empty
	if r.GatewayID == "" {
		return errors.New("gateway_id cannot be empty")
	}

	// NEGATIVE: CompanyID cannot be empty
	if r.CompanyID == "" {
		return errors.New("company_id cannot be empty")
	}

	// NEGATIVE: Timestamp cannot be zero
	if r.Timestamp.IsZero() {
		return errors.New("timestamp cannot be zero")
	}

	// NEGATIVE: Readings cannot be empty
	if len(r.Readings) == 0 {
		return errors.New("readings cannot be empty")
	}

	// NEGATIVE: Validate each reading
	for i, reading := range r.Readings {
		if err := reading.Validate(); err != nil {
			return fmt.Errorf("reading at index %d is invalid: %w", i, err)
		}
	}

	// HAPPY PATH: All validations passed
	return nil
}
