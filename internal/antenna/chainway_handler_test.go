package antenna

import (
	"context"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/chainway"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

// chainwayTagFrame builds a 0x83 frame with a 96-bit EPC, RSSI -92.9 dBm, on port ant.
func chainwayTagFrame(t *testing.T, ant byte) []byte {
	t.Helper()
	data := []byte{
		0x30, 0x00,
		0xE2, 0x00, 0x00, 0x17, 0x22, 0x0A, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB,
		0xFC, 0x5E,
		ant,
	}
	raw, err := chainway.Encode(chainway.CmdTagNotification, data)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newChainwayTestManager(locations map[int]string) (*AntennaManager, *mockCache, context.CancelFunc) {
	manager, _, cache, cancel := newTestAntennaManager()
	manager.config.Protocol = config.ProtocolChainway
	manager.config.PortLocations = locations
	return manager, cache, cancel
}

func TestProtocolHandlerFor_Chainway(t *testing.T) {
	if got := ProtocolHandlerFor(config.ProtocolChainway).Protocol(); got != config.ProtocolChainway {
		t.Fatalf("Protocol() = %q, want chainway", got)
	}
}

func TestHandlePacket_ChainwayTagStoresReading(t *testing.T) {
	tests := []struct {
		name          string
		locations     map[int]string
		ant           byte
		wantAntennaID string
	}{
		{"mapped port uses location", map[int]string{1: "entrada-x", 2: "salida-x"}, 2, "salida-x"},
		{"unmapped port falls back to antenna id", map[int]string{1: "entrada-x"}, 3, "ant-01"},
		{"no mapping falls back to antenna id", nil, 1, "ant-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, cache, cancel := newChainwayTestManager(tt.locations)
			defer cancel()

			if err := manager.HandlePacket(chainwayTagFrame(t, tt.ant)); err != nil {
				t.Fatalf("HandlePacket: %v", err)
			}

			cache.mu.Lock()
			defer cache.mu.Unlock()
			if len(cache.stored) != 1 {
				t.Fatalf("stored %d readings, want 1", len(cache.stored))
			}
			r := cache.stored[0]
			if r.EPC != "E2000017220A0123456789AB" || r.RSSI != -93 || r.AntennaID != tt.wantAntennaID || r.GatewayID != "gw-test-001" {
				t.Fatalf("reading = %+v", r)
			}
			status := manager.GetStatus()
			if status.ReadingCount != 1 || status.LastTagRSSI != -93 {
				t.Fatalf("stats = count %d rssi %d", status.ReadingCount, status.LastTagRSSI)
			}
		})
	}
}

func TestHandlePacket_ChainwayUnknownRSSIStoresZero(t *testing.T) {
	manager, cache, cancel := newChainwayTestManager(nil)
	defer cancel()
	raw, _ := chainway.Encode(chainway.CmdTagNotification, []byte{0x08, 0x00, 0xAB, 0xCD, 0xFF, 0xFF, 0x01})

	if err := manager.HandlePacket(raw); err != nil {
		t.Fatalf("HandlePacket: %v", err)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.stored) != 1 || cache.stored[0].RSSI != 0 {
		t.Fatalf("stored = %+v", cache.stored)
	}
}

func TestHandlePacket_ChainwayNonTagFrames(t *testing.T) {
	badChk := chainwayTagFrame(t, 1)
	badChk[len(badChk)-3] ^= 0xFF
	stopOK, _ := chainway.Encode(chainway.CmdStopInventoryResp, []byte{0x01})
	malformedTag, _ := chainway.Encode(chainway.CmdTagNotification, []byte{0x30, 0x00, 0x01})

	tests := []struct {
		name    string
		frame   []byte
		wantErr bool
	}{
		{"stop response is ignored", stopOK, false},
		{"unknown command is ignored", chainway.StartInventoryFrame(), false},
		{"bad checksum errors", badChk, true},
		{"malformed tag errors", malformedTag, true},
		{"generic bytes error", validUIITestPacket(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, cache, cancel := newChainwayTestManager(nil)
			defer cancel()

			err := manager.HandlePacket(tt.frame)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			cache.mu.Lock()
			defer cache.mu.Unlock()
			if len(cache.stored) != 0 {
				t.Fatalf("stored %d readings, want 0", len(cache.stored))
			}
		})
	}
}

func TestDataReader_ChainwaySplitAndMergedChunks(t *testing.T) {
	f1 := chainwayTagFrame(t, 1)
	f2 := chainwayTagFrame(t, 2)
	merged := append(append([]byte(nil), f1[5:]...), f2...)
	conn := &mockConn{readData: [][]byte{f1[:5], merged}}

	manager, cache, cancel := newChainwayTestManager(map[int]string{1: "entrada-x", 2: "salida-x"})
	defer cancel()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCtx()
	manager.wg.Add(1)
	go manager.dataReader(ctx, conn)
	waitForDataReaderExit(t, manager)

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.stored) != 2 || cache.stored[0].AntennaID != "entrada-x" || cache.stored[1].AntennaID != "salida-x" {
		t.Fatalf("stored = %+v", cache.stored)
	}
}

func TestDataReader_ChainwayIgnoresGenericFrames(t *testing.T) {
	conn := &mockConn{readData: [][]byte{mustBuildValidUIIPacket(t, 0x66)}}
	manager, cache, cancel := newChainwayTestManager(nil)
	defer cancel()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCtx()
	manager.wg.Add(1)
	go manager.dataReader(ctx, conn)
	waitForDataReaderExit(t, manager)

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.stored) != 0 {
		t.Fatalf("stored %d readings, want 0", len(cache.stored))
	}
}
