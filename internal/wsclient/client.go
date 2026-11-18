package wsclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
	"github.com/gorilla/websocket"
)

// errNotConnected is returned when there is no live connection.
var errNotConnected = errors.New("not connected")

// session is one live connection with its read loop.
type session struct {
	conn     *websocket.Conn
	incoming chan []byte   // messages from the server, read by the read loop
	done     chan struct{} // closed when the read loop ends
}

func (s *session) closed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// Client handles WebSocket connection to the cloud
type Client struct {
	url             string
	tokens          TokenProvider
	sess            *session
	mu              sync.RWMutex
	writeMu         sync.Mutex
	reconnectDelay  time.Duration
	responseTimeout time.Duration
}

// NewClient creates a new WebSocket client
func NewClient(url string, tokens TokenProvider) *Client {
	return &Client{
		url:             url,
		tokens:          tokens,
		reconnectDelay:  5 * time.Second,
		responseTimeout: 15 * time.Second,
	}
}

// Connect establishes WebSocket connection with JWT authentication and starts
// the read loop that answers server pings and detects a dropped connection.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sess != nil && !c.sess.closed() {
		return errors.New("already connected")
	}
	if c.sess != nil {
		c.sess.conn.Close()
		c.sess = nil
	}

	// Parse URL
	u, err := url.Parse(c.url)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Fetch the current token on every connect so reconnects pick up renewals
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return fmt.Errorf("obtain access token: %w", err)
	}
	headers := http.Header{
		"Authorization": []string{"Bearer " + token},
	}

	// Connect
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.Dial(u.String(), headers)
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			c.tokens.Invalidate(token)
		}
		return fmt.Errorf("failed to connect: %w", err)
	}

	sess := &session{
		conn:     conn,
		incoming: make(chan []byte, 16),
		done:     make(chan struct{}),
	}
	c.sess = sess
	go readLoop(sess)
	return nil
}

// readLoop is the only reader of the connection. Reading lets gorilla answer
// server pings with pongs; a read error means the connection is gone.
func readLoop(s *session) {
	defer close(s.done)
	for {
		_, msg, err := s.conn.ReadMessage()
		if err != nil {
			log.Printf("[WS] connection closed: %v", err)
			return
		}
		select {
		case s.incoming <- msg:
		default:
			log.Printf("[WS] dropping server message: receive buffer full")
		}
	}
}

func (c *Client) current() (*session, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.sess == nil || c.sess.closed() {
		return nil, errNotConnected
	}
	return c.sess, nil
}

// Disconnect closes the WebSocket connection
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sess == nil {
		return nil
	}

	// Send close message (WriteControl is safe alongside other writers)
	_ = c.sess.conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))

	err := c.sess.conn.Close()
	<-c.sess.done
	c.sess = nil
	return err
}

// IsConnected reports whether the connection is still alive
func (c *Client) IsConnected() bool {
	_, err := c.current()
	return err == nil
}

// Send sends a message over the WebSocket
func (c *Client) Send(data []byte) error {
	s, err := c.current()
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return s.conn.WriteMessage(websocket.TextMessage, data)
}

// Read returns the next message from the server, or an error once the
// connection is closed
func (c *Client) Read() ([]byte, error) {
	s, err := c.current()
	if err != nil {
		return nil, err
	}

	select {
	case msg := <-s.incoming:
		return msg, nil
	case <-s.done:
		return nil, errors.New("connection closed")
	}
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

// isServerEvent reports whether msg is a typed server event such as
// SESSION_READY ({"type": ..., "payload": ...}) rather than a sync response.
func isServerEvent(msg []byte) bool {
	var envelope struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(msg, &envelope) == nil && envelope.Type != ""
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

	s, err := c.current()
	if err != nil {
		return nil, fmt.Errorf("failed to send: %w", err)
	}

	// Send
	if err := c.Send(data); err != nil {
		return nil, fmt.Errorf("failed to send: %w", err)
	}

	// Read response, skipping server events
	timeout := time.NewTimer(c.responseTimeout)
	defer timeout.Stop()
	for {
		var respData []byte
		select {
		case respData = <-s.incoming:
		case <-s.done:
			return nil, errors.New("failed to read response: connection closed")
		case <-timeout.C:
			return nil, errors.New("failed to read response: timeout")
		}
		if isServerEvent(respData) {
			continue
		}

		// Parse response
		var resp models.SyncResponse
		if err := json.Unmarshal(respData, &resp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}
		return &resp, nil
	}
}
