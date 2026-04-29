package antenna

import (
	"testing"
	"time"
)

// === Task 3.3: RTN Code Routing Tests ===

// TestHandlePacket_ACK handles RTN 0x00 (ACK)
func TestHandlePacket_ACK(t *testing.T) {
	manager, mockClient, mockCache, _ := newTestAntennaManager()

	// RTN 0x00 packet: [start, pad1, pad2, rtn=0x00, len, data..., checksum]
	// Simple ACK: [0x7c, 0xff, 0xff, 0x00, 0x00, checksum]
	// Checksum: 0x7c + 0xff + 0xff + 0x00 + 0x00 = 0x27a, checksum = 0x86
	packet := []byte{0x7c, 0xff, 0xff, 0x00, 0x00, 0x86}

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
	// Format: [0x7c, 0xff, 0xff, 0x02, len, ANT, PC(2), EPC(N), RSSI, checksum]
	// Example: ANT=0x00, PC=0x3000 (12 words), EPC=E2003411B802011383258566, RSSI=0xC9
	// EPC bytes: 0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66
	// Total data: 1 + 2 + 12 + 1 = 16 bytes
	// Packet: [0x7c, 0xff, 0xff, 0x02, 0x10, 0x00, 0x30, 0x00, 0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66, 0xC9, checksum]
	packet := []byte{
		0x7c, 0xff, 0xff, 0x02, 0x10, // Header + RTN 0x02 + len=16
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
		0x7c, 0xff, 0xff, 0x02, 0x0E, // Header + RTN 0x02 + len=14 (1 ANT + 2 PC + 10 EPC + 1 RSSI)
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
		0x7c, 0xff, 0xff, 0x06, 0x04, // Header + RTN 0x06 + len=4
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
		0x7c, 0xff, 0xff, 0x07, 0x02, // Header + RTN 0x07 + len=2
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
		0x7c, 0xff, 0xff, 0x10, 0x00, // Header + RTN 0x10 + len=0
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
		0x7c, 0xff, 0xff, 0x99, 0x02, // Header + RTN 0x99 + len=2
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

// TestHandlePacket_InvalidChecksum verifies handling of invalid checksum
func TestHandlePacket_InvalidChecksum(t *testing.T) {
	manager, _, mockCache, _ := newTestAntennaManager()

	// Packet with invalid checksum
	packet := []byte{
		0x7c, 0xff, 0xff, 0x02, 0x10,
		0x00, 0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66,
		0xC9,
		0xFF, // Invalid checksum
	}

	// Should still process (checksum validation is optional per spec)
	err := manager.HandlePacket(packet)
	if err != nil {
		t.Fatalf("HandlePacket should not error for invalid checksum: %v", err)
	}

	// Should still create reading even with invalid checksum
	mockCache.mu.Lock()
	readingCount := len(mockCache.stored)
	mockCache.mu.Unlock()
	if readingCount != 1 {
		t.Errorf("expected 1 reading even with invalid checksum, got %d", readingCount)
	}
}

// TestHandlePacket_TooShort verifies handling of too-short packets
func TestHandlePacket_TooShort(t *testing.T) {
	manager, _, mockCache, _ := newTestAntennaManager()

	// Packet too short (< 6 bytes minimum)
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
	packet := []byte{0x00, 0xff, 0xff, 0x02, 0x00, 0x00}

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
		0x7c, 0xff, 0xff, 0x02, 0x10,
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
		0x7c, 0xff, 0xff, 0x02, 0x10,
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
