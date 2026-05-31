package protocol

import (
	"encoding/hex"
	"testing"
)

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	return b
}

func testChecksum(data []byte) byte {
	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	return byte((^sum + 1) & 0xff)
}

func TestParsePacket_CanonicalVectors(t *testing.T) {
	tests := []struct {
		name        string
		rawHex      string
		soi         byte
		address     [2]byte
		cid1        byte
		cid2OrRTN   byte
		length      byte
		wantInfoLen int
		wantTagUID  string
		checksumOK  bool
	}{
		{
			name:        "read UII command vector",
			rawHex:      "7CFFFF20000066",
			soi:         PacketStartCommand,
			address:     [2]byte{0xFF, 0xFF},
			cid1:        CIDReadTypeCUII,
			cid2OrRTN:   RTNACK,
			length:      0x00,
			wantInfoLen: 0,
			wantTagUID:  "",
			checksumOK:  true,
		},
		{
			name:        "response UII vector",
			rawHex:      "CCFFFF200210003000E2003411B802011383258566C983",
			soi:         PacketStartResponse,
			address:     [2]byte{0xFF, 0xFF},
			cid1:        CIDReadTypeCUII,
			cid2OrRTN:   RTNUIIData,
			length:      0x10,
			wantInfoLen: 16,
			wantTagUID:  "E2003411B802011383258566",
			checksumOK:  true,
		},
		{
			name:        "response invalid checksum still parses fields",
			rawHex:      "CCFFFF200210003000E2003411B802011383258566C9FF",
			soi:         PacketStartResponse,
			address:     [2]byte{0xFF, 0xFF},
			cid1:        CIDReadTypeCUII,
			cid2OrRTN:   RTNUIIData,
			length:      0x10,
			wantInfoLen: 16,
			wantTagUID:  "E2003411B802011383258566",
			checksumOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet, err := ParsePacket(mustDecodeHex(t, tt.rawHex))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if packet.SOI != tt.soi {
				t.Fatalf("expected SOI 0x%02X, got 0x%02X", tt.soi, packet.SOI)
			}
			if packet.Address != tt.address {
				t.Fatalf("expected address [%02X %02X], got [%02X %02X]", tt.address[0], tt.address[1], packet.Address[0], packet.Address[1])
			}
			if packet.CID1 != tt.cid1 {
				t.Fatalf("expected CID1 0x%02X, got 0x%02X", tt.cid1, packet.CID1)
			}
			if packet.CID2OrRTN != tt.cid2OrRTN {
				t.Fatalf("expected CID2/RTN 0x%02X, got 0x%02X", tt.cid2OrRTN, packet.CID2OrRTN)
			}
			if packet.Length != tt.length {
				t.Fatalf("expected length 0x%02X, got 0x%02X", tt.length, packet.Length)
			}
			if len(packet.Info) != tt.wantInfoLen {
				t.Fatalf("expected info len %d, got %d", tt.wantInfoLen, len(packet.Info))
			}
			if packet.TagUID != tt.wantTagUID {
				t.Fatalf("expected TagUID %q, got %q", tt.wantTagUID, packet.TagUID)
			}
			if packet.ChecksumOK != tt.checksumOK {
				t.Fatalf("expected ChecksumOK %v, got %v", tt.checksumOK, packet.ChecksumOK)
			}
		})
	}
}

func TestParsePacket_NonE280TagVector(t *testing.T) {
	base := []byte{PacketStartResponse, PacketPad1, PacketPad2, CIDReadTypeCUII, RTNUIIData, 0x0C, 0x01, 0x20, 0x00, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0x00}
	raw := append(base, testChecksum(base))

	packet, err := ParsePacket(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if packet.TagUID != "ABCDEF0123456789" {
		t.Fatalf("expected EPC ABCDEF0123456789, got %s", packet.TagUID)
	}
	if !packet.ChecksumOK {
		t.Fatal("expected valid checksum")
	}
}

func TestParsePacket_InvalidStartByteAndTruncation(t *testing.T) {
	tests := []struct {
		name   string
		rawHex string
		errMsg string
	}{
		{name: "invalid start byte", rawHex: "00FFFF20000066", errMsg: "invalid start byte"},
		{name: "truncated packet by length", rawHex: "CCFFFF200210003000E2003411B802011383258566", errMsg: "packet truncated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePacket(mustDecodeHex(t, tt.rawHex))
			if err == nil {
				t.Fatalf("expected error containing %q", tt.errMsg)
			}
		})
	}
}

func TestParsedPacket_IsDataPacket(t *testing.T) {
	tests := []struct {
		name string
		pkt  ParsedPacket
		want bool
	}{
		{name: "UII response is data", pkt: ParsedPacket{CID1: CID1ReadTypeCUII, CID2OrRTN: RTNUIIRead}, want: true},
		{name: "ack is not data", pkt: ParsedPacket{CID1: CID1ReadTypeCUII, CID2OrRTN: RTNACK}, want: false},
		{name: "TID response is not data", pkt: ParsedPacket{CID1: CID1ReadTypeCUII, CID2OrRTN: RTNTIDRead}, want: false},
		{name: "user response is not data", pkt: ParsedPacket{CID1: CID1ReadTypeCUII, CID2OrRTN: RTNUserRead}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.pkt.IsDataPacket()
			if got != tt.want {
				t.Fatalf("expected IsDataPacket=%v, got %v", tt.want, got)
			}
		})
	}
}

func TestProtocolConstants_DocumentRolesAndLegacyAliases(t *testing.T) {
	tests := []struct {
		name string
		got  byte
		want byte
	}{
		{name: "CID read Type C UII command", got: CIDReadTypeCUII, want: 0x20},
		{name: "CID1 read Type C UII alias", got: CID1ReadTypeCUII, want: CIDReadTypeCUII},
		{name: "RTN ACK response", got: RTNACK, want: 0x00},
		{name: "RTN UII data response", got: RTNUIIData, want: 0x02},
		{name: "RTN TID data response", got: RTNTIDData, want: 0x03},
		{name: "RTN user data response", got: RTNUserData, want: 0x04},
		{name: "legacy CmdACK keeps historical CID value", got: CmdACK, want: CIDReadTypeCUII},
		{name: "legacy RTN UII read alias", got: RTNUIIRead, want: RTNUIIData},
		{name: "legacy Cmd UII read alias", got: CmdUIIRead, want: RTNUIIData},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("expected 0x%02X, got 0x%02X", tt.want, tt.got)
			}
		})
	}
}
