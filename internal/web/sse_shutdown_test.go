package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
)

// Stopping the server while an SSE client is connected must end the stream
// without closing the subscriber channel twice (issue #133).
func TestStop_WithConnectedSSEClientDoesNotPanic(t *testing.T) {
	bus := events.NewEventBus(10)
	srv := NewServer("127.0.0.1", 0, bus, nil, nil, nil, "c")

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("SSE handler panicked: %v", r)
				return
			}
			done <- nil
		}()
		srv.SSEHandler(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/events", nil))
	}()
	waitUntil(t, "SSE subscriber", func() bool { srv.mu.RLock(); defer srv.mu.RUnlock(); return len(srv.sseClients) == 1 })

	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not return after Stop")
	}

	// The bus no longer holds the channel: publishing and closing stay safe.
	bus.Publish(events.TagDetected{EPC: "X", AntennaID: "a", Timestamp: time.Now()})
	bus.Close()
}
