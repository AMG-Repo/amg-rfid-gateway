package sync

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// mockCache for testing
type mockCache struct {
	readings     []cache.PendingReading
	markSyncedFn func([]int64) error
	mu           sync.Mutex
}

func (m *mockCache) Store(r models.Reading) error {
	return nil
}

func (m *mockCache) GetUnsynced(limit int) ([]cache.PendingReading, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.readings) > limit {
		return m.readings[:limit], nil
	}
	return m.readings, nil
}

func (m *mockCache) Count() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.readings), nil
}

func (m *mockCache) MarkSynced(ids []int64) error {
	if m.markSyncedFn != nil {
		return m.markSyncedFn(ids)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Remove synced readings
	var remaining []cache.PendingReading
	for _, r := range m.readings {
		synced := false
		for _, id := range ids {
			if r.ID == id {
				synced = true
				break
			}
		}
		if !synced {
			remaining = append(remaining, r)
		}
	}
	m.readings = remaining
	return nil
}

// mockWSClient for testing
type mockWSClient struct {
	connected      bool
	reconnectError error
	syncResponse   *models.SyncResponse
	syncError      error
	requestsSent   []models.SyncRequest
	mu             sync.Mutex
}

func (m *mockWSClient) Connect() error {
	m.connected = true
	return nil
}

func (m *mockWSClient) Disconnect() error {
	m.connected = false
	return nil
}

func (m *mockWSClient) IsConnected() bool {
	return m.connected
}

func (m *mockWSClient) Send(data []byte) error {
	return nil
}

func (m *mockWSClient) Read() ([]byte, error) {
	return nil, nil
}

func (m *mockWSClient) Reconnect() error {
	if m.reconnectError != nil {
		return m.reconnectError
	}
	m.connected = true
	return nil
}

func (m *mockWSClient) SendSyncRequest(req models.SyncRequest) (*models.SyncResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestsSent = append(m.requestsSent, req)
	if m.syncError != nil {
		return nil, m.syncError
	}
	return m.syncResponse, nil
}

func TestNewEngine(t *testing.T) {
	cache := &mockCache{}
	client := &mockWSClient{connected: true}
	config := EngineConfig{
		GatewayID:    "gw-001",
		CompanyID:    "comp-123",
		SyncInterval: 30 * time.Second,
		BatchSize:    100,
		MaxRetries:   5,
	}

	engine := NewEngine(cache, client, config)

	if engine == nil {
		t.Fatal("expected engine to be created")
	}
	if engine.config.GatewayID != "gw-001" {
		t.Errorf("expected gateway ID 'gw-001', got '%s'", engine.config.GatewayID)
	}
}

func TestEngine_Start_Stop(t *testing.T) {
	cache := &mockCache{}
	client := &mockWSClient{
		connected: true,
		syncResponse: &models.SyncResponse{
			Success:  true,
			Accepted: 0,
		},
	}
	config := EngineConfig{
		GatewayID:    "gw-001",
		CompanyID:    "comp-123",
		SyncInterval: 100 * time.Millisecond,
		BatchSize:    10,
		MaxRetries:   3,
	}

	engine := NewEngine(cache, client, config)

	// Start engine
	engine.Start()

	// Let it run briefly
	time.Sleep(150 * time.Millisecond)

	// Stop engine
	engine.Stop()

	if engine.IsRunning() {
		t.Error("expected engine to be stopped")
	}
}

func TestEngine_SyncOnce_NoData(t *testing.T) {
	cache := &mockCache{
		readings: []cache.PendingReading{},
	}
	client := &mockWSClient{
		connected: true,
		syncResponse: &models.SyncResponse{
			Success:  true,
			Accepted: 0,
		},
	}
	config := EngineConfig{
		GatewayID:    "gw-001",
		CompanyID:    "comp-123",
		SyncInterval: 30 * time.Second,
		BatchSize:    100,
		MaxRetries:   3,
	}

	engine := NewEngine(cache, client, config)

	err := engine.syncOnce(context.Background())
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestEngine_SyncOnce_WithData(t *testing.T) {
	cache := &mockCache{
		readings: []cache.PendingReading{
			{ID: 1, Reading: models.Reading{EPC: "EPC-1"}},
			{ID: 2, Reading: models.Reading{EPC: "EPC-2"}},
		},
	}
	client := &mockWSClient{
		connected: true,
		syncResponse: &models.SyncResponse{
			Success:  true,
			Accepted: 2,
		},
	}
	config := EngineConfig{
		GatewayID:    "gw-001",
		CompanyID:    "comp-123",
		SyncInterval: 30 * time.Second,
		BatchSize:    100,
		MaxRetries:   3,
	}

	engine := NewEngine(cache, client, config)

	err := engine.syncOnce(context.Background())
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	// Verify readings were marked as synced
	if len(cache.readings) != 0 {
		t.Errorf("expected 0 readings after sync, got %d", len(cache.readings))
	}
}

func TestEngine_SyncOnce_NotConnected(t *testing.T) {
	cache := &mockCache{}
	client := &mockWSClient{
		connected:      false,
		reconnectError: errors.New("reconnect failed"),
	}
	config := EngineConfig{
		GatewayID:    "gw-001",
		CompanyID:    "comp-123",
		SyncInterval: 30 * time.Second,
		BatchSize:    100,
		MaxRetries:   3,
	}

	engine := NewEngine(cache, client, config)

	err := engine.syncOnce(context.Background())
	if err == nil {
		t.Error("expected error when not connected")
	}
}

func TestEngine_SyncOnce_PartialFailure(t *testing.T) {
	cache := &mockCache{
		readings: []cache.PendingReading{
			{ID: 1, Reading: models.Reading{EPC: "EPC-1"}},
			{ID: 2, Reading: models.Reading{EPC: "EPC-2"}},
			{ID: 3, Reading: models.Reading{EPC: "EPC-3"}},
		},
	}
	client := &mockWSClient{
		connected: true,
		syncResponse: &models.SyncResponse{
			Success:    true,
			Accepted:   2,
			Duplicates: 0,
			Errors:     []string{"failed to process EPC-2"},
		},
	}
	config := EngineConfig{
		GatewayID:    "gw-001",
		CompanyID:    "comp-123",
		SyncInterval: 30 * time.Second,
		BatchSize:    100,
		MaxRetries:   3,
	}

	engine := NewEngine(cache, client, config)

	err := engine.syncOnce(context.Background())
	// Partial failure should not return error - we just retry later
	if err != nil {
		t.Errorf("expected no error for partial failure, got: %v", err)
	}
}

func TestEngine_RetryBackoff(t *testing.T) {
	attemptCount := 0
	cache := &mockCache{
		readings: []cache.PendingReading{
			{ID: 1, Reading: models.Reading{EPC: "EPC-1"}},
		},
	}
	client := &mockWSClient{
		connected: true,
		syncError: errors.New("connection failed"),
	}
	config := EngineConfig{
		GatewayID:    "gw-001",
		CompanyID:    "comp-123",
		SyncInterval: 30 * time.Second,
		BatchSize:    100,
		MaxRetries:   3,
	}

	engine := NewEngine(cache, client, config)

	// Manually trigger sync with retry
	for i := 0; i < 3; i++ {
		attemptCount++
		err := engine.syncOnce(context.Background())
		if err == nil {
			t.Error("expected error")
		}
	}

	if attemptCount != 3 {
		t.Errorf("expected 3 attempts, got %d", attemptCount)
	}
}

func TestEngine_GetStats(t *testing.T) {
	cache := &mockCache{}
	client := &mockWSClient{connected: true}
	config := EngineConfig{
		GatewayID:    "gw-001",
		CompanyID:    "comp-123",
		SyncInterval: 30 * time.Second,
		BatchSize:    100,
		MaxRetries:   3,
	}

	engine := NewEngine(cache, client, config)

	stats := engine.GetStats()

	if stats.GatewayID != "gw-001" {
		t.Errorf("expected gateway ID 'gw-001', got '%s'", stats.GatewayID)
	}
}

func TestEngineConfig_Validate(t *testing.T) {
	// Valid config
	config := EngineConfig{
		GatewayID:    "gw-001",
		CompanyID:    "comp-123",
		SyncInterval: 30 * time.Second,
		BatchSize:    100,
		MaxRetries:   3,
	}
	if err := config.Validate(); err != nil {
		t.Errorf("expected valid config, got: %v", err)
	}

	// Invalid - empty gateway ID
	config2 := EngineConfig{
		GatewayID: "",
	}
	if err := config2.Validate(); err == nil {
		t.Error("expected error for empty gateway ID")
	}

	// Invalid - empty company ID
	config3 := EngineConfig{
		GatewayID: "gw-001",
		CompanyID: "",
	}
	if err := config3.Validate(); err == nil {
		t.Error("expected error for empty company ID")
	}
}

// senderOnly is a request/response transport without a connection to manage,
// like the HTTP VPS client.
type senderOnly struct {
	resp *models.SyncResponse
	err  error
	sent []models.SyncRequest
}

func (s *senderOnly) SendSyncRequest(req models.SyncRequest) (*models.SyncResponse, error) {
	s.sent = append(s.sent, req)
	return s.resp, s.err
}

func pendingReadings(n int) []cache.PendingReading {
	var out []cache.PendingReading
	for i := 1; i <= n; i++ {
		out = append(out, cache.PendingReading{ID: int64(i), Reading: models.Reading{EPC: "EPC", GatewayID: "gw-001", Timestamp: time.Now()}})
	}
	return out
}

func TestEngine_SyncOnce_DuplicatesCountAsDelivered(t *testing.T) {
	c := &mockCache{readings: pendingReadings(3)}
	sender := &senderOnly{resp: &models.SyncResponse{Success: true, Accepted: 1, Duplicates: 2}}
	engine := NewEngine(c, sender, EngineConfig{GatewayID: "gw-001", CompanyID: "comp-123", BatchSize: 100})

	if err := engine.syncOnce(context.Background()); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	if n, _ := c.Count(); n != 0 {
		t.Errorf("pending after sync = %d, want 0 (duplicates are already stored server-side)", n)
	}
	if got := engine.GetStats().TotalSynced; got != 3 {
		t.Errorf("TotalSynced = %d, want 3", got)
	}
}

func TestEngine_SyncOnce_ResendOfAllDuplicatesDoesNotLoop(t *testing.T) {
	c := &mockCache{readings: pendingReadings(2)}
	sender := &senderOnly{resp: &models.SyncResponse{Success: true, Accepted: 0, Duplicates: 2}}
	engine := NewEngine(c, sender, EngineConfig{GatewayID: "gw-001", CompanyID: "comp-123", BatchSize: 100})

	if err := engine.syncOnce(context.Background()); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	if n, _ := c.Count(); n != 0 {
		t.Errorf("pending after all-duplicate resend = %d, want 0", n)
	}
}

func TestEngine_SyncOnce_RejectedReadingsAreTerminalByIndex(t *testing.T) {
	c := &mockCache{readings: pendingReadings(3)}
	var marked []int64
	c.markSyncedFn = func(ids []int64) error { marked = append(marked, ids...); return nil }
	sender := &senderOnly{resp: &models.SyncResponse{Success: true, Accepted: 2, Errors: []string{"reading[0]: epc is required"}}}
	engine := NewEngine(c, sender, EngineConfig{GatewayID: "gw-001", CompanyID: "comp-123", BatchSize: 100})

	if err := engine.syncOnce(context.Background()); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	if len(marked) != 3 {
		t.Fatalf("marked = %v, want all 3 ids (2 stored + 1 terminal rejection)", marked)
	}
	stats := engine.GetStats()
	if stats.TotalSynced != 2 || stats.TotalFailed != 1 {
		t.Errorf("TotalSynced=%d TotalFailed=%d, want 2 and 1", stats.TotalSynced, stats.TotalFailed)
	}
}

func TestEngine_SyncOnce_InconsistentCountsKeepReadingsPending(t *testing.T) {
	c := &mockCache{readings: pendingReadings(3)}
	sender := &senderOnly{resp: &models.SyncResponse{Success: true, Accepted: 1}}
	engine := NewEngine(c, sender, EngineConfig{GatewayID: "gw-001", CompanyID: "comp-123", BatchSize: 100})

	if err := engine.syncOnce(context.Background()); err != nil {
		t.Fatalf("syncOnce: %v (unaccounted readings are retried later, not an error)", err)
	}
	if n, _ := c.Count(); n != 3 {
		t.Errorf("pending = %d, want 3 (nothing marked when the response does not account for every reading)", n)
	}
}

func TestEngine_SenderWithoutConnection_ReportsLastSyncOutcome(t *testing.T) {
	c := &mockCache{readings: pendingReadings(1)}
	sender := &senderOnly{err: errors.New("vps down")}
	engine := NewEngine(c, sender, EngineConfig{GatewayID: "gw-001", CompanyID: "comp-123", BatchSize: 100})

	if err := engine.syncOnce(context.Background()); err == nil {
		t.Fatal("syncOnce succeeded; want error")
	}
	if engine.GetStats().IsConnected {
		t.Error("IsConnected = true after a failed request, want false")
	}
	if n, _ := c.Count(); n != 1 {
		t.Errorf("pending after failure = %d, want 1", n)
	}

	sender.err = nil
	sender.resp = &models.SyncResponse{Success: true, Accepted: 1}
	if err := engine.syncOnce(context.Background()); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	if !engine.GetStats().IsConnected {
		t.Error("IsConnected = false after a successful request, want true")
	}
	if len(sender.sent) != 2 || sender.sent[1].GatewayID != "gw-001" || sender.sent[1].CompanyID != "comp-123" {
		t.Errorf("requests = %+v, want 2 with gateway/company ids", sender.sent)
	}
}

func TestEngine_SyncOnce_NegativeCountsKeepReadingsPending(t *testing.T) {
	c := &mockCache{readings: pendingReadings(3)}
	sender := &senderOnly{resp: &models.SyncResponse{Success: true, Accepted: -1, Duplicates: 4}}
	engine := NewEngine(c, sender, EngineConfig{GatewayID: "gw-001", CompanyID: "comp-123", BatchSize: 100})

	if err := engine.syncOnce(context.Background()); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	if n, _ := c.Count(); n != 3 {
		t.Errorf("pending = %d, want 3 (malformed counts must not drain the batch)", n)
	}
}

func TestEngine_SyncOnce_CapsBatchAtServerLimit(t *testing.T) {
	c := &mockCache{readings: pendingReadings(MaxReadingsPerRequest + 20)}
	sender := &senderOnly{resp: &models.SyncResponse{Success: true, Accepted: MaxReadingsPerRequest}}
	engine := NewEngine(c, sender, EngineConfig{GatewayID: "gw-001", CompanyID: "comp-123", BatchSize: 1000})

	if err := engine.syncOnce(context.Background()); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("requests sent = %d, want 1", len(sender.sent))
	}
	if got := len(sender.sent[0].Readings); got != MaxReadingsPerRequest {
		t.Fatalf("sent %d readings, want %d", got, MaxReadingsPerRequest)
	}
	if n, _ := c.Count(); n != 20 {
		t.Errorf("pending = %d, want 20", n)
	}
}

func TestEngine_SenderWithoutConnection_IdleGatewayIsNotOffline(t *testing.T) {
	engine := NewEngine(&mockCache{}, &senderOnly{}, EngineConfig{GatewayID: "gw-001", CompanyID: "comp-123", BatchSize: 100})

	if err := engine.syncOnce(context.Background()); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	if !engine.GetStats().IsConnected {
		t.Error("IsConnected = false for an idle gateway with no failed request, want true")
	}
}
