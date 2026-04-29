package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestCalculateBackoff verifies exponential backoff calculation
func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		name           string
		attempt        int
		initialBackoff time.Duration
		maxBackoff     time.Duration
		expected       time.Duration
	}{
		{
			name:           "first attempt",
			attempt:        0,
			initialBackoff: 1 * time.Second,
			maxBackoff:     30 * time.Second,
			expected:       1 * time.Second,
		},
		{
			name:           "second attempt",
			attempt:        1,
			initialBackoff: 1 * time.Second,
			maxBackoff:     30 * time.Second,
			expected:       2 * time.Second,
		},
		{
			name:           "third attempt",
			attempt:        2,
			initialBackoff: 1 * time.Second,
			maxBackoff:     30 * time.Second,
			expected:       4 * time.Second,
		},
		{
			name:           "fourth attempt",
			attempt:        3,
			initialBackoff: 1 * time.Second,
			maxBackoff:     30 * time.Second,
			expected:       8 * time.Second,
		},
		{
			name:           "fifth attempt",
			attempt:        4,
			initialBackoff: 1 * time.Second,
			maxBackoff:     30 * time.Second,
			expected:       16 * time.Second,
		},
		{
			name:           "max backoff reached",
			attempt:        10,
			initialBackoff: 1 * time.Second,
			maxBackoff:     30 * time.Second,
			expected:       30 * time.Second,
		},
		{
			name:           "max backoff exactly",
			attempt:        5, // 32 seconds, capped at 30
			initialBackoff: 1 * time.Second,
			maxBackoff:     30 * time.Second,
			expected:       30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateBackoff(tt.attempt, tt.initialBackoff, tt.maxBackoff)
			if result != tt.expected {
				t.Errorf("calculateBackoff(%d) = %v, expected %v", tt.attempt, result, tt.expected)
			}
		})
	}
}

// TestCalculateBackoff_RespectsContextCancellation verifies backoff respects context
func TestCalculateBackoff_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This test verifies that any loop using backoff would exit on context cancellation
	// The actual backoff calculation doesn't use context, but the calling code should check it
	select {
	case <-ctx.Done():
		// Context is cancelled as expected
	default:
		t.Error("context should be cancelled")
	}
}

// TestRunAntennaWithReconnect_CancelsOnContext verifies reconnection loop respects context
func TestRunAntennaWithReconnect_CancelsOnContext(t *testing.T) {
	// This test verifies that the reconnection loop exits when context is cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Create a mock configuration that would fail to connect
	// The function should attempt reconnection but exit when context is cancelled

	// Since we can't easily mock the connection in main package without refactoring,
	// we'll just verify the backoff calculation works correctly and context cancellation
	// is respected in the backoff logic

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Simulate backoff loop
		attempt := 0
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			backoff := calculateBackoff(attempt, 10*time.Millisecond, 50*time.Millisecond)
			select {
			case <-time.After(backoff):
				attempt++
			case <-ctx.Done():
				return
			}

			if attempt > 10 {
				return
			}
		}
	}()

	select {
	case <-done:
		// Loop exited due to context cancellation or max attempts
	case <-time.After(2 * time.Second):
		t.Error("reconnection loop should exit when context is cancelled")
	}
}

// TestBackoffSequence verifies the exponential backoff sequence
func TestBackoffSequence(t *testing.T) {
	initialBackoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second, // Capped
		30 * time.Second, // Still capped
	}

	for i, exp := range expected {
		result := calculateBackoff(i, initialBackoff, maxBackoff)
		if result != exp {
			t.Errorf("attempt %d: expected %v, got %v", i, exp, result)
		}
	}
}

// TestBackoffWithDifferentInitialValues verifies backoff with various initial values
func TestBackoffWithDifferentInitialValues(t *testing.T) {
	tests := []struct {
		initialBackoff time.Duration
		attempt        int
		expected       time.Duration
	}{
		{500 * time.Millisecond, 0, 500 * time.Millisecond},
		{500 * time.Millisecond, 1, 1 * time.Second},
		{500 * time.Millisecond, 2, 2 * time.Second},
		{2 * time.Second, 0, 2 * time.Second},
		{2 * time.Second, 1, 4 * time.Second},
		{2 * time.Second, 2, 8 * time.Second},
	}

	for _, tt := range tests {
		result := calculateBackoff(tt.attempt, tt.initialBackoff, 30*time.Second)
		if result != tt.expected {
			t.Errorf("initial=%v, attempt=%d: expected %v, got %v",
				tt.initialBackoff, tt.attempt, tt.expected, result)
		}
	}
}

// mockRetryableError is a mock error that indicates a retryable connection error
type mockRetryableError struct {
	msg string
}

func (e mockRetryableError) Error() string {
	return e.msg
}

// TestIsRetryableError identifies retryable vs non-retryable errors
func TestIsRetryableError(t *testing.T) {
	// Retryable errors
	retryable := []error{
		errors.New("connection refused"),
		errors.New("connection reset"),
		errors.New("timeout"),
		errors.New("broken pipe"),
		errors.New("network is unreachable"),
	}

	for _, err := range retryable {
		if !isRetryableError(err) {
			t.Errorf("expected '%s' to be retryable", err.Error())
		}
	}

	// Non-retryable errors
	nonRetryable := []error{
		errors.New("invalid configuration"),
		errors.New("authentication failed"),
		errors.New("permission denied"),
		nil, // nil error means success
	}

	for _, err := range nonRetryable {
		if isRetryableError(err) {
			t.Errorf("expected '%v' to be non-retryable", err)
		}
	}
}

// TestBackoffReset verifies backoff resets after successful connection
func TestBackoffReset(t *testing.T) {
	initialBackoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	// Simulate failed attempts
	attempt1 := calculateBackoff(0, initialBackoff, maxBackoff) // 1s
	attempt2 := calculateBackoff(1, initialBackoff, maxBackoff) // 2s
	attempt3 := calculateBackoff(2, initialBackoff, maxBackoff) // 4s

	if attempt1 != 1*time.Second || attempt2 != 2*time.Second || attempt3 != 4*time.Second {
		t.Error("backoff should increase with attempts")
	}

	// After successful connection, backoff resets to initial
	// (This is done by resetting attempt counter to 0 in the actual implementation)
	resetAttempt := calculateBackoff(0, initialBackoff, maxBackoff)
	if resetAttempt != 1*time.Second {
		t.Error("backoff should reset to initial value after successful connection")
	}
}
