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

// Helper function for string pointers
func strPtr(s string) *string {
	return &s
}

// mockVPSClient is a mock implementation for testing
type mockVPSClient struct {
	fetchToolsFunc    func(companyID string) ([]localstore.Tool, error)
	fetchSyncDataFunc func(companyID string) (*SyncDataResponse, error)
	fetchUsersFunc    func(companyID string) ([]localstore.User, error)
	sendConfirmFunc   func(companyID, uii, action string) error
	sendConfirmV2Func func(companyID, uii, action, antennaID string) error
	callCount         atomic.Int32
}

func (m *mockVPSClient) FetchTools(companyID string) ([]localstore.Tool, error) {
	m.callCount.Add(1)
	if m.fetchToolsFunc != nil {
		return m.fetchToolsFunc(companyID)
	}
	return nil, nil
}

func (m *mockVPSClient) FetchSyncData(companyID string) (*SyncDataResponse, error) {
	m.callCount.Add(1)
	if m.fetchSyncDataFunc != nil {
		return m.fetchSyncDataFunc(companyID)
	}
	return &SyncDataResponse{Status: "OK", Tools: []localstore.SyncDataItem{}}, nil
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

func (m *mockVPSClient) SendConfirmationV2(companyID, uii, action, antennaID string) error {
	m.callCount.Add(1)
	if m.sendConfirmV2Func != nil {
		return m.sendConfirmV2Func(companyID, uii, action, antennaID)
	}
	return nil
}

// mockLocalStore is a mock implementation for testing
type mockLocalStore struct {
	upsertToolsFunc        func(rows []localstore.SyncDataItem) error
	toolsUpsertedFunc      func(tools []localstore.ToolRecord) error
	toolTagsUpsertedFunc   func(tags []localstore.ToolTagRecord) error
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

func (m *mockLocalStore) UpsertToolsFromSync(rows []localstore.SyncDataItem) error {
	if m.upsertToolsFunc != nil {
		return m.upsertToolsFunc(rows)
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

func (m *mockLocalStore) UpsertToolTags(tags []localstore.ToolTagRecord) error {
	if m.toolTagsUpsertedFunc != nil {
		return m.toolTagsUpsertedFunc(tags)
	}
	return nil
}

func (m *mockLocalStore) UpsertToolsRecords(tools []localstore.ToolRecord) error {
	if m.toolsUpsertedFunc != nil {
		return m.toolsUpsertedFunc(tools)
	}
	return nil
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

// TestToolsSync_SyncToolsAndUsers tests the tools/users sync loop using FetchSyncData
func TestToolsSync_SyncToolsAndUsers(t *testing.T) {
	syncDataFetched := make(chan bool, 1)
	usersFetched := make(chan bool, 1)
	toolsRecordsUpserted := make(chan bool, 1)
	toolTagsUpserted := make(chan bool, 1)
	usersUpserted := make(chan bool, 1)

	vpsClient := &mockVPSClient{
		fetchSyncDataFunc: func(companyID string) (*SyncDataResponse, error) {
			syncDataFetched <- true
			return &SyncDataResponse{
				Status: "OK",
				Tools: []localstore.SyncDataItem{
					{
						ID:       1,
						ToolID:   1,
						UII:      "E200123456",
						SKU:      "TOOL-001",
						Name:     "Hammer",
						Status:   "active",
						Location: strPtr("Almacen A"),
					},
				},
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
		toolsUpsertedFunc: func(tools []localstore.ToolRecord) error {
			if len(tools) == 1 && tools[0].SKU == "TOOL-001" {
				toolsRecordsUpserted <- true
			}
			return nil
		},
		toolTagsUpsertedFunc: func(tags []localstore.ToolTagRecord) error {
			if len(tags) == 1 && tags[0].UII == "E200123456" {
				toolTagsUpserted <- true
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
	case <-syncDataFetched:
		// Good
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for sync data fetch")
	}

	select {
	case <-usersFetched:
		// Good
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for users fetch")
	}

	select {
	case <-toolsRecordsUpserted:
		// Good
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for tool records upsert")
	}

	select {
	case <-toolTagsUpserted:
		// Good
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for tool tags upsert")
	}

	select {
	case <-usersUpserted:
		// Good
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for users upsert")
	}

	// Small delay to allow setVPSOnline(true) to complete after UpsertUsers returns.
	// This prevents a race condition where the channel send (buffered, non-blocking)
	// returns before performSync() has called setVPSOnline(true).
	time.Sleep(10 * time.Millisecond)

	// Check VPS is marked as online
	if !ts.IsVPSOnline() {
		t.Error("VPS should be marked as online after successful sync")
	}
}

// TestToolsSync_VPSOffline tests behavior when VPS is unreachable
func TestToolsSync_VPSOffline(t *testing.T) {
	vpsClient := &mockVPSClient{
		fetchSyncDataFunc: func(companyID string) (*SyncDataResponse, error) {
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
		sendConfirmV2Func: func(companyID, uii, action, antennaID string) error {
			confirmationsSent <- uii
			return nil
		},
	}

	store := &mockLocalStore{
		confirmations: []localstore.PendingConfirmation{
			{ID: 1, UII: "EPC-001", Action: "entrada", AntennaID: "ant-1"},
			{ID: 2, UII: "EPC-002", Action: "salida", AntennaID: "ant-2"},
		},
		getUnsyncedFunc: func(limit int) ([]localstore.PendingConfirmation, error) {
			return []localstore.PendingConfirmation{
				{ID: 1, UII: "EPC-001", Action: "entrada", AntennaID: "ant-1"},
				{ID: 2, UII: "EPC-002", Action: "salida", AntennaID: "ant-2"},
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
		sendConfirmV2Func: func(companyID, uii, action, antennaID string) error {
			return errors.New("connection refused")
		},
	}

	store := &mockLocalStore{
		confirmations: []localstore.PendingConfirmation{
			{ID: 1, UII: "EPC-001", Action: "entrada", RetryCount: 0, AntennaID: "ant-1"},
		},
		getUnsyncedFunc: func(limit int) ([]localstore.PendingConfirmation, error) {
			return []localstore.PendingConfirmation{
				{ID: 1, UII: "EPC-001", Action: "entrada", RetryCount: 0, AntennaID: "ant-1"},
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
		sendConfirmV2Func: func(companyID, uii, action, antennaID string) error {
			if vpsOnline.Load() {
				confirmationsSent <- uii
				return nil
			}
			return errors.New("connection refused")
		},
		fetchSyncDataFunc: func(companyID string) (*SyncDataResponse, error) {
			if vpsOnline.Load() {
				return &SyncDataResponse{Status: "OK", Tools: []localstore.SyncDataItem{{ID: 1, ToolID: 1, SKU: "TOOL-001"}}}, nil
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
			{ID: 1, UII: "EPC-001", Action: "entrada", AntennaID: "ant-1"},
		},
		getUnsyncedFunc: func(limit int) ([]localstore.PendingConfirmation, error) {
			if vpsOnline.Load() {
				return []localstore.PendingConfirmation{
					{ID: 1, UII: "EPC-001", Action: "entrada", AntennaID: "ant-1"},
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

	// Small delay to allow setVPSOnline(true) to complete after the confirmation is sent.
	// This prevents a race condition where the channel send (buffered, non-blocking)
	// returns before performFlush() has called setVPSOnline(true).
	time.Sleep(10 * time.Millisecond)

	// VPS should be marked as online
	if !ts.IsVPSOnline() {
		t.Error("VPS should be marked as online")
	}
}

// TestToolsSync_MultiplePendingConfirmations tests handling of multiple pending confirmations
func TestToolsSync_MultiplePendingConfirmations(t *testing.T) {
	confirmationsSent := make(chan string, 10)

	vpsClient := &mockVPSClient{
		sendConfirmV2Func: func(companyID, uii, action, antennaID string) error {
			confirmationsSent <- uii
			return nil
		},
	}

	store := &mockLocalStore{
		confirmations: []localstore.PendingConfirmation{
			{ID: 1, UII: "EPC-001", Action: "entrada", AntennaID: "ant-1"},
			{ID: 2, UII: "EPC-002", Action: "salida", AntennaID: "ant-2"},
			{ID: 3, UII: "EPC-003", Action: "entrada", AntennaID: "ant-1"},
			{ID: 4, UII: "EPC-004", Action: "salida", AntennaID: "ant-2"},
			{ID: 5, UII: "EPC-005", Action: "entrada", AntennaID: "ant-1"},
		},
		getUnsyncedFunc: func(limit int) ([]localstore.PendingConfirmation, error) {
			return []localstore.PendingConfirmation{
				{ID: 1, UII: "EPC-001", Action: "entrada", AntennaID: "ant-1"},
				{ID: 2, UII: "EPC-002", Action: "salida", AntennaID: "ant-2"},
				{ID: 3, UII: "EPC-003", Action: "entrada", AntennaID: "ant-1"},
				{ID: 4, UII: "EPC-004", Action: "salida", AntennaID: "ant-2"},
				{ID: 5, UII: "EPC-005", Action: "entrada", AntennaID: "ant-1"},
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

// TestToolsSync_SKUValidation_EmptySKURejected tests that items with empty SKU are rejected
func TestToolsSync_SKUValidation_EmptySKURejected(t *testing.T) {
	toolsRecordsUpserted := make(chan []localstore.ToolRecord, 1)
	toolTagsUpserted := make(chan []localstore.ToolTagRecord, 1)

	vpsClient := &mockVPSClient{
		fetchSyncDataFunc: func(companyID string) (*SyncDataResponse, error) {
			return &SyncDataResponse{
				Status: "OK",
				Tools: []localstore.SyncDataItem{
					{
						ID:       1,
						ToolID:   1,
						UII:      "E200123456",
						SKU:      "", // Empty SKU - should be rejected
						Name:     "Invalid Tool",
						Status:   "active",
					},
				},
			}, nil
		},
		fetchUsersFunc: func(companyID string) ([]localstore.User, error) {
			return []localstore.User{}, nil
		},
	}

	store := &mockLocalStore{
		toolsUpsertedFunc: func(tools []localstore.ToolRecord) error {
			toolsRecordsUpserted <- tools
			return nil
		},
		toolTagsUpsertedFunc: func(tags []localstore.ToolTagRecord) error {
			toolTagsUpserted <- tags
			return nil
		},
	}

	ts := NewToolsSync(vpsClient, store, "company123", 1*time.Hour, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ts.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer ts.Stop()

	// Wait a bit for sync to run
	time.Sleep(100 * time.Millisecond)

	// Verify no tools were upserted (empty SKU rejected)
	select {
	case tools := <-toolsRecordsUpserted:
		if len(tools) > 0 {
			t.Errorf("Expected 0 tool records upserted (empty SKU rejected), got %d", len(tools))
		}
	case <-time.After(200 * time.Millisecond):
		// Expected - no upserts should happen
	}

	select {
	case tags := <-toolTagsUpserted:
		if len(tags) > 0 {
			t.Errorf("Expected 0 tool tags upserted (empty SKU rejected), got %d", len(tags))
		}
	case <-time.After(200 * time.Millisecond):
		// Expected - no upserts should happen
	}
}

// TestToolsSync_SKUValidation_WhitespaceSKURejected tests that whitespace-only SKU is rejected
func TestToolsSync_SKUValidation_WhitespaceSKURejected(t *testing.T) {
	toolsRecordsUpserted := make(chan []localstore.ToolRecord, 1)
	toolTagsUpserted := make(chan []localstore.ToolTagRecord, 1)

	vpsClient := &mockVPSClient{
		fetchSyncDataFunc: func(companyID string) (*SyncDataResponse, error) {
			return &SyncDataResponse{
				Status: "OK",
				Tools: []localstore.SyncDataItem{
					{
						ID:       1,
						ToolID:   1,
						UII:      "E200123456",
						SKU:      "   ", // Whitespace-only SKU - should be rejected
						Name:     "Invalid Tool",
						Status:   "active",
					},
				},
			}, nil
		},
		fetchUsersFunc: func(companyID string) ([]localstore.User, error) {
			return []localstore.User{}, nil
		},
	}

	store := &mockLocalStore{
		toolsUpsertedFunc: func(tools []localstore.ToolRecord) error {
			toolsRecordsUpserted <- tools
			return nil
		},
		toolTagsUpsertedFunc: func(tags []localstore.ToolTagRecord) error {
			toolTagsUpserted <- tags
			return nil
		},
	}

	ts := NewToolsSync(vpsClient, store, "company123", 1*time.Hour, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ts.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer ts.Stop()

	// Wait a bit for sync to run
	time.Sleep(100 * time.Millisecond)

	// Verify no tools were upserted (whitespace-only SKU rejected)
	select {
	case tools := <-toolsRecordsUpserted:
		if len(tools) > 0 {
			t.Errorf("Expected 0 tool records upserted (whitespace SKU rejected), got %d", len(tools))
		}
	case <-time.After(200 * time.Millisecond):
		// Expected - no upserts should happen
	}

	select {
	case tags := <-toolTagsUpserted:
		if len(tags) > 0 {
			t.Errorf("Expected 0 tool tags upserted (whitespace SKU rejected), got %d", len(tags))
		}
	case <-time.After(200 * time.Millisecond):
		// Expected - no upserts should happen
	}
}

// TestToolsSync_SKUValidation_BatchContinuity tests that valid items are processed even when some have invalid SKUs
func TestToolsSync_SKUValidation_BatchContinuity(t *testing.T) {
	toolsRecordsUpserted := make(chan []localstore.ToolRecord, 1)
	toolTagsUpserted := make(chan []localstore.ToolTagRecord, 1)

	vpsClient := &mockVPSClient{
		fetchSyncDataFunc: func(companyID string) (*SyncDataResponse, error) {
			return &SyncDataResponse{
				Status: "OK",
				Tools: []localstore.SyncDataItem{
					{
						ID:       1,
						ToolID:   1,
						UII:      "E200111111",
						SKU:      "TOOL-VALID-001", // Valid SKU
						Name:     "Valid Tool 1",
						Status:   "active",
					},
					{
						ID:       2,
						ToolID:   2,
						UII:      "E200222222",
						SKU:      "", // Empty SKU - should be rejected
						Name:     "Invalid Tool",
						Status:   "active",
					},
					{
						ID:       3,
						ToolID:   3,
						UII:      "E200333333",
						SKU:      "TOOL-VALID-002", // Valid SKU
						Name:     "Valid Tool 2",
						Status:   "active",
					},
				},
			}, nil
		},
		fetchUsersFunc: func(companyID string) ([]localstore.User, error) {
			return []localstore.User{}, nil
		},
	}

	store := &mockLocalStore{
		toolsUpsertedFunc: func(tools []localstore.ToolRecord) error {
			toolsRecordsUpserted <- tools
			return nil
		},
		toolTagsUpsertedFunc: func(tags []localstore.ToolTagRecord) error {
			toolTagsUpserted <- tags
			return nil
		},
	}

	ts := NewToolsSync(vpsClient, store, "company123", 1*time.Hour, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ts.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer ts.Stop()

	// Wait for sync to run
	var upsertedTools []localstore.ToolRecord
	var upsertedTags []localstore.ToolTagRecord

	select {
	case upsertedTools = <-toolsRecordsUpserted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for tool records upsert")
	}

	select {
	case upsertedTags = <-toolTagsUpserted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for tool tags upsert")
	}

	// Should have 2 tools (valid ones), not 3
	if len(upsertedTools) != 2 {
		t.Errorf("Expected 2 tool records upserted (1 invalid SKU rejected), got %d", len(upsertedTools))
	}

	// Should have 2 tool tags (valid ones), not 3
	if len(upsertedTags) != 2 {
		t.Errorf("Expected 2 tool tags upserted (1 invalid SKU rejected), got %d", len(upsertedTags))
	}

	// Verify the valid tools are the ones we expect
	foundSKU1 := false
	foundSKU2 := false
	for _, tool := range upsertedTools {
		if tool.SKU == "TOOL-VALID-001" {
			foundSKU1 = true
		}
		if tool.SKU == "TOOL-VALID-002" {
			foundSKU2 = true
		}
	}

	if !foundSKU1 {
		t.Error("Expected TOOL-VALID-001 to be upserted")
	}
	if !foundSKU2 {
		t.Error("Expected TOOL-VALID-002 to be upserted")
	}

	// Verify the valid tags are the ones we expect
	foundUII1 := false
	foundUII3 := false
	for _, tag := range upsertedTags {
		if tag.UII == "E200111111" {
			foundUII1 = true
		}
		if tag.UII == "E200333333" {
			foundUII3 = true
		}
		// The invalid item (UII: E200222222) should NOT be present
		if tag.UII == "E200222222" {
			t.Error("Invalid item with empty SKU should not be upserted")
		}
	}

	if !foundUII1 {
		t.Error("Expected tag with UII E200111111 to be upserted")
	}
	if !foundUII3 {
		t.Error("Expected tag with UII E200333333 to be upserted")
	}
}

// TestToolsSync_SKUValidation_ValidSKUs tests that various valid SKU formats are accepted
func TestToolsSync_SKUValidation_ValidSKUs(t *testing.T) {
	toolsRecordsUpserted := make(chan []localstore.ToolRecord, 1)
	toolTagsUpserted := make(chan []localstore.ToolTagRecord, 1)

	vpsClient := &mockVPSClient{
		fetchSyncDataFunc: func(companyID string) (*SyncDataResponse, error) {
			return &SyncDataResponse{
				Status: "OK",
				Tools: []localstore.SyncDataItem{
					{
						ID:       1,
						ToolID:   1,
						UII:      "E200111111",
						SKU:      "A", // Single character - valid
						Name:     "Tool A",
						Status:   "active",
					},
					{
						ID:       2,
						ToolID:   2,
						UII:      "E200222222",
						SKU:      "TOOL-WITH-SPACES ", // Trailing space - trimmed, valid
						Name:     "Tool B",
						Status:   "active",
					},
					{
						ID:       3,
						ToolID:   3,
						UII:      "E200333333",
						SKU:      "TOOL-WITH-HYPHENS-123", // Hyphens and numbers - valid
						Name:     "Tool C",
						Status:   "active",
					},
				},
			}, nil
		},
		fetchUsersFunc: func(companyID string) ([]localstore.User, error) {
			return []localstore.User{}, nil
		},
	}

	store := &mockLocalStore{
		toolsUpsertedFunc: func(tools []localstore.ToolRecord) error {
			toolsRecordsUpserted <- tools
			return nil
		},
		toolTagsUpsertedFunc: func(tags []localstore.ToolTagRecord) error {
			toolTagsUpserted <- tags
			return nil
		},
	}

	ts := NewToolsSync(vpsClient, store, "company123", 1*time.Hour, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ts.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer ts.Stop()

	// Wait for sync to run
	var upsertedTools []localstore.ToolRecord
	var upsertedTags []localstore.ToolTagRecord

	select {
	case upsertedTools = <-toolsRecordsUpserted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for tool records upsert")
	}

	select {
	case upsertedTags = <-toolTagsUpserted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for tool tags upsert")
	}

	// All 3 should be processed
	if len(upsertedTools) != 3 {
		t.Errorf("Expected 3 tool records upserted, got %d", len(upsertedTools))
	}

	if len(upsertedTags) != 3 {
		t.Errorf("Expected 3 tool tags upserted, got %d", len(upsertedTags))
	}
}
