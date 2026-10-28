package rawtcp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/chainway"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

func TestRunAntenna_ChainwayStoresMappedReading(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	tag, err := chainway.Encode(chainway.CmdTagNotification, []byte{
		0x08, 0x00, 0xAB, 0xCD, 0xFC, 0x5E, 0x02,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, _ := listener.Accept()
		if conn == nil {
			return
		}
		defer conn.Close()
		// Split the frame across two writes, then send a generic frame that must be ignored.
		_, _ = conn.Write(tag[:6])
		time.Sleep(20 * time.Millisecond)
		_, _ = conn.Write(tag[6:])
		_, _ = conn.Write(mustBuildGenericUIIFrame(t))
		time.Sleep(100 * time.Millisecond)
	}()

	cache := &mockCache{}
	client := NewClient("127.0.0.1", listener.Addr().(*net.TCPAddr).Port)
	antenna := AntennaConfig{
		ID:            "ur4-1",
		Enabled:       true,
		Protocol:      config.ProtocolChainway,
		PortLocations: map[int]string{2: "salida-x"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := RunAntenna(ctx, client, cache, antenna); err != nil && err.Error() != "connection closed" {
		t.Fatalf("RunAntenna: %v", err)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.stored) != 1 {
		t.Fatalf("stored %d readings, want 1: %+v", len(cache.stored), cache.stored)
	}
	if r := cache.stored[0]; r.EPC != "ABCD" || r.AntennaID != "salida-x" || r.RSSI != -93 {
		t.Fatalf("reading = %+v", r)
	}
}
