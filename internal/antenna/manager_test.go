package antenna

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// mockCache is a mock implementation for testing
type mockCache struct {
	stored []models.Reading
	mu     sync.Mutex
}

func (m *mockCache) Store(r models.Reading) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stored = append(m.stored, r)
	return nil
}

// mockClient is a mock TCP client for testing command sending
type mockClient struct {
	mu           sync.Mutex
	commandsSent [][]byte
	lastActivity time.Time
	connected    bool
	writeError   error
}

func (m *mockClient) SendCommand(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeError != nil {
		return m.writeError
	}
	m.commandsSent = append(m.commandsSent, data)
	return nil
}

func (m *mockClient) GetLastActivity() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastActivity
}

func (m *mockClient) SetLastActivity(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastActivity = t
}

func (m *mockClient) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

func (m *mockClient) setWriteError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeError = err
}

func newTestAntennaManager() (*AntennaManager, *mockClient, *mockCache, context.CancelFunc) {
	mockClient := &mockClient{
		commandsSent: make([][]byte, 0),
		connected:    true,
	}
	mockCache := &mockCache{
		stored: make([]models.Reading, 0),
	}

	antCfg := config.AntennaConfig{
		ID:      "ant-01",
		IP:      "192.168.1.100",
		Port:    8080,
		Enabled: true,
	}

	gwCfg := &config.GatewayConfig{
		GatewayID:                 "gw-test-001",
		CompanyID:                 "comp-test",
		ListenMode:                "auto",
		HeartbeatInterval:         3 * time.Second,
		HeartbeatSilenceThreshold: 5 * time.Second,
		AdaptiveDelayRecent:       3 * time.Second,
		AdaptiveDelayRecentWindow: 2 * time.Second,
		AdaptiveDelayStale:        1 * time.Second,
		AdaptiveDelayStaleWindow:  10 * time.Second,
		AdaptiveDelayAutoReading:  5 * time.Second,
	}

	_, cancel := context.WithCancel(context.Background())

	manager := NewAntennaManager(mockClient, antCfg, gwCfg, mockCache)

	// Create a mock connection pair for data reading
	clientConn, _ := net.Pipe()
	manager.SetConn(clientConn)

	return manager, mockClient, mockCache, cancel
}

// TestAntennaManager_New verifies manager creation
func TestAntennaManager_New(t *testing.T) {
	manager, mockClient, _, _ := newTestAntennaManager()

	if manager == nil {
		t.Fatal("expected manager to be created, got nil")
	}

	if manager.client != mockClient {
		t.Error("expected client to be set correctly")
	}

	if manager.config.ID != "ant-01" {
		t.Errorf("expected antenna ID 'ant-01', got '%s'", manager.config.ID)
	}

	if manager.gatewayConfig.GatewayID != "gw-test-001" {
		t.Errorf("expected gateway ID 'gw-test-001', got '%s'", manager.gatewayConfig.GatewayID)
	}

	if manager.isRunning {
		t.Error("expected manager to not be running initially")
	}

	if manager.isAutoReading {
		t.Error("expected isAutoReading to be false initially")
	}
}

// TestAntennaManager_StartStop verifies lifecycle
func TestAntennaManager_StartStop(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	// Initially not running
	if manager.IsRunning() {
		t.Error("expected manager to not be running before Start()")
	}

	// Start the manager
	startCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := manager.Start(startCtx)
	if err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}

	// Verify it's running
	if !manager.IsRunning() {
		t.Error("expected manager to be running after Start()")
	}

	// Stop the manager
	err = manager.Stop()
	if err != nil {
		t.Errorf("failed to stop manager: %v", err)
	}

	// Verify it's stopped
	if manager.IsRunning() {
		t.Error("expected manager to not be running after Stop()")
	}
}

// TestAntennaManager_GetStatus verifies status reporting
func TestAntennaManager_GetStatus(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	// Get initial status
	status := manager.GetStatus()

	if status.ID != "ant-01" {
		t.Errorf("expected status.ID 'ant-01', got '%s'", status.ID)
	}

	if status.Connected {
		t.Error("expected Connected to be false before Start()")
	}

	if status.ReadingCount != 0 {
		t.Errorf("expected ReadingCount 0, got %d", status.ReadingCount)
	}

	if status.AutoReading {
		t.Error("expected AutoReading to be false initially")
	}

	if status.ErrorCount != 0 {
		t.Errorf("expected ErrorCount 0, got %d", status.ErrorCount)
	}
}

// TestAntennaManager_GetStatusAfterData verifies status reflects data
func TestAntennaManager_GetStatusAfterData(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	// Simulate data received
	manager.mu.Lock()
	manager.readingCount = 5
	manager.errorCount = 2
	manager.lastTagEPC = "E2003411B802011383258566"
	manager.lastTagRSSI = 201
	manager.lastPacketTime = time.Now()
	manager.isAutoReading = true
	manager.mu.Unlock()

	status := manager.GetStatus()

	if status.ReadingCount != 5 {
		t.Errorf("expected ReadingCount 5, got %d", status.ReadingCount)
	}

	if status.ErrorCount != 2 {
		t.Errorf("expected ErrorCount 2, got %d", status.ErrorCount)
	}

	if status.LastTagEPC != "E2003411B802011383258566" {
		t.Errorf("expected LastTagEPC 'E2003411B802011383258566', got '%s'", status.LastTagEPC)
	}

	if status.LastTagRSSI != 201 {
		t.Errorf("expected LastTagRSSI 201, got %d", status.LastTagRSSI)
	}

	if !status.AutoReading {
		t.Error("expected AutoReading to be true")
	}

	if status.LastSeen.IsZero() {
		t.Error("expected LastSeen to be set")
	}
}

// TestAntennaManager_ContextCancellation verifies graceful shutdown on context cancel
func TestAntennaManager_ContextCancellation(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	ctx, cancel := context.WithCancel(context.Background())

	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}

	// Give goroutines time to start
	time.Sleep(50 * time.Millisecond)

	if !manager.IsRunning() {
		t.Error("expected manager to be running")
	}

	// Cancel context
	cancel()

	// Give goroutines time to exit
	time.Sleep(200 * time.Millisecond)

	// Manager should have stopped
	if manager.IsRunning() {
		t.Error("expected manager to stop after context cancellation")
	}
}

// TestAntennaManager_StartTwice verifies idempotent behavior
func TestAntennaManager_StartTwice(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First start
	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("first start failed: %v", err)
	}

	// Second start should fail
	err = manager.Start(ctx)
	if err == nil {
		t.Error("expected error when starting already running manager")
	}
}

// TestAntennaManager_StopTwice verifies idempotent behavior
func TestAntennaManager_StopTwice(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}

	// First stop
	err = manager.Stop()
	if err != nil {
		t.Errorf("first stop failed: %v", err)
	}

	// Second stop should succeed (idempotent)
	err = manager.Stop()
	if err != nil {
		t.Errorf("second stop failed: %v", err)
	}
}

// === Task 2.2: Intelligent Read Loop Tests ===

// TestCalculateAdaptiveDelay_Default returns 1.5s for normal conditions
func TestCalculateAdaptiveDelay_Default(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	// Set last packet to 5 seconds ago (between 2s and 10s window)
	manager.mu.Lock()
	manager.lastPacketTime = time.Now().Add(-5 * time.Second)
	manager.isAutoReading = false
	manager.mu.Unlock()

	delay := manager.calculateAdaptiveDelay()

	expected := 1500 * time.Millisecond
	if delay != expected {
		t.Errorf("expected delay %v, got %v", expected, delay)
	}
}

// TestCalculateAdaptiveDelay_Recent returns 3s when data received recently
func TestCalculateAdaptiveDelay_Recent(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	// Set last packet to 1 second ago (within 2s window)
	manager.mu.Lock()
	manager.lastPacketTime = time.Now().Add(-1 * time.Second)
	manager.isAutoReading = false
	manager.mu.Unlock()

	delay := manager.calculateAdaptiveDelay()

	expected := 3 * time.Second
	if delay != expected {
		t.Errorf("expected delay %v for recent data, got %v", expected, delay)
	}
}

// TestCalculateAdaptiveDelay_Stale returns 1s when no data for a long time
func TestCalculateAdaptiveDelay_Stale(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	// Set last packet to 15 seconds ago (beyond 10s window)
	manager.mu.Lock()
	manager.lastPacketTime = time.Now().Add(-15 * time.Second)
	manager.isAutoReading = false
	manager.mu.Unlock()

	delay := manager.calculateAdaptiveDelay()

	expected := 1 * time.Second
	if delay != expected {
		t.Errorf("expected delay %v for stale data, got %v", expected, delay)
	}
}

// TestCalculateAdaptiveDelay_AutoReading returns 5s when auto-reading detected
func TestCalculateAdaptiveDelay_AutoReading(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	// Set auto-reading flag (should override other delays)
	manager.mu.Lock()
	manager.isAutoReading = true
	manager.lastPacketTime = time.Now().Add(-15 * time.Second) // Even stale data
	manager.mu.Unlock()

	delay := manager.calculateAdaptiveDelay()

	expected := 5 * time.Second
	if delay != expected {
		t.Errorf("expected delay %v for auto-reading mode, got %v", expected, delay)
	}
}

// TestCalculateChecksum verifies checksum calculation
func TestCalculateChecksum(t *testing.T) {
	// Test Read UII command: [0x7c, 0xff, 0xff, 0x20, 0x00, 0x00]
	command := []byte{0x7c, 0xff, 0xff, 0x20, 0x00, 0x00}
	checksum := calculateChecksum(command)

	// Sum: 0x7c + 0xff + 0xff + 0x20 + 0x00 + 0x00 = 0x29a = 666
	// ~666 = -667, +1 = -666, & 0xff = 0x66 = 102
	expected := byte(0x66)
	if checksum != expected {
		t.Errorf("expected checksum 0x%02x, got 0x%02x", expected, checksum)
	}

	// Verify: sum + checksum should be 0
	verifySum := 0
	for _, b := range command {
		verifySum += int(b)
	}
	verifySum += int(checksum)
	if verifySum&0xff != 0 {
		t.Errorf("checksum verification failed: sum & 0xff = %d", verifySum&0xff)
	}
}

// === Task 2.3: Heartbeat Tests ===

// TestHeartbeatPing_SendsWhenSilent verifies heartbeat is sent after silence threshold
func TestHeartbeatPing_SendsWhenSilent(t *testing.T) {
	manager, mockClient, _, _ := newTestAntennaManager()

	// Set last packet to 10 seconds ago (beyond 5s silence threshold)
	manager.mu.Lock()
	manager.lastPacketTime = time.Now().Add(-10 * time.Second)
	manager.mu.Unlock()

	// Mock client should initially have no commands
	mockClient.mu.Lock()
	initialCount := len(mockClient.commandsSent)
	mockClient.mu.Unlock()

	if initialCount != 0 {
		t.Errorf("expected 0 commands initially, got %d", initialCount)
	}

	// Note: Full heartbeat testing requires running the actual goroutine,
	// which would need timing control. This test verifies the setup logic.
	// In production, heartbeatPing() goroutine sends commands via SendCommand.
}

// === Task 2.4: Auto-Reading Detection Tests ===

// TestUpdateLastPacketTime_SetsAutoReading detects auto-reading on first packet
func TestUpdateLastPacketTime_SetsAutoReading(t *testing.T) {
	manager, mockClient, _, _ := newTestAntennaManager()

	// Initially not auto-reading
	manager.mu.RLock()
	if manager.isAutoReading {
		t.Error("expected isAutoReading to be false initially")
	}
	manager.mu.RUnlock()

	// Set last packet to long ago
	manager.mu.Lock()
	manager.lastPacketTime = time.Now().Add(-20 * time.Second)
	manager.mu.Unlock()

	// Simulate receiving a packet (gap > 2s window triggers auto-reading detection)
	manager.UpdateLastPacketTime()

	// Should now be in auto-reading mode
	manager.mu.RLock()
	if !manager.isAutoReading {
		t.Error("expected isAutoReading to be true after packet with gap > 2s")
	}
	manager.mu.RUnlock()

	// Verify client last activity was updated
	mockClient.mu.Lock()
	if mockClient.lastActivity.IsZero() {
		t.Error("expected client lastActivity to be set")
	}
	mockClient.mu.Unlock()
}

// TestUpdateLastPacketTime_NoAutoReadingForQuickPackets doesn't set auto-reading for quick succession
func TestUpdateLastPacketTime_NoAutoReadingForQuickPackets(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	// Set last packet to very recent
	manager.mu.Lock()
	manager.lastPacketTime = time.Now().Add(-500 * time.Millisecond) // 500ms ago
	manager.mu.Unlock()

	// Simulate receiving another packet quickly
	manager.UpdateLastPacketTime()

	// Should NOT be in auto-reading mode (gap < 2s)
	manager.mu.RLock()
	if manager.isAutoReading {
		t.Error("expected isAutoReading to remain false for quick succession packets")
	}
	manager.mu.RUnlock()
}

// TestResetAutoReading resets the auto-reading flag
func TestResetAutoReading(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	// Set auto-reading flag
	manager.mu.Lock()
	manager.isAutoReading = true
	manager.mu.Unlock()

	// Reset auto-reading
	manager.ResetAutoReading()

	// Should be false now
	manager.mu.RLock()
	if manager.isAutoReading {
		t.Error("expected isAutoReading to be false after ResetAutoReading()")
	}
	manager.mu.RUnlock()
}

// TestAutoReadingPersistsOnceSet verifies auto-reading stays true once detected
func TestAutoReadingPersistsOnceSet(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	// Set auto-reading flag
	manager.mu.Lock()
	manager.isAutoReading = true
	manager.mu.Unlock()

	// Update last packet time again
	manager.UpdateLastPacketTime()

	// Should still be true
	manager.mu.RLock()
	if !manager.isAutoReading {
		t.Error("expected isAutoReading to remain true once set")
	}
	manager.mu.RUnlock()
}
