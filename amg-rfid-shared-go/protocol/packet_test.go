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

func testFrame(info ...byte) []byte {
	frame := []byte{PacketStartResponse, PacketPad1, PacketPad2, CIDReadTypeCUII, RTNUIIData, byte(len(info))}
	frame = append(frame, info...)
	return append(frame, testChecksum(frame))
}

func requireFramesEqual(t *testing.T, got [][]byte, want ...[]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d frames, got %d", len(want), len(got))
	}
	for i := range want {
		if hex.EncodeToString(got[i]) != hex.EncodeToString(want[i]) {
			t.Fatalf("frame %d mismatch: expected %x, got %x", i, want[i], got[i])
		}
	}
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

func TestStreamExtractor_FragmentedFrames(t *testing.T) {
	tests := []struct {
		name   string
		chunks [][]byte
		frame  []byte
	}{
		{
			name:  "split across two reads",
			frame: testFrame(0x01, 0x20, 0x00, 0xAA, 0xBB, 0xC9),
		},
		{
			name:  "byte by byte emits only after final byte",
			frame: testFrame(0x02, 0x20, 0x00, 0xCC, 0xDD, 0xC8),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor := NewStreamExtractor()
			chunks := tt.chunks
			if chunks == nil {
				if tt.name == "byte by byte emits only after final byte" {
					for _, b := range tt.frame {
						chunks = append(chunks, []byte{b})
					}
				} else {
					chunks = [][]byte{tt.frame[:4], tt.frame[4:]}
				}
			}

			for i, chunk := range chunks[:len(chunks)-1] {
				got := extractor.Append(chunk)
				requireFramesEqual(t, got)
				if extractor.BufferedLen() == 0 {
					t.Fatalf("expected partial bytes retained after chunk %d", i)
				}
			}

			got := extractor.Append(chunks[len(chunks)-1])
			requireFramesEqual(t, got, tt.frame)
			if extractor.BufferedLen() != 0 {
				t.Fatalf("expected empty buffer after emitting frame, got %d", extractor.BufferedLen())
			}
		})
	}
}

func TestStreamExtractor_CoalescedAndNoise(t *testing.T) {
	first := testFrame(0x01, 0x20, 0x00, 0x10, 0xC9)
	second := testFrame(0x02, 0x20, 0x00, 0x20, 0xC8)

	tests := []struct {
		name  string
		chunk []byte
		want  [][]byte
	}{
		{
			name:  "coalesced frames emit in order",
			chunk: append(append([]byte{}, first...), second...),
			want:  [][]byte{first, second},
		},
		{
			name:  "leading noise discarded before SOI",
			chunk: append([]byte{0x00, 0x11, 0x22}, first...),
			want:  [][]byte{first},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor := NewStreamExtractor()
			got := extractor.Append(tt.chunk)
			requireFramesEqual(t, got, tt.want...)
			if extractor.BufferedLen() != 0 {
				t.Fatalf("expected empty buffer after extraction, got %d", extractor.BufferedLen())
			}
		})
	}
}

func TestStreamExtractor_InvalidChecksumAndBufferGuard(t *testing.T) {
	valid := testFrame(0x01, 0x20, 0x00, 0xAA, 0xC9)
	invalid := append([]byte{}, valid...)
	invalid[len(invalid)-1] ^= 0xFF

	t.Run("invalid checksum frame still emitted before valid frame", func(t *testing.T) {
		extractor := NewStreamExtractor()
		got := extractor.Append(append(invalid, valid...))
		requireFramesEqual(t, got, invalid, valid)
		if ValidateChecksum(got[0]) {
			t.Fatal("expected first emitted frame to keep invalid checksum")
		}
		if !ValidateChecksum(got[1]) {
			t.Fatal("expected second emitted frame to keep valid checksum")
		}
	})

	t.Run("overflow guard drops garbage and retains plausible SOI tail", func(t *testing.T) {
		extractor := NewStreamExtractorWithMaxBuffer(12)
		noise := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, PacketStartResponse, PacketPad1, PacketPad2, CIDReadTypeCUII, RTNUIIData, 0x02, 0x99}
		got := extractor.Append(noise)
		requireFramesEqual(t, got)
		if extractor.BufferedLen() != 7 {
			t.Fatalf("expected guard to retain 7-byte plausible SOI tail, got %d", extractor.BufferedLen())
		}

		completedTail := []byte{0x88}
		wantFrame := append(noise[6:], completedTail...)
		wantFrame = append(wantFrame, testChecksum(wantFrame))
		got = extractor.Append(wantFrame[7:])
		requireFramesEqual(t, got, wantFrame)
	})
}

func TestStreamExtractor_LengthBoundariesAndResync(t *testing.T) {
	t.Run("LENGTH boundary vectors and partial retention", func(t *testing.T) {
		tests := []struct {
			name           string
			length         byte
			info           []byte
			partialCut     int
			wantTotalBytes int
		}{
			{
				name:           "LENGTH zero emits minimum 7-byte frame",
				length:         0,
				info:           nil,
				partialCut:     6,
				wantTotalBytes: 7,
			},
			{
				name:           "LENGTH two emits 9-byte frame",
				length:         2,
				info:           []byte{0xAA, 0xBB},
				partialCut:     8,
				wantTotalBytes: 9,
			},
			{
				name:           "LENGTH five emits 12-byte frame",
				length:         5,
				info:           []byte{0x01, 0x02, 0x03, 0x04, 0x05},
				partialCut:     11,
				wantTotalBytes: 12,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				extractor := NewStreamExtractor()
				frame := testFrame(tt.info...)
				if int(frame[5]) != int(tt.length) {
					t.Fatalf("test setup mismatch: length byte=%d want=%d", frame[5], tt.length)
				}
				if len(frame) != tt.wantTotalBytes {
					t.Fatalf("test setup mismatch: total bytes=%d want=%d", len(frame), tt.wantTotalBytes)
				}

				got := extractor.Append(frame[:tt.partialCut])
				requireFramesEqual(t, got)
				if extractor.BufferedLen() != tt.partialCut {
					t.Fatalf("expected partial retention of %d bytes, got %d", tt.partialCut, extractor.BufferedLen())
				}

				got = extractor.Append(frame[tt.partialCut:])
				requireFramesEqual(t, got, frame)
				if extractor.BufferedLen() != 0 {
					t.Fatalf("expected empty buffer after full frame, got %d", extractor.BufferedLen())
				}
			})
		}
	})

	t.Run("declared length larger than currently buffered keeps partial and emits only when complete", func(t *testing.T) {
		extractor := NewStreamExtractor()
		frame := testFrame(0xAA, 0xBB, 0xCC, 0xDD)
		if frame[5] != 0x04 {
			t.Fatalf("test setup mismatch: expected length 4, got %d", frame[5])
		}

		got := extractor.Append(frame[:7])
		requireFramesEqual(t, got)
		if extractor.BufferedLen() != 7 {
			t.Fatalf("expected 7 retained bytes, got %d", extractor.BufferedLen())
		}

		got = extractor.Append(frame[7:10])
		requireFramesEqual(t, got)
		if extractor.BufferedLen() != 10 {
			t.Fatalf("expected 10 retained bytes before completion, got %d", extractor.BufferedLen())
		}

		got = extractor.Append(frame[10:])
		requireFramesEqual(t, got, frame)
		if extractor.BufferedLen() != 0 {
			t.Fatalf("expected empty buffer after completion, got %d", extractor.BufferedLen())
		}
	})

	t.Run("corrupt declared length sequence resyncs to later valid SOI/frame", func(t *testing.T) {
		extractor := NewStreamExtractorWithMaxBuffer(16)

		corruptHeader := []byte{PacketStartResponse, PacketPad1, PacketPad2, CIDReadTypeCUII, RTNUIIData, 0x08, 0x99, 0x88}
		got := extractor.Append(corruptHeader)
		requireFramesEqual(t, got)

		valid := testFrame(0x01, 0x20, 0x00, 0xAB, 0xC9)
		trailer := []byte{0x44, 0x55, 0x66}
		got = extractor.Append(append(append([]byte{}, valid...), trailer...))
		requireFramesEqual(t, got, valid)
		if extractor.BufferedLen() != 0 {
			t.Fatalf("expected trailing non-SOI noise discarded after extraction, got %d buffered bytes", extractor.BufferedLen())
		}
	})
}
