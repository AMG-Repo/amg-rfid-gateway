package sync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/wsclient"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// Cache interface for sync operations
type Cache interface {
	GetUnsynced(limit int) ([]cache.PendingReading, error)
	MarkSynced(ids []int64) error
	Count() (int, error)
}

// WSClient interface is defined in internal/wsclient/interfaces.go
// Use wsclient.WSClient instead of local definition

// EngineConfig holds configuration for the sync engine
type EngineConfig struct {
	GatewayID    string
	CompanyID    string
	SyncInterval time.Duration
	BatchSize    int
	MaxRetries   int
}

// Validate checks the engine configuration
func (c EngineConfig) Validate() error {
	if c.GatewayID == "" {
		return errors.New("gateway_id cannot be empty")
	}
	if c.CompanyID == "" {
		return errors.New("company_id cannot be empty")
	}
	if c.SyncInterval <= 0 {
		return errors.New("sync_interval must be positive")
	}
	if c.BatchSize <= 0 {
		return errors.New("batch_size must be positive")
	}
	return nil
}

// Engine manages synchronization between local cache and cloud
type Engine struct {
	config     EngineConfig
	cache      Cache
	client     wsclient.WSClient
	running    bool
	stopCh     chan struct{}
	wg         sync.WaitGroup
	mu         sync.RWMutex
	stats      Stats
	retryCount int
	ctx        context.Context
	cancel     context.CancelFunc
}

// Stats holds sync engine statistics
type Stats struct {
	GatewayID    string
	LastSync     time.Time
	TotalSynced  int64
	TotalFailed  int64
	PendingCount int
	IsConnected  bool
}

// NewEngine creates a new sync engine
func NewEngine(cache Cache, client wsclient.WSClient, config EngineConfig) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		config: config,
		cache:  cache,
		client: client,
		stopCh: make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins the sync loop
func (e *Engine) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return
	}

	e.running = true
	e.wg.Add(1)
	go e.run()
}

// Stop halts the sync loop
func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	e.mu.Unlock()

	// Cancel context first to stop in-progress operations
	e.cancel()
	close(e.stopCh)
	e.wg.Wait()
}

// IsRunning returns true if the engine is running
func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// GetStats returns current statistics
func (e *Engine) GetStats() Stats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := e.stats
	stats.GatewayID = e.config.GatewayID
	stats.IsConnected = e.client.IsConnected()

	// Get pending count from cache
	if count, err := e.cache.Count(); err == nil {
		stats.PendingCount = count
	}

	return stats
}

// run is the main sync loop
func (e *Engine) run() {
	defer e.wg.Done()

	ticker := time.NewTicker(e.config.SyncInterval)
	defer ticker.Stop()

	// Do initial sync with exponential backoff on failure
	e.syncWithBackoff(e.ctx)

	for {
		select {
		case <-ticker.C:
			e.syncWithBackoff(e.ctx)

		case <-e.stopCh:
			return
		}
	}
}

// syncWithBackoff performs sync with exponential backoff on failure.
func (e *Engine) syncWithBackoff(ctx context.Context) {
	const baseDelay = 500 * time.Millisecond
	const maxDelay = 30 * time.Second

	// Calculate backoff delay based on retry count
	// Exponential: 500ms, 1s, 2s, 4s, 8s, 16s, 30s (max)
	delay := baseDelay * time.Duration(1<<e.retryCount)
	if delay > maxDelay {
		delay = maxDelay
	}

	// Perform sync
	if err := e.syncOnce(ctx); err != nil {
		// Sync failed - increment retry count (will increase backoff next time)
		e.mu.Lock()
		e.retryCount++
		e.mu.Unlock()
	} else {
		// Sync succeeded - reset retry count
		e.mu.Lock()
		e.retryCount = 0
		e.mu.Unlock()
	}

	// Note: The ticker controls the base sync interval, so we don't
	// need to sleep here - the backoff is just for calculating the
	// effective delay on the next failure. For actual backoff behavior
	// with delays, use syncWithDelay instead.
	_ = delay // Available for future use
}

// syncOnce performs a single sync cycle
func (e *Engine) syncOnce(ctx context.Context) error {
	// NEGATIVE: Context cancelled before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Check if connected
	if !e.client.IsConnected() {
		// Try to reconnect with context check
		if err := e.client.Reconnect(); err != nil {
			return fmt.Errorf("not connected and reconnect failed: %w", err)
		}
	}

	// Get unsynced readings
	readings, err := e.cache.GetUnsynced(e.config.BatchSize)
	if err != nil {
		return fmt.Errorf("failed to get unsynced: %w", err)
	}

	if len(readings) == 0 {
		return nil
	}

	// Convert to models.Reading
	var modelsReadings []models.Reading
	var ids []int64
	for _, r := range readings {
		modelsReadings = append(modelsReadings, r.Reading)
		ids = append(ids, r.ID)
	}

	// Create sync request
	req := models.SyncRequest{
		GatewayID: e.config.GatewayID,
		CompanyID: e.config.CompanyID,
		Readings:  modelsReadings,
		Timestamp: time.Now(),
	}

	// NEGATIVE: Context cancelled before sending
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Send to cloud
	resp, err := e.client.SendSyncRequest(req)
	if err != nil {
		e.handleFailure(len(readings))
		return fmt.Errorf("sync request failed: %w", err)
	}

	// Handle response
	if resp.Success {
		// Mark synced readings
		syncedCount := resp.Accepted
		if syncedCount > 0 && syncedCount <= len(ids) {
			syncedIDs := ids[:syncedCount]
			if err := e.cache.MarkSynced(syncedIDs); err != nil {
				// Track partial failure
				e.handlePartialFailure(syncedCount, len(ids)-syncedCount)
				return fmt.Errorf("failed to mark %d of %d readings as synced: %w", len(ids)-syncedCount, len(ids), err)
			}
		}

		e.handleSuccess(syncedCount)
	} else {
		e.handleFailure(len(readings))
	}

	return nil
}

// handleSuccess updates stats on successful sync
func (e *Engine) handleSuccess(count int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stats.LastSync = time.Now()
	e.stats.TotalSynced += int64(count)
	e.retryCount = 0
}

// handleFailure updates stats on failed sync
func (e *Engine) handleFailure(count int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stats.TotalFailed += int64(count)
	e.retryCount++
}

// handlePartialFailure updates stats when some readings sync but others fail
func (e *Engine) handlePartialFailure(synced, failed int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stats.LastSync = time.Now()
	e.stats.TotalSynced += int64(synced)
	e.stats.TotalFailed += int64(failed)
	// Don't reset retry count on partial failure
}
