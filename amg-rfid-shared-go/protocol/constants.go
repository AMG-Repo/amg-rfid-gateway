package protocol

import "time"

// Protocol version
const (
	Version = "1.0"
)

// Buffer sizes
const (
	// MaxReadingBuffer is the maximum number of readings to buffer in memory
	MaxReadingBuffer = 1000

	// MaxSyncBatch is the maximum number of readings per sync request
	MaxSyncBatch = 100
)

// Timeouts
const (
	// ConnectionTimeout is the timeout for establishing connections
	ConnectionTimeout = 10 * time.Second

	// SyncTimeout is the timeout for sync operations
	SyncTimeout = 30 * time.Second

	// HealthCheckInterval is how often to check health status
	HealthCheckInterval = 30 * time.Second
)

// Retry configuration
const (
	// MaxRetries is the maximum number of retry attempts
	MaxRetries = 5

	// InitialBackoff is the initial retry backoff duration
	InitialBackoff = 1 * time.Second

	// MaxBackoff is the maximum retry backoff duration
	MaxBackoff = 60 * time.Second
)

// Command codes (REQ-A004)
const (
	// CmdGetReaderInfo is the Get Reader Info command (heartbeat ping)
	CmdGetReaderInfo byte = 0x01
)

// Response codes (RTN codes) (REQ-A004)
const (
	// RtnUIIData is the UII data response code
	RtnUIIData byte = 0x02

	// RtnTagData is the Tag Data response code
	RtnTagData byte = 0x06

	// RtnError is the Error response code
	RtnError byte = 0x07

	// RtnHeartbeatResponse is the Heartbeat response code
	RtnHeartbeatResponse byte = 0x10
)
