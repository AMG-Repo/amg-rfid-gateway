package logx

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		SetLevel("info")
	})
	return &buf
}

func TestSetLevel_FiltersDebugAndInfo(t *testing.T) {
	for _, tc := range []struct {
		level     string
		wantDebug bool
		wantInfo  bool
	}{
		{"debug", true, true},
		{"DEBUG", true, true},
		{"info", false, true},
		{"", false, true},
		{"warn", false, false},
		{"error", false, false},
	} {
		t.Run(tc.level, func(t *testing.T) {
			buf := capture(t)
			if err := SetLevel(tc.level); err != nil {
				t.Fatalf("SetLevel(%q): %v", tc.level, err)
			}
			Debugf("d %d", 1)
			Infof("i %d", 2)
			out := buf.String()
			if got := strings.Contains(out, "[DEBUG] d 1"); got != tc.wantDebug {
				t.Errorf("debug logged = %v, want %v (out %q)", got, tc.wantDebug, out)
			}
			if got := strings.Contains(out, "[INFO] i 2"); got != tc.wantInfo {
				t.Errorf("info logged = %v, want %v (out %q)", got, tc.wantInfo, out)
			}
		})
	}
}

// An unknown level is reported and falls back to info, so a typo never
// silences the gateway.
func TestSetLevel_UnknownFallsBackToInfo(t *testing.T) {
	buf := capture(t)
	if err := SetLevel("verbose"); err == nil {
		t.Fatal("SetLevel(verbose) returned nil error")
	}
	Debugf("d")
	Infof("i")
	if out := buf.String(); strings.Contains(out, "[DEBUG]") || !strings.Contains(out, "[INFO] i") {
		t.Errorf("out = %q, want info only", out)
	}
}
