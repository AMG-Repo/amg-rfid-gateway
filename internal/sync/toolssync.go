package sync

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

// MaxRetries is the retry count that marks a pending confirmation as stuck.
// Only a permanent rejection from the VPS parks a confirmation there; transient
// failures keep retrying with a capped backoff and never reach it.
const MaxRetries = 10

// statusCoder is implemented by VPS client errors that carry the HTTP status.
type statusCoder interface {
	StatusCode() int
}

// isPermanentRejection reports whether the VPS answered with a status that
// resending the same confirmation cannot fix: a 4xx other than 401 (token),
// 408 (timeout) and 429 (rate limit). Network errors and 5xx are transient.
func isPermanentRejection(err error) bool {
	var sc statusCoder
	if !errors.As(err, &sc) {
		return false
	}
	code := sc.StatusCode()
	if code < 400 || code >= 500 {
		return false
	}
	switch code {
	case 401, 408, 429:
		return false
	}
	return true
}

// ConfirmationParker is optionally implemented by stores that can park a
// confirmation the VPS rejected, so it is counted as stuck and not resent.
type ConfirmationParker interface {
	ParkConfirmation(id int64, retryCount int) error
}

// SyncDataResponse represents the response from the gateway sync endpoint
type SyncDataResponse struct {
	Status    string                    `json:"status"`
	Tools     []localstore.SyncDataItem `json:"tools"`
	Timestamp time.Time                 `json:"timestamp"`
}

// VPSClient defines the interface for VPS API operations
type VPSClient interface {
	FetchSyncData(companyID string) (*SyncDataResponse, error)
	FetchUsers(companyID string) ([]localstore.User, error)
	SendConfirmation(companyID, uii, action string) error
	SendConfirmationV2(companyID, uii, action, antennaID string) error
}

// IdempotentConfirmationSender is optionally implemented by VPS clients that
// send a confirmation's idempotency id and confirm instant (issue #146), so the
// VPS dedupes replays of a confirmation it already applied.
type IdempotentConfirmationSender interface {
	SendIdempotentConfirmation(companyID, uii, action, antennaID, confirmationID string, occurredAt time.Time) error
}

// LocalStore defines the interface for local storage operations
type LocalStore interface {
	UpsertToolsFromSync(rows []localstore.SyncDataItem) error
	UpsertToolTags(tags []localstore.ToolTagRecord) error
	UpsertToolsRecords(tools []localstore.ToolRecord) error
	UpsertUsers(users []localstore.User) error
	GetUnsyncedConfirmations(limit int) ([]localstore.PendingConfirmation, error)
	MarkConfirmationSynced(id int64) error
	IncrementRetryCount(id int64) error
}

// RetryableConfirmationLister is optionally implemented by stores that can list
// only pending confirmations still under the retry limit.
type RetryableConfirmationLister interface {
	GetRetryableConfirmations(limit, maxRetries int) ([]localstore.PendingConfirmation, error)
}

// ConfirmationRetryScheduler is optionally implemented by stores that back off
// between send attempts of a failed confirmation (retryInterval doubling per
// failure). Stores without it retry on every flush.
type ConfirmationRetryScheduler interface {
	ScheduleConfirmationRetry(id int64, base time.Duration, now time.Time) error
}

// PendingMovementReapplier is optionally implemented by stores that re-apply
// the location of confirmations not yet sent after a sync rewrote the tags.
type PendingMovementReapplier interface {
	ReapplyPendingMovements() error
}

// KanbanFetcher is implemented by VPS clients that can fetch the kanban catalog.
// It is optional so VPS clients without kanban support keep working unchanged.
type KanbanFetcher interface {
	FetchKanbans(companyID string) ([]localstore.Kanban, error)
}

// ConfirmationPurger is implemented by local stores that delete synced
// confirmations past their retention. It is optional so stores without it keep
// working unchanged.
type ConfirmationPurger interface {
	PurgeSyncedConfirmations(cutoff time.Time) (int64, error)
}

// confirmationPurgeInterval is how often the flush loop purges synced confirmations.
const confirmationPurgeInterval = time.Hour

// KanbanStore is implemented by local stores that cache kanban links and the
// kanban catalog. It is optional so stores without kanban support keep working.
type KanbanStore interface {
	ApplyKanbanLinks(items []localstore.SyncDataItem) error
	ReplaceKanbanCatalog(kanbans []localstore.Kanban) error
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
	syncNow       chan struct{}

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
		syncNow:       make(chan struct{}, 1),
	}
}

// RequestSync asks the sync loop to run a tools/users/kanban sync right away
// instead of waiting for the next interval. It never blocks; requests made
// while one is already pending are coalesced. Without a running loop the
// request is held until the loop starts.
func (t *ToolsSync) RequestSync() {
	select {
	case t.syncNow <- struct{}{}:
	default:
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
		case <-t.syncNow:
			t.performSync()
		}
	}
}

// performSync fetches sync data and users from VPS and updates local store.
// Uses normalized schema: FetchSyncData -> UpsertToolsRecords + UpsertToolTags
func (t *ToolsSync) performSync() {
	// Fetch sync data using new gateway endpoint (normalized schema)
	syncResp, err := t.vpsClient.FetchSyncData(t.companyID)
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

	// Normalize sync data into tools (master) and tool_tags (instances)
	toolRecords := make([]localstore.ToolRecord, 0)
	toolTagRecords := make([]localstore.ToolTagRecord, 0)
	seenToolIDs := make(map[int64]bool)

	for _, item := range syncResp.Tools {
		// Validate SKU is non-empty and non-whitespace
		if strings.TrimSpace(item.SKU) == "" {
			log.Printf("[ToolsSync] Skipping item ID=%d: missing or empty SKU (UII=%s)", item.ID, item.UII)
			continue
		}

		// Only upsert each tool master once
		if !seenToolIDs[item.ToolID] {
			seenToolIDs[item.ToolID] = true
			toolRecords = append(toolRecords, localstore.ToolRecord{
				ID:                 item.ToolID,
				CompanyID:          t.companyID,
				SKU:                item.SKU,
				Name:               item.Name,
				Description:        item.Description,
				DefaultDestination: item.ToolDestination,
			})
		}

		// Create tool tag record for each sync item
		toolTagRecords = append(toolTagRecords, localstore.ToolTagRecord{
			ID:          item.ID,
			ToolID:      item.ToolID,
			UII:         item.UII,
			UnitNumber:  item.UnitNumber,
			Status:      item.Status,
			Location:    item.Location,
			LocationID:  item.LocationID,
			DisplayName: item.DisplayName,
			Notes:       item.Notes,
			Active:      item.Active,
			KanbanZone:  item.KanbanZone,
		})
	}

	// Upsert tool master records
	if len(toolRecords) > 0 {
		if err := t.store.UpsertToolsRecords(toolRecords); err != nil {
			log.Printf("[ToolsSync] Failed to upsert tool records: %v", err)
			return
		}
	}

	// Upsert tool tag records
	if len(toolTagRecords) > 0 {
		if err := t.store.UpsertToolTags(toolTagRecords); err != nil {
			log.Printf("[ToolsSync] Failed to upsert tool tags: %v", err)
			return
		}
	}

	// The gateway knows movements before the VPS: keep the location of
	// confirmations still pending instead of the VPS's older view.
	if r, ok := t.store.(PendingMovementReapplier); ok {
		if err := r.ReapplyPendingMovements(); err != nil {
			log.Printf("[ToolsSync] Failed to re-apply pending movements: %v", err)
		}
	}

	// Mirror kanban links (clears stale ones) now that tags are stored.
	// A failure here must not block users or mark the VPS offline.
	t.applyKanbanLinks(syncResp.Tools, toolTagRecords)

	// Upsert users
	if err := t.store.UpsertUsers(users); err != nil {
		log.Printf("[ToolsSync] Failed to upsert users: %v", err)
		return
	}

	// Refresh the kanban catalog used by the assignment picker (best effort).
	t.refreshKanbanCatalog()

	// Mark VPS as online after successful sync
	t.setVPSOnline(true)

	log.Printf("[ToolsSync] Synced %d tools, %d tool_tags, and %d users", len(toolRecords), len(toolTagRecords), len(users))
}

// applyKanbanLinks mirrors the kanban link of every item in the VPS snapshot,
// including items skipped as tool tags (empty SKU): a link does not need a
// stored tag, and skipping it would lose a legitimate assignment when tags
// swap UIIs. Nothing to do when no tag was stored and the snapshot is empty.
func (t *ToolsSync) applyKanbanLinks(items []localstore.SyncDataItem, stored []localstore.ToolTagRecord) {
	ks, ok := t.store.(KanbanStore)
	if !ok || (len(stored) == 0 && len(items) == 0) {
		return
	}
	linkItems := items

	if err := ks.ApplyKanbanLinks(linkItems); err != nil {
		log.Printf("[ToolsSync] Failed to cache kanban links: %v", err)
	}
}

// refreshKanbanCatalog fetches and caches the kanban catalog. Failures (for
// example a VPS that does not expose the endpoint yet) are logged only.
func (t *ToolsSync) refreshKanbanCatalog() {
	fetcher, ok := t.vpsClient.(KanbanFetcher)
	if !ok {
		return
	}
	ks, ok := t.store.(KanbanStore)
	if !ok {
		return
	}

	kanbans, err := fetcher.FetchKanbans(t.companyID)
	if err != nil {
		log.Printf("[ToolsSync] Failed to fetch kanban catalog: %v", err)
		return
	}
	if err := ks.ReplaceKanbanCatalog(kanbans); err != nil {
		log.Printf("[ToolsSync] Failed to cache kanban catalog: %v", err)
	}
}

// flushPendingConfirmations periodically tries to send pending confirmations to VPS.
func (t *ToolsSync) flushPendingConfirmations() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.retryInterval)
	defer ticker.Stop()

	var lastPurge time.Time
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			if time.Since(lastPurge) >= confirmationPurgeInterval {
				lastPurge = time.Now()
				t.purgeSyncedConfirmations()
			}
			t.performFlush()
		}
	}
}

// purgeSyncedConfirmations deletes synced confirmations older than the
// retention. Failures are logged only; pending confirmations are never touched.
func (t *ToolsSync) purgeSyncedConfirmations() {
	purger, ok := t.store.(ConfirmationPurger)
	if !ok {
		return
	}
	removed, err := purger.PurgeSyncedConfirmations(time.Now().Add(-localstore.ConfirmationRetention))
	if err != nil {
		log.Printf("[ToolsSync] Failed to purge synced confirmations: %v", err)
		return
	}
	if removed > 0 {
		log.Printf("[ToolsSync] Purged %d synced confirmations older than %s", removed, localstore.ConfirmationRetention)
	}
}

// performFlush attempts to send all pending confirmations to VPS.
// Confirmations exceeding MaxRetries remain queued for manual inspection (per spec).
func (t *ToolsSync) performFlush() {
	// Get pending confirmations; stores that can filter out stuck ones do it in
	// the query so they never fill the batch and starve newer confirmations.
	var confirmations []localstore.PendingConfirmation
	var err error
	if lister, ok := t.store.(RetryableConfirmationLister); ok {
		confirmations, err = lister.GetRetryableConfirmations(100, MaxRetries)
	} else {
		confirmations, err = t.store.GetUnsyncedConfirmations(100)
	}
	if err != nil {
		log.Printf("[ToolsSync] Failed to get unsynced confirmations: %v", err)
		return
	}

	if len(confirmations) == 0 {
		return
	}

	log.Printf("[ToolsSync] Flushing %d pending confirmations", len(confirmations))

	vpsReachable := true
	for _, conf := range confirmations {
		// Check if retry count exceeded - per spec, remain queued for manual inspection
		if conf.RetryCount >= MaxRetries {
			log.Printf("[ToolsSync] Confirmation %d exceeded max retries (%d), remaining queued for manual inspection", conf.ID, MaxRetries)
			// Do NOT mark as synced - keep it in queue for manual inspection
			continue
		}

		// Send confirmation with antenna_id using new gateway endpoint
		err := t.sendConfirmation(conf)
		if err != nil && isPermanentRejection(err) {
			// The VPS answered: resending cannot help. Park it for the operator.
			log.Printf("[ToolsSync] VPS rejected confirmation %d, parked until requeued: %v", conf.ID, err)
			if parker, ok := t.store.(ConfirmationParker); ok {
				if err := parker.ParkConfirmation(conf.ID, MaxRetries); err != nil {
					log.Printf("[ToolsSync] Failed to park confirmation %d: %v", conf.ID, err)
				}
			} else if err := t.store.IncrementRetryCount(conf.ID); err != nil {
				log.Printf("[ToolsSync] Failed to increment retry count: %v", err)
			}
		} else if err != nil {
			log.Printf("[ToolsSync] Failed to send confirmation %d: %v", conf.ID, err)
			vpsReachable = false
			// Count the failure for the backoff, but never up to MaxRetries:
			// a transient failure must not make the confirmation stuck.
			if conf.RetryCount+1 < MaxRetries {
				if err := t.store.IncrementRetryCount(conf.ID); err != nil {
					log.Printf("[ToolsSync] Failed to increment retry count: %v", err)
					continue
				}
			}
			if scheduler, ok := t.store.(ConfirmationRetryScheduler); ok {
				// Wait longer after each failure instead of retrying every tick.
				if err := scheduler.ScheduleConfirmationRetry(conf.ID, t.retryInterval, time.Now()); err != nil {
					log.Printf("[ToolsSync] Failed to schedule retry for confirmation %d: %v", conf.ID, err)
				}
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

	// The VPS is online unless a send failed without an answer from it
	t.setVPSOnline(vpsReachable)
}

// sendConfirmation sends a queued confirmation with the id and confirm instant
// it was queued with, identical on every retry. Rows queued before occurred_at
// was recorded omit it (zero), so the VPS stamps the movement as before: their
// timestamp could be out of the VPS range and be rejected on every resend.
func (t *ToolsSync) sendConfirmation(conf localstore.PendingConfirmation) error {
	sender, ok := t.vpsClient.(IdempotentConfirmationSender)
	if !ok {
		return t.vpsClient.SendConfirmationV2(t.companyID, conf.UII, conf.Action, conf.AntennaID)
	}
	return sender.SendIdempotentConfirmation(t.companyID, conf.UII, conf.Action, conf.AntennaID, conf.ConfirmationID, conf.OccurredAt)
}
