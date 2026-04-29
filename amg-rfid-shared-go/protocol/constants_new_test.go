package protocol

import (
	"testing"
)

func TestCommandConstants(t *testing.T) {
	// Verify new command codes per REQ-A004
	if CmdGetReaderInfo != 0x01 {
		t.Errorf("expected CmdGetReaderInfo = 0x01, got 0x%02x", CmdGetReaderInfo)
	}
}

func TestResponseConstants(t *testing.T) {
	// Verify new RTN codes per REQ-A004
	if RtnUIIData != 0x02 {
		t.Errorf("expected RtnUIIData = 0x02, got 0x%02x", RtnUIIData)
	}

	if RtnTagData != 0x06 {
		t.Errorf("expected RtnTagData = 0x06, got 0x%02x", RtnTagData)
	}

	if RtnError != 0x07 {
		t.Errorf("expected RtnError = 0x07, got 0x%02x", RtnError)
	}

	if RtnHeartbeatResponse != 0x10 {
		t.Errorf("expected RtnHeartbeatResponse = 0x10, got 0x%02x", RtnHeartbeatResponse)
	}
}

func TestResponseConstantValues(t *testing.T) {
	// Verify that response codes don't overlap with existing codes
	responseCodes := []byte{
		RtnUIIData,
		RtnTagData,
		RtnError,
		RtnHeartbeatResponse,
	}

	// All should be unique
	seen := make(map[byte]bool)
	for _, code := range responseCodes {
		if seen[code] {
			t.Errorf("duplicate response code: 0x%02x", code)
		}
		seen[code] = true
	}
}
