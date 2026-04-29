package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/antenna"
	cachepkg "github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-gateway/internal/health"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/monitoring"
	"github.com/amg-rfid/amg-rfid-gateway/internal/rawtcp"
	syncpkg "github.com/amg-rfid/amg-rfid-gateway/internal/sync"
	"github.com/amg-rfid/amg-rfid-gateway/internal/tui"
	"github.com/amg-rfid/amg-rfid-gateway/internal/verify"
	"github.com/amg-rfid/amg-rfid-gateway/internal/web"
	"github.com/amg-rfid/amg-rfid-gateway/internal/wsclient"
)

const (
	version   = "0.1.0"
	appName   = "AMG RFID Gateway"
	dataDir   = "./data"
	configDir = "./configs"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", filepath.Join(configDir, "config.yaml"), "Path to config file")
	flag.Parse()

	log.Printf("%s v%s starting...", appName, version)

	// Load configuration
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Gateway ID: %s", cfg.GatewayID)
	log.Printf("Company ID: %s", cfg.CompanyID)
	log.Printf("Cloud URL: %s", cfg.CloudURL)
	log.Printf("Antennas: %d", len(cfg.Antennas))

	// Initialize SQLite cache
	dbPath := filepath.Join(dataDir, "cache.db")
	cacheStore, err := cachepkg.NewSQLite(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}
	defer cacheStore.Close()
	log.Println("Cache initialized")

	// Initialize event bus
	eventBus := events.NewEventBus(1000)
	log.Println("Event bus initialized")

	// Initialize local store
	localStore := localstore.New(cacheStore.GetDB())
	log.Println("Local store initialized")

	// Initialize verifier (with adapter for ToolStore interface)
	toolStoreAdapter := &toolStoreAdapter{store: localStore}
	verifier := verify.NewVerifier(toolStoreAdapter, cfg.Antennas)
	log.Println("Verifier initialized")

	// Initialize VPS HTTP client
	vpsClient := httpclient.NewVPSClient(cfg.VPSAPIURL, 10*time.Second)
	log.Println("VPS client initialized")

	// Initialize tools sync service
	toolsSync := syncpkg.NewToolsSync(
		vpsClient,
		localStore,
		cfg.CompanyID,
		cfg.SyncToolsInterval,
		cfg.ConfirmationRetryInterval,
	)
	log.Println("Tools sync initialized")

	// Initialize web server (if enabled)
	var webServer *web.Server
	if cfg.WebEnabled {
		webServer = web.NewServer(
			cfg.WebListenAddr,
			cfg.WebPort,
			eventBus,
			localStore,
			verifier,
			vpsClient,
			cfg.CompanyID,
		)
		log.Printf("Web server initialized on %s:%d", cfg.WebListenAddr, cfg.WebPort)
	}

	// Initialize WebSocket client
	wsClient := wsclient.NewClient(cfg.CloudURL, cfg.JWTSecret)

	// Initialize sync engine
	syncConfig := syncpkg.EngineConfig{
		GatewayID:    cfg.GatewayID,
		CompanyID:    cfg.CompanyID,
		SyncInterval: cfg.SyncInterval,
		BatchSize:    cfg.BatchSize,
		MaxRetries:   cfg.MaxRetries,
	}
	syncEngine := syncpkg.NewEngine(cacheStore, wsClient, syncConfig)

	// Initialize health monitor
	healthMonitor := health.NewMonitor(cfg.GatewayID, cfg.CompanyID, version, cacheStore, syncEngine)

	// Initialize metrics
	metricsCollector := monitoring.NewMetrics(cacheStore, syncEngine)

	// Initialize antenna provider for bridge server
	antennaProvider := antenna.NewAntennaManagerProvider()

	// Initialize and start bridge server for TUI
	bridgeServer := tui.NewBridgeServer("", healthMonitor, antennaProvider)
	bridgeCtx, bridgeCancel := context.WithCancel(context.Background())
	defer bridgeCancel()

	if err := bridgeServer.Start(bridgeCtx); err != nil {
		log.Printf("Warning: Failed to start bridge server: %v", err)
	} else {
		log.Println("Bridge server started for TUI communication")
	}

	// Start antenna goroutines with context for cancellation
	antennaCtx, antennaCancel := context.WithCancel(context.Background())
	defer antennaCancel()

	var antennaWg sync.WaitGroup
	for _, ant := range cfg.Antennas {
		if !ant.Enabled {
			log.Printf("Antenna %s is disabled, skipping", ant.ID)
			continue
		}

		antennaWg.Add(1)
		go func(antenna config.AntennaConfig) {
			defer antennaWg.Done()
			runAntennaWithReconnect(antennaCtx, antenna, cacheStore, healthMonitor, cfg, antennaProvider, eventBus)
		}(ant)
	}

	// Start sync engine
	syncEngine.Start()
	log.Println("Sync engine started")

	// Start health monitor
	healthMonitor.Start()
	log.Println("Health monitor started")

	// Start tools sync service
	toolsSyncCtx, toolsSyncCancel := context.WithCancel(context.Background())
	defer toolsSyncCancel()
	if err := toolsSync.Start(toolsSyncCtx); err != nil {
		log.Printf("Warning: Failed to start tools sync: %v", err)
	} else {
		log.Println("Tools sync started")
	}

	// Start web server (if enabled)
	if webServer != nil {
		webCtx, webCancel := context.WithCancel(context.Background())
		defer webCancel()
		if err := webServer.Start(webCtx); err != nil {
			log.Printf("Warning: Failed to start web server: %v", err)
		} else {
			log.Printf("Web server started on %s:%d", cfg.WebListenAddr, cfg.WebPort)
		}
	}

	// Start HTTP server for health and metrics
	go startHTTPServer(cfg.HealthPort, healthMonitor, metricsCollector)
	log.Printf("HTTP server started on port %d", cfg.HealthPort)

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Gateway running. Press Ctrl+C to stop.")
	<-sigCh

	log.Println("Shutting down...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop components
	syncEngine.Stop()
	healthMonitor.Stop()

	// Stop tools sync
	toolsSync.Stop()
	log.Println("Tools sync stopped")

	// Stop web server (if enabled)
	if webServer != nil {
		if err := webServer.Stop(ctx); err != nil {
			log.Printf("Warning: Error stopping web server: %v", err)
		}
		log.Println("Web server stopped")
	}

	// Stop bridge server
	bridgeCancel()
	if err := bridgeServer.Stop(); err != nil {
		log.Printf("Warning: Error stopping bridge server: %v", err)
	}

	// Cancel antenna context to signal goroutines to stop
	antennaCancel()

	// Wait for antennas to finish
	done := make(chan struct{})
	go func() {
		antennaWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All antennas stopped")
	case <-ctx.Done():
		log.Println("Shutdown timeout, forcing exit")
	}

	log.Println("Gateway stopped")
}

// toolStoreAdapter wraps localstore to implement verify.ToolStore interface
type toolStoreAdapter struct {
	store *localstore.LocalStore
}

// GetToolByUII implements verify.ToolStore interface
func (a *toolStoreAdapter) GetToolByUII(uii string) (*verify.Tool, error) {
	tool, err := a.store.GetToolByUII(uii)
	if err != nil {
		return nil, err
	}
	if tool == nil {
		return nil, nil
	}
	return &verify.Tool{
		ID:       tool.ID,
		SKU:      tool.SKU,
		Name:     tool.Name,
		UII:      tool.UII,
		Location: tool.Location,
		Status:   tool.Status,
	}, nil
}

// loadConfig loads configuration from file or environment
func loadConfig(path string) (*config.GatewayConfig, error) {
	// Try to load from file first
	if _, err := os.Stat(path); err == nil {
		cfg, err := config.LoadFromYAML(path)
		if err == nil {
			return cfg, nil
		}
		log.Printf("Warning: failed to load config from %s: %v", path, err)
	}

	// Fall back to environment variables
	log.Println("Loading configuration from environment variables")
	cfg := config.LoadFromEnv()

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

// getConn retrieves the underlying net.Conn from a rawtcp.Client
// This is needed to pass to the AntennaManager for data reading.
type connGetter interface {
	GetConn() net.Conn
}

// runAntenna runs a single antenna client using AntennaManager
func runAntenna(ctx context.Context, antennaCfg config.AntennaConfig, cacheStore *cachepkg.SQLite, monitor *health.Monitor, gwCfg *config.GatewayConfig, antennaProvider *antenna.AntennaManagerProvider, eventBus *events.EventBus) {
	log.Printf("Starting antenna %s at %s:%d", antennaCfg.ID, antennaCfg.IP, antennaCfg.Port)

	// Update health status as connecting
	monitor.UpdateAntennaStatus(antennaCfg.ID, false)

	client := rawtcp.NewClient(antennaCfg.IP, antennaCfg.Port)

	// Connect to antenna
	if err := client.Connect(); err != nil {
		log.Printf("Antenna %s failed to connect: %v", antennaCfg.ID, err)
		return
	}

	// Get the underlying connection for the manager
	var conn net.Conn
	if cg, ok := interface{}(client).(connGetter); ok {
		conn = cg.GetConn()
	}

	// Update health status as connected
	monitor.UpdateAntennaStatus(antennaCfg.ID, true)
	log.Printf("Antenna %s connected", antennaCfg.ID)

	// Create and start AntennaManager
	manager := antenna.NewAntennaManager(client, antennaCfg, gwCfg, cacheStore)

	// Set the connection for data reading
	manager.SetConn(conn)

	// Wire event bus for tag detection events
	if eventBus != nil {
		manager.SetEventBus(eventBus)
	}

	// Add manager to provider for TUI access
	antennaProvider.AddManager(manager)

	// Start the manager (this blocks until connection drops or context cancelled)
	if err := manager.Start(ctx); err != nil {
		log.Printf("Antenna %s manager failed to start: %v", antennaCfg.ID, err)
		client.Disconnect()
		return
	}

	// Wait for manager to stop
	<-manager.Done()

	client.Disconnect()

	// Update health status as disconnected
	monitor.UpdateAntennaStatus(antennaCfg.ID, false)
	log.Printf("Antenna %s stopped", antennaCfg.ID)
}

// runAntennaWithReconnect wraps runAntenna with exponential backoff reconnection.
// Reconnects on connection loss with backoff: 1s → 2s → 4s → 8s → 16s → 30s (max)
func runAntennaWithReconnect(ctx context.Context, antennaCfg config.AntennaConfig, cacheStore *cachepkg.SQLite, monitor *health.Monitor, gwCfg *config.GatewayConfig, antennaProvider *antenna.AntennaManagerProvider, eventBus *events.EventBus) {
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			log.Printf("Antenna %s: reconnection loop cancelled", antennaCfg.ID)
			return
		default:
		}

		// Run the antenna (this blocks until connection drops or context cancelled)
		runAntenna(ctx, antennaCfg, cacheStore, monitor, gwCfg, antennaProvider, eventBus)

		// Check if context was cancelled
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Connection dropped, calculate backoff
		backoff := calculateBackoff(attempt, gwCfg.ReconnectInitialBackoff, gwCfg.ReconnectMaxBackoff)
		attempt++

		log.Printf("Antenna %s: connection lost, reconnecting in %v (attempt %d)",
			antennaCfg.ID, backoff, attempt)

		// Wait for backoff duration or context cancellation
		select {
		case <-time.After(backoff):
			// Continue to reconnect
		case <-ctx.Done():
			log.Printf("Antenna %s: reconnection cancelled during backoff", antennaCfg.ID)
			return
		}
	}
}

// calculateBackoff calculates exponential backoff duration.
// Sequence: initial → initial×2 → initial×4 → ... → max
func calculateBackoff(attempt int, initialBackoff, maxBackoff time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	// Calculate backoff: initial * 2^attempt
	backoff := initialBackoff * time.Duration(1<<attempt)

	// Cap at max backoff
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	return backoff
}

// isRetryableError returns true if the error indicates a retryable connection failure.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"broken pipe",
		"network is unreachable",
		"no route to host",
		"i/o timeout",
		"EOF",
	}

	for _, pattern := range retryablePatterns {
		if containsString(errStr, pattern) {
			return true
		}
	}

	return false
}

// containsString checks if a string contains a substring (case-insensitive)
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			findSubstring(s, substr)))
}

// findSubstring searches for substring in string
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// startHTTPServer starts the HTTP server for health and metrics endpoints
func startHTTPServer(port int, monitor *health.Monitor, metrics *monitoring.Metrics) {
	mux := http.NewServeMux()

	// Health endpoint
	mux.Handle("/health", health.HealthHandler(monitor))

	// Metrics endpoint
	mux.Handle("/metrics", monitoring.MetricsHandler(metrics))

	// Simple status endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status": "ok", "version": "%s"}`, version)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("HTTP server error: %v", err)
	}
}
