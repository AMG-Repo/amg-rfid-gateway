package monitoring

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	syncengine "github.com/amg-rfid/amg-rfid-gateway/internal/sync"
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
	stats syncengine.Stats
}

func (m *mockSyncEngine) GetStats() syncengine.Stats {
	return m.stats
}

func TestNewMetrics(t *testing.T) {
	cache := &mockCache{count: 100}
	syncEngine := &mockSyncEngine{}

	metrics := NewMetrics(cache, syncEngine)

	if metrics == nil {
		t.Fatal("expected metrics to be created")
	}
}

func TestMetricsHandler(t *testing.T) {
	cache := &mockCache{count: 100}
	syncEngine := &mockSyncEngine{
		stats: syncengine.Stats{
			TotalSynced: 1000,
			TotalFailed: 50,
			IsConnected: true,
		},
	}

	metrics := NewMetrics(cache, syncEngine)
	handler := MetricsHandler(metrics)

	// Create request
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()

	// Serve
	handler.ServeHTTP(rr, req)

	// Check status
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Check content type
	contentType := rr.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("expected content type 'text/plain', got '%s'", contentType)
	}

	// Check body contains expected metrics
	body := rr.Body.String()
	expectedMetrics := []string{
		"gateway_readings_pending",
		"gateway_sync_success_total",
		"gateway_sync_failure_total",
		"gateway_uptime_seconds",
		"gateway_antennas_connected",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(body, metric) {
			t.Errorf("expected body to contain '%s'", metric)
		}
	}
}

func TestMetricsHandler_MethodNotAllowed(t *testing.T) {
	cache := &mockCache{}
	syncEngine := &mockSyncEngine{}

	metrics := NewMetrics(cache, syncEngine)
	handler := MetricsHandler(metrics)

	// POST request should not be allowed
	req := httptest.NewRequest("POST", "/metrics", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}

func TestMetrics_Update(t *testing.T) {
	cache := &mockCache{count: 200}
	syncEngine := &mockSyncEngine{
		stats: syncengine.Stats{
			TotalSynced: 5000,
			TotalFailed: 100,
			IsConnected: true,
		},
	}

	metrics := NewMetrics(cache, syncEngine)

	// Update should not panic
	metrics.Update()
}

func TestMetrics_FormatValue(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		expected string
	}{
		{"zero", 0, "0"},
		{"positive", 100, "100"},
		{"negative", -50, "-50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &mockCache{}
			syncEngine := &mockSyncEngine{}
			metrics := NewMetrics(cache, syncEngine)

			result := metrics.formatValue(tt.value)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestMetrics_WithAntennaCount(t *testing.T) {
	cache := &mockCache{count: 50}
	syncEngine := &mockSyncEngine{}

	metrics := NewMetrics(cache, syncEngine)
	metrics.SetAntennaCount(4)

	handler := MetricsHandler(metrics)
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "gateway_antennas_connected 4") {
		t.Errorf("expected body to contain 'gateway_antennas_connected 4', got:\n%s", body)
	}
}

func TestMetrics_WithUptime(t *testing.T) {
	cache := &mockCache{count: 50}
	syncEngine := &mockSyncEngine{}

	metrics := NewMetrics(cache, syncEngine)
	metrics.SetUptime(3600)

	handler := MetricsHandler(metrics)
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "gateway_uptime_seconds 3600") {
		t.Errorf("expected body to contain 'gateway_uptime_seconds 3600', got:\n%s", body)
	}
}

func TestMetrics_CacheInterface(t *testing.T) {
	// Test that our Cache interface is compatible
	var _ Cache = (*mockCache)(nil)
}

func TestMetrics_SyncEngineInterface(t *testing.T) {
	// Test that our SyncEngine interface is compatible
	var _ SyncEngine = (*mockSyncEngine)(nil)
}
