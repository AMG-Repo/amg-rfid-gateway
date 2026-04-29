package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/sync"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// mockCache for testing
type mockCache struct {
	count int
}

func (m *mockCache) Count() (int, error) {
	return m.count, nil
}

// mockSyncEngine for testing
type mockSyncEngine struct {
	stats sync.Stats
}

func (m *mockSyncEngine) GetStats() sync.Stats {
	return m.stats
}

func TestNewMonitor(t *testing.T) {
	cache := &mockCache{count: 100}
	syncEngine := &mockSyncEngine{
		stats: sync.Stats{
			GatewayID:   "gw-001",
			IsConnected: true,
		},
	}

	monitor := NewMonitor("gw-001", "comp-123", "v1.0.0", cache, syncEngine)

	if monitor == nil {
		t.Fatal("expected monitor to be created")
	}
	if monitor.gatewayID != "gw-001" {
		t.Errorf("expected gateway ID 'gw-001', got '%s'", monitor.gatewayID)
	}
}

func TestMonitor_GetStatus(t *testing.T) {
	cache := &mockCache{count: 100}
	syncEngine := &mockSyncEngine{
		stats: sync.Stats{
			GatewayID:    "gw-001",
			IsConnected:  true,
			LastSync:     time.Now(),
			TotalSynced:  1000,
			PendingCount: 100,
		},
	}

	monitor := NewMonitor("gw-001", "comp-123", "v1.0.0", cache, syncEngine)

	status := monitor.GetStatus()

	if status.GatewayID != "gw-001" {
		t.Errorf("expected gateway ID 'gw-001', got '%s'", status.GatewayID)
	}
	if status.CompanyID != "comp-123" {
		t.Errorf("expected company ID 'comp-123', got '%s'", status.CompanyID)
	}
	if status.Version != "v1.0.0" {
		t.Errorf("expected version 'v1.0.0', got '%s'", status.Version)
	}
	if status.CacheSize != 100 {
		t.Errorf("expected cache size 100, got %d", status.CacheSize)
	}
}

func TestMonitor_UpdateAntennaStatus(t *testing.T) {
	cache := &mockCache{}
	syncEngine := &mockSyncEngine{}

	monitor := NewMonitor("gw-001", "comp-123", "v1.0.0", cache, syncEngine)

	// Add antenna status
	monitor.UpdateAntennaStatus("ant-1", true)

	status := monitor.GetStatus()
	if len(status.Antennas) != 1 {
		t.Errorf("expected 1 antenna, got %d", len(status.Antennas))
	}

	if status.Antennas[0].ID != "ant-1" {
		t.Errorf("expected antenna ID 'ant-1', got '%s'", status.Antennas[0].ID)
	}

	if !status.Antennas[0].Connected {
		t.Error("expected antenna to be connected")
	}
}

func TestMonitor_UpdateAntennaStatus_Disconnected(t *testing.T) {
	cache := &mockCache{}
	syncEngine := &mockSyncEngine{}

	monitor := NewMonitor("gw-001", "comp-123", "v1.0.0", cache, syncEngine)

	// Add antenna status as disconnected
	monitor.UpdateAntennaStatus("ant-1", false)

	status := monitor.GetStatus()
	if status.Antennas[0].Connected {
		t.Error("expected antenna to be disconnected")
	}
}

func TestMonitor_Start_Stop(t *testing.T) {
	cache := &mockCache{}
	syncEngine := &mockSyncEngine{}

	monitor := NewMonitor("gw-001", "comp-123", "v1.0.0", cache, syncEngine)
	monitor.checkInterval = 100 * time.Millisecond // Speed up for tests

	monitor.Start()

	// Let it run briefly
	time.Sleep(150 * time.Millisecond)

	monitor.Stop()

	if monitor.IsRunning() {
		t.Error("expected monitor to be stopped")
	}
}

func TestHealthHandler(t *testing.T) {
	cache := &mockCache{count: 50}
	syncEngine := &mockSyncEngine{
		stats: sync.Stats{
			GatewayID:    "gw-001",
			IsConnected:  true,
			LastSync:     time.Now(),
			TotalSynced:  500,
			PendingCount: 50,
		},
	}

	monitor := NewMonitor("gw-001", "comp-123", "v1.0.0", cache, syncEngine)

	// Create handler
	handler := HealthHandler(monitor)

	// Create request
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	// Serve
	handler.ServeHTTP(rr, req)

	// Check status
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Parse response
	var status models.GatewayHealthStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if status.GatewayID != "gw-001" {
		t.Errorf("expected gateway ID 'gw-001', got '%s'", status.GatewayID)
	}
	if status.CacheSize != 50 {
		t.Errorf("expected cache size 50, got %d", status.CacheSize)
	}
}

func TestHealthHandler_MethodNotAllowed(t *testing.T) {
	cache := &mockCache{}
	syncEngine := &mockSyncEngine{}

	monitor := NewMonitor("gw-001", "comp-123", "v1.0.0", cache, syncEngine)
	handler := HealthHandler(monitor)

	// POST request should not be allowed
	req := httptest.NewRequest("POST", "/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}

func TestMonitor_GetUptime(t *testing.T) {
	cache := &mockCache{}
	syncEngine := &mockSyncEngine{}

	monitor := NewMonitor("gw-001", "comp-123", "v1.0.0", cache, syncEngine)
	monitor.Start()

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	uptime := monitor.GetUptime()
	if uptime < 50*time.Millisecond {
		t.Errorf("expected uptime >= 50ms, got %v", uptime)
	}

	monitor.Stop()
}

func TestMonitor_StatusString(t *testing.T) {
	tests := []struct {
		connected bool
		pending   int
		expected  string
	}{
		{true, 0, "online"},
		{true, 100, "syncing"},
		{false, 0, "offline"},
	}

	for _, tt := range tests {
		cache := &mockCache{count: tt.pending}
		syncEngine := &mockSyncEngine{
			stats: sync.Stats{
				IsConnected: tt.connected,
			},
		}

		monitor := NewMonitor("gw-001", "comp-123", "v1.0.0", cache, syncEngine)
		status := monitor.GetStatus()

		if status.Status != tt.expected {
			t.Errorf("connected=%v, pending=%d: expected status '%s', got '%s'",
				tt.connected, tt.pending, tt.expected, status.Status)
		}
	}
}
