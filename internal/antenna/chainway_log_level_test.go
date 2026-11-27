package antenna

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/amg-rfid/amg-rfid-gateway/internal/logx"
)

// At the default info level a raw tag read is stored but not logged: at tens
// of reads per second the log would grow with the read rate.
func TestHandlePacket_ChainwayTagNotLoggedAtInfo(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev); logx.SetLevel("info") })

	for _, tc := range []struct {
		level string
		want  bool
	}{{"info", false}, {"debug", true}} {
		buf.Reset()
		logx.SetLevel(tc.level)
		manager, cache, cancel := newChainwayTestManager(nil)
		if err := manager.HandlePacket(chainwayTagFrame(t, 1)); err != nil {
			t.Fatalf("HandlePacket: %v", err)
		}
		cancel()
		if len(cache.stored) != 1 {
			t.Fatalf("level %s: stored %d readings, want 1", tc.level, len(cache.stored))
		}
		if got := strings.Contains(buf.String(), "Chainway tag from antenna"); got != tc.want {
			t.Errorf("level %s: raw read logged = %v, want %v (log %q)", tc.level, got, tc.want, buf.String())
		}
	}
}
