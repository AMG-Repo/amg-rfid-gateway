package models

import (
	"testing"
	"time"
)

func TestReading_Validation_RawRSSI_Valid(t *testing.T) {
	// RSSI is signed integer dBm (<= 0, 0 = unknown) per REQ-A008
	validReadings := []struct {
		name string
		rssi int
	}{
		{"zero RSSI (unknown)", 0},
		{"strongest RSSI -1", -1},
		{"generic 0xC9 = -55 dBm", -55},
		{"chainway -66 dBm", -66},
		{"generic 0x80 = -128 dBm", -128},
		{"chainway floor -199 dBm", -199},
	}

	for _, tc := range validReadings {
		t.Run(tc.name, func(t *testing.T) {
			r := Reading{
				AntennaID: "ANT-001",
				GatewayID: "gw-001",
				EPC:       "E200341502001080220A0C64",
				RSSI:      tc.rssi,
				Timestamp: time.Now(),
			}

			if err := r.Validate(); err != nil {
				t.Errorf("expected RSSI %d to be valid, got error: %v", tc.rssi, err)
			}
		})
	}
}

func TestReading_Validation_RawRSSI_Invalid(t *testing.T) {
	// RSSI must be <= 0 dBm (positive values are the retired raw/magnitude scale)
	invalidReadings := []struct {
		name string
		rssi int
	}{
		{"positive RSSI 1", 1},
		{"positive RSSI 55", 55},
		{"old raw byte 201", 201},
		{"old raw byte 255", 255},
	}

	for _, tc := range invalidReadings {
		t.Run(tc.name, func(t *testing.T) {
			r := Reading{
				AntennaID: "ANT-001",
				GatewayID: "gw-001",
				EPC:       "E200341502001080220A0C64",
				RSSI:      tc.rssi,
				Timestamp: time.Now(),
			}

			if err := r.Validate(); err == nil {
				t.Errorf("expected RSSI %d to be invalid", tc.rssi)
			}
		})
	}
}

func TestReading_Validation_GatewayIDPopulated(t *testing.T) {
	// GatewayID is required per REQ-A009
	r := Reading{
		AntennaID: "ANT-001",
		GatewayID: "gw-pi5-001",
		EPC:       "E200341502001080220A0C64",
		RSSI:      -55,
		Timestamp: time.Now(),
	}

	if err := r.Validate(); err != nil {
		t.Errorf("expected reading with GatewayID to be valid, got error: %v", err)
	}

	if r.GatewayID != "gw-pi5-001" {
		t.Errorf("expected GatewayID 'gw-pi5-001', got '%s'", r.GatewayID)
	}
}
