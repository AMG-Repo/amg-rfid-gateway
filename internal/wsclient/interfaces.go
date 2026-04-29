package wsclient

import "github.com/amg-rfid/amg-rfid-shared-go/models"

// WSClient interface for WebSocket operations.
// This interface is defined here to avoid circular dependencies
// and to allow the sync package to depend on it without
// knowing WebSocket implementation details.
type WSClient interface {
	Connect() error
	Disconnect() error
	IsConnected() bool
	Send(data []byte) error
	Read() ([]byte, error)
	Reconnect() error
	SendSyncRequest(req models.SyncRequest) (*models.SyncResponse, error)
}
