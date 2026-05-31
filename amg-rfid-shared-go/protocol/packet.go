package protocol

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// Packet structure (generic RFID antenna protocol):
// [SOI, ADR1, ADR2, CID1, CID2/RTN, LENGTH, ...INFO, CHKSUM]
//
// Checksum: two's complement of sum of all bytes except checksum
//   sum += data[i] for i in 0..len-2
//   checksum = (~sum + 1) & 0xff

const (
	// Packet framing - command packets start with 0x7C, responses with 0xCC.
	PacketStartCommand  byte = 0x7C
	PacketStartResponse byte = 0xCC
	PacketPad1          byte = 0xff
	PacketPad2          byte = 0xff

	// Minimum valid packet: SOI + ADR(2) + CID1 + CID2/RTN + LEN + CHKSUM
	MinPacketSize = 7

	// CID codes.
	CIDReadTypeCUII byte = 0x20

	// CID1ReadTypeCUII is kept for compatibility with older call sites.
	CID1ReadTypeCUII byte = CIDReadTypeCUII

	// RTN/CID2 codes
	RTNACK       byte = 0x00
	RTNUIIData   byte = 0x02
	RTNTIDData   byte = 0x03
	RTNUserData  byte = 0x04
	RTNTagData   byte = 0x06
	RTNError     byte = 0x07
	RTNHeartbeat byte = 0x10

	// RTN aliases kept for compatibility with older call sites.
	RTNUIIRead  byte = RTNUIIData
	RTNTIDRead  byte = RTNTIDData
	RTNUserRead byte = RTNUserData

	// Deprecated: legacy Cmd* names are ambiguous because the protocol uses
	// CID for command identity and RTN for response routing. Use CID*/RTN*
	// constants for new code. CmdACK historically meant CID 0x20; keep that
	// value so the public legacy API cannot silently drift to RTNACK (0x00).
	CmdACK       byte = CIDReadTypeCUII
	CmdUIIRead   byte = RTNUIIData
	CmdTIDRead   byte = RTNTIDData
	CmdUserRead  byte = RTNUserData
	CmdHeartbeat byte = RTNHeartbeat
)

// ParsedPacket represents a decoded RFID packet from a reader.
type ParsedPacket struct {
	SOI        byte
	Address    [2]byte // ADR1, ADR2
	CID1       byte
	CID2OrRTN  byte
	Length     byte
	Info       []byte
	RawHex     string
	ChecksumOK bool
	TagUID     string // Extracted UII (hex string), empty if not a data packet

	// Legacy aliases kept for compatibility with older call sites.
	// NOTE: these are aliases of CID1/Length/Info, not RTN semantics.
	CommandCode byte
	DataLength  byte
	Data        []byte
}

// ParsePacket parses a raw byte slice into a ParsedPacket.
// Uses the SAME checksum algorithm as Node.js implementation.
func ParsePacket(raw []byte) (*ParsedPacket, error) {
	// NEGATIVE: Check minimum size
	if len(raw) < MinPacketSize {
		return nil, fmt.Errorf("packet too short: %d bytes (min %d)", len(raw), MinPacketSize)
	}

	// NEGATIVE: Validate framing - accept command and response SOI
	if raw[0] != PacketStartCommand && raw[0] != PacketStartResponse {
		return nil, fmt.Errorf("invalid start byte: 0x%02x (expected 0x%02x or 0x%02x)", raw[0], PacketStartCommand, PacketStartResponse)
	}

	// NEGATIVE: Validate padding bytes
	if raw[1] != PacketPad1 || raw[2] != PacketPad2 {
		return nil, fmt.Errorf("invalid padding bytes: [0x%02x, 0x%02x]", raw[1], raw[2])
	}

	cid1 := raw[3]
	cid2OrRTN := raw[4]
	dataLen := raw[5]

	// NEGATIVE: Validate total size matches declared length
	expectedSize := 6 + int(dataLen) + 1 // header(6) + info + checksum
	if len(raw) < expectedSize {
		return nil, fmt.Errorf("packet truncated: have %d bytes, need %d", len(raw), expectedSize)
	}

	// Extract info portion
	var info []byte
	if dataLen > 0 {
		info = make([]byte, dataLen)
		copy(info, raw[6:6+int(dataLen)])
	}

	// Checksum: TWO'S COMPLEMENT (same as Node.js)
	// sum += data[i] for all bytes except last
	// checksum = (~sum + 1) & 0xff
	checksumIdx := 6 + int(dataLen)
	sum := 0
	for i := 0; i < checksumIdx; i++ {
		sum += int(raw[i])
	}
	calculatedChecksum := byte((^sum + 1) & 0xff)
	receivedChecksum := raw[checksumIdx]
	checksumOK := calculatedChecksum == receivedChecksum

	rawHex := hex.EncodeToString(raw[:checksumIdx+1])

	// Extract UII if this is a Type C UII response.
	tagUID := ""
	if cid1 == CIDReadTypeCUII && cid2OrRTN == RTNUIIData && len(info) > 0 {
		if parsed, err := ParseAntennaData(info); err == nil {
			tagUID = parsed.EPC
		}
	}

	// HAPPY PATH: Return parsed packet
	return &ParsedPacket{
		SOI:        raw[0],
		Address:    [2]byte{raw[1], raw[2]},
		CID1:       cid1,
		CID2OrRTN:  cid2OrRTN,
		Length:     dataLen,
		Info:       info,
		RawHex:     rawHex,
		ChecksumOK: checksumOK,
		TagUID:     tagUID,

		CommandCode: cid1,
		DataLength:  dataLen,
		Data:        info,
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
	case CIDReadTypeCUII:
		return "READ_TYPE_C_UII"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02x)", code)
	}
}

// IsDataPacket returns true if the packet contains tag data.
func (p *ParsedPacket) IsDataPacket() bool {
	return p.CID1 == CIDReadTypeCUII && p.CID2OrRTN == RTNUIIData
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
