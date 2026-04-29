package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// SyncTransport abstracts the transport layer for sync operations.
// This allows the sync engine to work with any transport (WebSocket, HTTP, etc.)
// without knowing the implementation details.
type SyncTransport interface {
	// Connect establishes the transport connection
	Connect() error
	// Disconnect closes the transport connection
	Disconnect() error
	// IsConnected returns true if the transport is connected
	IsConnected() bool
	// Send sends data over the transport
	Send(data []byte) error
	// Receive receives data from the transport
	Receive() ([]byte, error)
	// Reconnect attempts to reconnect the transport
	Reconnect() error
}

// SyncService provides high-level sync operations over any transport.
// It abstracts the protocol details (JSON serialization, request/response mapping)
// from the sync engine.
type SyncService struct {
	transport SyncTransport
}

// NewSyncService creates a new sync service with the given transport.
func NewSyncService(transport SyncTransport) *SyncService {
	return &SyncService{
		transport: transport,
	}
}

// SendSyncRequest sends a sync request and waits for response.
// This method abstracts the transport details and handles serialization.
func (s *SyncService) SendSyncRequest(ctx context.Context, req models.SyncRequest) (*models.SyncResponse, error) {
	// NEGATIVE: Context cancelled before sending
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Ensure connection
	if !s.transport.IsConnected() {
		if err := s.transport.Reconnect(); err != nil {
			return nil, fmt.Errorf("transport not connected: %w", err)
		}
	}

	// Validate request
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Serialize request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send with context check
	sendCh := make(chan error, 1)
	go func() {
		sendCh <- s.transport.Send(data)
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-sendCh:
		if err != nil {
			return nil, fmt.Errorf("failed to send: %w", err)
		}
	}

	// Receive response with context check
	receiveCh := make(chan struct {
		data []byte
		err  error
	}, 1)
	go func() {
		data, err := s.transport.Receive()
		receiveCh <- struct {
			data []byte
			err  error
		}{data, err}
	}()

	var respData []byte
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-receiveCh:
		if result.err != nil {
			return nil, fmt.Errorf("failed to read response: %w", result.err)
		}
		respData = result.data
	}

	// Parse response
	var resp models.SyncResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}

// GetTransport returns the underlying transport (for health checks, etc.)
func (s *SyncService) GetTransport() SyncTransport {
	return s.transport
}
