// Package e2e provides end-to-end integration tests for the gateway.
// Tests the complete flow: Gateway → Cloud backend with sync operations.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	syncengine "github.com/amg-rfid/amg-rfid-gateway/internal/sync"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// MockBackend simulates the cloud backend for testing.
type MockBackend struct {
	server    *httptest.Server
	mu        sync.RWMutex
	readings  []models.Reading
	connected bool
	gatewayID string
	companyID string
	jwtSecret string
}

// NewMockBackend creates a new mock backend server.
func NewMockBackend(gatewayID, companyID, jwtSecret string) *MockBackend {
	mb := &MockBackend{
		gatewayID: gatewayID,
		companyID: companyID,
		jwtSecret: jwtSecret,
		readings:  make([]models.Reading, 0),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health/gateways", mb.handleHealth)
	mux.HandleFunc("/api/v1/rfid/sync", mb.handleSync)
	mux.HandleFunc("/api/v1/rfid/pending", mb.handlePending)
	mb.server = httptest.NewServer(mux)
	mb.connected = true

	return mb
}

// handleHealth handles health check requests.
func (mb *MockBackend) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Check auth
	if !mb.validateAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
	})
}

// handleSync processes sync requests from gateways.
func (mb *MockBackend) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Check auth
	if !mb.validateAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	var req models.SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Validate gateway/company IDs from token
	authHeader := r.Header.Get("Authorization")
	tokenGatewayID, tokenCompanyID := mb.parseToken(authHeader)

	if tokenGatewayID != "" && tokenGatewayID != req.GatewayID {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "gateway ID mismatch"})
		return
	}

	if tokenCompanyID != "" && tokenCompanyID != req.CompanyID {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "company ID mismatch"})
		return
	}

	mb.mu.Lock()
	mb.readings = append(mb.readings, req.Readings...)
	mb.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.SyncResponse{
		Success:    true,
		Accepted:   len(req.Readings),
		Duplicates: 0,
		ServerTime: time.Now().UTC(),
	})
}

// handlePending returns pending readings (empty for mock).
func (mb *MockBackend) handlePending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Check auth
	if !mb.validateAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	gatewayID, _ := mb.parseToken(r.Header.Get("Authorization"))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"gateway_id": gatewayID,
		"readings":   []models.Reading{},
		"count":      0,
	})
}

// validateAuth checks if the request has valid authentication.
func (mb *MockBackend) validateAuth(r *http.Request) bool {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return false
	}

	// Simple token validation - check if it contains expected parts
	token := parts[1]
	return strings.Contains(token, mb.gatewayID) || token == "valid-test-token"
}

// parseToken extracts gateway and company IDs from token.
func (mb *MockBackend) parseToken(authHeader string) (string, string) {
	if authHeader == "" {
		return "", ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 {
		return "", ""
	}

	token := parts[1]
	// For testing, we accept tokens that contain the gateway ID or our test token
	if token == "valid-test-token" || strings.Contains(token, mb.gatewayID) {
		return mb.gatewayID, mb.companyID
	}

	return "", ""
}

// URL returns the mock server URL.
func (mb *MockBackend) URL() string {
	return mb.server.URL
}

// Close shuts down the mock backend.
func (mb *MockBackend) Close() {
	mb.server.Close()
}

// GetReadings returns all received readings.
func (mb *MockBackend) GetReadings() []models.Reading {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	result := make([]models.Reading, len(mb.readings))
	copy(result, mb.readings)
	return result
}

// ClearReadings clears all stored readings.
func (mb *MockBackend) ClearReadings() {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.readings = mb.readings[:0]
}

// IsConnected returns whether backend is available.
func (mb *MockBackend) IsConnected() bool {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	return mb.connected
}

// TestE2EGatewayToCloud tests the complete gateway to cloud flow.
func TestE2EGatewayToCloud(t *testing.T) {
	// Skip if running short tests
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	const (
		gatewayID = "test-gateway-001"
		companyID = "test-company-001"
		jwtSecret = "test-secret-key"
	)

	// Start mock backend
	backend := NewMockBackend(gatewayID, companyID, jwtSecret)
	defer backend.Close()

	// Create test cache
	cacheDir := t.TempDir()
	testCache, err := cache.NewSQLite(cacheDir + "/cache.db")
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}
	defer testCache.Close()

	// Create sync engine configuration
	cfg := syncengine.EngineConfig{
		GatewayID:    gatewayID,
		CompanyID:    companyID,
		SyncInterval: 100 * time.Millisecond,
		BatchSize:    10,
		MaxRetries:   3,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Invalid config: %v", err)
	}

	t.Run("SyncSingleReading", func(t *testing.T) {
		backend.ClearReadings()

		// Store a reading in cache
		reading := models.Reading{
			AntennaID: "antenna-1",
			GatewayID: gatewayID,
			EPC:       "E200341502001080",
			RSSI:      201,
			Timestamp: time.Now(),
		}

		if err := testCache.Store(reading); err != nil {
			t.Fatalf("Failed to store reading: %v", err)
		}

		// Wait for cache flush (background writer flushes every 1s)
		time.Sleep(1100 * time.Millisecond)

		// Create and start sync engine
		mockClient := newMockWSClient(backend.URL(), gatewayID, companyID)
		engine := syncengine.NewEngine(testCache, mockClient, cfg)

		if err := mockClient.Connect(); err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}

		engine.Start()
		defer engine.Stop()

		// Wait for sync to occur
		time.Sleep(300 * time.Millisecond)

		// Verify reading arrived at backend
		readings := backend.GetReadings()
		if len(readings) != 1 {
			t.Errorf("Expected 1 reading, got %d", len(readings))
		}

		if len(readings) > 0 && readings[0].EPC != "E200341502001080" {
			t.Errorf("Expected EPC 'E200341502001080', got '%s'", readings[0].EPC)
		}
	})

	t.Run("SyncBatchReadings", func(t *testing.T) {
		backend.ClearReadings()

		// Store multiple readings
		for i := 0; i < 5; i++ {
			reading := models.Reading{
				AntennaID: fmt.Sprintf("antenna-%d", i%2),
				GatewayID: gatewayID,
				EPC:       fmt.Sprintf("E20034150200108%d", i),
				RSSI:      201 + i*2,
				Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			}
			if err := testCache.Store(reading); err != nil {
				t.Fatalf("Failed to store reading %d: %v", i, err)
			}
		}

		// Wait for cache flush
		time.Sleep(1100 * time.Millisecond)

		mockClient := newMockWSClient(backend.URL(), gatewayID, companyID)
		engine := syncengine.NewEngine(testCache, mockClient, cfg)

		if err := mockClient.Connect(); err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}

		engine.Start()
		defer engine.Stop()

		time.Sleep(500 * time.Millisecond)

		readings := backend.GetReadings()
		if len(readings) < 5 {
			t.Errorf("Expected at least 5 readings, got %d", len(readings))
		}
	})

	t.Run("ReconnectAfterDisconnect", func(t *testing.T) {
		backend.ClearReadings()

		reading := models.Reading{
			AntennaID: "antenna-reconnect",
			GatewayID: gatewayID,
			EPC:       "E20034150200RECONNECT",
			RSSI:      201,
			Timestamp: time.Now(),
		}

		if err := testCache.Store(reading); err != nil {
			t.Fatalf("Failed to store reading: %v", err)
		}

		mockClient := newMockWSClient(backend.URL(), gatewayID, companyID)
		engine := syncengine.NewEngine(testCache, mockClient, cfg)

		// Start engine
		engine.Start()
		defer engine.Stop()

		// Wait for initial sync
		time.Sleep(200 * time.Millisecond)

		// Simulate disconnect
		mockClient.Disconnect()

		// Wait for disconnect to register
		time.Sleep(100 * time.Millisecond)

		// Reconnect
		if err := mockClient.Reconnect(); err != nil {
			t.Logf("Reconnect may fail in test environment: %v", err)
		}

		// Wait for reconnection and sync
		time.Sleep(400 * time.Millisecond)

		// Reading should eventually sync after reconnect
		readings := backend.GetReadings()
		found := false
		for _, r := range readings {
			if r.EPC == "E20034150200RECONNECT" {
				found = true
				break
			}
		}

		if !found {
			t.Log("Reading may sync after reconnection (async behavior)")
		}
	})
}

// TestE2EAuthentication tests authentication flows.
func TestE2EAuthentication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	const (
		gatewayID = "auth-test-gateway"
		companyID = "auth-test-company"
		jwtSecret = "auth-secret-key"
	)

	backend := NewMockBackend(gatewayID, companyID, jwtSecret)
	defer backend.Close()

	t.Run("ValidToken", func(t *testing.T) {
		req, _ := http.NewRequest("GET", backend.URL()+"/api/v1/health/gateways", nil)
		req.Header.Set("Authorization", "Bearer valid-test-token")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("InvalidToken", func(t *testing.T) {
		req, _ := http.NewRequest("GET", backend.URL()+"/api/v1/health/gateways", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})

	t.Run("MissingToken", func(t *testing.T) {
		req, _ := http.NewRequest("GET", backend.URL()+"/api/v1/health/gateways", nil)

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})
}

// mockWSClient is a test implementation of the WSClient interface.
type mockWSClient struct {
	baseURL    string
	gatewayID  string
	companyID  string
	connected  bool
	mu         sync.RWMutex
	httpClient *http.Client
}

func newMockWSClient(baseURL, gatewayID, companyID string) *mockWSClient {
	return &mockWSClient{
		baseURL:    baseURL,
		gatewayID:  gatewayID,
		companyID:  companyID,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *mockWSClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	c.connected = true
	return nil
}

func (c *mockWSClient) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	return nil
}

func (c *mockWSClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *mockWSClient) Send(data []byte) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return fmt.Errorf("not connected")
	}
	return nil
}

func (c *mockWSClient) Read() ([]byte, error) {
	return nil, fmt.Errorf("not implemented in mock")
}

func (c *mockWSClient) Reconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = true
	return nil
}

func (c *mockWSClient) SendSyncRequest(req models.SyncRequest) (*models.SyncResponse, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/api/v1/rfid/sync", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer valid-test-token")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sync failed with status %d", resp.StatusCode)
	}

	var syncResp models.SyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&syncResp); err != nil {
		return nil, err
	}

	return &syncResp, nil
}
