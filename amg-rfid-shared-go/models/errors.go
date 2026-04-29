package models

import (
	"fmt"
)

// ErrorCode represents a canonical error code for gateway↔cloud protocol.
type ErrorCode string

// Canonical error codes for gateway↔cloud communication.
const (
	ErrGatewayAuthFailed ErrorCode = "GATEWAY_AUTH_FAILED"
	ErrInvalidRequest    ErrorCode = "INVALID_REQUEST"
	ErrRateLimited       ErrorCode = "RATE_LIMITED"
	ErrSyncFailed        ErrorCode = "SYNC_FAILED"
	ErrDatabaseError     ErrorCode = "DATABASE_ERROR"
	ErrInvalidEPC        ErrorCode = "INVALID_EPC"
	ErrDuplicateReading  ErrorCode = "DUPLICATE_READING"
	ErrGatewayNotFound   ErrorCode = "GATEWAY_NOT_FOUND"
	ErrVersionMismatch   ErrorCode = "VERSION_MISMATCH"
)

// Error returns the string representation of the error code.
func (e ErrorCode) Error() string {
	return string(e)
}

// IsRetryable returns true if the error is transient and can be retried.
func (e ErrorCode) IsRetryable() bool {
	switch e {
	case ErrSyncFailed, ErrDatabaseError, ErrGatewayNotFound:
		return true
	default:
		return false
	}
}

// GatewayError represents a structured error response.
type GatewayError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Details string    `json:"details,omitempty"`
}

// Error returns a human-readable error message.
func (e *GatewayError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewGatewayError creates a new gateway error.
func NewGatewayError(code ErrorCode, message string) *GatewayError {
	return &GatewayError{
		Code:    code,
		Message: message,
	}
}

// NewGatewayErrorWithDetails creates a new gateway error with details.
func NewGatewayErrorWithDetails(code ErrorCode, message, details string) *GatewayError {
	return &GatewayError{
		Code:    code,
		Message: message,
		Details: details,
	}
}
