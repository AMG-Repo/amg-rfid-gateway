package protocol

import (
	"testing"
)

func TestParsePacket_ValidUII(t *testing.T) {
	// Simulate a UII data packet:
	// [0x7c, 0xff, 0xff, 0x02, 0x04, 0xAB, 0xCD, 0xEF, 0x01, checksum]
	data := []byte{0x7c, 0xff, 0xff, 0x02, 0x04, 0xAB, 0xCD, 0xEF, 0x01}

	// Calculate checksum: two's complement of sum
	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	checksum := byte((^sum + 1) & 0xff)
	data = append(data, checksum)

	packet, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if packet.CommandCode != CmdUIIRead {
		t.Errorf("expected command 0x02, got 0x%02x", packet.CommandCode)
	}

	if packet.DataLength != 4 {
		t.Errorf("expected data length 4, got %d", packet.DataLength)
	}

	if !packet.ChecksumOK {
		t.Error("expected checksum to be valid")
	}

	if packet.TagUID != "abcdef01" {
		t.Errorf("expected UII 'abcdef01', got '%s'", packet.TagUID)
	}
}

func TestParsePacket_Heartbeat(t *testing.T) {
	// Heartbeat: [0x7c, 0xff, 0xff, 0x10, 0x00, checksum]
	data := []byte{0x7c, 0xff, 0xff, 0x10, 0x00}

	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	checksum := byte((^sum + 1) & 0xff)
	data = append(data, checksum)

	packet, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if packet.CommandCode != CmdHeartbeat {
		t.Errorf("expected command 0x10, got 0x%02x", packet.CommandCode)
	}

	if packet.TagUID != "" {
		t.Errorf("heartbeat should have no UII, got '%s'", packet.TagUID)
	}
}

func TestParsePacket_ACK(t *testing.T) {
	// ACK: [0x7c, 0xff, 0xff, 0x20, 0x00, checksum]
	data := []byte{0x7c, 0xff, 0xff, 0x20, 0x00}

	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	checksum := byte((^sum + 1) & 0xff)
	data = append(data, checksum)

	packet, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if packet.CommandCode != CmdACK {
		t.Errorf("expected command 0x20, got 0x%02x", packet.CommandCode)
	}
}

func TestParsePacket_TooShort(t *testing.T) {
	_, err := ParsePacket([]byte{0x7c, 0xff})
	if err == nil {
		t.Error("expected error for short packet")
	}
}

func TestParsePacket_InvalidStart(t *testing.T) {
	_, err := ParsePacket([]byte{0x00, 0xff, 0xff, 0x02, 0x00, 0x00})
	if err == nil {
		t.Error("expected error for invalid start byte")
	}
}

func TestParsePacket_BadChecksum(t *testing.T) {
	// Valid packet but wrong checksum
	data := []byte{0x7c, 0xff, 0xff, 0x02, 0x02, 0xAB, 0xCD, 0xFF} // 0xFF is wrong checksum

	packet, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if packet.ChecksumOK {
		t.Error("expected checksum to be invalid")
	}
}

func TestParsePacket_AltStartByte(t *testing.T) {
	// Test alternative start byte 0xCC
	data := []byte{0xCC, 0xff, 0xff, 0x02, 0x04, 0xAB, 0xCD, 0xEF, 0x01}

	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	checksum := byte((^sum + 1) & 0xff)
	data = append(data, checksum)

	packet, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if packet.CommandCode != CmdUIIRead {
		t.Errorf("expected command 0x02, got 0x%02x", packet.CommandCode)
	}
}

func TestValidateChecksum_Valid(t *testing.T) {
	// Create a valid packet
	data := []byte{0x7c, 0xff, 0xff, 0x02, 0x04, 0xAB, 0xCD, 0xEF, 0x01}
	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	checksum := byte((^sum + 1) & 0xff)
	data = append(data, checksum)

	if !ValidateChecksum(data) {
		t.Error("expected checksum to be valid")
	}
}

func TestValidateChecksum_Invalid(t *testing.T) {
	// Too short
	if ValidateChecksum([]byte{0x7c, 0xff}) {
		t.Error("expected checksum validation to fail for short data")
	}

	// Wrong checksum
	data := []byte{0x7c, 0xff, 0xff, 0x02, 0x02, 0xAB, 0xCD, 0xFF}
	if ValidateChecksum(data) {
		t.Error("expected checksum validation to fail for wrong checksum")
	}
}

func TestCommandName(t *testing.T) {
	tests := []struct {
		code     byte
		expected string
	}{
		{CmdUIIRead, "UII_READ"},
		{CmdTIDRead, "TID_READ"},
		{CmdUserRead, "USER_READ"},
		{CmdACK, "ACK"},
		{CmdHeartbeat, "HEARTBEAT"},
		{0xFF, "UNKNOWN(0xff)"},
	}

	for _, tt := range tests {
		result := CommandName(tt.code)
		if result != tt.expected {
			t.Errorf("CommandName(0x%02x) = %s, want %s", tt.code, result, tt.expected)
		}
	}
}

func TestParsedPacket_IsDataPacket(t *testing.T) {
	// UII read is a data packet
	packet := &ParsedPacket{CommandCode: CmdUIIRead}
	if !packet.IsDataPacket() {
		t.Error("UII_READ should be a data packet")
	}

	// TID read is a data packet
	packet = &ParsedPacket{CommandCode: CmdTIDRead}
	if !packet.IsDataPacket() {
		t.Error("TID_READ should be a data packet")
	}

	// USER read is a data packet
	packet = &ParsedPacket{CommandCode: CmdUserRead}
	if !packet.IsDataPacket() {
		t.Error("USER_READ should be a data packet")
	}

	// ACK is not a data packet
	packet = &ParsedPacket{CommandCode: CmdACK}
	if packet.IsDataPacket() {
		t.Error("ACK should not be a data packet")
	}

	// Heartbeat is not a data packet
	packet = &ParsedPacket{CommandCode: CmdHeartbeat}
	if packet.IsDataPacket() {
		t.Error("HEARTBEAT should not be a data packet")
	}
}
