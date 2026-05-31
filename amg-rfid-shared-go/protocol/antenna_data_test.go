package protocol

import (
	"testing"
)

func TestParseAntennaData_CompletePacket(t *testing.T) {
	// Real antenna packet format: ANT=0x00, PC=0x3000, EPC=0xE2003411B802011383258566, RSSI=0xC9
	// Data: [ANT(1), PC(2), EPC(12), RSSI(1)] = 16 bytes total
	// ANT: 0x00
	// PC: 0x30 0x00 (big-endian) = 0x3000 -> length bits = 6 words = 12 EPC bytes = 24 hex chars
	// EPC: 0xE2 0x00 0x34 0x11 0xB8 0x02 0x01 0x13 0x83 0x25 0x85 0x66
	// RSSI: 0xC9 (201 decimal)
	data := []byte{
		0x00,       // ANT
		0x30, 0x00, // PC (big-endian)
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66, // EPC
		0xC9, // RSSI
	}

	parsed, err := ParseAntennaData(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.AntennaNum != 0x00 {
		t.Errorf("expected AntennaNum 0x00, got 0x%02x", parsed.AntennaNum)
	}

	if parsed.PC != 0x3000 {
		t.Errorf("expected PC 0x3000, got 0x%04x", parsed.PC)
	}

	if parsed.EPC != "E2003411B802011383258566" {
		t.Errorf("expected EPC 'E2003411B802011383258566', got '%s'", parsed.EPC)
	}

	if parsed.RSSI != 0xC9 {
		t.Errorf("expected RSSI 0xC9, got 0x%02x", parsed.RSSI)
	}

	// RawHex should contain full data
	if parsed.RawHex == "" {
		t.Error("expected RawHex to be non-empty")
	}
}

func TestParseAntennaData_With003000Prefix(t *testing.T) {
	// Some antennas prepend 003000 to the EPC - we need to strip it
	// PC=0x3000 means 12 EPC bytes (24 hex chars)
	// If UII starts with 003000 (bytes 0x00 0x30 0x00), strip that prefix
	data := []byte{
		0x00,       // ANT
		0x30, 0x00, // PC length bits encode 6 words -> 12 bytes
		0x00, 0x30, 0x00, 0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, // EPC with 003000 prefix
		0xAA, // RSSI
	}

	parsed, err := ParseAntennaData(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 003000 prefix should be stripped
	if parsed.EPC != "E2003411B802011383" {
		t.Errorf("expected EPC 'E2003411B802011383' (003000 stripped), got '%s'", parsed.EPC)
	}
}

func TestParseAntennaData_ZeroRSSI(t *testing.T) {
	// RSSI byte is 0x00 - should still be valid
	data := []byte{
		0x01,       // ANT (different antenna)
		0x20, 0x00, // PC length bits encode 4 words -> 8 EPC bytes
		0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, // EPC
		0x00, // RSSI = 0 (valid - means no signal)
	}

	parsed, err := ParseAntennaData(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.RSSI != 0x00 {
		t.Errorf("expected RSSI 0x00, got 0x%02x", parsed.RSSI)
	}

	if parsed.AntennaNum != 0x01 {
		t.Errorf("expected AntennaNum 0x01, got 0x%02x", parsed.AntennaNum)
	}
}

func TestParseAntennaData_TooShort(t *testing.T) {
	// Minimum: ANT(1) + PC(2) + RSSI(1) = 4 bytes (even with 0 EPC bytes)
	data := []byte{0x00, 0x30, 0x00} // Only 3 bytes

	_, err := ParseAntennaData(data)
	if err == nil {
		t.Error("expected error for data too short")
	}
}

func TestParseAntennaData_TruncatedEPC(t *testing.T) {
	// PC says 12 EPC bytes, but we only provide 6
	data := []byte{
		0x00,       // ANT
		0x30, 0x00, // PC = 12 words
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, // Only 6 bytes instead of 12
		0xC9, // RSSI
	}

	_, err := ParseAntennaData(data)
	if err == nil {
		t.Error("expected error for truncated EPC data")
	}
}

func TestParsedAntennaData_ToReading(t *testing.T) {
	data := &ParsedAntennaData{
		AntennaNum: 0x00,
		PC:         0x3000,
		EPC:        "E2003411B802011383258566",
		RSSI:       0xC9,
		RawHex:     "003000E2003411B802011383258566C9",
	}

	reading := data.ToReading("ant-01", "gw-pi5-001")

	if reading.AntennaID != "ant-01" {
		t.Errorf("expected AntennaID 'ant-01', got '%s'", reading.AntennaID)
	}

	if reading.GatewayID != "gw-pi5-001" {
		t.Errorf("expected GatewayID 'gw-pi5-001', got '%s'", reading.GatewayID)
	}

	if reading.EPC != "E2003411B802011383258566" {
		t.Errorf("expected EPC 'E2003411B802011383258566', got '%s'", reading.EPC)
	}

	// RSSI should be raw byte value as positive int (0xC9 = 201)
	if reading.RSSI != 201 {
		t.Errorf("expected RSSI 201, got %d", reading.RSSI)
	}

	if reading.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}
