package models

import (
	"testing"
)

func TestErrorCode_Constants(t *testing.T) {
	// Verify all error codes are defined and have expected values
	tests := []struct {
		code     ErrorCode
		expected string
	}{
		{ErrGatewayAuthFailed, "GATEWAY_AUTH_FAILED"},
		{ErrInvalidRequest, "INVALID_REQUEST"},
		{ErrRateLimited, "RATE_LIMITED"},
		{ErrSyncFailed, "SYNC_FAILED"},
		{ErrDatabaseError, "DATABASE_ERROR"},
		{ErrInvalidEPC, "INVALID_EPC"},
		{ErrDuplicateReading, "DUPLICATE_READING"},
		{ErrGatewayNotFound, "GATEWAY_NOT_FOUND"},
		{ErrVersionMismatch, "VERSION_MISMATCH"},
	}

	for _, tt := range tests {
		if string(tt.code) != tt.expected {
			t.Errorf("ErrorCode %v = %s, want %s", tt.code, tt.code, tt.expected)
		}
	}
}

func TestErrorCode_Error(t *testing.T) {
	// Test that Error() returns the string representation
	code := ErrGatewayAuthFailed
	if code.Error() != "GATEWAY_AUTH_FAILED" {
		t.Errorf("Error() = %s, want GATEWAY_AUTH_FAILED", code.Error())
	}
}

func TestNewGatewayError(t *testing.T) {
	err := NewGatewayError(ErrGatewayAuthFailed, "invalid token")

	if err.Code != ErrGatewayAuthFailed {
		t.Errorf("expected code GATEWAY_AUTH_FAILED, got %s", err.Code)
	}
	if err.Message != "invalid token" {
		t.Errorf("expected message 'invalid token', got %s", err.Message)
	}
	if err.Details != "" {
		t.Errorf("expected empty details, got %s", err.Details)
	}
}

func TestNewGatewayErrorWithDetails(t *testing.T) {
	err := NewGatewayErrorWithDetails(ErrInvalidRequest, "missing field", "gateway_id is required")

	if err.Code != ErrInvalidRequest {
		t.Errorf("expected code INVALID_REQUEST, got %s", err.Code)
	}
	if err.Message != "missing field" {
		t.Errorf("expected message 'missing field', got %s", err.Message)
	}
	if err.Details != "gateway_id is required" {
		t.Errorf("expected details 'gateway_id is required', got %s", err.Details)
	}
}

func TestGatewayError_Error(t *testing.T) {
	err := NewGatewayError(ErrSyncFailed, "network timeout")

	expected := "SYNC_FAILED: network timeout"
	if err.Error() != expected {
		t.Errorf("Error() = %s, want %s", err.Error(), expected)
	}
}

func TestGatewayError_ErrorWithDetails(t *testing.T) {
	err := NewGatewayErrorWithDetails(ErrDatabaseError, "insert failed", "connection refused")

	expected := "DATABASE_ERROR: insert failed (connection refused)"
	if err.Error() != expected {
		t.Errorf("Error() = %s, want %s", err.Error(), expected)
	}
}

func TestIsRetryable(t *testing.T) {
	// Test retryable errors
	retryableErrors := []ErrorCode{
		ErrSyncFailed,
		ErrDatabaseError,
		ErrGatewayNotFound,
	}

	for _, code := range retryableErrors {
		if !code.IsRetryable() {
			t.Errorf("expected %s to be retryable", code)
		}
	}

	// Test non-retryable errors
	nonRetryableErrors := []ErrorCode{
		ErrGatewayAuthFailed,
		ErrInvalidRequest,
		ErrRateLimited,
		ErrInvalidEPC,
		ErrDuplicateReading,
		ErrVersionMismatch,
	}

	for _, code := range nonRetryableErrors {
		if code.IsRetryable() {
			t.Errorf("expected %s to be non-retryable", code)
		}
	}
}
