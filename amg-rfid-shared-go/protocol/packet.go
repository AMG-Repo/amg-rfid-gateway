package protocol

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// Packet structure (AMG readers):
// [0x7c, 0xff, 0xff, command_code, data_length, ...data, checksum]
//
// Response codes:
//   0x02 = UII data
//   0x20 = Success ACK
//   0x10 = Heartbeat
//
// Checksum: two's complement of sum of all bytes except checksum
//   sum += data[i] for i in 0..len-2
//   checksum = (~sum + 1) & 0xff

const (
	// Packet framing - AMG readers can send 0x7C or 0xCC as start byte
	PacketStart    byte = 0x7c
	PacketStartAlt byte = 0xCC // Alternative start byte from some readers
	PacketPad1     byte = 0xff
	PacketPad2     byte = 0xff

	// Minimum valid packet: start + pad1 + pad2 + cmd + len + checksum
	MinPacketSize = 6

	// Response/command codes
	CmdUIIRead   byte = 0x02 // UII data from reader
	CmdTIDRead   byte = 0x03 // TID data from reader
	CmdUserRead  byte = 0x04 // USER data from reader
	CmdACK       byte = 0x20 // Success acknowledgement
	CmdHeartbeat byte = 0x10 // Heartbeat keepalive
)

// ParsedPacket represents a decoded RFID packet from a reader.
type ParsedPacket struct {
	CommandCode byte
	DataLength  byte
	Data        []byte
	RawHex      string
	ChecksumOK  bool
	TagUID      string // Extracted UII (hex string), empty if not a data packet
}

// ParsePacket parses a raw byte slice into a ParsedPacket.
// Uses the SAME checksum algorithm as Node.js implementation.
func ParsePacket(raw []byte) (*ParsedPacket, error) {
	// NEGATIVE: Check minimum size
	if len(raw) < MinPacketSize {
		return nil, fmt.Errorf("packet too short: %d bytes (min %d)", len(raw), MinPacketSize)
	}

	// NEGATIVE: Validate framing - accept both 0x7C and 0xCC
	if raw[0] != PacketStart && raw[0] != PacketStartAlt {
		return nil, fmt.Errorf("invalid start byte: 0x%02x (expected 0x%02x or 0x%02x)", raw[0], PacketStart, PacketStartAlt)
	}

	// NEGATIVE: Validate padding bytes
	if raw[1] != PacketPad1 || raw[2] != PacketPad2 {
		return nil, fmt.Errorf("invalid padding bytes: [0x%02x, 0x%02x]", raw[1], raw[2])
	}

	cmdCode := raw[3]
	dataLen := raw[4]

	// NEGATIVE: Validate total size matches declared length
	expectedSize := 5 + int(dataLen) + 1 // header(5) + data + checksum
	if len(raw) < expectedSize {
		return nil, fmt.Errorf("packet truncated: have %d bytes, need %d", len(raw), expectedSize)
	}

	// Extract data portion
	var data []byte
	if dataLen > 0 {
		data = make([]byte, dataLen)
		copy(data, raw[5:5+int(dataLen)])
	}

	// Checksum: TWO'S COMPLEMENT (same as Node.js)
	// sum += data[i] for all bytes except last
	// checksum = (~sum + 1) & 0xff
	checksumIdx := 5 + int(dataLen)
	sum := 0
	for i := 0; i < checksumIdx; i++ {
		sum += int(raw[i])
	}
	calculatedChecksum := byte((^sum + 1) & 0xff)
	receivedChecksum := raw[checksumIdx]
	checksumOK := calculatedChecksum == receivedChecksum

	rawHex := hex.EncodeToString(raw[:checksumIdx+1])

	// Extract UII if this is a data packet (RTN code 0x02, 0x03, 0x04)
	tagUID := ""
	if (cmdCode == CmdUIIRead || cmdCode == CmdTIDRead || cmdCode == CmdUserRead) && len(data) > 0 {
		tagUID = hex.EncodeToString(data)
	}

	// HAPPY PATH: Return parsed packet
	return &ParsedPacket{
		CommandCode: cmdCode,
		DataLength:  dataLen,
		Data:        data,
		RawHex:      rawHex,
		ChecksumOK:  checksumOK,
		TagUID:      tagUID,
	}, nil
}

// ValidateChecksum validates the checksum of a raw packet (same as Node.js).
func ValidateChecksum(data []byte) bool {
	// NEGATIVE: Check minimum size
	if len(data) < 7 {
		return false
	}

	sum := 0
	for i := 0; i < len(data)-1; i++ {
		sum += int(data[i])
	}
	calculatedChecksum := byte((^sum + 1) & 0xff)
	receivedChecksum := data[len(data)-1]

	// HAPPY PATH
	return receivedChecksum == calculatedChecksum
}

// CommandName returns a human-readable name for the command code.
func CommandName(code byte) string {
	switch code {
	case CmdUIIRead:
		return "UII_READ"
	case CmdTIDRead:
		return "TID_READ"
	case CmdUserRead:
		return "USER_READ"
	case CmdACK:
		return "ACK"
	case CmdHeartbeat:
		return "HEARTBEAT"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02x)", code)
	}
}

// IsDataPacket returns true if the packet contains tag data.
func (p *ParsedPacket) IsDataPacket() bool {
	return p.CommandCode == CmdUIIRead ||
		p.CommandCode == CmdTIDRead ||
		p.CommandCode == CmdUserRead
}

// ParsedAntennaData represents parsed ANT, PC, EPC, RSSI from antenna data packet.
// Used for RTN 0x02 (UII data) responses per REQ-A008.
type ParsedAntennaData struct {
	AntennaNum byte   // ANT byte (0x00, 0x01, etc.)
	PC         uint16 // Protocol Control (2 bytes, big-endian)
	EPC        string // Electronic Product Code (hex string, no 003000 prefix)
	RSSI       byte   // Raw RSSI byte (e.g., 0xC9 = 201)
	RawHex     string // Full raw hex for debugging
}

// ParseAntennaData extracts ANT, PC, EPC, RSSI from antenna data packet.
// data is the data portion of a RTN 0x02 packet (starting at byte 6 of full packet).
// Format: [ANT(1), PC(2), EPC(N), RSSI(1)] where N is determined by PC.
func ParseAntennaData(data []byte) (*ParsedAntennaData, error) {
	// NEGATIVE: Minimum size check (ANT + PC + RSSI = 4 bytes minimum, even with 0 EPC)
	if len(data) < 4 {
		return nil, fmt.Errorf("data too short: %d bytes (min 4)", len(data))
	}

	antNum := data[0]
	pc := binary.BigEndian.Uint16(data[1:3])

	// PC encodes EPC length in words (1 word = 2 bytes = 4 hex chars)
	// Bits 10-15 of PC contain the EPC length in words
	epcWords := (pc >> 11) & 0x1F
	epcBytes := int(epcWords) * 2

	// NEGATIVE: Validate data length matches PC
	expectedLen := 3 + epcBytes + 1 // ANT + PC + EPC + RSSI
	if len(data) < expectedLen {
		return nil, fmt.Errorf("data shorter than PC indicates: need %d, have %d", expectedLen, len(data))
	}

	epcData := data[3 : 3+epcBytes]
	epcHex := hex.EncodeToString(epcData)

	// Strip 003000 prefix if present (protocol prefix, not part of tag)
	// Match Node.js behavior from raw_server.go
	if len(epcHex) > 6 && strings.ToUpper(epcHex[:6]) == "003000" {
		epcHex = epcHex[6:]
	}

	rssi := data[3+epcBytes]

	// HAPPY PATH
	return &ParsedAntennaData{
		AntennaNum: antNum,
		PC:         pc,
		EPC:        strings.ToUpper(epcHex),
		RSSI:       rssi,
		RawHex:     hex.EncodeToString(data),
	}, nil
}

// ToReading converts ParsedAntennaData to models.Reading per REQ-A008, REQ-A009.
func (p *ParsedAntennaData) ToReading(antennaID, gatewayID string) models.Reading {
	return models.Reading{
		AntennaID: antennaID,
		GatewayID: gatewayID,
		EPC:       p.EPC,
		RSSI:      int(p.RSSI), // Raw byte value as positive int (0-255)
		Timestamp: time.Now(),
		Synced:    false,
	}
}
