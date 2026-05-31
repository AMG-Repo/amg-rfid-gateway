package antenna

import (
	"errors"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// === Task 3.3: RTN Code Routing Tests ===

// TestHandlePacket_ACK handles RTN 0x00 (ACK)
func TestHandlePacket_ACK(t *testing.T) {
	manager, mockClient, mockCache, _ := newTestAntennaManager()

	// Generic format: [SOI, ADR1, ADR2, CID1, RTN/CID2, LEN, INFO..., CHKSUM]
	// ACK response: [0x7c, 0xff, 0xff, 0x20, 0x00, 0x00, checksum]
	packet := []byte{0x7c, 0xff, 0xff, 0x20, 0x00, 0x00}
	packet = append(packet, calculateTestChecksum(packet))

	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket failed for ACK: %v", err)
	}

	// ACK should update last packet time
	manager.mu.RLock()
	if time.Since(manager.lastPacketTime) > time.Second {
		t.Error("expected lastPacketTime to be updated for ACK")
	}
	manager.mu.RUnlock()

	// ACK should not create any readings
	mockCache.mu.Lock()
	readingCount := len(mockCache.stored)
	mockCache.mu.Unlock()
	if readingCount != 0 {
		t.Errorf("expected 0 readings stored for ACK, got %d", readingCount)
	}

	// Verify client last activity was updated
	mockClient.mu.Lock()
	if mockClient.lastActivity.IsZero() {
		t.Error("expected client lastActivity to be updated for ACK")
	}
	mockClient.mu.Unlock()
}

// TestHandlePacket_UIIData handles RTN 0x02 (UII Data)
func TestHandlePacket_UIIData(t *testing.T) {
	manager, mockClient, mockCache, _ := newTestAntennaManager()

	// RTN 0x02 packet with UII data
	// Format: [0x7c, 0xff, 0xff, 0x20, 0x02, len, ANT, PC(2), EPC(N), RSSI, checksum]
	// Example: ANT=0x00, PC=0x3000 (12 words), EPC=E2003411B802011383258566, RSSI=0xC9
	// EPC bytes: 0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66
	// Total data: 1 + 2 + 12 + 1 = 16 bytes
	// Packet: [0x7c, 0xff, 0xff, 0x20, 0x02, 0x10, 0x00, 0x30, 0x00, 0xE2, ... , 0xC9, checksum]
	packet := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x10, // Header + CID1 + RTN 0x02 + len=16
		0x00,       // ANT
		0x30, 0x00, // PC = 0x3000
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66, // EPC
		0xC9, // RSSI = 201
	}
	// Calculate checksum
	checksum := calculateTestChecksum(packet[:len(packet)])
	packet = append(packet, checksum)

	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket failed for UII data: %v", err)
	}

	// Should create a reading
	mockCache.mu.Lock()
	readingCount := len(mockCache.stored)
	if readingCount != 1 {
		t.Fatalf("expected 1 reading stored for UII data, got %d", readingCount)
	}
	reading := mockCache.stored[0]
	mockCache.mu.Unlock()

	// Verify reading fields
	if reading.AntennaID != "ant-01" {
		t.Errorf("expected AntennaID 'ant-01', got '%s'", reading.AntennaID)
	}
	if reading.GatewayID != "gw-test-001" {
		t.Errorf("expected GatewayID 'gw-test-001', got '%s'", reading.GatewayID)
	}
	if reading.EPC != "E2003411B802011383258566" {
		t.Errorf("expected EPC 'E2003411B802011383258566', got '%s'", reading.EPC)
	}
	if reading.RSSI != 201 {
		t.Errorf("expected RSSI 201, got %d", reading.RSSI)
	}

	// Verify manager stats
	manager.mu.RLock()
	if manager.readingCount != 1 {
		t.Errorf("expected readingCount 1, got %d", manager.readingCount)
	}
	if manager.lastTagEPC != "E2003411B802011383258566" {
		t.Errorf("expected lastTagEPC 'E2003411B802011383258566', got '%s'", manager.lastTagEPC)
	}
	if manager.lastTagRSSI != 201 {
		t.Errorf("expected lastTagRSSI 201, got %d", manager.lastTagRSSI)
	}
	manager.mu.RUnlock()

	// Verify client last activity was updated
	mockClient.mu.Lock()
	if mockClient.lastActivity.IsZero() {
		t.Error("expected client lastActivity to be updated for UII data")
	}
	mockClient.mu.Unlock()
}

// TestHandlePacket_UIIData_With003000Prefix handles UII data with 003000 prefix
func TestHandlePacket_UIIData_With003000Prefix(t *testing.T) {
	manager, _, mockCache, _ := newTestAntennaManager()

	// UII data with 003000 prefix - should be stripped
	// EPC bytes: 003000E2003411B80201 (003000 prefix + E2003411B80201)
	// 003000 = 00 30 00 (3 bytes)
	// E2003411B80201 = E2 00 34 11 B8 02 01 (7 bytes)
	// Total EPC = 10 bytes = 5 words
	// PC = 5 << 11 = 0x2800
	packet := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x0E, // Header + CID1 + RTN 0x02 + len=14
		0x00,       // ANT
		0x28, 0x00, // PC = 0x2800 (5 words = 10 bytes)
		// EPC with 003000 prefix: 00 30 00 E2 00 34 11 B8 02 01
		0x00, 0x30, 0x00, 0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01,
		0xC9, // RSSI
	}
	checksum := calculateTestChecksum(packet[:len(packet)])
	packet = append(packet, checksum)

	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket failed: %v", err)
	}

	mockCache.mu.Lock()
	if len(mockCache.stored) != 1 {
		t.Fatalf("expected 1 reading, got %d", len(mockCache.stored))
	}
	reading := mockCache.stored[0]
	mockCache.mu.Unlock()

	// Verify 003000 prefix was stripped - EPC should be E2003411B80201
	if reading.EPC != "E2003411B80201" {
		t.Errorf("expected EPC 'E2003411B80201' without 003000 prefix, got '%s'", reading.EPC)
	}
}

// TestHandlePacket_TagData handles RTN 0x06 (Tag Data)
func TestHandlePacket_TagData(t *testing.T) {
	manager, _, mockCache, _ := newTestAntennaManager()

	// RTN 0x06 packet with tag data
	packet := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x06, 0x04, // Header + CID1 + RTN 0x06 + len=4
		0x00, 0x01, 0x02, 0x03, // Some tag data
	}
	checksum := calculateTestChecksum(packet[:len(packet)])
	packet = append(packet, checksum)

	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket failed for Tag Data: %v", err)
	}

	// Tag Data should NOT create a reading (only UII data does)
	mockCache.mu.Lock()
	readingCount := len(mockCache.stored)
	mockCache.mu.Unlock()
	if readingCount != 0 {
		t.Errorf("expected 0 readings stored for Tag Data, got %d", readingCount)
	}
}

// TestHandlePacket_Error handles RTN 0x07 (Error)
func TestHandlePacket_Error(t *testing.T) {
	manager, _, mockCache, _ := newTestAntennaManager()

	initialErrorCount := manager.errorCount

	// RTN 0x07 error packet
	packet := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x07, 0x02, // Header + CID1 + RTN 0x07 + len=2
		0x01, 0x02, // Error data
	}
	checksum := calculateTestChecksum(packet[:len(packet)])
	packet = append(packet, checksum)

	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket failed for Error: %v", err)
	}

	// Error should increment errorCount
	manager.mu.RLock()
	if manager.errorCount != initialErrorCount+1 {
		t.Errorf("expected errorCount %d, got %d", initialErrorCount+1, manager.errorCount)
	}
	manager.mu.RUnlock()

	// Error should NOT create a reading
	mockCache.mu.Lock()
	readingCount := len(mockCache.stored)
	mockCache.mu.Unlock()
	if readingCount != 0 {
		t.Errorf("expected 0 readings stored for Error, got %d", readingCount)
	}
}

// TestHandlePacket_HeartbeatResponse handles RTN 0x10 (Heartbeat Response)
func TestHandlePacket_HeartbeatResponse(t *testing.T) {
	manager, mockClient, _, _ := newTestAntennaManager()

	// Set last packet time to old value
	manager.mu.Lock()
	oldTime := time.Now().Add(-10 * time.Second)
	manager.lastPacketTime = oldTime
	manager.mu.Unlock()

	// RTN 0x10 heartbeat response packet
	packet := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x10, 0x00, // Header + CID1 + RTN 0x10 + len=0
	}
	checksum := calculateTestChecksum(packet[:len(packet)])
	packet = append(packet, checksum)

	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket failed for Heartbeat Response: %v", err)
	}

	// Should update last packet time
	manager.mu.RLock()
	if !manager.lastPacketTime.After(oldTime) {
		t.Error("expected lastPacketTime to be updated for Heartbeat Response")
	}
	manager.mu.RUnlock()

	// Verify client last activity was updated
	mockClient.mu.Lock()
	if mockClient.lastActivity.IsZero() {
		t.Error("expected client lastActivity to be updated for Heartbeat Response")
	}
	mockClient.mu.Unlock()
}

// TestHandlePacket_UnknownRTN handles unknown RTN codes
func TestHandlePacket_UnknownRTN(t *testing.T) {
	manager, _, mockCache, _ := newTestAntennaManager()

	// Unknown RTN code 0x99
	packet := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x99, 0x02, // Header + CID1 + RTN 0x99 + len=2
		0xAB, 0xCD, // Some data
	}
	checksum := calculateTestChecksum(packet[:len(packet)])
	packet = append(packet, checksum)

	// Should not error, just log and continue
	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket should not error for unknown RTN: %v", err)
	}

	// Unknown RTN should NOT create a reading
	mockCache.mu.Lock()
	readingCount := len(mockCache.stored)
	mockCache.mu.Unlock()
	if readingCount != 0 {
		t.Errorf("expected 0 readings stored for unknown RTN, got %d", readingCount)
	}
}

// TestHandlePacket_InvalidChecksum verifies invalid checksum is rejected for storage
func TestHandlePacket_InvalidChecksum(t *testing.T) {
	manager, _, mockCache, _ := newTestAntennaManager()

	// Packet with invalid checksum
	packet := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x10,
		0x00, 0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66,
		0xC9,
		0xFF, // Invalid checksum
	}

	// Should not hard-fail the loop, but must not store invalid data.
	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket should not error for invalid checksum: %v", err)
	}

	// Invalid checksum packets must not be persisted.
	mockCache.mu.Lock()
	readingCount := len(mockCache.stored)
	mockCache.mu.Unlock()
	if readingCount != 0 {
		t.Errorf("expected 0 readings for invalid checksum, got %d", readingCount)
	}
}

func TestHandlePacket_UIIReadRTNWithNonUIICID1_DoesNotStore(t *testing.T) {
	manager, _, mockCache, _ := newTestAntennaManager()

	packet := []byte{
		0x7c, 0xff, 0xff, 0x01, 0x02, 0x10,
		0x00, 0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66,
		0xC9,
	}
	packet = append(packet, calculateTestChecksum(packet))

	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket should not error for non-UII CID1 with RTN=0x02: %v", err)
	}

	mockCache.mu.Lock()
	readingCount := len(mockCache.stored)
	mockCache.mu.Unlock()
	if readingCount != 0 {
		t.Errorf("expected 0 readings when CID1 is not 0x20, got %d", readingCount)
	}
}

// TestHandlePacket_TooShort verifies handling of too-short packets
func TestHandlePacket_TooShort(t *testing.T) {
	manager, _, mockCache, _ := newTestAntennaManager()

	// Packet too short (< 7 bytes minimum)
	packet := []byte{0x7c, 0xff, 0xff}

	err := manager.HandlePacket(packet)
	if err == nil {
		t.Error("expected error for too-short packet")
	}

	// Should NOT create a reading
	mockCache.mu.Lock()
	readingCount := len(mockCache.stored)
	mockCache.mu.Unlock()
	if readingCount != 0 {
		t.Errorf("expected 0 readings for short packet, got %d", readingCount)
	}
}

// TestHandlePacket_InvalidStartByte verifies handling of invalid start byte
func TestHandlePacket_InvalidStartByte(t *testing.T) {
	manager, _, mockCache, _ := newTestAntennaManager()

	// Packet with invalid start byte
	packet := []byte{0x00, 0xff, 0xff, 0x20, 0x02, 0x00, 0x00}

	err := manager.HandlePacket(packet)
	if err == nil {
		t.Error("expected error for invalid start byte")
	}

	// Should NOT create a reading
	mockCache.mu.Lock()
	readingCount := len(mockCache.stored)
	mockCache.mu.Unlock()
	if readingCount != 0 {
		t.Errorf("expected 0 readings for invalid start byte, got %d", readingCount)
	}
}

func TestHandlePacket_ZebraNonGenericBytesReturnsUnsupportedProtocol(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "non generic start byte with enough bytes",
			data: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		},
		{
			name: "short non generic frame",
			data: []byte{0x01, 0x02, 0x03},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, mockClient, mockCache, _ := newTestAntennaManager()
			manager.config.Protocol = config.ProtocolZebra

			err := manager.HandlePacket(tt.data)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrUnsupportedProtocol)
			assert.NotContains(t, err.Error(), "invalid start byte")
			assert.NotContains(t, err.Error(), "packet too short")

			mockCache.mu.Lock()
			stored := len(mockCache.stored)
			mockCache.mu.Unlock()
			assert.Equal(t, 0, stored)

			mockClient.mu.Lock()
			activityUpdated := !mockClient.lastActivity.IsZero()
			mockClient.mu.Unlock()
			assert.True(t, activityUpdated)
		})
	}
}

// calculateTestChecksum calculates checksum for test packets
func calculateTestChecksum(data []byte) byte {
	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	return byte((^sum + 1) & 0xff)
}

// TestHandlePacket_DetectsAutoReading verifies auto-reading detection during packet handling
func TestHandlePacket_DetectsAutoReading(t *testing.T) {
	manager, _, _, _ := newTestAntennaManager()

	// Set last packet time to old value (> 2s gap triggers auto-reading)
	manager.mu.Lock()
	manager.lastPacketTime = time.Now().Add(-5 * time.Second)
	manager.isAutoReading = false
	manager.mu.Unlock()

	// UII data packet
	packet := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x10,
		0x00, 0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66,
		0xC9,
	}
	checksum := calculateTestChecksum(packet[:len(packet)])
	packet = append(packet, checksum)

	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket failed: %v", err)
	}

	// Should detect auto-reading (gap > 2s)
	manager.mu.RLock()
	if !manager.isAutoReading {
		t.Error("expected isAutoReading to be true after packet with gap > 2s")
	}
	manager.mu.RUnlock()
}

// TestHandlePacket_UsesParseAntennaData verifies RSSI extraction uses ParseAntennaData
func TestHandlePacket_UsesParseAntennaData(t *testing.T) {
	manager, _, mockCache, _ := newTestAntennaManager()

	// UII packet with known RSSI value
	packet := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x10,
		0x00, 0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66,
		0xC9, // RSSI = 201
	}
	checksum := calculateTestChecksum(packet[:len(packet)])
	packet = append(packet, checksum)

	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket failed: %v", err)
	}

	// Verify RSSI was extracted correctly
	mockCache.mu.Lock()
	if len(mockCache.stored) != 1 {
		t.Fatalf("expected 1 reading, got %d", len(mockCache.stored))
	}
	reading := mockCache.stored[0]
	mockCache.mu.Unlock()

	if reading.RSSI != 201 {
		t.Errorf("expected RSSI 201 (from ParseAntennaData), got %d", reading.RSSI)
	}
}

func TestHandlePacket_ProtocolDispatch(t *testing.T) {
	tests := []struct {
		name         string
		protocol     config.AntennaProtocol
		wantErr      error
		wantStored   int
		wantLastEPC  string
		wantRSSI     int
		wantActivity bool
	}{
		{
			name:         "generic frame uses generic behavior",
			protocol:     config.ProtocolGeneric,
			wantStored:   1,
			wantLastEPC:  "E2003411B802011383258566",
			wantRSSI:     201,
			wantActivity: true,
		},
		{
			name:         "zebra does not parse as generic",
			protocol:     config.ProtocolZebra,
			wantErr:      ErrUnsupportedProtocol,
			wantStored:   0,
			wantActivity: true,
		},
		{
			name:         "unknown protocol fails without generic fallback",
			protocol:     config.AntennaProtocol("future-protocol"),
			wantErr:      ErrUnsupportedProtocol,
			wantStored:   0,
			wantActivity: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, mockClient, mockCache, _ := newTestAntennaManager()
			manager.config.Protocol = tt.protocol

			err := manager.HandlePacket(validUIITestPacket())
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			} else if err != nil {
				t.Fatalf("HandlePacket failed: %v", err)
			}

			mockCache.mu.Lock()
			stored := len(mockCache.stored)
			mockCache.mu.Unlock()
			if stored != tt.wantStored {
				t.Fatalf("expected %d readings stored, got %d", tt.wantStored, stored)
			}

			manager.mu.RLock()
			lastEPC := manager.lastTagEPC
			lastRSSI := manager.lastTagRSSI
			manager.mu.RUnlock()
			if lastEPC != tt.wantLastEPC {
				t.Fatalf("expected last EPC %q, got %q", tt.wantLastEPC, lastEPC)
			}
			if lastRSSI != tt.wantRSSI {
				t.Fatalf("expected last RSSI %d, got %d", tt.wantRSSI, lastRSSI)
			}

			mockClient.mu.Lock()
			activityUpdated := !mockClient.lastActivity.IsZero()
			mockClient.mu.Unlock()
			if activityUpdated != tt.wantActivity {
				t.Fatalf("expected activity updated=%v, got %v", tt.wantActivity, activityUpdated)
			}
		})
	}
}

func TestHandlePacket_MixedProtocolFailureIsolation(t *testing.T) {
	genericManager, _, genericCache, _ := newTestAntennaManager()
	unsupportedManager, _, unsupportedCache, _ := newTestAntennaManager()
	unsupportedManager.config.ID = "ant-zebra"
	unsupportedManager.config.Protocol = config.ProtocolZebra

	if err := unsupportedManager.HandlePacket(validUIITestPacket()); !errors.Is(err, ErrUnsupportedProtocol) {
		t.Fatalf("expected unsupported protocol error, got %v", err)
	}

	if err := genericManager.HandlePacket(validUIITestPacket()); err != nil {
		t.Fatalf("generic manager should continue after unrelated unsupported antenna failure: %v", err)
	}

	unsupportedCache.mu.Lock()
	unsupportedStored := len(unsupportedCache.stored)
	unsupportedCache.mu.Unlock()
	if unsupportedStored != 0 {
		t.Fatalf("expected unsupported antenna to store 0 readings, got %d", unsupportedStored)
	}

	genericCache.mu.Lock()
	genericStored := len(genericCache.stored)
	genericCache.mu.Unlock()
	if genericStored != 1 {
		t.Fatalf("expected generic antenna to store 1 reading, got %d", genericStored)
	}
}
