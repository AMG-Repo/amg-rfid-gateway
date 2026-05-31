package rawtcp

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
	"github.com/amg-rfid/amg-rfid-shared-go/protocol"
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

func TestNewClient(t *testing.T) {
	client := NewClient("192.168.1.100", 6000)

	if client.ip != "192.168.1.100" {
		t.Errorf("expected IP '192.168.1.100', got '%s'", client.ip)
	}
	if client.port != 6000 {
		t.Errorf("expected port 6000, got %d", client.port)
	}
}

func TestClient_Address(t *testing.T) {
	client := NewClient("192.168.1.100", 6000)
	addr := client.Address()

	expected := "192.168.1.100:6000"
	if addr != expected {
		t.Errorf("expected address '%s', got '%s'", expected, addr)
	}
}

func TestClient_Connect_Disconnect(t *testing.T) {
	// Create mock TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Get the port
	addr := listener.Addr().(*net.TCPAddr)

	client := NewClient("127.0.0.1", addr.Port)

	// Start server in background
	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			// Read some data and close
			buf := make([]byte, 1024)
			conn.Read(buf)
			time.Sleep(50 * time.Millisecond)
			conn.Close()
		}
	}()

	// Connect
	err = client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	if !client.IsConnected() {
		t.Error("expected client to be connected")
	}

	// Disconnect
	err = client.Disconnect()
	if err != nil {
		t.Errorf("failed to disconnect: %v", err)
	}

	if client.IsConnected() {
		t.Error("expected client to be disconnected")
	}
}

func TestClient_Connect_InvalidAddress(t *testing.T) {
	client := NewClient("invalid.address.here", 99999)

	err := client.Connect()
	if err == nil {
		t.Error("expected error for invalid address, got nil")
	}
}

func TestParsePacket_ValidDataPacket(t *testing.T) {
	// Valid response packet with UII data
	// [0xCC, 0xFF, 0xFF, CID1=0x20, RTN=0x02, LEN=0x04, INFO(4), CHKSUM]
	data := []byte{0xCC, 0xff, 0xff, 0x20, 0x02, 0x04, 0xE2, 0x00, 0x34, 0x15, 0x00}

	packet, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("failed to parse packet: %v", err)
	}

	if packet.CID1 != protocol.CID1ReadTypeCUII || packet.CID2OrRTN != protocol.RTNUIIRead {
		t.Errorf("expected CID1/RTN 0x20/0x02, got 0x%02x/0x%02x", packet.CID1, packet.CID2OrRTN)
	}

	if !packet.IsDataPacket() {
		t.Error("expected packet to be a data packet")
	}
}

func TestParsePacket_InvalidStartByte(t *testing.T) {
	// Invalid start byte
	data := []byte{0x00, 0xff, 0xff, 0x20, 0x00, 0x00, 0x00}

	_, err := ParsePacket(data)
	if err == nil {
		t.Error("expected error for invalid start byte, got nil")
	}
}

func TestParsePacket_TooShort(t *testing.T) {
	// Packet too short
	data := []byte{0x7c, 0xff, 0xff}

	_, err := ParsePacket(data)
	if err == nil {
		t.Error("expected error for short packet, got nil")
	}
}

func TestAntennaClient_Run(t *testing.T) {
	// Create mock TCP server that sends a valid packet
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	cache := &mockCache{}

	// Valid UII packet: [SOI, ADR1, ADR2, CID1=0x20, RTN=0x02, LEN, INFO..., CHKSUM]
	// Checksum calculation: sum of all bytes except checksum, then two's complement
	// For simplicity, let's send a packet and let the server calculate
	packet := []byte{0xCC, 0xff, 0xff, 0x20, 0x02, 0x04, 0xE2, 0x00, 0x34, 0x15, 0x4A}

	// Start mock server
	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			// Send packet
			conn.Write(packet)
			// Keep connection open for a bit
			time.Sleep(200 * time.Millisecond)
		}
	}()

	client := NewClient("127.0.0.1", addr.Port)
	antenna := AntennaConfig{
		ID:      "ant-1",
		Enabled: true,
	}

	// Run client (will connect, read, and close)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	go func() {
		done <- RunAntenna(ctx, client, cache, antenna)
	}()

	// Wait for completion or timeout
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, errors.New("connection closed")) {
			t.Logf("RunAntenna returned: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		// Expected - the connection will be closed by server
	}

	// Give time for processing
	time.Sleep(100 * time.Millisecond)
}

func TestRunAntenna_InvalidChecksumDataPacket_DoesNotStoreReading(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	cache := &mockCache{}

	// Canonical frame with invalid checksum byte (should parse but be rejected for storage).
	invalidChecksumPacket := []byte{0xCC, 0xFF, 0xFF, 0x20, 0x02, 0x04, 0xE2, 0x00, 0x34, 0x15, 0x00}

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			_, _ = conn.Write(invalidChecksumPacket)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	client := NewClient("127.0.0.1", addr.Port)
	antenna := AntennaConfig{ID: "ant-1", Enabled: true}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err = RunAntenna(ctx, client, cache, antenna)
	if err == nil {
		t.Fatal("expected RunAntenna to stop with connection closed or context deadline")
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.stored) != 0 {
		t.Fatalf("expected no readings stored for invalid checksum packet, got %d", len(cache.stored))
	}
}

func TestAntennaConfig_Validate(t *testing.T) {
	// Valid config
	ant := AntennaConfig{
		ID:      "ant-1",
		Enabled: true,
	}
	if err := ant.Validate(); err != nil {
		t.Errorf("expected valid config, got: %v", err)
	}

	// Invalid - empty ID
	ant2 := AntennaConfig{
		ID:      "",
		Enabled: true,
	}
	if err := ant2.Validate(); err == nil {
		t.Error("expected error for empty ID")
	}
}
