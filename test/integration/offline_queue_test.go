package integration

import (
	"context"
	"errors"
	stdsync "sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	syncpkg "github.com/amg-rfid/amg-rfid-gateway/internal/sync"
)

// Integration test for offline queue functionality
// This test verifies:
// 1. When VPS is unreachable, confirmations are saved locally
// 2. When VPS comes back online, pending confirmations are flushed

// mockStoreWithConfirmations tracks all stored confirmations
type mockStoreWithConfirmations struct {
	confirmations []localstore.PendingConfirmation
	mu            stdsync.Mutex
	nextID        int64
}

func (m *mockStoreWithConfirmations) GetToolByUII(uii string) (*localstore.Tool, error) {
	return nil, nil
}

func (m *mockStoreWithConfirmations) GetUserByRFIDTag(rfidTag string) (*localstore.User, error) {
	return nil, nil
}

func (m *mockStoreWithConfirmations) UpsertTools(tools []localstore.Tool) error {
	return nil
}

func (m *mockStoreWithConfirmations) UpsertUsers(users []localstore.User) error {
	return nil
}

func (m *mockStoreWithConfirmations) CreateConfirmation(uii, action, antennaID string, timestamp time.Time, maxPending int) (*localstore.PendingConfirmation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextID++
	conf := localstore.PendingConfirmation{
		ID:         m.nextID,
		UII:        uii,
		Action:     action,
		AntennaID:  antennaID,
		Timestamp:  timestamp,
		Synced:     false,
		RetryCount: 0,
		CreatedAt:  time.Now(),
	}
	m.confirmations = append(m.confirmations, conf)
	return &conf, nil
}

func (m *mockStoreWithConfirmations) GetUnsyncedConfirmations(limit int) ([]localstore.PendingConfirmation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var unsynced []localstore.PendingConfirmation
	for _, conf := range m.confirmations {
		if !conf.Synced {
			unsynced = append(unsynced, conf)
		}
	}
	return unsynced, nil
}

func (m *mockStoreWithConfirmations) MarkConfirmationSynced(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.confirmations {
		if m.confirmations[i].ID == id {
			m.confirmations[i].Synced = true
			break
		}
	}
	return nil
}

func (m *mockStoreWithConfirmations) IncrementRetryCount(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.confirmations {
		if m.confirmations[i].ID == id {
			m.confirmations[i].RetryCount++
			break
		}
	}
	return nil
}

func (m *mockStoreWithConfirmations) GetToolsCount() (int, error) {
	return 0, nil
}

func (m *mockStoreWithConfirmations) GetUsersCount() (int, error) {
	return 0, nil
}

func (m *mockStoreWithConfirmations) GetPendingConfirmationsCount() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, conf := range m.confirmations {
		if !conf.Synced {
			count++
		}
	}
	return count, nil
}

func (m *mockStoreWithConfirmations) GetConfirmationCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.confirmations)
}

func (m *mockStoreWithConfirmations) GetUnsyncedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, conf := range m.confirmations {
		if !conf.Synced {
			count++
		}
	}
	return count
}

// mockVPSWithControl allows controlling VPS availability
type mockVPSWithControl struct {
	online       atomic.Bool
	sentConfirms []string
	mu           stdsync.Mutex
	fetchCalls   atomic.Int32
	confirmCalls atomic.Int32
}

func (m *mockVPSWithControl) FetchTools(companyID string) ([]localstore.Tool, error) {
	m.fetchCalls.Add(1)
	if !m.online.Load() {
		return nil, errors.New("VPS offline")
	}
	return []localstore.Tool{}, nil
}

func (m *mockVPSWithControl) FetchUsers(companyID string) ([]localstore.User, error) {
	if !m.online.Load() {
		return nil, errors.New("VPS offline")
	}
	return []localstore.User{}, nil
}

func (m *mockVPSWithControl) SendConfirmation(companyID, uii, action string) error {
	m.confirmCalls.Add(1)
	if !m.online.Load() {
		return errors.New("VPS offline")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentConfirms = append(m.sentConfirms, uii)
	return nil
}

func (m *mockVPSWithControl) SetOnline(online bool) {
	m.online.Store(online)
}

func (m *mockVPSWithControl) GetSentConfirmations() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.sentConfirms...)
}

func (m *mockVPSWithControl) ClearSentConfirmations() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentConfirms = []string{}
}

// TestOfflineQueue_SaveWhenOffline verifies that confirmations are saved locally when VPS is offline
func TestOfflineQueue_SaveWhenOffline(t *testing.T) {
	vps := &mockVPSWithControl{}
	vps.SetOnline(false) // Start with VPS offline

	store := &mockStoreWithConfirmations{}

	toolsSync := syncpkg.NewToolsSync(
		vps,
		store,
		"company123",
		100*time.Millisecond, // Fast sync for testing
		50*time.Millisecond,  // Fast retry for testing
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start tools sync
	if err := toolsSync.Start(ctx); err != nil {
		t.Fatalf("Failed to start tools sync: %v", err)
	}
	defer toolsSync.Stop()

	// Wait for initial sync attempt (should fail)
	time.Sleep(150 * time.Millisecond)

	// Verify VPS is marked offline
	if toolsSync.IsVPSOnline() {
		t.Error("VPS should be marked as offline")
	}

	// Simulate creating confirmations (normally done via web UI or auto-confirm)
	// We'll add them directly to the store
	now := time.Now()
	store.CreateConfirmation("EPC-001", "entrada", "ANT-01", now, 0)
	store.CreateConfirmation("EPC-002", "salida", "ANT-02", now.Add(1*time.Second), 0)
	store.CreateConfirmation("EPC-003", "entrada", "ANT-01", now.Add(2*time.Second), 0)

	// Wait for flush attempts (should fail since VPS is offline)
	time.Sleep(100 * time.Millisecond)

	// Verify confirmations are still pending (not synced)
	if store.GetUnsyncedCount() != 3 {
		t.Errorf("Expected 3 unsynced confirmations, got %d", store.GetUnsyncedCount())
	}

	// Verify no confirmations were sent
	if len(vps.GetSentConfirmations()) != 0 {
		t.Errorf("Expected 0 sent confirmations, got %d", len(vps.GetSentConfirmations()))
	}

	t.Logf("SUCCESS: %d confirmations queued while VPS was offline", store.GetUnsyncedCount())
}

// TestOfflineQueue_FlushWhenOnline verifies that pending confirmations are flushed when VPS comes back online
func TestOfflineQueue_FlushWhenOnline(t *testing.T) {
	vps := &mockVPSWithControl{}
	vps.SetOnline(false) // Start with VPS offline

	store := &mockStoreWithConfirmations{}

	toolsSync := syncpkg.NewToolsSync(
		vps,
		store,
		"company123",
		100*time.Millisecond, // Fast sync for testing
		50*time.Millisecond,  // Fast retry for testing
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start tools sync
	if err := toolsSync.Start(ctx); err != nil {
		t.Fatalf("Failed to start tools sync: %v", err)
	}
	defer toolsSync.Stop()

	// Wait for initial sync attempt (should fail)
	time.Sleep(150 * time.Millisecond)

	// Verify VPS is offline
	if toolsSync.IsVPSOnline() {
		t.Error("VPS should be marked as offline initially")
	}

	// Create some confirmations while offline
	now := time.Now()
	store.CreateConfirmation("EPC-001", "entrada", "ANT-01", now, 0)
	store.CreateConfirmation("EPC-002", "salida", "ANT-02", now.Add(1*time.Second), 0)
	store.CreateConfirmation("EPC-003", "entrada", "ANT-01", now.Add(2*time.Second), 0)

	// Wait for flush attempts (should fail)
	time.Sleep(100 * time.Millisecond)

	// Now bring VPS online
	vps.SetOnline(true)

	// Wait for flush to succeed
	time.Sleep(150 * time.Millisecond)

	// Verify confirmations were sent
	sent := vps.GetSentConfirmations()
	if len(sent) != 3 {
		t.Errorf("Expected 3 sent confirmations, got %d: %v", len(sent), sent)
	}

	// Verify all confirmations are now synced
	if store.GetUnsyncedCount() != 0 {
		t.Errorf("Expected 0 unsynced confirmations, got %d", store.GetUnsyncedCount())
	}

	// Verify VPS is now marked online
	if !toolsSync.IsVPSOnline() {
		t.Error("VPS should be marked as online after successful flush")
	}

	t.Logf("SUCCESS: All %d confirmations flushed when VPS came back online", len(sent))
}

// TestOfflineQueue_IntermittentConnectivity tests the behavior with intermittent connectivity
func TestOfflineQueue_IntermittentConnectivity(t *testing.T) {
	vps := &mockVPSWithControl{}
	vps.SetOnline(true) // Start online

	store := &mockStoreWithConfirmations{}

	toolsSync := syncpkg.NewToolsSync(
		vps,
		store,
		"company123",
		100*time.Millisecond, // Fast sync for testing
		50*time.Millisecond,  // Fast retry for testing
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start tools sync
	if err := toolsSync.Start(ctx); err != nil {
		t.Fatalf("Failed to start tools sync: %v", err)
	}
	defer toolsSync.Stop()

	// Wait for initial sync
	time.Sleep(150 * time.Millisecond)

	// Create first batch of confirmations (should send immediately since online)
	now := time.Now()
	store.CreateConfirmation("EPC-001", "entrada", "ANT-01", now, 0)

	time.Sleep(100 * time.Millisecond)

	// Verify first confirmation was sent
	sent := vps.GetSentConfirmations()
	if len(sent) != 1 {
		t.Errorf("Expected 1 sent confirmation, got %d", len(sent))
	}

	// Now go offline
	vps.SetOnline(false)
	vps.ClearSentConfirmations()

	// Create more confirmations while offline
	store.CreateConfirmation("EPC-002", "salida", "ANT-02", now.Add(1*time.Second), 0)
	store.CreateConfirmation("EPC-003", "entrada", "ANT-01", now.Add(2*time.Second), 0)

	time.Sleep(100 * time.Millisecond)

	// Verify new confirmations were not sent
	sent = vps.GetSentConfirmations()
	if len(sent) != 0 {
		t.Errorf("Expected 0 sent confirmations while offline, got %d", len(sent))
	}

	// Come back online
	vps.SetOnline(true)

	time.Sleep(150 * time.Millisecond)

	// Verify queued confirmations were sent
	sent = vps.GetSentConfirmations()
	if len(sent) != 2 {
		t.Errorf("Expected 2 sent confirmations after coming back online, got %d: %v", len(sent), sent)
	}

	// Verify all synced
	if store.GetUnsyncedCount() != 0 {
		t.Errorf("Expected 0 unsynced confirmations, got %d", store.GetUnsyncedCount())
	}

	t.Logf("SUCCESS: Intermittent connectivity handled correctly, sent: %v", sent)
}

// TestOfflineQueue_RetryCountIncrements tests that retry count is incremented on failures
func TestOfflineQueue_RetryCountIncrements(t *testing.T) {
	vps := &mockVPSWithControl{}
	vps.SetOnline(false) // Start offline

	store := &mockStoreWithConfirmations{}

	toolsSync := syncpkg.NewToolsSync(
		vps,
		store,
		"company123",
		1*time.Hour,         // Don't sync tools during this test
		50*time.Millisecond, // Fast retry for testing
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start tools sync
	if err := toolsSync.Start(ctx); err != nil {
		t.Fatalf("Failed to start tools sync: %v", err)
	}
	defer toolsSync.Stop()

	// Create a confirmation
	now := time.Now()
	store.CreateConfirmation("EPC-001", "entrada", "ANT-01", now, 0)

	// Wait for multiple retry attempts
	time.Sleep(200 * time.Millisecond)

	// Check that retry count was incremented
	store.mu.Lock()
	retryCount := store.confirmations[0].RetryCount
	store.mu.Unlock()

	if retryCount == 0 {
		t.Error("Expected retry count to be incremented after failed attempts")
	}

	t.Logf("SUCCESS: Retry count was incremented to %d after failed attempts", retryCount)
}
