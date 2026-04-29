package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSyncRequest_Validation_EmptyGatewayID(t *testing.T) {
	// NEGATIVE: GatewayID cannot be empty
	r := SyncRequest{
		GatewayID: "",
		CompanyID: "company-123",
		Readings:  []Reading{},
		Timestamp: time.Now(),
	}

	if err := r.Validate(); err == nil {
		t.Error("expected error for empty GatewayID")
	}
}

func TestSyncRequest_Validation_EmptyCompanyID(t *testing.T) {
	// NEGATIVE: CompanyID cannot be empty
	r := SyncRequest{
		GatewayID: "rpi-001",
		CompanyID: "",
		Readings:  []Reading{},
		Timestamp: time.Now(),
	}

	if err := r.Validate(); err == nil {
		t.Error("expected error for empty CompanyID")
	}
}

func TestSyncRequest_Validation_ZeroTimestamp(t *testing.T) {
	// NEGATIVE: Timestamp cannot be zero
	r := SyncRequest{
		GatewayID: "rpi-001",
		CompanyID: "company-123",
		Readings:  []Reading{},
		Timestamp: time.Time{},
	}

	if err := r.Validate(); err == nil {
		t.Error("expected error for zero timestamp")
	}
}

func TestSyncRequest_Validation_Valid(t *testing.T) {
	// HAPPY PATH: All fields valid
	now := time.Now()
	r := SyncRequest{
		GatewayID: "rpi-001",
		CompanyID: "company-123",
		Readings: []Reading{
			{
				AntennaID: "ANT-001",
				EPC:       "E200341502001080220A0C64",
				RSSI:      201, // Raw byte value (0xC9) per REQ-A008
				Timestamp: now,
			},
		},
		Timestamp: now,
	}

	if err := r.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSyncRequest_JSONMarshal(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	r := SyncRequest{
		GatewayID: "rpi-001",
		CompanyID: "company-123",
		Readings: []Reading{
			{
				AntennaID: "ANT-001",
				EPC:       "E200341502001080220A0C64",
				RSSI:      201, // Raw byte value (0xC9) per REQ-A008
				Timestamp: now,
			},
		},
		Timestamp: now,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if result["gateway_id"] != "rpi-001" {
		t.Errorf("expected gateway_id 'rpi-001', got %v", result["gateway_id"])
	}
	if result["company_id"] != "company-123" {
		t.Errorf("expected company_id 'company-123', got %v", result["company_id"])
	}

	readings, ok := result["readings"].([]interface{})
	if !ok || len(readings) != 1 {
		t.Fatalf("expected 1 reading, got %v", result["readings"])
	}
}

func TestSyncRequest_JSONUnmarshal(t *testing.T) {
	jsonData := `{
		"gateway_id": "rpi-002",
		"company_id": "company-456",
		"readings": [
			{
				"antenna_id": "ANT-002",
				"epc": "E200341502001080220A0C65",
				"rssi": 201,
				"timestamp": "2026-04-10T12:30:00Z"
			}
		],
		"timestamp": "2026-04-10T12:30:00Z"
	}`

	var r SyncRequest
	if err := json.Unmarshal([]byte(jsonData), &r); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if r.GatewayID != "rpi-002" {
		t.Errorf("expected GatewayID 'rpi-002', got %s", r.GatewayID)
	}
	if r.CompanyID != "company-456" {
		t.Errorf("expected CompanyID 'company-456', got %s", r.CompanyID)
	}
	if len(r.Readings) != 1 {
		t.Errorf("expected 1 reading, got %d", len(r.Readings))
	}
}
