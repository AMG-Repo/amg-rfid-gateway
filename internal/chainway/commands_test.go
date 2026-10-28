package chainway

import (
	"bytes"
	"errors"
	"testing"
)

func TestCommandFramesMatchVendorLiterals(t *testing.T) {
	if got := StartInventoryFrame(); !bytes.Equal(got, startFrame) {
		t.Fatalf("StartInventoryFrame = % X, want % X", got, startFrame)
	}
	if got := StopInventoryFrame(); !bytes.Equal(got, stopFrame) {
		t.Fatalf("StopInventoryFrame = % X, want % X", got, stopFrame)
	}
}

func TestParseStopResponse(t *testing.T) {
	tests := []struct {
		name    string
		frame   Frame
		wantOK  bool
		wantErr error
	}{
		{"ok", Frame{Cmd: CmdStopInventoryResp, Data: []byte{0x01}}, true, nil},
		{"failed", Frame{Cmd: CmdStopInventoryResp, Data: []byte{0x00}}, false, nil},
		{"empty data", Frame{Cmd: CmdStopInventoryResp}, false, ErrPayload},
		{"wrong cmd", Frame{Cmd: CmdTagNotification, Data: []byte{0x01}}, false, ErrUnexpectedCmd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := ParseStopResponse(tt.frame)
			if !errors.Is(err, tt.wantErr) || ok != tt.wantOK {
				t.Fatalf("ParseStopResponse = (%v, %v), want (%v, %v)", ok, err, tt.wantOK, tt.wantErr)
			}
		})
	}
}

// pc96 announces a 12-byte (6-word) EPC: PC[0] = 6 << 3.
var (
	pc96  = []byte{0x30, 0x00}
	epc96 = []byte{0xE2, 0x00, 0x00, 0x17, 0x22, 0x0A, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB}
	tid   = []byte{0xE2, 0x80, 0x11, 0x05, 0x20, 0x00, 0x71, 0x22, 0x33, 0x44, 0x55, 0x66}
	user  = []byte{0xDE, 0xAD, 0xBE, 0xEF}
	// 0xFC5E = 64606 -> (65535-64606)/10 = 92.9 -> -92.9 dBm.
	rssi929 = []byte{0xFC, 0x5E}
)

func TestParseTag(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantEPC   string
		wantTID   string
		wantUser  string
		wantRSSI  float64
		wantValid bool
		wantAnt   int
	}{
		{"epc only", concat(pc96, epc96, rssi929, []byte{0x01}),
			"E2000017220A0123456789AB", "", "", -92.9, true, 1},
		{"epc and tid", concat(pc96, epc96, tid, rssi929, []byte{0x02}),
			"E2000017220A0123456789AB", "E28011052000712233445566", "", -92.9, true, 2},
		{"epc tid user", concat(pc96, epc96, tid, user, rssi929, []byte{0x04}),
			"E2000017220A0123456789AB", "E28011052000712233445566", "DEADBEEF", -92.9, true, 4},
		{"short epc", concat([]byte{0x08, 0x00}, []byte{0xAB, 0xCD}, rssi929, []byte{0x03}),
			"ABCD", "", "", -92.9, true, 3},
		{"rssi zero magnitude unknown", concat(pc96, epc96, []byte{0xFF, 0xFF}, []byte{0x01}),
			"E2000017220A0123456789AB", "", "", 0, false, 1},
		{"rssi at 200 unknown", concat(pc96, epc96, []byte{0xF8, 0x2F}, []byte{0x01}),
			"E2000017220A0123456789AB", "", "", 0, false, 1},
		{"rssi just below 200 valid", concat(pc96, epc96, []byte{0xF8, 0x30}, []byte{0x01}),
			"E2000017220A0123456789AB", "", "", -199.9, true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag, err := ParseTag(tt.data)
			if err != nil {
				t.Fatalf("ParseTag error: %v", err)
			}
			if tag.EPC != tt.wantEPC || tag.TID != tt.wantTID || tag.User != tt.wantUser {
				t.Fatalf("ids = (%q, %q, %q), want (%q, %q, %q)",
					tag.EPC, tag.TID, tag.User, tt.wantEPC, tt.wantTID, tt.wantUser)
			}
			if tag.RSSIValid != tt.wantValid || (tt.wantValid && !closeTo(tag.RSSI, tt.wantRSSI)) {
				t.Fatalf("RSSI = (%v, %v), want (%v, %v)", tag.RSSI, tag.RSSIValid, tt.wantRSSI, tt.wantValid)
			}
			if tag.Antenna != tt.wantAnt {
				t.Fatalf("Antenna = %d, want %d", tag.Antenna, tt.wantAnt)
			}
		})
	}
}

func TestParseTagRejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"pc only", pc96},
		{"epc shorter than pc", concat(pc96, epc96[:6], rssi929, []byte{0x01})},
		{"missing antenna", concat(pc96, epc96, rssi929)},
		{"partial tid", concat(pc96, epc96, tid[:5], rssi929, []byte{0x01})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseTag(tt.data); !errors.Is(err, ErrPayload) {
				t.Fatalf("err = %v, want ErrPayload", err)
			}
		})
	}
}

func TestParseTagFromFrame(t *testing.T) {
	raw, err := Encode(CmdTagNotification, concat(pc96, epc96, rssi929, []byte{0x01}))
	if err != nil {
		t.Fatal(err)
	}
	frames := NewStreamExtractor().Append(raw)
	if len(frames) != 1 {
		t.Fatalf("got %d frames", len(frames))
	}
	f, err := Decode(frames[0])
	if err != nil || f.Cmd != CmdTagNotification {
		t.Fatalf("Decode = (%+v, %v)", f, err)
	}
	tag, err := ParseTag(f.Data)
	if err != nil || tag.EPC != "E2000017220A0123456789AB" || tag.Antenna != 1 {
		t.Fatalf("ParseTag = (%+v, %v)", tag, err)
	}
}

func closeTo(a, b float64) bool {
	d := a - b
	return d < 1e-6 && d > -1e-6
}
