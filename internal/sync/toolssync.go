package sync

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

// MaxRetries is the maximum number of retry attempts for pending confirmations.
// Confirmations exceeding this limit are abandoned to prevent infinite retries.
const MaxRetries = 10

// VPSClient defines the interface for VPS API operations
type VPSClient interface {
	FetchSyncData(companyID string) ([]localstore.SyncDataItem, error)
	FetchUsers(companyID string) ([]localstore.User, error)
	SendGatewayConfirmation(companyID, uii, action, antennaID string) error
}

// LocalStore defines the interface for local storage operations
type LocalStore interface {
	UpsertToolsFromSync(rows []localstore.SyncDataItem) error
	UpsertUsers(users []localstore.User) error
	GetUnsyncedConfirmations(limit int) ([]localstore.PendingConfirmation, error)
	MarkConfirmationSynced(id int64) error
	IncrementRetryCount(id int64) error
}

// ToolsSync manages synchronization of tools/users with VPS and handles
// pending confirmations queue for offline scenarios.
type ToolsSync struct {
	vpsClient     VPSClient
	store         LocalStore
	companyID     string
	syncInterval  time.Duration
	retryInterval time.Duration
	vpsOnline     bool
	mu            sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewToolsSync creates a new ToolsSync instance.
func NewToolsSync(
	vpsClient VPSClient,
	store LocalStore,
	companyID string,
	syncInterval time.Duration,
	retryInterval time.Duration,
) *ToolsSync {
	return &ToolsSync{
		vpsClient:     vpsClient,
		store:         store,
		companyID:     companyID,
		syncInterval:  syncInterval,
		retryInterval: retryInterval,
		vpsOnline:     false,
	}
}

// Start launches the sync goroutines.
// Returns error if already running.
func (t *ToolsSync) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cancel != nil {
		return nil // Already running
	}

	t.ctx, t.cancel = context.WithCancel(ctx)

	// Start sync loop
	t.wg.Add(1)
	go t.syncToolsUsersLoop()

	// Start flush loop
	t.wg.Add(1)
	go t.flushPendingConfirmations()

	return nil
}

// Stop cancels the context and waits for goroutines to finish.
func (t *ToolsSync) Stop() {
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.mu.Unlock()

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Goroutines stopped
	case <-time.After(5 * time.Second):
		log.Printf("[ToolsSync] Timeout waiting for goroutines to stop")
	}
}

// IsRunning returns true if the sync service is running.
func (t *ToolsSync) IsRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cancel != nil
}

// IsVPSOnline returns true if VPS was last known to be online.
func (t *ToolsSync) IsVPSOnline() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.vpsOnline
}

// setVPSOnline sets the VPS online status thread-safely.
func (t *ToolsSync) setVPSOnline(online bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.vpsOnline = online
}

// syncToolsUsersLoop periodically syncs tools and users from VPS.
func (t *ToolsSync) syncToolsUsersLoop() {
	defer t.wg.Done()

	// Do initial sync immediately
	t.performSync()

	ticker := time.NewTicker(t.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.performSync()
		}
	}
}

// performSync fetches sync data and users from VPS and updates local store.
func (t *ToolsSync) performSync() {
	// Fetch sync data (flattened tools+tags from gateway endpoint)
	syncData, err := t.vpsClient.FetchSyncData(t.companyID)
	if err != nil {
		log.Printf("[ToolsSync] Failed to fetch sync data: %v", err)
		t.setVPSOnline(false)
		return
	}

	// Fetch users
	users, err := t.vpsClient.FetchUsers(t.companyID)
	if err != nil {
		log.Printf("[ToolsSync] Failed to fetch users: %v", err)
		t.setVPSOnline(false)
		return
	}

	// Upsert sync data (normalized tools + tool_tags)
	if err := t.store.UpsertToolsFromSync(syncData); err != nil {
		log.Printf("[ToolsSync] Failed to upsert sync data: %v", err)
		return
	}

	// Upsert users
	if err := t.store.UpsertUsers(users); err != nil {
		log.Printf("[ToolsSync] Failed to upsert users: %v", err)
		return
	}

	// Mark VPS as online after successful sync
	t.setVPSOnline(true)

	log.Printf("[ToolsSync] Synced %d tool tags and %d users", len(syncData), len(users))
}

// flushPendingConfirmations periodically tries to send pending confirmations to VPS.
func (t *ToolsSync) flushPendingConfirmations() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.retryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.performFlush()
		}
	}
}

// performFlush attempts to send all pending confirmations to VPS.
// Confirmations exceeding MaxRetries are abandoned to prevent infinite retries.
func (t *ToolsSync) performFlush() {
	// Get pending confirmations
	confirmations, err := t.store.GetUnsyncedConfirmations(100)
	if err != nil {
		log.Printf("[ToolsSync] Failed to get unsynced confirmations: %v", err)
		return
	}

	if len(confirmations) == 0 {
		return
	}

	log.Printf("[ToolsSync] Flushing %d pending confirmations", len(confirmations))

	allSucceeded := true
	for _, conf := range confirmations {
		// Check if retry count exceeded
		if conf.RetryCount >= MaxRetries {
			log.Printf("[ToolsSync] Confirmation %d exceeded max retries (%d), abandoning", conf.ID, MaxRetries)
			// Mark as synced (abandoned) to prevent further retries
			if err := t.store.MarkConfirmationSynced(conf.ID); err != nil {
				log.Printf("[ToolsSync] Failed to mark abandoned confirmation %d as synced: %v", conf.ID, err)
			}
			continue
		}

		err := t.vpsClient.SendGatewayConfirmation(t.companyID, conf.UII, conf.Action, conf.AntennaID)
		if err != nil {
			log.Printf("[ToolsSync] Failed to send confirmation %d: %v", conf.ID, err)
			allSucceeded = false
			// Increment retry count
			if err := t.store.IncrementRetryCount(conf.ID); err != nil {
				log.Printf("[ToolsSync] Failed to increment retry count: %v", err)
			}
		} else {
			// Mark as synced
			if err := t.store.MarkConfirmationSynced(conf.ID); err != nil {
				log.Printf("[ToolsSync] Failed to mark confirmation %d as synced: %v", conf.ID, err)
			} else {
				log.Printf("[ToolsSync] Confirmation %d synced successfully", conf.ID)
			}
		}
	}

	// Update VPS status based on whether all confirmations succeeded
	if allSucceeded {
		t.setVPSOnline(true)
	} else {
		t.setVPSOnline(false)
	}
}
