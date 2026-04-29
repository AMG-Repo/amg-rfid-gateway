package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestGatewayHealthStatus_Validation_EmptyGatewayID(t *testing.T) {
	// NEGATIVE: GatewayID cannot be empty
	h := GatewayHealthStatus{
		GatewayID: "",
		CompanyID: "company-123",
		Status:    "online",
		Antennas:  []AntennaStatus{},
		CacheSize: 0,
		LastSync:  time.Now(),
		Uptime:    3600,
		Version:   "v1.0.0",
	}

	if err := h.Validate(); err == nil {
		t.Error("expected error for empty GatewayID")
	}
}

func TestGatewayHealthStatus_Validation_InvalidStatus(t *testing.T) {
	// NEGATIVE: Status must be valid
	h := GatewayHealthStatus{
		GatewayID: "rpi-001",
		CompanyID: "company-123",
		Status:    "invalid_status",
		Antennas:  []AntennaStatus{},
		CacheSize: 0,
		LastSync:  time.Now(),
		Uptime:    3600,
		Version:   "v1.0.0",
	}

	if err := h.Validate(); err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestGatewayHealthStatus_Validation_NegativeCacheSize(t *testing.T) {
	// NEGATIVE: CacheSize cannot be negative
	h := GatewayHealthStatus{
		GatewayID: "rpi-001",
		CompanyID: "company-123",
		Status:    "online",
		Antennas:  []AntennaStatus{},
		CacheSize: -1,
		LastSync:  time.Now(),
		Uptime:    3600,
		Version:   "v1.0.0",
	}

	if err := h.Validate(); err == nil {
		t.Error("expected error for negative CacheSize")
	}
}

func TestGatewayHealthStatus_Validation_NegativeUptime(t *testing.T) {
	// NEGATIVE: Uptime cannot be negative
	h := GatewayHealthStatus{
		GatewayID: "rpi-001",
		CompanyID: "company-123",
		Status:    "online",
		Antennas:  []AntennaStatus{},
		CacheSize: 0,
		LastSync:  time.Now(),
		Uptime:    -1,
		Version:   "v1.0.0",
	}

	if err := h.Validate(); err == nil {
		t.Error("expected error for negative Uptime")
	}
}

func TestGatewayHealthStatus_Validation_Valid(t *testing.T) {
	// HAPPY PATH: All fields valid
	h := GatewayHealthStatus{
		GatewayID: "rpi-001",
		CompanyID: "company-123",
		Status:    "online",
		Antennas: []AntennaStatus{
			{ID: "ANT-001", Connected: true, LastSeen: time.Now()},
		},
		CacheSize: 100,
		LastSync:  time.Now(),
		Uptime:    3600,
		Version:   "v1.0.0",
	}

	if err := h.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGatewayHealthStatus_ValidStatuses(t *testing.T) {
	validStatuses := []string{"online", "offline", "syncing", "error"}

	for _, status := range validStatuses {
		h := GatewayHealthStatus{
			GatewayID: "rpi-001",
			CompanyID: "company-123",
			Status:    status,
			CacheSize: 0,
			LastSync:  time.Now(),
			Uptime:    3600,
			Version:   "v1.0.0",
		}

		if err := h.Validate(); err != nil {
			t.Errorf("unexpected error for status '%s': %v", status, err)
		}
	}
}

func TestGatewayHealthStatus_JSONMarshal(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	h := GatewayHealthStatus{
		GatewayID: "rpi-001",
		CompanyID: "company-123",
		Status:    "online",
		Antennas: []AntennaStatus{
			{ID: "ANT-001", Connected: true, LastSeen: now},
		},
		CacheSize: 100,
		LastSync:  now,
		Uptime:    3600,
		Version:   "v1.0.0",
	}

	data, err := json.Marshal(h)
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
	if result["status"] != "online" {
		t.Errorf("expected status 'online', got %v", result["status"])
	}
	if result["cache_size"] != float64(100) {
		t.Errorf("expected cache_size 100, got %v", result["cache_size"])
	}
}

func TestAntennaStatus_Validation(t *testing.T) {
	// NEGATIVE: Empty ID
	a := AntennaStatus{ID: ""}
	if err := a.Validate(); err == nil {
		t.Error("expected error for empty ID")
	}

	// HAPPY PATH: Valid antenna status
	a = AntennaStatus{
		ID:        "ANT-001",
		Connected: true,
		LastSeen:  time.Now(),
	}
	if err := a.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
