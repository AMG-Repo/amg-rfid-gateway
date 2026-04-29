package models

import (
	"testing"
	"time"
)

func TestReading_Validation_RawRSSI_Valid(t *testing.T) {
	// RSSI can now be 0-255 (raw byte value) per REQ-A008
	validReadings := []struct {
		name string
		rssi int
	}{
		{"zero RSSI", 0},
		{"low RSSI", 50},
		{"mid RSSI", 128},
		{"high RSSI (0xC9 = 201)", 201},
		{"max RSSI (0xFF = 255)", 255},
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
	// RSSI must be >= 0 (no negative values when storing raw bytes)
	invalidReadings := []struct {
		name string
		rssi int
	}{
		{"negative RSSI -1", -1},
		{"negative RSSI -50", -50},
		{"negative RSSI -120", -120},
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
		RSSI:      201,
		Timestamp: time.Now(),
	}

	if err := r.Validate(); err != nil {
		t.Errorf("expected reading with GatewayID to be valid, got error: %v", err)
	}

	if r.GatewayID != "gw-pi5-001" {
		t.Errorf("expected GatewayID 'gw-pi5-001', got '%s'", r.GatewayID)
	}
}
