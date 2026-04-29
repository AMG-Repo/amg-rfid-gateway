// Package web provides HTTP server and API for local verification frontend.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/verify"
)

//go:embed all:static
var staticFS embed.FS

// Server provides HTTP endpoints for the verification frontend.
type Server struct {
	httpServer *http.Server
	eventBus   *events.EventBus
	store      *localstore.LocalStore
	verifier   *verify.Verifier
	vpsClient  *httpclient.VPSClient
	companyID  string
	config     *config.GatewayConfig
	sseClients map[chan events.TagDetected]struct{}
	mu         sync.RWMutex
}

// NewServer creates a new web server.
func NewServer(
	addr string,
	port int,
	eventBus *events.EventBus,
	store *localstore.LocalStore,
	verifier *verify.Verifier,
	vpsClient *httpclient.VPSClient,
	companyID string,
) *Server {
	return NewServerWithConfig(addr, port, eventBus, store, verifier, vpsClient, companyID, nil)
}

// NewServerWithConfig creates a new web server with config.
func NewServerWithConfig(
	addr string,
	port int,
	eventBus *events.EventBus,
	store *localstore.LocalStore,
	verifier *verify.Verifier,
	vpsClient *httpclient.VPSClient,
	companyID string,
	cfg *config.GatewayConfig,
) *Server {
	return &Server{
		eventBus:   eventBus,
		store:      store,
		verifier:   verifier,
		vpsClient:  vpsClient,
		companyID:  companyID,
		config:     cfg,
		sseClients: make(map[chan events.TagDetected]struct{}),
		httpServer: &http.Server{
			Addr:         fmt.Sprintf("%s:%d", addr, port),
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 60 * time.Second, // Longer for SSE
			IdleTimeout:  60 * time.Second,
		},
	}
}

// staticFileServer serves files from the embedded static directory
type staticFileServer struct {
	fs http.FileSystem
}

func (s *staticFileServer) Open(name string) (http.File, error) {
	// Serve files from the static subdirectory
	return s.fs.Open("static" + name)
}

// Start initializes routes and starts the HTTP server.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Static files - strip the static/ prefix by using a custom wrapper
	staticHandler := http.FileServer(&staticFileServer{fs: http.FS(staticFS)})
	mux.Handle("/", staticHandler)

	// API routes
	mux.HandleFunc("/events", s.SSEHandler)
	mux.HandleFunc("/api/tags", s.handleTags)
	mux.HandleFunc("/api/confirm", s.handleConfirm)
	mux.HandleFunc("/api/status", s.handleStatus)

	s.httpServer.Handler = mux

	// Start server in goroutine
	go func() {
		log.Printf("[Web] Server starting on %s", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Web] Server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	log.Printf("[Web] Shutting down server...")

	// Close all SSE clients
	s.mu.Lock()
	for ch := range s.sseClients {
		close(ch)
		delete(s.sseClients, ch)
	}
	s.mu.Unlock()

	return s.httpServer.Shutdown(ctx)
}

// response helpers
func (s *Server) jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[Web] Failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *Server) jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
