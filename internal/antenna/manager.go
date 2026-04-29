// Package antenna provides antenna management with intelligent read loop and heartbeat.
package antenna

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
	"github.com/amg-rfid/amg-rfid-shared-go/protocol"
)

// CommandSender defines the interface for sending commands to the antenna.
// Implemented by rawtcp.Client.
type CommandSender interface {
	SendCommand(data []byte) error
	GetLastActivity() time.Time
	SetLastActivity(t time.Time)
	IsConnected() bool
}

// Cache defines the interface for storing readings.
type Cache interface {
	Store(r models.Reading) error
}

// AntennaStatus represents the current status of an antenna for TUI display.
type AntennaStatus struct {
	ID           string    `json:"id"`
	Connected    bool      `json:"connected"`
	ReadingCount uint64    `json:"reading_count"`
	LastTagEPC   string    `json:"last_tag_epc,omitempty"`
	LastTagRSSI  int       `json:"last_tag_rssi,omitempty"`
	LastSeen     time.Time `json:"last_seen"`
	AutoReading  bool      `json:"auto_reading"`
	ErrorCount   uint64    `json:"error_count"`
}

// AntennaManager orchestrates the intelligent read loop, heartbeat, and auto-reading detection.
type AntennaManager struct {
	client        CommandSender
	config        config.AntennaConfig
	gatewayConfig *config.GatewayConfig
	cache         Cache
	conn          net.Conn
	eventBus      *events.EventBus // NEW: Event bus for tag detection events

	// State (protected by mu)
	mu             sync.RWMutex
	lastPacketTime time.Time
	isAutoReading  bool
	isRunning      bool
	readingCount   uint64
	errorCount     uint64
	lastTagEPC     string
	lastTagRSSI    int

	// Concurrency control
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	doneCh   chan struct{}
	doneOnce sync.Once
}

// NewAntennaManager creates a new antenna manager.
func NewAntennaManager(
	client CommandSender,
	antCfg config.AntennaConfig,
	gwCfg *config.GatewayConfig,
	cache Cache,
) *AntennaManager {
	return &AntennaManager{
		client:         client,
		config:         antCfg,
		gatewayConfig:  gwCfg,
		cache:          cache,
		lastPacketTime: time.Now(),
		doneCh:         make(chan struct{}),
	}
}

// SetConn sets the TCP connection for the manager (must be called before Start).
func (m *AntennaManager) SetConn(conn net.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.conn = conn
}

// SetEventBus injects the event bus for publishing tag detection events.
func (m *AntennaManager) SetEventBus(bus *events.EventBus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventBus = bus
}

// Done returns a channel that is closed when the manager stops.
func (m *AntennaManager) Done() <-chan struct{} {
	return m.doneCh
}

// Start begins the intelligent read loop, heartbeat, and data reader.
// Returns error if manager is already running.
func (m *AntennaManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return errors.New("antenna manager already running")
	}

	if m.conn == nil {
		return errors.New("connection not set, call SetConn first")
	}

	// Create child context
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Mark as running
	m.isRunning = true

	// Start goroutines
	m.wg.Add(4)
	go m.intelligentReadLoop()
	go m.heartbeatPing()
	go m.dataReader(m.ctx, m.conn)
	go m.monitorContext(ctx)

	return nil
}

// monitorContext monitors the parent context and stops the manager when cancelled.
func (m *AntennaManager) monitorContext(parentCtx context.Context) {
	defer m.wg.Done()

	for {
		select {
		case <-parentCtx.Done():
			// Parent context cancelled - stop the manager
			m.mu.Lock()
			if m.isRunning {
				m.isRunning = false
				if m.cancel != nil {
					m.cancel()
				}
			}
			m.mu.Unlock()
			m.closeDoneOnce()
			return
		case <-m.ctx.Done():
			// Child context cancelled (e.g., by Stop())
			m.closeDoneOnce()
			return
		case <-time.After(100 * time.Millisecond):
			// Periodic check to ensure responsiveness
			continue
		}
	}
}

// Stop gracefully shuts down all goroutines.
func (m *AntennaManager) Stop() error {
	m.mu.Lock()
	if !m.isRunning {
		m.mu.Unlock()
		return nil
	}

	m.isRunning = false

	// Cancel context to stop goroutines
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.closeDoneOnce()
		return nil
	case <-time.After(5 * time.Second):
		m.closeDoneOnce()
		return errors.New("timeout waiting for antenna manager to stop")
	}
}

// closeDoneOnce closes doneCh exactly once, safe for concurrent calls.
func (m *AntennaManager) closeDoneOnce() {
	m.doneOnce.Do(func() {
		close(m.doneCh)
	})
}

// IsRunning returns true if the manager is active.
func (m *AntennaManager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isRunning
}

// GetStatus returns current antenna status for TUI display.
func (m *AntennaManager) GetStatus() AntennaStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return AntennaStatus{
		ID:           m.config.ID,
		Connected:    m.isRunning && m.client.IsConnected(),
		ReadingCount: m.readingCount,
		LastTagEPC:   m.lastTagEPC,
		LastTagRSSI:  m.lastTagRSSI,
		LastSeen:     m.lastPacketTime,
		AutoReading:  m.isAutoReading,
		ErrorCount:   m.errorCount,
	}
}

// intelligentReadLoop sends Read UII commands with adaptive delay.
// Runs as a goroutine until context cancellation or write error.
// In passive mode, does NOT send commands — just waits (dataReader handles incoming data).
func (m *AntennaManager) intelligentReadLoop() {
	defer m.wg.Done()

	// Passive mode: don't send commands, antenna auto-sends data
	if m.gatewayConfig.ListenMode == "passive" {
		log.Printf("[Antenna %s] Read loop in PASSIVE mode — not sending commands", m.config.ID)
		<-m.ctx.Done()
		return
	}

	// Wait 2 seconds before first command (like Node.js)
	select {
	case <-time.After(2 * time.Second):
	case <-m.ctx.Done():
		return
	}

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		// Calculate adaptive delay
		delay := m.calculateAdaptiveDelay()

		// Send Read UII command: [0x7c, 0xff, 0xff, 0x20, 0x00, 0x00, checksum]
		command := []byte{0x7c, 0xff, 0xff, 0x20, 0x00, 0x00}
		checksum := calculateChecksum(command)
		fullCommand := append(command, checksum)

		err := m.client.SendCommand(fullCommand)
		if err != nil {
			// Write error - connection dead, cancel context to trigger reconnection
			m.cancel()
			return
		}

		// Wait for next iteration
		select {
		case <-time.After(delay):
		case <-m.ctx.Done():
			return
		}
	}
}

// calculateAdaptiveDelay determines the delay based on data recency.
// Returns: 3s (recent data), 1s (stale data), 1.5s (default), or 5s (auto-reading).
func (m *AntennaManager) calculateAdaptiveDelay() time.Duration {
	m.mu.RLock()
	isAuto := m.isAutoReading
	lastPacket := m.lastPacketTime
	m.mu.RUnlock()

	// Auto-reading mode overrides all
	if isAuto {
		return m.gatewayConfig.AdaptiveDelayAutoReading
	}

	timeSinceLastData := time.Since(lastPacket)

	// Recent data (< 2s): wait longer (tags are being read actively)
	if timeSinceLastData < m.gatewayConfig.AdaptiveDelayRecentWindow {
		return m.gatewayConfig.AdaptiveDelayRecent
	}

	// Stale data (> 10s): wait shorter (no tags, check more often)
	if timeSinceLastData > m.gatewayConfig.AdaptiveDelayStaleWindow {
		return m.gatewayConfig.AdaptiveDelayStale
	}

	// Default: 1.5s
	return 1500 * time.Millisecond
}

// heartbeatPing sends heartbeat commands when the connection is silent.
// Runs as a goroutine until context cancellation.
func (m *AntennaManager) heartbeatPing() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.gatewayConfig.HeartbeatInterval)
	defer ticker.Stop()

	consecutiveSilentIntervals := 0

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			// Check if we should send heartbeat
			m.mu.RLock()
			timeSinceLastPacket := time.Since(m.lastPacketTime)
			m.mu.RUnlock()

			if timeSinceLastPacket <= m.gatewayConfig.HeartbeatSilenceThreshold {
				// Data received recently, reset counter
				consecutiveSilentIntervals = 0
				continue
			}

			// Send heartbeat ping
			consecutiveSilentIntervals++

			// Send Get Reader Info command: [0x7c, 0xff, 0xff, 0x01, 0x00, 0x00, checksum]
			command := []byte{0x7c, 0xff, 0xff, 0x01, 0x00, 0x00}
			checksum := calculateChecksum(command)
			fullCommand := append(command, checksum)

			err := m.client.SendCommand(fullCommand)
			if err != nil {
				// Write error - connection dead, cancel context to trigger reconnection
				m.cancel()
				return
			}

			// Detect dead connection after 3 silent intervals without response
			if consecutiveSilentIntervals >= 3 {
				// Cancel context to trigger reconnection
				m.mu.Lock()
				if m.cancel != nil {
					m.cancel()
				}
				m.mu.Unlock()
				return
			}
		}
	}
}

// UpdateLastPacketTime updates the last packet time and detects auto-reading.
// Called by data reader when a packet is received.
func (m *AntennaManager) UpdateLastPacketTime() {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldTime := m.lastPacketTime
	m.lastPacketTime = time.Now()

	// Detect auto-reading: if data arrives without being polled
	// (we detect this on first packet after a significant gap)
	if !m.isAutoReading && time.Since(oldTime) > m.gatewayConfig.AdaptiveDelayRecentWindow {
		m.isAutoReading = true
	}

	// Update client's last activity
	m.client.SetLastActivity(m.lastPacketTime)
}

// ResetAutoReading resets the auto-reading flag (called when no data for 10s).
func (m *AntennaManager) ResetAutoReading() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isAutoReading {
		m.isAutoReading = false
	}
}

// HandlePacket processes a raw packet based on its RTN code.
// Routes to appropriate handler based on RTN code at byte index 4.
func (m *AntennaManager) HandlePacket(data []byte) error {
	// NEGATIVE: Check minimum packet size
	if len(data) < 6 {
		return fmt.Errorf("packet too short: %d bytes (min 6)", len(data))
	}

	// NEGATIVE: Validate framing
	if data[0] != 0x7c && data[0] != 0xcc {
		return fmt.Errorf("invalid start byte: 0x%02x", data[0])
	}

	// NEGATIVE: Validate padding
	if data[1] != 0xff || data[2] != 0xff {
		return fmt.Errorf("invalid padding bytes")
	}

	// Extract RTN code (byte 4)
	rtnCode := data[3]

	// Update last packet time (shared by all handlers)
	m.UpdateLastPacketTime()

	// Route based on RTN code
	switch rtnCode {
	case 0x00:
		return m.handleACK(data)
	case 0x02:
		return m.handleUIIData(data)
	case 0x06:
		return m.handleTagData(data)
	case 0x07:
		return m.handleError(data)
	case 0x10:
		return m.handleHeartbeatResponse(data)
	default:
		return m.handleUnknownRTN(data, rtnCode)
	}
}

// handleACK processes RTN 0x00 (ACK) packets.
func (m *AntennaManager) handleACK(data []byte) error {
	// ACK confirms command received, just log at debug level
	log.Printf("[DEBUG] ACK received from antenna %s", m.config.ID)
	return nil
}

// handleUIIData processes RTN 0x02 (UII Data) packets.
// Parses ANT/PC/EPC/RSSI and stores Reading in cache.
func (m *AntennaManager) handleUIIData(data []byte) error {
	// Extract data portion (starts at byte 5)
	if len(data) < 6 {
		return fmt.Errorf("UII data packet too short")
	}

	dataLen := int(data[4])
	if len(data) < 5+dataLen+1 {
		return fmt.Errorf("UII data packet truncated: expected %d bytes, have %d", 5+dataLen+1, len(data))
	}

	// Data portion: [ANT(1), PC(2), EPC(N), RSSI(1)]
	dataPortion := data[5 : 5+dataLen]

	// Parse antenna data using shared protocol package
	parsed, err := protocol.ParseAntennaData(dataPortion)
	if err != nil {
		return fmt.Errorf("failed to parse UII data: %w", err)
	}

	// Create Reading using ToReading method
	reading := parsed.ToReading(m.config.ID, m.gatewayConfig.GatewayID)

	// Store in cache
	if err := m.cache.Store(reading); err != nil {
		return fmt.Errorf("failed to store reading: %w", err)
	}

	// Publish event for real-time UI (SSE)
	if m.eventBus != nil {
		m.eventBus.Publish(events.TagDetected{
			EPC:       reading.EPC,
			RSSI:      reading.RSSI,
			AntennaID: m.config.ID,
			Timestamp: reading.Timestamp,
		})
	}

	// Update manager stats
	m.mu.Lock()
	m.readingCount++
	m.lastTagEPC = reading.EPC
	m.lastTagRSSI = reading.RSSI
	m.mu.Unlock()

	log.Printf("[INFO] UII detected from antenna %s: EPC=%s RSSI=%d",
		m.config.ID, reading.EPC, reading.RSSI)

	return nil
}

// handleTagData processes RTN 0x06 (Tag Data) packets.
// Logs and forwards but does NOT create a Reading.
func (m *AntennaManager) handleTagData(data []byte) error {
	// Tag Data is logged but not stored as a Reading
	log.Printf("[INFO] Tag Data received from antenna %s (not a UII scan)", m.config.ID)
	return nil
}

// handleError processes RTN 0x07 (Error) packets.
// Logs error and increments error counter.
func (m *AntennaManager) handleError(data []byte) error {
	// Log error with hex payload
	log.Printf("[ERROR] Antenna %s error: %X", m.config.ID, data)

	// Increment error counter
	m.mu.Lock()
	m.errorCount++
	m.mu.Unlock()

	return nil
}

// handleHeartbeatResponse processes RTN 0x10 (Heartbeat Response) packets.
func (m *AntennaManager) handleHeartbeatResponse(data []byte) error {
	// Heartbeat response confirms connection is alive
	log.Printf("[DEBUG] Heartbeat response received from antenna %s", m.config.ID)
	return nil
}

// handleUnknownRTN handles unknown/unsupported RTN codes.
func (m *AntennaManager) handleUnknownRTN(data []byte, rtnCode byte) error {
	// Log warning with RTN code and hex payload
	log.Printf("[WARN] Unknown RTN code 0x%02x from antenna %s: %X",
		rtnCode, m.config.ID, data)
	return nil
}

// dataReader continuously reads packets from the TCP connection.
// Validates packet checksum and routes via HandlePacket.
// Exits on read error or context cancellation.
func (m *AntennaManager) dataReader(ctx context.Context, conn net.Conn) {
	defer m.wg.Done()
	// NOTE: Do NOT close conn here — connection lifecycle is managed by rawtcp.Client

	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set read deadline to allow periodic context checking
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

		n, err := conn.Read(buf)
		if err != nil {
			// Check if it's a timeout (we can continue)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			// EOF or other error - connection closed
			if err != io.EOF {
				log.Printf("[WARN] Read error from antenna %s: %v", m.config.ID, err)
			}
			return
		}

		if n == 0 {
			continue
		}

		// Process the packet
		packet := make([]byte, n)
		copy(packet, buf[:n])

		// Validate checksum before processing
		if len(packet) >= 7 {
			if !protocol.ValidateChecksum(packet) {
				log.Printf("[WARN] Invalid checksum from antenna %s, skipping packet: %X", m.config.ID, packet)
				continue // Skip corrupted packets
			}
		}

		// Handle the packet
		if err := m.HandlePacket(packet); err != nil {
			log.Printf("[ERROR] Failed to handle packet from antenna %s: %v", m.config.ID, err)
			// Continue processing - don't stop on parse errors
		}
	}
}

// calculateChecksum computes the two's complement checksum for commands.
func calculateChecksum(data []byte) byte {
	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	return byte((^sum + 1) & 0xff)
}
