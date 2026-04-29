package protocol

import (
	"testing"
)

func TestProtocolVersion(t *testing.T) {
	// Verify protocol version is defined
	if Version == "" {
		t.Error("Version should not be empty")
	}

	if Version != "1.0" {
		t.Errorf("expected version '1.0', got %s", Version)
	}
}

func TestBufferSizes(t *testing.T) {
	// Verify buffer size constants
	if MaxReadingBuffer <= 0 {
		t.Error("MaxReadingBuffer should be positive")
	}

	if MaxSyncBatch <= 0 {
		t.Error("MaxSyncBatch should be positive")
	}

	if MaxSyncBatch > MaxReadingBuffer {
		t.Error("MaxSyncBatch should not exceed MaxReadingBuffer")
	}
}

func TestTimeoutDurations(t *testing.T) {
	// Verify timeout constants are reasonable
	if ConnectionTimeout <= 0 {
		t.Error("ConnectionTimeout should be positive")
	}

	if SyncTimeout <= 0 {
		t.Error("SyncTimeout should be positive")
	}

	if HealthCheckInterval <= 0 {
		t.Error("HealthCheckInterval should be positive")
	}
}

func TestRetryConfiguration(t *testing.T) {
	// Verify retry configuration
	if MaxRetries < 0 {
		t.Error("MaxRetries should not be negative")
	}

	if InitialBackoff <= 0 {
		t.Error("InitialBackoff should be positive")
	}

	if MaxBackoff <= 0 {
		t.Error("MaxBackoff should be positive")
	}

	if MaxBackoff < InitialBackoff {
		t.Error("MaxBackoff should be >= InitialBackoff")
	}
}
