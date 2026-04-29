package sync

import (
	"context"
	"errors"
	stdsync "sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

// mockVPSClient is a mock implementation for testing
type mockVPSClient struct {
	fetchToolsFunc  func(companyID string) ([]localstore.Tool, error)
	fetchUsersFunc  func(companyID string) ([]localstore.User, error)
	sendConfirmFunc func(companyID, uii, action string) error
	callCount       atomic.Int32
}

func (m *mockVPSClient) FetchTools(companyID string) ([]localstore.Tool, error) {
	m.callCount.Add(1)
	if m.fetchToolsFunc != nil {
		return m.fetchToolsFunc(companyID)
	}
	return nil, nil
}

func (m *mockVPSClient) FetchUsers(companyID string) ([]localstore.User, error) {
	m.callCount.Add(1)
	if m.fetchUsersFunc != nil {
		return m.fetchUsersFunc(companyID)
	}
	return nil, nil
}

func (m *mockVPSClient) SendConfirmation(companyID, uii, action string) error {
	m.callCount.Add(1)
	if m.sendConfirmFunc != nil {
		return m.sendConfirmFunc(companyID, uii, action)
	}
	return nil
}

// mockLocalStore is a mock implementation for testing
type mockLocalStore struct {
	upsertToolsFunc        func(tools []localstore.Tool) error
	upsertUsersFunc        func(users []localstore.User) error
	getUnsyncedFunc        func(limit int) ([]localstore.PendingConfirmation, error)
	markSyncedFunc         func(id int64) error
	incrementRetryFunc     func(id int64) error
	createConfirmationFunc func(uii, action, antennaID string, timestamp time.Time) (*localstore.PendingConfirmation, error)
	getToolByUIIFunc       func(uii string) (*localstore.Tool, error)
	toolsCount             int
	usersCount             int
	pendingCount           int
	confirmations          []localstore.PendingConfirmation
	mu                     struct {
		stdsync.Mutex
		markSyncedIDs []int64
	}
}

func (m *mockLocalStore) GetToolByUII(uii string) (*localstore.Tool, error) {
	if m.getToolByUIIFunc != nil {
		return m.getToolByUIIFunc(uii)
	}
	return nil, nil
}

func (m *mockLocalStore) GetUserByRFIDTag(rfidTag string) (*localstore.User, error) {
	return nil, nil
}

func (m *mockLocalStore) UpsertTools(tools []localstore.Tool) error {
	if m.upsertToolsFunc != nil {
		return m.upsertToolsFunc(tools)
	}
	return nil
}

func (m *mockLocalStore) UpsertUsers(users []localstore.User) error {
	if m.upsertUsersFunc != nil {
		return m.upsertUsersFunc(users)
	}
	return nil
}

func (m *mockLocalStore) CreateConfirmation(uii, action, antennaID string, timestamp time.Time, maxPending int) (*localstore.PendingConfirmation, error) {
	if m.createConfirmationFunc != nil {
		return m.createConfirmationFunc(uii, action, antennaID, timestamp)
	}
	return &localstore.PendingConfirmation{
		ID:        1,
		UII:       uii,
		Action:    action,
		AntennaID: antennaID,
		Timestamp: timestamp,
		Synced:    false,
	}, nil
}

func (m *mockLocalStore) GetUnsyncedConfirmations(limit int) ([]localstore.PendingConfirmation, error) {
	if m.getUnsyncedFunc != nil {
		return m.getUnsyncedFunc(limit)
	}
	return m.confirmations, nil
}

func (m *mockLocalStore) MarkConfirmationSynced(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mu.markSyncedIDs = append(m.mu.markSyncedIDs, id)
	if m.markSyncedFunc != nil {
		return m.markSyncedFunc(id)
	}
	return nil
}

func (m *mockLocalStore) IncrementRetryCount(id int64) error {
	if m.incrementRetryFunc != nil {
		return m.incrementRetryFunc(id)
	}
	return nil
}

func (m *mockLocalStore) GetToolsCount() (int, error) {
	return m.toolsCount, nil
}

func (m *mockLocalStore) GetUsersCount() (int, error) {
	return m.usersCount, nil
}

func (m *mockLocalStore) GetPendingConfirmationsCount() (int, error) {
	return m.pendingCount, nil
}

func (m *mockLocalStore) GetMarkedSyncedIDs() []int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mu.markSyncedIDs
}

// TestNewToolsSync tests the constructor
func TestNewToolsSync(t *testing.T) {
	vpsClient := &mockVPSClient{}
	store := &mockLocalStore{}

	ts := NewToolsSync(vpsClient, store, "company123", 5*time.Minute, 30*time.Second)

	if ts == nil {
		t.Fatal("NewToolsSync returned nil")
	}

	if ts.vpsClient != vpsClient {
		t.Error("vpsClient not set correctly")
	}

	if ts.store != store {
		t.Error("store not set correctly")
	}

	if ts.companyID != "company123" {
		t.Errorf("companyID = %s, want company123", ts.companyID)
	}

	if ts.syncInterval != 5*time.Minute {
		t.Errorf("syncInterval = %v, want 5m", ts.syncInterval)
	}

	if ts.retryInterval != 30*time.Second {
		t.Errorf("retryInterval = %v, want 30s", ts.retryInterval)
	}

	if ts.vpsOnline {
		t.Error("vpsOnline should be false initially")
	}
}

// TestToolsSync_StartStop tests starting and stopping the sync service
func TestToolsSync_StartStop(t *testing.T) {
	vpsClient := &mockVPSClient{}
	store := &mockLocalStore{}

	ts := NewToolsSync(vpsClient, store, "company123", 100*time.Millisecond, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the service
	err := ts.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let it run for a short time
	time.Sleep(50 * time.Millisecond)

	// Check it's running
	if !ts.IsRunning() {
		t.Error("ToolsSync should be running")
	}

	// Stop the service
	ts.Stop()

	// Wait for stop to complete
	time.Sleep(50 * time.Millisecond)

	// Check it's stopped
	if ts.IsRunning() {
		t.Error("ToolsSync should be stopped")
	}
}

// TestToolsSync_SyncToolsAndUsers tests the tools/users sync loop
func TestToolsSync_SyncToolsAndUsers(t *testing.T) {
	toolsFetched := make(chan bool, 1)
	usersFetched := make(chan bool, 1)
	toolsUpserted := make(chan bool, 1)
	usersUpserted := make(chan bool, 1)

	vpsClient := &mockVPSClient{
		fetchToolsFunc: func(companyID string) ([]localstore.Tool, error) {
			toolsFetched <- true
			return []localstore.Tool{
				{ID: 1, CompanyID: companyID, SKU: "TOOL-001", UII: "E200123456"},
			}, nil
		},
		fetchUsersFunc: func(companyID string) ([]localstore.User, error) {
			usersFetched <- true
			return []localstore.User{
				{ID: 1, CompanyID: companyID, Name: "John", RFIDTag: "ABC123"},
			}, nil
		},
	}

	store := &mockLocalStore{
		upsertToolsFunc: func(tools []localstore.Tool) error {
			if len(tools) == 1 && tools[0].SKU == "TOOL-001" {
				toolsUpserted <- true
			}
			return nil
		},
		upsertUsersFunc: func(users []localstore.User) error {
			if len(users) == 1 && users[0].Name == "John" {
				usersUpserted <- true
			}
			return nil
		},
	}

	ts := NewToolsSync(vpsClient, store, "company123", 50*time.Millisecond, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ts.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer ts.Stop()

	// Wait for first sync iteration
	select {
	case <-toolsFetched:
		// Good
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for tools fetch")
	}

	select {
	case <-usersFetched:
		// Good
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for users fetch")
	}

	select {
	case <-toolsUpserted:
		// Good
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for tools upsert")
	}

	select {
	case <-usersUpserted:
		// Good
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for users upsert")
	}

	// Check VPS is marked as online
	if !ts.IsVPSOnline() {
		t.Error("VPS should be marked as online after successful sync")
	}
}

// TestToolsSync_VPSOffline tests behavior when VPS is unreachable
func TestToolsSync_VPSOffline(t *testing.T) {
	vpsClient := &mockVPSClient{
		fetchToolsFunc: func(companyID string) ([]localstore.Tool, error) {
			return nil, errors.New("connection refused")
		},
		fetchUsersFunc: func(companyID string) ([]localstore.User, error) {
			return nil, errors.New("connection refused")
		},
	}

	store := &mockLocalStore{}

	ts := NewToolsSync(vpsClient, store, "company123", 50*time.Millisecond, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ts.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer ts.Stop()

	// Wait for sync attempt
	time.Sleep(100 * time.Millisecond)

	// VPS should be marked as offline
	if ts.IsVPSOnline() {
		t.Error("VPS should be marked as offline after failed sync")
	}
}

// TestToolsSync_FlushPendingConfirmations tests the flush loop
func TestToolsSync_FlushPendingConfirmations(t *testing.T) {
	confirmationsSent := make(chan string, 2)
	markedSynced := make(chan int64, 2)

	vpsClient := &mockVPSClient{
		sendConfirmFunc: func(companyID, uii, action string) error {
			confirmationsSent <- uii
			return nil
		},
	}

	store := &mockLocalStore{
		confirmations: []localstore.PendingConfirmation{
			{ID: 1, UII: "EPC-001", Action: "entrada"},
			{ID: 2, UII: "EPC-002", Action: "salida"},
		},
		getUnsyncedFunc: func(limit int) ([]localstore.PendingConfirmation, error) {
			return []localstore.PendingConfirmation{
				{ID: 1, UII: "EPC-001", Action: "entrada"},
				{ID: 2, UII: "EPC-002", Action: "salida"},
			}, nil
		},
		markSyncedFunc: func(id int64) error {
			markedSynced <- id
			return nil
		},
	}

	ts := NewToolsSync(vpsClient, store, "company123", 1*time.Hour, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ts.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer ts.Stop()

	// Wait for confirmations to be flushed
	var sentUIIs []string
	for i := 0; i < 2; i++ {
		select {
		case uii := <-confirmationsSent:
			sentUIIs = append(sentUIIs, uii)
		case <-time.After(200 * time.Millisecond):
			t.Fatal("Timeout waiting for confirmation send")
		}
	}

	// Check both confirmations were sent
	if len(sentUIIs) != 2 {
		t.Errorf("Expected 2 confirmations sent, got %d", len(sentUIIs))
	}

	// Wait for both to be marked as synced
	var syncedIDs []int64
	for i := 0; i < 2; i++ {
		select {
		case id := <-markedSynced:
			syncedIDs = append(syncedIDs, id)
		case <-time.After(200 * time.Millisecond):
			t.Fatal("Timeout waiting for mark synced")
		}
	}

	if len(syncedIDs) != 2 {
		t.Errorf("Expected 2 confirmations marked synced, got %d", len(syncedIDs))
	}
}

// TestToolsSync_QueueWhenOffline tests that confirmations are queued when VPS is offline
func TestToolsSync_QueueWhenOffline(t *testing.T) {
	vpsClient := &mockVPSClient{
		sendConfirmFunc: func(companyID, uii, action string) error {
			return errors.New("connection refused")
		},
	}

	store := &mockLocalStore{
		confirmations: []localstore.PendingConfirmation{
			{ID: 1, UII: "EPC-001", Action: "entrada", RetryCount: 0},
		},
		getUnsyncedFunc: func(limit int) ([]localstore.PendingConfirmation, error) {
			return []localstore.PendingConfirmation{
				{ID: 1, UII: "EPC-001", Action: "entrada", RetryCount: 0},
			}, nil
		},
		incrementRetryFunc: func(id int64) error {
			// Retry count should be incremented on failure
			return nil
		},
	}

	ts := NewToolsSync(vpsClient, store, "company123", 1*time.Hour, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ts.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer ts.Stop()

	// Wait for flush attempt
	time.Sleep(100 * time.Millisecond)

	// VPS should be marked as offline after failed confirmation
	if ts.IsVPSOnline() {
		t.Error("VPS should be marked as offline after failed confirmation send")
	}
}

// TestToolsSync_FlushWhenBackOnline tests that pending confirmations are flushed when VPS comes back
func TestToolsSync_FlushWhenBackOnline(t *testing.T) {
	var vpsOnline atomic.Bool
	confirmationsSent := make(chan string, 1)

	vpsClient := &mockVPSClient{
		sendConfirmFunc: func(companyID, uii, action string) error {
			if vpsOnline.Load() {
				confirmationsSent <- uii
				return nil
			}
			return errors.New("connection refused")
		},
		fetchToolsFunc: func(companyID string) ([]localstore.Tool, error) {
			if vpsOnline.Load() {
				return []localstore.Tool{{ID: 1, SKU: "TOOL-001"}}, nil
			}
			return nil, errors.New("connection refused")
		},
		fetchUsersFunc: func(companyID string) ([]localstore.User, error) {
			if vpsOnline.Load() {
				return []localstore.User{{ID: 1, Name: "John"}}, nil
			}
			return nil, errors.New("connection refused")
		},
	}

	store := &mockLocalStore{
		confirmations: []localstore.PendingConfirmation{
			{ID: 1, UII: "EPC-001", Action: "entrada"},
		},
		getUnsyncedFunc: func(limit int) ([]localstore.PendingConfirmation, error) {
			if vpsOnline.Load() {
				return []localstore.PendingConfirmation{
					{ID: 1, UII: "EPC-001", Action: "entrada"},
				}, nil
			}
			return []localstore.PendingConfirmation{}, nil
		},
	}

	ts := NewToolsSync(vpsClient, store, "company123", 50*time.Millisecond, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start with VPS offline
	vpsOnline.Store(false)

	err := ts.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer ts.Stop()

	// Wait for first attempt (VPS offline)
	time.Sleep(100 * time.Millisecond)

	// Now bring VPS online
	vpsOnline.Store(true)

	// Wait for sync to happen and confirmation to be flushed
	select {
	case uii := <-confirmationsSent:
		if uii != "EPC-001" {
			t.Errorf("Expected EPC-001, got %s", uii)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Timeout waiting for confirmation to be flushed after VPS came back online")
	}

	// VPS should be marked as online
	if !ts.IsVPSOnline() {
		t.Error("VPS should be marked as online")
	}
}

// TestToolsSync_MultiplePendingConfirmations tests handling of multiple pending confirmations
func TestToolsSync_MultiplePendingConfirmations(t *testing.T) {
	confirmationsSent := make(chan string, 10)

	vpsClient := &mockVPSClient{
		sendConfirmFunc: func(companyID, uii, action string) error {
			confirmationsSent <- uii
			return nil
		},
	}

	store := &mockLocalStore{
		confirmations: []localstore.PendingConfirmation{
			{ID: 1, UII: "EPC-001", Action: "entrada"},
			{ID: 2, UII: "EPC-002", Action: "salida"},
			{ID: 3, UII: "EPC-003", Action: "entrada"},
			{ID: 4, UII: "EPC-004", Action: "salida"},
			{ID: 5, UII: "EPC-005", Action: "entrada"},
		},
		getUnsyncedFunc: func(limit int) ([]localstore.PendingConfirmation, error) {
			return []localstore.PendingConfirmation{
				{ID: 1, UII: "EPC-001", Action: "entrada"},
				{ID: 2, UII: "EPC-002", Action: "salida"},
				{ID: 3, UII: "EPC-003", Action: "entrada"},
				{ID: 4, UII: "EPC-004", Action: "salida"},
				{ID: 5, UII: "EPC-005", Action: "entrada"},
			}, nil
		},
	}

	ts := NewToolsSync(vpsClient, store, "company123", 1*time.Hour, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ts.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer ts.Stop()

	// Wait for all confirmations to be sent
	var sentUIIs []string
	timeout := time.After(500 * time.Millisecond)
done:
	for len(sentUIIs) < 5 {
		select {
		case uii := <-confirmationsSent:
			sentUIIs = append(sentUIIs, uii)
		case <-timeout:
			break done
		}
	}

	if len(sentUIIs) != 5 {
		t.Errorf("Expected 5 confirmations sent, got %d: %v", len(sentUIIs), sentUIIs)
	}
}
