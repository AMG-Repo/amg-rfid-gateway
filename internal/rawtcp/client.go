package rawtcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
	"github.com/amg-rfid/amg-rfid-shared-go/protocol"
)

// Cache interface for storing readings
type Cache interface {
	Store(r models.Reading) error
}

// AntennaConfig holds configuration for antenna operations
type AntennaConfig struct {
	ID      string
	Enabled bool
}

// Validate checks the antenna configuration
func (a AntennaConfig) Validate() error {
	if a.ID == "" {
		return errors.New("antenna id cannot be empty")
	}
	return nil
}

// Client handles TCP connection to an RFID antenna
type Client struct {
	ip           string
	port         int
	conn         net.Conn
	mu           sync.RWMutex
	reader       *bufio.Reader
	lastActivity time.Time
}

// NewClient creates a new TCP client for an antenna
func NewClient(ip string, port int) *Client {
	return &Client{
		ip:   ip,
		port: port,
	}
}

// Address returns the full address (IP:Port)
func (c *Client) Address() string {
	return fmt.Sprintf("%s:%d", c.ip, c.port)
}

// GetConn returns the underlying net.Conn. Used by AntennaManager for data reading.
func (c *Client) GetConn() net.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

// Connect establishes connection to the antenna
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return errors.New("already connected")
	}

	address := c.Address()
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", address, err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)
	return nil
}

// Disconnect closes the connection
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	c.reader = nil
	return err
}

// IsConnected returns true if connected
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil
}

// SendCommand sends a raw command to the antenna with proper synchronization.
// This satisfies the CommandSender interface from antenna package.
func (c *Client) SendCommand(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return errors.New("not connected")
	}

	_, err := c.conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	return nil
}

// SendReadUII sends the Read UII command: [0x7c, 0xff, 0xff, 0x20, 0x00, 0x00, checksum]
func (c *Client) SendReadUII() error {
	command := []byte{0x7c, 0xff, 0xff, 0x20, 0x00, 0x00}
	checksum := calculateCommandChecksum(command)
	fullCommand := append(command, checksum)
	return c.SendCommand(fullCommand)
}

// SendHeartbeat sends the Get Reader Info (heartbeat) command: [0x7c, 0xff, 0xff, 0x01, 0x00, 0x00, checksum]
func (c *Client) SendHeartbeat() error {
	command := []byte{0x7c, 0xff, 0xff, 0x01, 0x00, 0x00}
	checksum := calculateCommandChecksum(command)
	fullCommand := append(command, checksum)
	return c.SendCommand(fullCommand)
}

// Write writes raw bytes to the connection.
func (c *Client) Write(data []byte) error {
	return c.SendCommand(data)
}

// SetLastActivity updates the last activity timestamp.
func (c *Client) SetLastActivity(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastActivity = t
}

// GetLastActivity returns the last activity timestamp.
func (c *Client) GetLastActivity() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastActivity
}

// calculateCommandChecksum computes the two's complement checksum for commands.
// Sum all bytes, then two's complement: (~sum + 1) & 0xFF
func calculateCommandChecksum(data []byte) byte {
	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	return byte((^sum + 1) & 0xff)
}

// Read reads raw bytes from the connection.
// NOTE: This does NOT hold the mutex — it is designed to be called from a
// single reader goroutine only. The bufio.Reader is safe for single-reader use.
// SendCommand() uses the write mutex separately, so reads and writes don't block each other.
func (c *Client) Read(buf []byte) (int, error) {
	if c.reader == nil {
		return 0, errors.New("not connected")
	}

	return c.reader.Read(buf)
}

// ParsePacket parses a raw packet using the shared protocol
func ParsePacket(data []byte) (*protocol.ParsedPacket, error) {
	return protocol.ParsePacket(data)
}

// RunAntenna runs the read loop for an antenna
// This is designed to be run in a goroutine
// Respects context cancellation for graceful shutdown.
func RunAntenna(ctx context.Context, client *Client, cache Cache, antenna AntennaConfig) error {
	// Validate antenna config
	if err := antenna.Validate(); err != nil {
		return fmt.Errorf("invalid antenna config: %w", err)
	}

	// Don't run if disabled
	if !antenna.Enabled {
		return errors.New("antenna is disabled")
	}

	// Connect to antenna
	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Disconnect()

	// Read loop
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := client.Read(buf)
		if err != nil {
			if err == io.EOF {
				return errors.New("connection closed")
			}
			return fmt.Errorf("read error: %w", err)
		}

		if n == 0 {
			continue
		}

		// Parse packet
		packet, err := ParsePacket(buf[:n])
		if err != nil {
			// Invalid packet, log and continue
			continue
		}

		// Only process data packets
		if !packet.IsDataPacket() {
			continue
		}

		// Create reading
		reading := models.Reading{
			AntennaID: antenna.ID,
			EPC:       packet.TagUID,
			RSSI:      -50, // Default RSSI since packet doesn't have it
			Timestamp: time.Now(),
		}

		// Store in cache
		if err := cache.Store(reading); err != nil {
			// Log error but continue - don't stop the antenna
			continue
		}
	}
}
