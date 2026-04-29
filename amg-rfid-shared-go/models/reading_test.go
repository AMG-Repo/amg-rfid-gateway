package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestReading_Validation_EmptyAntennaID(t *testing.T) {
	// NEGATIVE: AntennaID cannot be empty
	r := Reading{
		AntennaID: "",
		EPC:       "E200341502001080220A0C64",
		RSSI:      -45,
		Timestamp: time.Now(),
	}

	if err := r.Validate(); err == nil {
		t.Error("expected error for empty AntennaID")
	}
}

func TestReading_Validation_EmptyEPC(t *testing.T) {
	// NEGATIVE: EPC cannot be empty
	r := Reading{
		AntennaID: "ANT-001",
		EPC:       "",
		RSSI:      -45,
		Timestamp: time.Now(),
	}

	if err := r.Validate(); err == nil {
		t.Error("expected error for empty EPC")
	}
}

func TestReading_Validation_InvalidRSSI(t *testing.T) {
	// NEGATIVE: RSSI cannot be less than -100
	r := Reading{
		AntennaID: "ANT-001",
		EPC:       "E200341502001080220A0C64",
		RSSI:      -150,
		Timestamp: time.Now(),
	}

	if err := r.Validate(); err == nil {
		t.Error("expected error for invalid RSSI")
	}
}

func TestReading_Validation_ZeroTimestamp(t *testing.T) {
	// NEGATIVE: Timestamp cannot be zero
	r := Reading{
		AntennaID: "ANT-001",
		EPC:       "E200341502001080220A0C64",
		RSSI:      -45,
		Timestamp: time.Time{},
	}

	if err := r.Validate(); err == nil {
		t.Error("expected error for zero timestamp")
	}
}

func TestReading_Validation_Valid(t *testing.T) {
	// HAPPY PATH: All fields valid
	r := Reading{
		AntennaID:  "ANT-001",
		GatewayID:  "rpi-001",
		EPC:        "E200341502001080220A0C64",
		RSSI:       201, // Raw byte value (0xC9) per REQ-A008
		Timestamp:  time.Now(),
		Synced:     false,
		RetryCount: 0,
	}

	if err := r.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReading_JSONMarshal(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	r := Reading{
		AntennaID:  "ANT-001",
		GatewayID:  "rpi-001",
		EPC:        "E200341502001080220A0C64",
		RSSI:       201, // Raw byte value (0xC9) per REQ-A008
		Timestamp:  now,
		Synced:     true,
		RetryCount: 2,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify JSON structure
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if result["antenna_id"] != "ANT-001" {
		t.Errorf("expected antenna_id 'ANT-001', got %v", result["antenna_id"])
	}
	if result["epc"] != "E200341502001080220A0C64" {
		t.Errorf("expected epc 'E200341502001080220A0C64', got %v", result["epc"])
	}
	if result["rssi"] != float64(201) {
		t.Errorf("expected rssi 201, got %v", result["rssi"])
	}
}

func TestReading_JSONUnmarshal(t *testing.T) {
	jsonData := `{
		"antenna_id": "ANT-002",
		"gateway_id": "rpi-002",
		"epc": "E200341502001080220A0C65",
		"rssi": 201,
		"timestamp": "2026-04-10T12:30:00Z",
		"synced": true,
		"retry_count": 3
	}`

	var r Reading
	if err := json.Unmarshal([]byte(jsonData), &r); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if r.AntennaID != "ANT-002" {
		t.Errorf("expected AntennaID 'ANT-002', got %s", r.AntennaID)
	}
	if r.GatewayID != "rpi-002" {
		t.Errorf("expected GatewayID 'rpi-002', got %s", r.GatewayID)
	}
	if r.EPC != "E200341502001080220A0C65" {
		t.Errorf("expected EPC 'E200341502001080220A0C65', got %s", r.EPC)
	}
	if r.RSSI != 201 {
		t.Errorf("expected RSSI 201, got %d", r.RSSI)
	}
	if !r.Synced {
		t.Error("expected Synced to be true")
	}
	if r.RetryCount != 3 {
		t.Errorf("expected RetryCount 3, got %d", r.RetryCount)
	}
}

func TestReading_TableName(t *testing.T) {
	var r Reading
	if r.TableName() != "readings" {
		t.Errorf("expected table name 'readings', got %s", r.TableName())
	}
}
