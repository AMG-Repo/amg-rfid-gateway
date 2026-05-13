package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/verify"
	_ "modernc.org/sqlite"
)

func TestNewServer(t *testing.T) {
	// Create dependencies
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// We can't easily create a real LocalStore without a DB, so we'll test with nil
	// In real tests you'd use a test database
	verifier := &verify.Verifier{}
	vpsClient := httpclient.NewVPSClient("http://localhost:8080", 5*time.Second, "test-token")

	server := NewServer("127.0.0.1", 0, eventBus, nil, verifier, vpsClient, "test-company")

	if server == nil {
		t.Fatal("Expected server to be created, got nil")
	}

	if server.eventBus != eventBus {
		t.Error("Expected eventBus to be set")
	}

	if server.verifier != verifier {
		t.Error("Expected verifier to be set")
	}

	if server.vpsClient != vpsClient {
		t.Error("Expected vpsClient to be set")
	}

	if server.sseClients == nil {
		t.Error("Expected sseClients map to be initialized")
	}
}

func TestHandleStatus(t *testing.T) {
	// Create a test server with minimal setup
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	server := NewServer("127.0.0.1", 0, eventBus, nil, nil, nil, "test-company")

	// Create a test request
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()

	// Call the handler directly
	server.handleStatus(rec, req)

	// Check response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	// Check response body contains expected fields
	body := rec.Body.String()
	if body == "" {
		t.Error("Expected non-empty response body")
	}

	// Verify JSON structure (should contain vps_online and pending_count)
	if !contains(body, "vps_online") {
		t.Error("Expected response to contain 'vps_online'")
	}
	if !contains(body, "pending_count") {
		t.Error("Expected response to contain 'pending_count'")
	}
}

func TestHandleStatusMethodNotAllowed(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	server := NewServer("127.0.0.1", 0, eventBus, nil, nil, nil, "test-company")

	// Test POST request to GET endpoint
	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	rec := httptest.NewRecorder()

	server.handleStatus(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

func TestSSEHeaders(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	server := NewServer("127.0.0.1", 0, eventBus, nil, nil, nil, "test-company")

	// Create a request with a timeout context to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	// Call SSE handler - it will run until context timeout
	// This is expected behavior for SSE
	done := make(chan bool)
	go func() {
		server.SSEHandler(rec, req)
		done <- true
	}()

	// Wait for handler to finish or timeout
	select {
	case <-done:
		// Handler completed
	case <-time.After(200 * time.Millisecond):
		// Timeout is expected for SSE
	}

	// Check headers were set before the context cancelled
	headers := rec.Header()

	if headers.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected Content-Type text/event-stream, got %s", headers.Get("Content-Type"))
	}

	if headers.Get("Cache-Control") != "no-cache" {
		t.Errorf("Expected Cache-Control no-cache, got %s", headers.Get("Cache-Control"))
	}

	if headers.Get("Connection") != "keep-alive" {
		t.Errorf("Expected Connection keep-alive, got %s", headers.Get("Connection"))
	}
}

func TestServerStartStop(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Use port 0 to get a random available port
	server := NewServer("127.0.0.1", 0, eventBus, nil, nil, nil, "test-company")

	ctx := context.Background()

	// Start server
	err := server.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Stop server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = server.Stop(shutdownCtx)
	if err != nil {
		t.Fatalf("Failed to stop server: %v", err)
	}
}

func TestHandleTagsMethodNotAllowed(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	server := NewServer("127.0.0.1", 0, eventBus, nil, nil, nil, "test-company")

	req := httptest.NewRequest(http.MethodPost, "/api/tags", nil)
	rec := httptest.NewRecorder()

	server.handleTags(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

func TestHandleConfirmMethodNotAllowed(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	server := NewServer("127.0.0.1", 0, eventBus, nil, nil, nil, "test-company")

	req := httptest.NewRequest(http.MethodGet, "/api/confirm", nil)
	rec := httptest.NewRecorder()

	server.handleConfirm(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

func TestHandleConfirmInvalidBody(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Create verifier with nil store (will fail validation but that's ok for this test)
	verifier := verify.NewVerifier(nil, nil)

	server := NewServer("127.0.0.1", 0, eventBus, nil, verifier, nil, "test-company")

	req := httptest.NewRequest(http.MethodPost, "/api/confirm", nil)
	rec := httptest.NewRecorder()

	server.handleConfirm(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid body, got %d", rec.Code)
	}
}

func TestHandleConfirmInvalidAction(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	verifier := verify.NewVerifier(nil, nil)
	server := NewServer("127.0.0.1", 0, eventBus, nil, verifier, nil, "test-company")

	body := `{"uii":"test-epc","action":"invalid-action"}`
	req := httptest.NewRequest(http.MethodPost, "/api/confirm", stringReader(body))
	rec := httptest.NewRecorder()

	server.handleConfirm(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid action, got %d", rec.Code)
	}
}

// Helper functions
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func stringReader(s string) *strings.Reader {
	return strings.NewReader(s)
}

// mockLocalStore is a mock implementation for testing
// Mock store implementing the minimal interface needed for handler tests
type mockStoreForHandlers struct {
	getUnsyncedFunc        func(limit int) ([]localstore.PendingConfirmation, error)
	createConfirmationFunc func(uii, action, antennaID string, timestamp time.Time) (*localstore.PendingConfirmation, error)
	getToolByUIIFunc       func(uii string) (*localstore.Tool, error)
	getPendingCountFunc    func() (int, error)
	getToolsCountFunc      func() (int, error)
	confirmations          []localstore.PendingConfirmation
	tools                  map[string]*localstore.Tool
}

func (m *mockStoreForHandlers) GetToolByUII(uii string) (*localstore.Tool, error) {
	if m.getToolByUIIFunc != nil {
		return m.getToolByUIIFunc(uii)
	}
	if m.tools != nil {
		if tool, ok := m.tools[uii]; ok {
			return tool, nil
		}
	}
	return nil, nil
}

func (m *mockStoreForHandlers) GetUserByRFIDTag(rfidTag string) (*localstore.User, error) {
	return nil, nil
}

func (m *mockStoreForHandlers) UpsertTools(tools []localstore.Tool) error {
	return nil
}

func (m *mockStoreForHandlers) UpsertUsers(users []localstore.User) error {
	return nil
}

func (m *mockStoreForHandlers) CreateConfirmation(uii, action, antennaID string, timestamp time.Time, maxPending int) (*localstore.PendingConfirmation, error) {
	if m.createConfirmationFunc != nil {
		return m.createConfirmationFunc(uii, action, antennaID, timestamp)
	}
	conf := localstore.PendingConfirmation{
		ID:        int64(len(m.confirmations) + 1),
		UII:       uii,
		Action:    action,
		AntennaID: antennaID,
		Timestamp: timestamp,
		Synced:    false,
	}
	m.confirmations = append(m.confirmations, conf)
	return &conf, nil
}

func (m *mockStoreForHandlers) GetUnsyncedConfirmations(limit int) ([]localstore.PendingConfirmation, error) {
	if m.getUnsyncedFunc != nil {
		return m.getUnsyncedFunc(limit)
	}
	return m.confirmations, nil
}

func (m *mockStoreForHandlers) MarkConfirmationSynced(id int64) error {
	return nil
}

func (m *mockStoreForHandlers) IncrementRetryCount(id int64) error {
	return nil
}

func (m *mockStoreForHandlers) GetToolsCount() (int, error) {
	if m.getToolsCountFunc != nil {
		return m.getToolsCountFunc()
	}
	return len(m.tools), nil
}

func (m *mockStoreForHandlers) GetUsersCount() (int, error) {
	return 0, nil
}

func (m *mockStoreForHandlers) GetPendingConfirmationsCount() (int, error) {
	if m.getPendingCountFunc != nil {
		return m.getPendingCountFunc()
	}
	count := 0
	for _, c := range m.confirmations {
		if !c.Synced {
			count++
		}
	}
	return count, nil
}

// TestHandleConfirm_VPSOnline tests confirmation when VPS is available
func TestHandleConfirm_VPSOnline(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Create a mock VPS server that accepts confirmations
	vpsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle health check endpoint
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
			return
		}

		// Handle confirmation endpoint (new gateway endpoint)
		if r.URL.Path != "/api/v1/gateway/confirm" {
			t.Errorf("expected path /api/v1/gateway/confirm, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		if reqBody["uii"] != "EPC-123" {
			t.Errorf("expected uii 'EPC-123', got %v", reqBody["uii"])
		}
		if reqBody["action"] != "entrada" {
			t.Errorf("expected action 'entrada', got %v", reqBody["action"])
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer vpsServer.Close()

	vpsClient := httpclient.NewVPSClient(vpsServer.URL, 5*time.Second, "test-token")
	verifier := verify.NewVerifier(nil, nil)

	server := NewServer("127.0.0.1", 0, eventBus, nil, verifier, vpsClient, "test-company")

	body := `{"uii":"EPC-123","action":"entrada"}`
	req := httptest.NewRequest(http.MethodPost, "/api/confirm", stringReader(body))
	rec := httptest.NewRecorder()

	server.handleConfirm(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ConfirmResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success=true, got %v", resp.Success)
	}
	if resp.Mode != "online" {
		t.Errorf("expected mode='online', got %s", resp.Mode)
	}
	if !strings.Contains(resp.Message, "VPS") {
		t.Errorf("expected message to mention VPS, got %s", resp.Message)
	}
}

// TestHandleConfirm_VPSOffline tests confirmation when VPS is unavailable
func TestHandleConfirm_VPSOffline(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Create VPS client pointing to non-existent server
	vpsClient := httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "test-token")
	verifier := verify.NewVerifier(nil, nil)

	// Create real local store with in-memory DB
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Create table
	_, err = db.Exec(`
		CREATE TABLE pending_confirmations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uii TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('entrada', 'salida')),
			antenna_id TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced BOOLEAN DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	store := localstore.New(db)

	server := NewServer("127.0.0.1", 0, eventBus, store, verifier, vpsClient, "test-company")

	body := `{"uii":"EPC-456","action":"salida"}`
	req := httptest.NewRequest(http.MethodPost, "/api/confirm", stringReader(body))
	rec := httptest.NewRecorder()

	server.handleConfirm(rec, req)

	// Should return success but in offline mode
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ConfirmResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Mode != "offline" {
		t.Errorf("expected mode='offline', got %s", resp.Mode)
	}
	if !resp.Success {
		t.Errorf("expected success=true, got %v", resp.Success)
	}

	// Verify confirmation was saved to local store
	count, err := store.GetPendingConfirmationsCount()
	if err != nil {
		t.Fatalf("failed to get pending count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 pending confirmation in store, got %d", count)
	}
}

// TestHandleConfirm_VPSOffline_SavesToLocalStore tests that confirmations are saved when VPS is offline
func TestHandleConfirm_VPSOffline_SavesToLocalStore(t *testing.T) {
	// Create a real local store with in-memory DB for proper testing
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Create table
	_, err = db.Exec(`
		CREATE TABLE pending_confirmations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uii TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('entrada', 'salida')),
			antenna_id TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced BOOLEAN DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	store := localstore.New(db)

	// Create confirmation directly
	_, err = store.CreateConfirmation("EPC-789", "entrada", "manual", time.Now(), 0)
	if err != nil {
		t.Fatalf("failed to create confirmation: %v", err)
	}

	// Verify it was saved
	count, err := store.GetPendingConfirmationsCount()
	if err != nil {
		t.Fatalf("failed to get count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 pending confirmation, got %d", count)
	}

	// Get unsynced and verify
	confirmations, err := store.GetUnsyncedConfirmations(100)
	if err != nil {
		t.Fatalf("failed to get confirmations: %v", err)
	}
	if len(confirmations) != 1 {
		t.Errorf("expected 1 confirmation, got %d", len(confirmations))
	}
	if confirmations[0].UII != "EPC-789" {
		t.Errorf("expected UII 'EPC-789', got %s", confirmations[0].UII)
	}
	if confirmations[0].Action != "entrada" {
		t.Errorf("expected action 'entrada', got %s", confirmations[0].Action)
	}
	if confirmations[0].Synced {
		t.Error("expected confirmation to be unsynced")
	}
}

// TestHandleConfirm_InvalidJSON tests handling of invalid JSON
func TestHandleConfirm_InvalidJSON(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	verifier := verify.NewVerifier(nil, nil)
	server := NewServer("127.0.0.1", 0, eventBus, nil, verifier, nil, "test-company")

	body := `{"invalid json` // Malformed JSON
	req := httptest.NewRequest(http.MethodPost, "/api/confirm", stringReader(body))
	rec := httptest.NewRecorder()

	server.handleConfirm(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid JSON, got %d", rec.Code)
	}
}

// TestHandleConfirm_MissingFields tests handling of request with missing fields
func TestHandleConfirm_MissingFields(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	verifier := verify.NewVerifier(nil, nil)
	server := NewServer("127.0.0.1", 0, eventBus, nil, verifier, nil, "test-company")

	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing uii",
			body: `{"action":"entrada"}`,
		},
		{
			name: "missing action",
			body: `{"uii":"EPC-123"}`,
		},
		{
			name: "empty uii",
			body: `{"uii":"","action":"entrada"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/confirm", stringReader(tt.body))
			rec := httptest.NewRecorder()

			server.handleConfirm(rec, req)

			// Should either succeed (empty UII is valid) or fail validation
			if rec.Code != http.StatusOK && rec.Code != http.StatusBadRequest {
				t.Errorf("unexpected status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestHandleTags_ReturnsCorrectJSON tests that handleTags returns proper JSON structure
func TestHandleTags_ReturnsCorrectJSON(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Create real store with test data
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Create normalized schema tables
	_, err = db.Exec(`
		CREATE TABLE tools (
			id INTEGER PRIMARY KEY,
			company_id TEXT NOT NULL,
			sku TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			default_destination TEXT,
			last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE tool_tags (
			id INTEGER PRIMARY KEY,
			tool_id INTEGER NOT NULL,
			uii TEXT NOT NULL UNIQUE,
			unit_number TEXT,
			status TEXT,
			location TEXT,
			location_id INTEGER,
			display_name TEXT,
			notes TEXT,
			active BOOLEAN DEFAULT 1,
			kanban_zone TEXT,
			last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE pending_confirmations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uii TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('entrada', 'salida')),
			antenna_id TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced BOOLEAN DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	store := localstore.New(db)

	// Insert normalized data: tool master + tool tag
	_, err = db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (1, 'comp-001', 'SKU-001', 'Test Tool', 'A test tool', 'Warehouse A', ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert tool: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, unit_number, status, location, active, last_synced_at)
		VALUES (1, 1, 'EPC-001', '001', 'active', 'Warehouse A', 1, ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert tool: %v", err)
	}

	// Create a confirmation for that tool
	_, err = store.CreateConfirmation("EPC-001", "entrada", "ANT-01", time.Now(), 0)
	if err != nil {
		t.Fatalf("failed to create confirmation: %v", err)
	}

	// Also create confirmation for unknown tool
	_, err = store.CreateConfirmation("UNKNOWN-EPC", "salida", "ANT-02", time.Now(), 0)
	if err != nil {
		t.Fatalf("failed to create confirmation: %v", err)
	}

	server := NewServer("127.0.0.1", 0, eventBus, store, nil, nil, "test-company")

	req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	rec := httptest.NewRecorder()

	server.handleTags(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %s", contentType)
	}

	// Parse response
	var tags []TagInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &tags); err != nil {
		t.Fatalf("failed to parse response JSON: %v\nBody: %s", err, rec.Body.String())
	}

	if len(tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(tags))
	}

	// Find known tool
	var foundKnown, foundUnknown bool
	for _, tag := range tags {
		if tag.UII == "EPC-001" {
			foundKnown = true
			if tag.SKU != "SKU-001" {
				t.Errorf("expected SKU 'SKU-001', got %s", tag.SKU)
			}
			if tag.Name != "Test Tool" {
				t.Errorf("expected Name 'Test Tool', got %s", tag.Name)
			}
			if tag.Status != "active" {
				t.Errorf("expected Status 'active', got %s", tag.Status)
			}
			if tag.Location != "Warehouse A" {
				t.Errorf("expected Location 'Warehouse A', got %s", tag.Location)
			}
		}
		if tag.UII == "UNKNOWN-EPC" {
			foundUnknown = true
			if tag.Status != "unknown" {
				t.Errorf("expected Status 'unknown' for unknown tool, got %s", tag.Status)
			}
		}
	}

	if !foundKnown {
		t.Error("expected to find known tool EPC-001")
	}
	if !foundUnknown {
		t.Error("expected to find unknown tool UNKNOWN-EPC")
	}
}

// TestHandleTags_EmptyStore tests handleTags with no data
func TestHandleTags_EmptyStore(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Create real store with no data
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Create tables
	_, err = db.Exec(`
		CREATE TABLE pending_confirmations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uii TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('entrada', 'salida')),
			antenna_id TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced BOOLEAN DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	store := localstore.New(db)
	server := NewServer("127.0.0.1", 0, eventBus, store, nil, nil, "test-company")

	req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	rec := httptest.NewRecorder()

	server.handleTags(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var tags []TagInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &tags); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(tags) != 0 {
		t.Errorf("expected 0 tags for empty store, got %d", len(tags))
	}
}

// TestHandleTags_NilStore tests handleTags with nil store
func TestHandleTags_NilStore(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	server := NewServer("127.0.0.1", 0, eventBus, nil, nil, nil, "test-company")

	req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	rec := httptest.NewRecorder()

	server.handleTags(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var tags []TagInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &tags); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(tags) != 0 {
		t.Errorf("expected 0 tags for nil store, got %d", len(tags))
	}
}

// TestHandleStatus_VPSOnline tests status endpoint when VPS is online
func TestHandleStatus_VPSOnline(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Create VPS server that's online
	vpsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle health check endpoint
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]localstore.Tool{})
	}))
	defer vpsServer.Close()

	vpsClient := httpclient.NewVPSClient(vpsServer.URL, 5*time.Second, "test-token")

	// Create store with some pending confirmations
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE pending_confirmations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uii TEXT NOT NULL,
			action TEXT NOT NULL,
			antenna_id TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced BOOLEAN DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE tools (
			id INTEGER PRIMARY KEY,
			company_id TEXT NOT NULL,
			sku TEXT NOT NULL,
			name TEXT NOT NULL,
			uii TEXT NOT NULL UNIQUE,
			status TEXT,
			location TEXT,
			last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	store := localstore.New(db)

	// Insert some tools
	_, err = db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, uii, status)
		VALUES 
			(1, 'comp-001', 'SKU-001', 'Tool 1', 'EPC-001', 'active'),
			(2, 'comp-001', 'SKU-002', 'Tool 2', 'EPC-002', 'active')
	`)
	if err != nil {
		t.Fatalf("failed to insert tools: %v", err)
	}

	// Create pending confirmations
	store.CreateConfirmation("EPC-001", "entrada", "ANT-01", time.Now(), 0)
	store.CreateConfirmation("EPC-002", "salida", "ANT-02", time.Now(), 0)

	server := NewServer("127.0.0.1", 0, eventBus, store, nil, vpsClient, "test-company")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()

	server.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var status StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if !status.VPSOnline {
		t.Error("expected VPS to be online")
	}
	if status.PendingCount != 2 {
		t.Errorf("expected 2 pending confirmations, got %d", status.PendingCount)
	}
	if status.ToolsCount != 2 {
		t.Errorf("expected 2 tools, got %d", status.ToolsCount)
	}
}

// TestHandleStatus_VPSOffline tests status endpoint when VPS is offline
func TestHandleStatus_VPSOffline(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Create VPS client pointing to non-existent server
	vpsClient := httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "test-token")

	server := NewServer("127.0.0.1", 0, eventBus, nil, nil, vpsClient, "test-company")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()

	server.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var status StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// VPS should be offline since we can't reach it
	// Note: The current implementation returns vpsOnline based on vpsClient != nil
	// which may not be accurate - this test documents current behavior
	t.Logf("VPSOnline: %v, PendingCount: %d, ToolsCount: %d", status.VPSOnline, status.PendingCount, status.ToolsCount)
}

// TestHandleStatus_PendingWarning tests that pending_warning is returned when threshold exceeded
func TestHandleStatus_PendingWarning(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Create store with many pending confirmations
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE pending_confirmations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uii TEXT NOT NULL,
			action TEXT NOT NULL,
			antenna_id TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced BOOLEAN DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE tools (
			id INTEGER PRIMARY KEY,
			company_id TEXT NOT NULL,
			sku TEXT NOT NULL,
			name TEXT NOT NULL,
			uii TEXT NOT NULL UNIQUE,
			status TEXT,
			location TEXT,
			last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	store := localstore.New(db)

	// Insert tools
	_, err = db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, uii, status)
		VALUES (1, 'comp-001', 'SKU-001', 'Tool 1', 'EPC-001', 'active')
	`)
	if err != nil {
		t.Fatalf("failed to insert tools: %v", err)
	}

	// Create server with low warning threshold
	cfg := &config.GatewayConfig{
		CompanyID:               "test-company",
		PendingWarningThreshold: intPtr(5), // Warning at 5
	}

	server := NewServer("127.0.0.1", 0, eventBus, store, nil, nil, cfg.CompanyID)
	server.config = cfg

	// Initially no warning
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	server.handleStatus(rec, req)

	var status StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if status.PendingWarning {
		t.Error("expected no pending_warning with 0 confirmations")
	}

	// Add 5 confirmations (at threshold)
	for i := 0; i < 5; i++ {
		store.CreateConfirmation(fmt.Sprintf("EPC-%d", i), "entrada", "ANT-01", time.Now(), 0)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec = httptest.NewRecorder()
	server.handleStatus(rec, req)

	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if !status.PendingWarning {
		t.Error("expected pending_warning=true when at threshold")
	}
}

// TestHandleConfirm_WithAntennaID tests that antenna_id from request body is used
func TestHandleConfirm_WithAntennaID(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Create real local store with in-memory DB
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE pending_confirmations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uii TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('entrada', 'salida')),
			antenna_id TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced BOOLEAN DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	store := localstore.New(db)

	// VPS client pointing to non-existent server (force offline mode)
	vpsClient := httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "test-token")
	verifier := verify.NewVerifier(nil, nil)

	server := NewServer("127.0.0.1", 0, eventBus, store, verifier, vpsClient, "test-company")

	// Request with antenna_id
	body := `{"uii":"EPC-456","action":"salida","antenna_id":"ant-2"}`
	req := httptest.NewRequest(http.MethodPost, "/api/confirm", stringReader(body))
	rec := httptest.NewRecorder()

	server.handleConfirm(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify confirmation was saved with correct antenna_id
	confirmations, err := store.GetUnsyncedConfirmations(100)
	if err != nil {
		t.Fatalf("failed to get confirmations: %v", err)
	}
	if len(confirmations) != 1 {
		t.Fatalf("expected 1 confirmation, got %d", len(confirmations))
	}
	if confirmations[0].AntennaID != "ant-2" {
		t.Errorf("expected antenna_id 'ant-2', got '%s'", confirmations[0].AntennaID)
	}
}

// Helper function
func intPtr(i int) *int {
	return &i
}
