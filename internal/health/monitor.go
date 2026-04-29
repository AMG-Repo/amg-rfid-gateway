package health

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	syncengine "github.com/amg-rfid/amg-rfid-gateway/internal/sync"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// Cache interface for health monitoring
type Cache interface {
	Count() (int, error)
}

// SyncEngine interface for sync status
type SyncEngine interface {
	GetStats() syncengine.Stats
}

// Monitor tracks health status of the gateway
type Monitor struct {
	gatewayID     string
	companyID     string
	version       string
	cache         Cache
	syncEngine    SyncEngine
	antennas      map[string]models.AntennaStatus
	startTime     time.Time
	running       bool
	stopCh        chan struct{}
	wg            sync.WaitGroup
	mu            sync.RWMutex
	checkInterval time.Duration
}

// NewMonitor creates a new health monitor
func NewMonitor(gatewayID, companyID, version string, cache Cache, syncEngine SyncEngine) *Monitor {
	return &Monitor{
		gatewayID:     gatewayID,
		companyID:     companyID,
		version:       version,
		cache:         cache,
		syncEngine:    syncEngine,
		antennas:      make(map[string]models.AntennaStatus),
		startTime:     time.Now(),
		stopCh:        make(chan struct{}),
		checkInterval: 30 * time.Second,
	}
}

// Start begins health monitoring
func (m *Monitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}

	m.running = true
	m.wg.Add(1)
	go m.run()
}

// Stop halts health monitoring
func (m *Monitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	close(m.stopCh)
	m.wg.Wait()
}

// IsRunning returns true if the monitor is running
func (m *Monitor) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// run is the main monitoring loop
func (m *Monitor) run() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.updateStatus()

		case <-m.stopCh:
			return
		}
	}
}

// updateStatus refreshes internal state
func (m *Monitor) updateStatus() {
	// Update antenna statuses with current time
	m.mu.Lock()
	for id, ant := range m.antennas {
		ant.LastSeen = time.Now()
		m.antennas[id] = ant
	}
	m.mu.Unlock()
}

// UpdateAntennaStatus updates the status of an antenna
func (m *Monitor) UpdateAntennaStatus(antennaID string, connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.antennas[antennaID] = models.AntennaStatus{
		ID:        antennaID,
		Connected: connected,
		LastSeen:  time.Now(),
	}
}

// GetStatus returns the current health status
func (m *Monitor) GetStatus() models.GatewayHealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get cache size
	cacheSize, _ := m.cache.Count()

	// Get sync stats
	syncStats := m.syncEngine.GetStats()

	// Build antenna list
	antennas := make([]models.AntennaStatus, 0, len(m.antennas))
	for _, ant := range m.antennas {
		antennas = append(antennas, ant)
	}

	// Determine status
	status := "online"
	if !syncStats.IsConnected {
		status = "offline"
	} else if cacheSize > 0 {
		status = "syncing"
	}

	return models.GatewayHealthStatus{
		GatewayID: m.gatewayID,
		CompanyID: m.companyID,
		Status:    status,
		Antennas:  antennas,
		CacheSize: cacheSize,
		LastSync:  syncStats.LastSync,
		Uptime:    int64(time.Since(m.startTime).Seconds()),
		Version:   m.version,
	}
}

// GetUptime returns the current uptime
func (m *Monitor) GetUptime() time.Duration {
	return time.Since(m.startTime)
}

// HealthHandler returns an HTTP handler for health checks
func HealthHandler(monitor *Monitor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Get status
		status := monitor.GetStatus()

		// Write response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(status)
	})
}
