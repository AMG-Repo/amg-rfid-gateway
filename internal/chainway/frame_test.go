package chainway

import (
	"bytes"
	"errors"
	"testing"
)

var (
	startFrame = []byte{0xA5, 0x5A, 0x00, 0x0A, 0x82, 0x00, 0x00, 0x88, 0x0D, 0x0A}
	stopFrame  = []byte{0xA5, 0x5A, 0x00, 0x08, 0x8C, 0x84, 0x0D, 0x0A}
)

func TestEncodeMatchesVendorLiterals(t *testing.T) {
	tests := []struct {
		name string
		cmd  byte
		data []byte
		want []byte
	}{
		{"start inventory", 0x82, []byte{0x00, 0x00}, startFrame},
		{"stop inventory", 0x8C, nil, stopFrame},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Encode(tt.cmd, tt.data)
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("Encode = % X, want % X", got, tt.want)
			}
		})
	}
}

func TestEncodeRejectsOversizedData(t *testing.T) {
	_, err := Encode(0x82, make([]byte, MaxFrameLen-FrameOverhead+1))
	if !errors.Is(err, ErrFrameLength) {
		t.Fatalf("err = %v, want ErrFrameLength", err)
	}
}

func TestDecode(t *testing.T) {
	badChk := append([]byte(nil), startFrame...)
	badChk[7] ^= 0xFF
	badTrailer := append([]byte(nil), stopFrame...)
	badTrailer[7] = 0x00
	badHeader := append([]byte(nil), stopFrame...)
	badHeader[0] = 0x00
	badLen := append([]byte(nil), stopFrame...)
	badLen[3] = 0x09

	tests := []struct {
		name    string
		raw     []byte
		wantCmd byte
		data    []byte
		wantErr error
	}{
		{"start", startFrame, 0x82, []byte{0x00, 0x00}, nil},
		{"stop no data", stopFrame, 0x8C, []byte{}, nil},
		{"bad checksum", badChk, 0, nil, ErrChecksum},
		{"bad trailer", badTrailer, 0, nil, ErrTrailer},
		{"bad header", badHeader, 0, nil, ErrHeader},
		{"len mismatch", badLen, 0, nil, ErrFrameLength},
		{"too short", []byte{0xA5, 0x5A, 0x00}, 0, nil, ErrFrameLength},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Decode(tt.raw)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Decode error: %v", err)
			}
			if f.Cmd != tt.wantCmd || !bytes.Equal(f.Data, tt.data) {
				t.Fatalf("Decode = {%02X % X}, want {%02X % X}", f.Cmd, f.Data, tt.wantCmd, tt.data)
			}
		})
	}
}

func TestDecodeRoundTripsEncode(t *testing.T) {
	data := []byte{0x30, 0x00, 0xE2, 0x00, 0x11, 0x22, 0xFF, 0x9C, 0x01}
	raw, err := Encode(0x83, data)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if f.Cmd != 0x83 || !bytes.Equal(f.Data, data) {
		t.Fatalf("round trip = {%02X % X}", f.Cmd, f.Data)
	}
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestStreamExtractor(t *testing.T) {
	badChk := append([]byte(nil), startFrame...)
	badChk[7] ^= 0xFF
	badLenHeader := []byte{0xA5, 0x5A, 0x00, 0x03}        // LEN < 8
	hugeLenHeader := []byte{0xA5, 0x5A, 0x08, 0x01, 0x83} // LEN > 2048

	tests := []struct {
		name         string
		chunks       [][]byte
		want         [][]byte
		wantBuffered int
	}{
		{"single frame", [][]byte{startFrame}, [][]byte{startFrame}, 0},
		{"merged frames", [][]byte{concat(startFrame, stopFrame)}, [][]byte{startFrame, stopFrame}, 0},
		{"split header", [][]byte{startFrame[:1], startFrame[1:]}, [][]byte{startFrame}, 0},
		{"split body", [][]byte{startFrame[:5], startFrame[5:9], startFrame[9:]}, [][]byte{startFrame}, 0},
		{"split across merged", [][]byte{concat(startFrame, stopFrame[:3]), stopFrame[3:]}, [][]byte{startFrame, stopFrame}, 0},
		{"leading garbage", [][]byte{concat([]byte{0x00, 0xA5, 0x11}, stopFrame)}, [][]byte{stopFrame}, 0},
		{"bad checksum resyncs", [][]byte{concat(badChk, stopFrame)}, [][]byte{stopFrame}, 0},
		{"invalid small len resyncs", [][]byte{concat(badLenHeader, stopFrame)}, [][]byte{stopFrame}, 0},
		{"invalid huge len resyncs", [][]byte{concat(hugeLenHeader, stopFrame)}, [][]byte{stopFrame}, 0},
		{"partial frame retained", [][]byte{startFrame[:6]}, nil, 6},
		{"lone A5 retained", [][]byte{concat([]byte{0x01, 0x02}, []byte{0xA5})}, nil, 1},
		{"pure garbage dropped", [][]byte{{0x01, 0x02, 0x03}}, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewStreamExtractor()
			var got [][]byte
			for _, c := range tt.chunks {
				got = append(got, e.Append(c)...)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d frames (% X), want %d", len(got), got, len(tt.want))
			}
			for i := range got {
				if !bytes.Equal(got[i], tt.want[i]) {
					t.Fatalf("frame %d = % X, want % X", i, got[i], tt.want[i])
				}
			}
			if e.BufferedLen() != tt.wantBuffered {
				t.Fatalf("BufferedLen = %d, want %d", e.BufferedLen(), tt.wantBuffered)
			}
		})
	}
}

func TestStreamExtractorFramesDoNotAliasBuffer(t *testing.T) {
	e := NewStreamExtractor()
	chunk := concat(startFrame, stopFrame)
	frames := e.Append(chunk)
	chunk[4] = 0xEE
	if frames[0][4] != 0x82 {
		t.Fatalf("frame aliases input chunk")
	}
}

func TestStreamExtractorBufferCap(t *testing.T) {
	e := NewStreamExtractorWithMaxBuffer(16)
	// A valid header announcing a 2048-byte frame that never completes.
	pending := []byte{0xA5, 0x5A, 0x08, 0x00, 0x83}
	e.Append(pending)
	e.Append(make([]byte, 32))
	if e.BufferedLen() > 16 {
		t.Fatalf("BufferedLen = %d exceeds cap 16", e.BufferedLen())
	}
	// Extractor recovers and still yields later frames.
	if got := e.Append(stopFrame); len(got) != 1 || !bytes.Equal(got[0], stopFrame) {
		t.Fatalf("after overflow got % X, want stop frame", got)
	}
}

func TestStreamExtractorReset(t *testing.T) {
	e := NewStreamExtractor()
	e.Append(startFrame[:4])
	e.Reset()
	if e.BufferedLen() != 0 {
		t.Fatalf("BufferedLen after Reset = %d", e.BufferedLen())
	}
	if got := e.Append(stopFrame); len(got) != 1 {
		t.Fatalf("after Reset got %d frames", len(got))
	}
}
