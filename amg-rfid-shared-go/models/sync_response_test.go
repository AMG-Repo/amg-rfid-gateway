package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSyncResponse_Validation(t *testing.T) {
	// HAPPY PATH: Valid response
	r := SyncResponse{
		Success:    true,
		Accepted:   10,
		Duplicates: 2,
		Errors:     []string{},
		NextSyncIn: 30,
		ServerTime: time.Now(),
	}

	if err := r.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSyncResponse_Validation_InvalidAccepted(t *testing.T) {
	// NEGATIVE: Accepted cannot be negative
	r := SyncResponse{
		Success:    true,
		Accepted:   -1,
		Duplicates: 0,
		Errors:     []string{},
		NextSyncIn: 30,
		ServerTime: time.Now(),
	}

	if err := r.Validate(); err == nil {
		t.Error("expected error for negative Accepted")
	}
}

func TestSyncResponse_Validation_InvalidDuplicates(t *testing.T) {
	// NEGATIVE: Duplicates cannot be negative
	r := SyncResponse{
		Success:    true,
		Accepted:   10,
		Duplicates: -1,
		Errors:     []string{},
		NextSyncIn: 30,
		ServerTime: time.Now(),
	}

	if err := r.Validate(); err == nil {
		t.Error("expected error for negative Duplicates")
	}
}

func TestSyncResponse_Validation_ZeroServerTime(t *testing.T) {
	// NEGATIVE: ServerTime cannot be zero
	r := SyncResponse{
		Success:    true,
		Accepted:   10,
		Duplicates: 0,
		Errors:     []string{},
		NextSyncIn: 30,
		ServerTime: time.Time{},
	}

	if err := r.Validate(); err == nil {
		t.Error("expected error for zero ServerTime")
	}
}

func TestSyncResponse_JSONMarshal(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	r := SyncResponse{
		Success:    true,
		Accepted:   10,
		Duplicates: 2,
		Errors:     []string{"error1", "error2"},
		NextSyncIn: 30,
		ServerTime: now,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if result["success"] != true {
		t.Errorf("expected success true, got %v", result["success"])
	}
	if result["accepted"] != float64(10) {
		t.Errorf("expected accepted 10, got %v", result["accepted"])
	}
	if result["duplicates"] != float64(2) {
		t.Errorf("expected duplicates 2, got %v", result["duplicates"])
	}
	if result["next_sync_in"] != float64(30) {
		t.Errorf("expected next_sync_in 30, got %v", result["next_sync_in"])
	}
}

func TestSyncResponse_JSONUnmarshal(t *testing.T) {
	jsonData := `{
		"success": true,
		"accepted": 25,
		"duplicates": 5,
		"errors": ["invalid_epc", "duplicate_reading"],
		"next_sync_in": 60,
		"server_time": "2026-04-10T12:45:00Z"
	}`

	var r SyncResponse
	if err := json.Unmarshal([]byte(jsonData), &r); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if !r.Success {
		t.Error("expected Success to be true")
	}
	if r.Accepted != 25 {
		t.Errorf("expected Accepted 25, got %d", r.Accepted)
	}
	if r.Duplicates != 5 {
		t.Errorf("expected Duplicates 5, got %d", r.Duplicates)
	}
	if len(r.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(r.Errors))
	}
	if r.NextSyncIn != 60 {
		t.Errorf("expected NextSyncIn 60, got %d", r.NextSyncIn)
	}
}

func TestSyncResponse_WithErrors(t *testing.T) {
	r := SyncResponse{
		Success:    false,
		Accepted:   0,
		Duplicates: 0,
		Errors:     []string{"auth_failed", "invalid_gateway_id"},
		NextSyncIn: 0,
		ServerTime: time.Now(),
	}

	if r.Success {
		t.Error("expected Success to be false")
	}
	if len(r.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(r.Errors))
	}
}
