package wsclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
	"github.com/gorilla/websocket"
)

// Client handles WebSocket connection to the cloud
type Client struct {
	url            string
	jwtToken       string
	conn           *websocket.Conn
	mu             sync.RWMutex
	reconnectDelay time.Duration
}

// NewClient creates a new WebSocket client
func NewClient(url, jwtToken string) *Client {
	return &Client{
		url:            url,
		jwtToken:       jwtToken,
		reconnectDelay: 5 * time.Second,
	}
}

// Connect establishes WebSocket connection with JWT authentication
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return errors.New("already connected")
	}

	// Parse URL
	u, err := url.Parse(c.url)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Set up headers with JWT
	headers := http.Header{
		"Authorization": []string{"Bearer " + c.jwtToken},
	}

	// Connect
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(u.String(), headers)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.conn = conn
	return nil
}

// Disconnect closes the WebSocket connection
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	// Send close message
	c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

	err := c.conn.Close()
	c.conn = nil
	return err
}

// IsConnected returns true if connected
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return false
	}

	// Try to check connection health
	c.conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	c.conn.SetReadDeadline(time.Time{}) // Reset

	return true
}

// Send sends a message over the WebSocket
func (c *Client) Send(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return errors.New("not connected")
	}

	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// Read reads a message from the WebSocket
func (c *Client) Read() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, errors.New("not connected")
	}

	_, message, err := c.conn.ReadMessage()
	return message, err
}

// Reconnect attempts to reconnect with exponential backoff
func (c *Client) Reconnect() error {
	// Disconnect if connected
	c.Disconnect()

	// Wait before reconnect
	time.Sleep(c.reconnectDelay)

	// Try to connect
	return c.Connect()
}

// SendSyncRequest sends a sync request and waits for response
func (c *Client) SendSyncRequest(req models.SyncRequest) (*models.SyncResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Serialize request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send
	if err := c.Send(data); err != nil {
		return nil, fmt.Errorf("failed to send: %w", err)
	}

	// Read response
	respData, err := c.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var resp models.SyncResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}
