// Package tui provides the Bubbletea-based terminal UI for gateway configuration.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/antenna"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

const defaultSocketPath = "/tmp/amg-gateway.sock"

// HealthMonitor defines the interface for getting health status.
type HealthMonitor interface {
	GetStatus() models.GatewayHealthStatus
	GetUptime() time.Duration
}

// AntennaProvider defines the interface for getting antenna statuses.
type AntennaProvider interface {
	GetAntennaStatuses() []antenna.AntennaStatus
}

// ConfigProvider defines the interface for getting configuration.
type ConfigProvider interface {
	GetConfig() *config.GatewayConfig
}

// ConfigUpdater defines the interface for updating configuration.
type ConfigUpdater interface {
	UpdateConfig(*config.GatewayConfig) error
}

// ConfigReloader defines the interface for reloading configuration.
type ConfigReloader interface {
	ReloadConfig() error
}

// BridgeRequest represents a request to the bridge server.
type BridgeRequest struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// BridgeResponse represents a response from the bridge server.
type BridgeResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// BridgeServer provides a Unix socket server for TUI-to-gateway communication.
type BridgeServer struct {
	socketPath string
	listener   net.Listener
	health     HealthMonitor
	antennas   AntennaProvider

	// Config interfaces (optional - nil means not implemented)
	config   ConfigProvider
	updater  ConfigUpdater
	reloader ConfigReloader

	// State
	running bool
	mu      sync.RWMutex
	wg      sync.WaitGroup
}

// NewBridgeServer creates a new bridge server.
func NewBridgeServer(socketPath string, health HealthMonitor, antennas AntennaProvider) *BridgeServer {
	if socketPath == "" {
		socketPath = defaultSocketPath
	}

	return &BridgeServer{
		socketPath: socketPath,
		health:     health,
		antennas:   antennas,
	}
}

// Start starts the bridge server.
func (s *BridgeServer) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return errors.New("bridge server already running")
	}

	// Remove existing socket file if it exists
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	// Create listener
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}

	// Set socket permissions for same-user/group access only
	if err := os.Chmod(s.socketPath, 0770); err != nil {
		log.Printf("[Bridge] Warning: failed to set socket permissions: %v", err)
	}

	s.listener = listener
	s.running = true

	// Start accepting connections
	s.wg.Add(1)
	go s.serve(ctx)

	log.Printf("[Bridge] Server started on %s", s.socketPath)
	return nil
}

// Stop stops the bridge server.
func (s *BridgeServer) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}

	s.running = false
	listener := s.listener
	s.mu.Unlock()

	// Close listener to stop accepting new connections
	if listener != nil {
		listener.Close()
	}

	// Wait for existing connections to finish
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean up socket file
		os.Remove(s.socketPath)
		log.Printf("[Bridge] Server stopped")
		return nil
	case <-time.After(5 * time.Second):
		os.Remove(s.socketPath)
		return errors.New("timeout waiting for bridge server to stop")
	}
}

// IsRunning returns true if the server is running.
func (s *BridgeServer) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// SetConfigProvider sets the config provider interface.
func (s *BridgeServer) SetConfigProvider(provider ConfigProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = provider
}

// SetConfigUpdater sets the config updater interface.
func (s *BridgeServer) SetConfigUpdater(updater ConfigUpdater) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updater = updater
}

// SetConfigReloader sets the config reloader interface.
func (s *BridgeServer) SetConfigReloader(reloader ConfigReloader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloader = reloader
}

// serve accepts and handles connections.
func (s *BridgeServer) serve(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set accept timeout to allow context checking
		s.listener.(*net.UnixListener).SetDeadline(time.Now().Add(100 * time.Millisecond))

		conn, err := s.listener.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}
			// Check if we're still running
			s.mu.RLock()
			running := s.running
			s.mu.RUnlock()
			if !running {
				return
			}
			log.Printf("[Bridge] Accept error: %v", err)
			continue
		}

		// Handle connection in goroutine
		s.wg.Add(1)
		go s.handleConnection(ctx, conn)
	}
}

// handleConnection handles a single client connection.
func (s *BridgeServer) handleConnection(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	// Set read/write deadlines
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Read request
	var req BridgeRequest
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&req); err != nil {
		s.sendError(conn, "invalid request: "+err.Error())
		return
	}

	// Route request
	var resp BridgeResponse
	switch req.Path {
	case "/status":
		resp = s.handleGetStatus(req)
	case "/antennas":
		resp = s.handleGetAntennas(req)
	case "/config":
		if req.Method == "GET" || req.Method == "" {
			resp = s.handleGetConfig(req)
		} else if req.Method == "POST" {
			resp = s.handlePostConfig(req)
		} else {
			resp = BridgeResponse{
				Success: false,
				Error:   "method not allowed: " + req.Method,
			}
		}
	case "/config/reload":
		resp = s.handlePostConfigReload(req)
	default:
		resp = BridgeResponse{
			Success: false,
			Error:   "not found: " + req.Path,
		}
	}

	// Send response
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(resp); err != nil {
		log.Printf("[Bridge] Failed to send response: %v", err)
	}
}

// handleGetStatus handles GET /status requests.
func (s *BridgeServer) handleGetStatus(req BridgeRequest) BridgeResponse {
	if req.Method != "GET" && req.Method != "" {
		return BridgeResponse{
			Success: false,
			Error:   "method not allowed: " + req.Method,
		}
	}

	status := s.health.GetStatus()
	return BridgeResponse{
		Success: true,
		Data:    status,
	}
}

// handleGetAntennas handles GET /antennas requests.
func (s *BridgeServer) handleGetAntennas(req BridgeRequest) BridgeResponse {
	if req.Method != "GET" && req.Method != "" {
		return BridgeResponse{
			Success: false,
			Error:   "method not allowed: " + req.Method,
		}
	}

	antennas := s.antennas.GetAntennaStatuses()
	return BridgeResponse{
		Success: true,
		Data:    antennas,
	}
}

// handleGetConfig handles GET /config requests.
func (s *BridgeServer) handleGetConfig(req BridgeRequest) BridgeResponse {
	if req.Method != "GET" && req.Method != "" {
		return BridgeResponse{
			Success: false,
			Error:   "method not allowed: " + req.Method,
		}
	}

	// Check if config provider is available
	s.mu.RLock()
	provider := s.config
	s.mu.RUnlock()

	if provider == nil {
		return BridgeResponse{
			Success: false,
			Error:   "config provider not implemented",
		}
	}

	cfg := provider.GetConfig()
	return BridgeResponse{
		Success: true,
		Data:    cfg,
	}
}

// handlePostConfig handles POST /config requests.
func (s *BridgeServer) handlePostConfig(req BridgeRequest) BridgeResponse {
	if req.Method != "POST" {
		return BridgeResponse{
			Success: false,
			Error:   "method not allowed: " + req.Method,
		}
	}

	// Check if config updater is available
	s.mu.RLock()
	updater := s.updater
	s.mu.RUnlock()

	if updater == nil {
		return BridgeResponse{
			Success: false,
			Error:   "config updater not implemented",
		}
	}

	// Parse config from request body
	var cfg config.GatewayConfig
	if err := json.Unmarshal(req.Body, &cfg); err != nil {
		return BridgeResponse{
			Success: false,
			Error:   "invalid config body: " + err.Error(),
		}
	}

	// Update the config
	if err := updater.UpdateConfig(&cfg); err != nil {
		return BridgeResponse{
			Success: false,
			Error:   "failed to update config: " + err.Error(),
		}
	}

	return BridgeResponse{
		Success: true,
	}
}

// handlePostConfigReload handles POST /config/reload requests.
func (s *BridgeServer) handlePostConfigReload(req BridgeRequest) BridgeResponse {
	if req.Method != "POST" && req.Method != "" {
		return BridgeResponse{
			Success: false,
			Error:   "method not allowed: " + req.Method,
		}
	}

	// Check if config reloader is available
	s.mu.RLock()
	reloader := s.reloader
	s.mu.RUnlock()

	if reloader == nil {
		return BridgeResponse{
			Success: false,
			Error:   "config reloader not implemented",
		}
	}

	// Trigger reload
	if err := reloader.ReloadConfig(); err != nil {
		return BridgeResponse{
			Success: false,
			Error:   "failed to reload config: " + err.Error(),
		}
	}

	return BridgeResponse{
		Success: true,
	}
}

// sendError sends an error response.
func (s *BridgeServer) sendError(conn net.Conn, msg string) {
	resp := BridgeResponse{
		Success: false,
		Error:   msg,
	}
	encoder := json.NewEncoder(conn)
	encoder.Encode(resp)
}
