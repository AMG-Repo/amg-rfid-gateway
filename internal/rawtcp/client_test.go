package rawtcp

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	antennapkg "github.com/amg-rfid/amg-rfid-gateway/internal/antenna"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
	"github.com/amg-rfid/amg-rfid-shared-go/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	packet := mustBuildGenericUIIFrame(t)

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
		if err != nil && err.Error() != "connection closed" {
			t.Fatalf("expected connection closed or nil, got: %v", err)
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

func TestRunAntenna_StreamReassemblyScenarios(t *testing.T) {
	validPacketA := mustBuildGenericUIIFrameWithEPCLastByte(t, 0x66)
	validPacketB := mustBuildGenericUIIFrameWithEPCLastByte(t, 0x67)
	invalidPacket := append([]byte(nil), validPacketA...)
	invalidPacket[len(invalidPacket)-1] ^= 0xFF

	tests := []struct {
		name          string
		chunks        [][]byte
		expectedStore int
	}{
		{
			name: "split frame across reads stores exactly one reading",
			chunks: [][]byte{
				append([]byte(nil), validPacketA[:5]...),
				append([]byte(nil), validPacketA[5:]...),
			},
			expectedStore: 1,
		},
		{
			name: "coalesced frames in single read store all in order",
			chunks: [][]byte{
				append(append([]byte(nil), validPacketA...), validPacketB...),
			},
			expectedStore: 2,
		},
		{
			name: "noise before SOI is ignored and frame is stored",
			chunks: [][]byte{
				append([]byte{0x01, 0x02, 0x03, 0x04}, validPacketA...),
			},
			expectedStore: 1,
		},
		{
			name: "invalid checksum followed by valid frame stores only valid",
			chunks: [][]byte{
				append(append([]byte(nil), invalidPacket...), validPacketB...),
			},
			expectedStore: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("failed to create listener: %v", err)
			}
			defer listener.Close()

			addr := listener.Addr().(*net.TCPAddr)
			cache := &mockCache{}

			go func() {
				conn, _ := listener.Accept()
				if conn == nil {
					return
				}
				defer conn.Close()
				for _, chunk := range tc.chunks {
					_, _ = conn.Write(chunk)
				}
				time.Sleep(100 * time.Millisecond)
			}()

			client := NewClient("127.0.0.1", addr.Port)
			antenna := AntennaConfig{ID: "ant-1", Enabled: true}

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			err = RunAntenna(ctx, client, cache, antenna)
			if err != nil && err.Error() != "connection closed" {
				t.Fatalf("expected connection closed or nil, got: %v", err)
			}

			cache.mu.Lock()
			stored := len(cache.stored)
			cache.mu.Unlock()

			if stored != tc.expectedStore {
				t.Fatalf("expected %d readings stored, got %d", tc.expectedStore, stored)
			}
		})
	}
}

func TestRunAntenna_ProtocolAwareDispatch(t *testing.T) {
	tests := []struct {
		name          string
		protocol      config.AntennaProtocol
		expectedStore int
		expectedEPC   string
	}{
		{
			name:          "generic dispatch stores generic reading",
			protocol:      config.ProtocolGeneric,
			expectedStore: 1,
			expectedEPC:   "E2003411B802011383258566",
		},
		{
			name:          "unsupported protocol emits no tag storage",
			protocol:      config.ProtocolZebra,
			expectedStore: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("failed to create listener: %v", err)
			}
			defer listener.Close()

			addr := listener.Addr().(*net.TCPAddr)
			cache := &mockCache{}

			go func() {
				conn, _ := listener.Accept()
				if conn == nil {
					return
				}
				defer conn.Close()
				_, _ = conn.Write(mustBuildGenericUIIFrame(t))
				time.Sleep(100 * time.Millisecond)
			}()

			client := NewClient("127.0.0.1", addr.Port)
			antenna := AntennaConfig{ID: "ant-1", Enabled: true, Protocol: tt.protocol}

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			err = RunAntenna(ctx, client, cache, antenna)
			if tt.protocol == config.ProtocolZebra {
				if !errors.Is(err, antennapkg.ErrUnsupportedProtocol) {
					t.Fatalf("expected unsupported protocol error, got: %v", err)
				}
			} else if err != nil && err.Error() != "connection closed" {
				t.Fatalf("expected connection closed or nil, got: %v", err)
			}

			cache.mu.Lock()
			stored := append([]models.Reading(nil), cache.stored...)
			cache.mu.Unlock()

			if len(stored) != tt.expectedStore {
				t.Fatalf("expected %d readings stored, got %d", tt.expectedStore, len(stored))
			}
			if tt.expectedStore > 0 && stored[0].EPC != tt.expectedEPC {
				t.Fatalf("expected EPC %q, got %q", tt.expectedEPC, stored[0].EPC)
			}
		})
	}
}

func TestRunAntenna_ZebraReturnsUnsupportedProtocol(t *testing.T) {
	client, _, closeServer := newPacketServerClient(t, [][]byte{{0x01, 0x02, 0x03, 0x04}})
	defer closeServer()
	cache := &mockCache{}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := RunAntenna(ctx, client, cache, AntennaConfig{
		ID:       "ant-zebra",
		Enabled:  true,
		Protocol: config.ProtocolZebra,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, antennapkg.ErrUnsupportedProtocol)
	assert.Contains(t, err.Error(), "ant-zebra")

	cache.mu.Lock()
	stored := len(cache.stored)
	cache.mu.Unlock()
	assert.Equal(t, 0, stored)
}

func TestRunAntenna_MixedProtocolFailureIsolation(t *testing.T) {
	genericCache := &mockCache{}
	unsupportedCache := &mockCache{}

	genericClient, genericPort, closeGeneric := newPacketServerClient(t, [][]byte{mustBuildGenericUIIFrame(t)})
	defer closeGeneric()
	unsupportedClient, unsupportedPort, closeUnsupported := newPacketServerClient(t, [][]byte{mustBuildGenericUIIFrame(t)})
	defer closeUnsupported()

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	genericDone := make(chan error, 1)
	unsupportedDone := make(chan error, 1)
	go func() {
		genericDone <- RunAntenna(ctx, genericClient, genericCache, AntennaConfig{
			ID:       "ant-generic",
			Enabled:  true,
			Protocol: config.ProtocolGeneric,
		})
	}()
	go func() {
		unsupportedDone <- RunAntenna(ctx, unsupportedClient, unsupportedCache, AntennaConfig{
			ID:       "ant-zebra",
			Enabled:  true,
			Protocol: config.ProtocolZebra,
		})
	}()

	waitForRunAntennaResult(t, genericDone, genericPort)
	waitForRunAntennaResult(t, unsupportedDone, unsupportedPort)

	genericCache.mu.Lock()
	genericStored := len(genericCache.stored)
	genericCache.mu.Unlock()
	if genericStored != 1 {
		t.Fatalf("expected generic antenna to store 1 reading, got %d", genericStored)
	}

	unsupportedCache.mu.Lock()
	unsupportedStored := len(unsupportedCache.stored)
	unsupportedCache.mu.Unlock()
	if unsupportedStored != 0 {
		t.Fatalf("expected unsupported antenna to store 0 readings, got %d", unsupportedStored)
	}
}

func newPacketServerClient(t *testing.T, chunks [][]byte) (*Client, int, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go func() {
		conn, _ := listener.Accept()
		if conn == nil {
			return
		}
		defer conn.Close()
		for _, chunk := range chunks {
			_, _ = conn.Write(chunk)
		}
		time.Sleep(100 * time.Millisecond)
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	return NewClient("127.0.0.1", port), port, func() { _ = listener.Close() }
}

func waitForRunAntennaResult(t *testing.T, done <-chan error, port int) {
	t.Helper()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, antennapkg.ErrUnsupportedProtocol) && err.Error() != "connection closed" {
			t.Fatalf("RunAntenna on port %d returned unexpected error: %v", port, err)
		}
	case <-time.After(time.Second):
		t.Fatalf("RunAntenna on port %d did not finish in time", port)
	}
}

func mustBuildRawTCPDataPacket(t *testing.T, epcLastByte byte) []byte {
	t.Helper()
	packet := []byte{0xCC, 0xFF, 0xFF, 0x20, 0x02, 0x04, 0xE2, 0x00, 0x34, epcLastByte, 0x00}
	sum := 0
	for _, b := range packet[:len(packet)-1] {
		sum += int(b)
	}
	packet[len(packet)-1] = byte((^sum + 1) & 0xFF)
	return packet
}

func mustBuildGenericUIIFrame(t *testing.T) []byte {
	t.Helper()
	return mustBuildGenericUIIFrameWithEPCLastByte(t, 0x66)
}

func mustBuildGenericUIIFrameWithEPCLastByte(t *testing.T, epcLastByte byte) []byte {
	t.Helper()
	packet := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x10,
		0x00,
		0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, epcLastByte,
		0xC9,
	}
	sum := 0
	for _, b := range packet {
		sum += int(b)
	}
	return append(packet, byte((^sum+1)&0xff))
}

func TestAntennaConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		antenna     AntennaConfig
		expectError bool
	}{
		{
			name: "valid default protocol",
			antenna: AntennaConfig{
				ID:      "ant-1",
				Enabled: true,
			},
		},
		{
			name: "valid zebra protocol",
			antenna: AntennaConfig{
				ID:       "ant-1",
				Enabled:  true,
				Protocol: config.ProtocolZebra,
			},
		},
		{
			name: "invalid empty id",
			antenna: AntennaConfig{
				ID:      "",
				Enabled: true,
			},
			expectError: true,
		},
		{
			name: "invalid unsupported protocol",
			antenna: AntennaConfig{
				ID:       "ant-1",
				Enabled:  true,
				Protocol: config.AntennaProtocol("alien"),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.antenna.Validate()
			if tt.expectError {
				if err == nil {
					t.Fatal("expected validation error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("expected valid config, got: %v", err)
			}
		})
	}
}
