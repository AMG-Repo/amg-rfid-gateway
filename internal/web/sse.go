package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
)

// EnrichedTagDetected extends TagDetected with suggestion fields.
type EnrichedTagDetected struct {
	events.TagDetected
	SuggestedAction string `json:"suggested_action"`
	SuggestedReason string `json:"suggested_reason"`
}

// SSEHandler handles Server-Sent Events for real-time tag detection.
func (s *Server) SSEHandler(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Subscribe to event bus
	ch := s.eventBus.Subscribe()

	// Add to sseClients for tracking
	s.mu.Lock()
	s.sseClients[ch] = struct{}{}
	s.mu.Unlock()

	// Cleanup on exit
	defer func() {
		s.mu.Lock()
		delete(s.sseClients, ch)
		s.mu.Unlock()
		s.eventBus.Unsubscribe(ch)
	}()

	// Create flusher for streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial connection message
	fmt.Fprintf(w, "event: connected\ndata: %s\n\n", `{"status":"connected"}`)
	flusher.Flush()

	log.Printf("[Web] SSE client connected from %s", r.RemoteAddr)

	// Heartbeat ticker
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	// Event loop
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				log.Printf("[Web] SSE channel closed")
				return
			}

			// Enrich event with suggestions
			enriched := EnrichedTagDetected{
				TagDetected:     event,
				SuggestedAction: "unknown",
				SuggestedReason: "unknown:no_verifier",
			}

			// Call verifier to get suggestion if available
			if s.verifier != nil {
				suggestion := s.verifier.SuggestAction(event.EPC, event.AntennaID)
				enriched.SuggestedAction = string(suggestion.Action)
				enriched.SuggestedReason = suggestion.Reason
			}

			// Serialize enriched event to JSON
			data, err := json.Marshal(enriched)
			if err != nil {
				log.Printf("[Web] Failed to marshal event: %v", err)
				continue
			}

			// Send event
			fmt.Fprintf(w, "event: tag_detected\ndata: %s\n\n", string(data))
			flusher.Flush()

		case <-r.Context().Done():
			log.Printf("[Web] SSE client disconnected from %s", r.RemoteAddr)
			return

		case <-heartbeat.C:
			// Send heartbeat
			fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", `{"time":"`+time.Now().Format(time.RFC3339)+`"}`)
			flusher.Flush()
		}
	}
}
