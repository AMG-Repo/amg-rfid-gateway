package monitoring

import (
	"fmt"
	"net/http"
	"sync"

	syncengine "github.com/amg-rfid/amg-rfid-gateway/internal/sync"
)

// Cache interface for metrics
type Cache interface {
	Count() (int, error)
}

// SyncEngine interface for metrics
type SyncEngine interface {
	GetStats() syncengine.Stats
}

// Metrics collects and exposes Prometheus-style metrics
type Metrics struct {
	cache        Cache
	syncEngine   SyncEngine
	antennaCount int
	uptime       int64
	mu           sync.RWMutex
}

// NewMetrics creates a new metrics collector
func NewMetrics(cache Cache, syncEngine SyncEngine) *Metrics {
	return &Metrics{
		cache:      cache,
		syncEngine: syncEngine,
	}
}

// SetAntennaCount updates the antenna count
func (m *Metrics) SetAntennaCount(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.antennaCount = count
}

// SetUptime updates the uptime in seconds
func (m *Metrics) SetUptime(uptime int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uptime = uptime
}

// Update refreshes metric values from dependencies
func (m *Metrics) Update() {
	// This is a placeholder for future metric updates
	// Currently, values are fetched on-demand in the handler
}

// formatValue formats an integer value for Prometheus output
func (m *Metrics) formatValue(value int) string {
	return fmt.Sprintf("%d", value)
}

// MetricsHandler returns an HTTP handler for Prometheus metrics
func MetricsHandler(metrics *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Get current values
		pendingCount, _ := metrics.cache.Count()
		stats := metrics.syncEngine.GetStats()

		metrics.mu.RLock()
		antennaCount := metrics.antennaCount
		uptime := metrics.uptime
		metrics.mu.RUnlock()

		// Build Prometheus format output
		output := fmt.Sprintf(`# HELP gateway_readings_pending Number of readings pending sync
# TYPE gateway_readings_pending gauge
gateway_readings_pending %d

# HELP gateway_sync_success_total Total number of successful syncs
# TYPE gateway_sync_success_total counter
gateway_sync_success_total %d

# HELP gateway_sync_failure_total Total number of failed syncs
# TYPE gateway_sync_failure_total counter
gateway_sync_failure_total %d

# HELP gateway_uptime_seconds Gateway uptime in seconds
# TYPE gateway_uptime_seconds gauge
gateway_uptime_seconds %d

# HELP gateway_antennas_connected Number of connected antennas
# TYPE gateway_antennas_connected gauge
gateway_antennas_connected %d
`, pendingCount, stats.TotalSynced, stats.TotalFailed, uptime, antennaCount)

		// Write response
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(output))
	})
}
