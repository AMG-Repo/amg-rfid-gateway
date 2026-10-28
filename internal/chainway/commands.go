package chainway

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	CmdStartInventory    byte = 0x82
	CmdTagNotification   byte = 0x83
	CmdStopInventory     byte = 0x8C
	CmdStopInventoryResp byte = 0x8D

	tidLen      = 12
	rssiAntLen  = 3 // RSSI(2) + ANT(1)
	maxRSSIUnit = 200.0
)

var (
	ErrPayload       = errors.New("chainway: malformed payload")
	ErrUnexpectedCmd = errors.New("chainway: unexpected command")
)

// Tag is a parsed 0x83 tag notification.
type Tag struct {
	PC   uint16
	EPC  string // uppercase hex
	TID  string // uppercase hex, empty when absent
	User string // uppercase hex, empty when absent
	// RSSI is in dBm and only meaningful when RSSIValid is true.
	RSSI      float64
	RSSIValid bool
	Antenna   int // reader port 1..4
}

// StartInventoryFrame returns the start-inventory command frame.
func StartInventoryFrame() []byte {
	return mustEncode(CmdStartInventory, []byte{0x00, 0x00})
}

// StopInventoryFrame returns the stop-inventory command frame.
func StopInventoryFrame() []byte {
	return mustEncode(CmdStopInventory, nil)
}

func mustEncode(cmd byte, data []byte) []byte {
	raw, err := Encode(cmd, data)
	if err != nil {
		panic(err)
	}
	return raw
}

// ParseStopResponse reports whether a 0x8D stop-inventory response is OK.
func ParseStopResponse(f Frame) (bool, error) {
	if f.Cmd != CmdStopInventoryResp {
		return false, fmt.Errorf("%w: 0x%02X", ErrUnexpectedCmd, f.Cmd)
	}
	if len(f.Data) < 1 {
		return false, fmt.Errorf("%w: empty stop response", ErrPayload)
	}
	return f.Data[0] == 0x01, nil
}

// ParseTag parses the DATA of a 0x83 tag notification:
//
//	PC(2) | EPC(N) | [TID(12)] | [USER] | RSSI(2) | ANT(1), N = (PC[0] >> 3) * 2
func ParseTag(data []byte) (Tag, error) {
	if len(data) < 2+rssiAntLen {
		return Tag{}, fmt.Errorf("%w: %d bytes", ErrPayload, len(data))
	}
	epcLen := int(data[0]>>3) * 2
	extra := len(data) - 2 - epcLen - rssiAntLen
	if extra < 0 {
		return Tag{}, fmt.Errorf("%w: PC announces %d-byte EPC, have %d bytes", ErrPayload, epcLen, len(data))
	}
	if extra > 0 && extra < tidLen {
		return Tag{}, fmt.Errorf("%w: %d trailing bytes, too short for TID", ErrPayload, extra)
	}

	pos := 2
	tag := Tag{
		PC:  binary.BigEndian.Uint16(data[0:2]),
		EPC: upperHex(data[pos : pos+epcLen]),
	}
	pos += epcLen
	if extra > 0 {
		tag.TID = upperHex(data[pos : pos+tidLen])
		pos += tidLen
		tag.User = upperHex(data[pos : pos+extra-tidLen])
		pos += extra - tidLen
	}

	magnitude := float64(65535-int(binary.BigEndian.Uint16(data[pos:pos+2]))) / 10
	if magnitude > 0 && magnitude < maxRSSIUnit {
		tag.RSSI = -magnitude
		tag.RSSIValid = true
	}
	tag.Antenna = int(data[pos+2])
	return tag, nil
}

func upperHex(b []byte) string {
	return strings.ToUpper(hex.EncodeToString(b))
}
