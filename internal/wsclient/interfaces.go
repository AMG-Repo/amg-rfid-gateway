package wsclient

import (
	"context"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

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

// TokenProvider supplies the access token sent on each WebSocket handshake.
// Invalidate is called with a token the backend rejected with 401.
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
	Invalidate(token string)
}

// StaticToken is a TokenProvider that always returns the same token.
type StaticToken string

// Token returns the fixed token.
func (s StaticToken) Token(context.Context) (string, error) { return string(s), nil }

// Invalidate is a no-op: a fixed token cannot be renewed.
func (StaticToken) Invalidate(string) {}
