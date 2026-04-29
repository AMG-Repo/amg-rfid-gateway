package models

import (
	"errors"
	"time"
)

// Reading represents a single RFID tag reading from an antenna.
type Reading struct {
	AntennaID  string    `json:"antenna_id"`
	GatewayID  string    `json:"gateway_id"`
	EPC        string    `json:"epc"`
	RSSI       int       `json:"rssi"`
	Timestamp  time.Time `json:"timestamp"`
	Synced     bool      `json:"synced"`
	RetryCount int       `json:"retry_count"`
}

// Validate checks that the reading has valid data.
// Uses NEGATIVE PROGRAMMING: check what should NOT be, early returns.
func (r Reading) Validate() error {
	// NEGATIVE: AntennaID cannot be empty
	if r.AntennaID == "" {
		return errors.New("antenna_id cannot be empty")
	}

	// NEGATIVE: EPC cannot be empty
	if r.EPC == "" {
		return errors.New("epc cannot be empty")
	}

	// NEGATIVE: RSSI must be >= 0 (raw byte value 0-255) per REQ-A008
	if r.RSSI < 0 {
		return errors.New("rssi cannot be negative")
	}

	// NEGATIVE: Timestamp cannot be zero
	if r.Timestamp.IsZero() {
		return errors.New("timestamp cannot be zero")
	}

	// HAPPY PATH: All validations passed
	return nil
}

// TableName returns the database table name for this model.
func (r Reading) TableName() string {
	return "readings"
}
