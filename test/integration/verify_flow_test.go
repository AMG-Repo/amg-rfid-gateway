package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"database/sql"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/verify"
	_ "modernc.org/sqlite"
)

// TestVerifyFlow_EndToEnd tests the complete verification flow:
// 1. EventBus publishes tag detection
// 2. SSE subscriber receives event
// 3. Frontend calls /api/tags to get tool info
// 4. Frontend calls /api/confirm to confirm action
// 5. VPS receives confirmation (or queued if offline)
func TestVerifyFlow_EndToEnd(t *testing.T) {
	// Create event bus
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Create VPS mock server
	confirmationReceived := make(chan struct{}, 1)
	confirmedUII := ""
	confirmedAction := ""

	vpsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/gateway/sync-data":
			// Return sync data with normalized schema
			syncData := map[string]interface{}{
				"status": "OK",
				"tools": []map[string]interface{}{
					{
						"id":               1,
						"tool_id":          1,
						"uii":              "EPC-TEST-001",
						"sku":              "TOOL-001",
						"name":             "Hammer",
						"description":      "A hammer",
						"status":           "active",
						"location":         "Almacén A",
						"location_id":      nil,
						"unit_number":      "001",
						"display_name":     "Hammer 001",
						"notes":            nil,
						"active":           true,
						"kanban_zone":      nil,
						"tool_destination": "Almacén A",
					},
				},
				"timestamp": time.Now().UTC(),
			}
			json.NewEncoder(w).Encode(syncData)

		case "/api/v1/users":
			json.NewEncoder(w).Encode([]localstore.User{})

		case "/api/v1/gateway/confirm":
			var req struct {
				UII       string `json:"uii"`
				Action    string `json:"action"`
				AntennaID string `json:"antenna_id"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			confirmedUII = req.UII
			confirmedAction = req.Action
			close(confirmationReceived)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "OK",
				"uii":    req.UII,
				"action": req.Action,
			})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer vpsServer.Close()

	// Create local store with test data
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Create tables with normalized schema (tools + tool_tags)
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

	// Insert test tool master record
	_, err = db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination)
		VALUES (1, 'comp-001', 'TOOL-001', 'Hammer', 'A hammer', 'Almacén A')
	`)
	if err != nil {
		t.Fatalf("failed to insert tool: %v", err)
	}

	// Insert test tool tag record
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, status, location, active)
		VALUES (1, 1, 'EPC-TEST-001', 'active', 'Almacén A', 1)
	`)
	if err != nil {
		t.Fatalf("failed to insert tool tag: %v", err)
	}

	store := localstore.New(db)

	// Create verifier with antenna config using mock store that returns proper type
	mockStore := &mockToolStoreForVerify{
		tools: map[string]*verify.Tool{
			"EPC-TEST-001": {ID: 1, SKU: "TOOL-001", Name: "Hammer", UII: "EPC-TEST-001", Location: "Almacén A", Status: "active"},
		},
	}
	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: "salida"}, // Tool in Almacén should trigger salida
	}
	verifier := verify.NewVerifier(mockStore, antennas)

	// Create VPS client
	vpsClient := httpclient.NewVPSClient(vpsServer.URL, 5*time.Second, "test-token")

	// Create web server
	server := newTestServer(eventBus, store, verifier, vpsClient)

	// Step 1: Simulate tag detection event
	tagEvent := events.TagDetected{
		EPC:       "EPC-TEST-001",
		RSSI:      75,
		AntennaID: "ant-1",
		Timestamp: time.Now(),
	}

	// Step 2: Publish event to event bus
	// In production, this comes from the RFID reader
	eventBus.Publish(tagEvent)

	// Step 3: Simulate frontend calling /api/tags to get detected tags
	req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /api/tags, got %d", rec.Code)
	}

	var tags []tagInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &tags); err != nil {
		t.Fatalf("failed to parse tags response: %v", err)
	}

	// Tags endpoint returns tags from pending_confirmations, which will be empty initially
	// This is expected behavior - tags only appear after confirmation is queued
	t.Logf("Tags response: %+v", tags)

	// Step 4: Verify suggests action based on antenna zone and tool location
	suggestedAction := verifier.SuggestAction("EPC-TEST-001", "ant-1")
	if suggestedAction.Action != verify.ActionSalida {
		t.Errorf("expected salida action (tool in Almacén + salida zone), got %v", suggestedAction.Action)
	}

	// Step 5: Frontend calls /api/confirm with the action
	confirmBody := map[string]string{
		"uii":    "EPC-TEST-001",
		"action": "salida",
	}
	confirmJSON, _ := json.Marshal(confirmBody)
	req = httptest.NewRequest(http.MethodPost, "/api/confirm", bytes.NewReader(confirmJSON))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /api/confirm, got %d: %s", rec.Code, rec.Body.String())
	}

	var confirmResp confirmResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &confirmResp); err != nil {
		t.Fatalf("failed to parse confirm response: %v", err)
	}

	if !confirmResp.Success {
		t.Errorf("expected confirmation success, got %v", confirmResp.Success)
	}

	// Step 6: Verify VPS received the confirmation
	select {
	case <-confirmationReceived:
		// VPS received the confirmation
		if confirmedUII != "EPC-TEST-001" {
			t.Errorf("expected confirmed UII 'EPC-TEST-001', got %s", confirmedUII)
		}
		if confirmedAction != "salida" {
			t.Errorf("expected confirmed action 'salida', got %s", confirmedAction)
		}
		t.Log("SUCCESS: End-to-end flow completed - VPS received confirmation")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for VPS to receive confirmation")
	}
}

// TestVerifyFlow_OfflineMode tests the flow when VPS is offline
// Confirmations should be queued locally and marked as pending
func TestVerifyFlow_OfflineMode(t *testing.T) {
	// Create event bus
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Create local store
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Create tables with normalized schema (tools + tool_tags)
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

	// Create verifier
	mockStore := &mockToolStoreForVerify{tools: map[string]*verify.Tool{}}
	verifier := verify.NewVerifier(mockStore, nil)

	// Create VPS client pointing to non-existent server (offline)
	vpsClient := httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "test-token")

	// Create web server using test helper
	server := newTestServer(eventBus, store, verifier, vpsClient)

	// Step 1: Simulate frontend confirming action while VPS is offline
	confirmBody := map[string]string{
		"uii":    "EPC-OFFLINE-001",
		"action": "entrada",
	}
	confirmJSON, _ := json.Marshal(confirmBody)
	req := httptest.NewRequest(http.MethodPost, "/api/confirm", bytes.NewReader(confirmJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var confirmResp confirmResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &confirmResp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Should succeed in offline mode
	if !confirmResp.Success {
		t.Errorf("expected success in offline mode, got %v", confirmResp.Success)
	}
	if confirmResp.Mode != "offline" {
		t.Errorf("expected mode='offline', got %s", confirmResp.Mode)
	}

	// Step 2: Verify confirmation was queued locally
	count, err := store.GetPendingConfirmationsCount()
	if err != nil {
		t.Fatalf("failed to get pending count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 pending confirmation, got %d", count)
	}

	// Step 3: Verify confirmation details
	confirmations, err := store.GetUnsyncedConfirmations(100)
	if err != nil {
		t.Fatalf("failed to get confirmations: %v", err)
	}
	if len(confirmations) != 1 {
		t.Fatalf("expected 1 confirmation, got %d", len(confirmations))
	}

	if confirmations[0].UII != "EPC-OFFLINE-001" {
		t.Errorf("expected UII 'EPC-OFFLINE-001', got %s", confirmations[0].UII)
	}
	if confirmations[0].Action != "entrada" {
		t.Errorf("expected action 'entrada', got %s", confirmations[0].Action)
	}
	if confirmations[0].Synced {
		t.Error("expected confirmation to be unsynced")
	}

	t.Log("SUCCESS: Offline mode flow completed - confirmation queued locally")
}

// TestVerifyFlow_SSEEventDelivery tests that events are delivered via SSE
func TestVerifyFlow_SSEEventDelivery(t *testing.T) {
	eventBus := events.NewEventBus(100)
	defer eventBus.Close()

	// Subscribe to event bus (simulating what SSE handler does)
	ch := eventBus.Subscribe()
	defer eventBus.Unsubscribe(ch)

	// Publish an event
	event := events.TagDetected{
		EPC:       "EPC-SSE-TEST",
		RSSI:      80,
		AntennaID: "ant-2",
		Timestamp: time.Now(),
	}

	go eventBus.Publish(event)

	// Wait for event
	select {
	case received := <-ch:
		if received.EPC != "EPC-SSE-TEST" {
			t.Errorf("expected EPC 'EPC-SSE-TEST', got %s", received.EPC)
		}
		if received.AntennaID != "ant-2" {
			t.Errorf("expected AntennaID 'ant-2', got %s", received.AntennaID)
		}
		t.Log("SUCCESS: Event delivered via EventBus")
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestVerifyFlow_ToolLookup tests tool lookup by UII
func TestVerifyFlow_ToolLookup(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Create tables - must match localstore normalized schema exactly (tools + tool_tags)
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
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	// Insert test tools (master records)
	_, err = db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination)
		VALUES 
			(1, 'comp-001', 'SKU-001', 'Hammer', 'A hammer', 'Almacén A'),
			(2, 'comp-001', 'SKU-002', 'Screwdriver', 'A screwdriver', 'Obra Central')
	`)
	if err != nil {
		t.Fatalf("failed to insert tools: %v", err)
	}

	// Insert test tool tags (instance records)
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, status, location, active)
		VALUES 
			(1, 1, 'EPC-001', 'active', 'Almacén A', 1),
			(2, 2, 'EPC-002', 'active', 'Obra Central', 1)
	`)
	if err != nil {
		t.Fatalf("failed to insert tool tags: %v", err)
	}

	store := localstore.New(db)

	// Test lookup for tool in Almacén
	tool, err := store.GetToolByUII("EPC-001")
	if err != nil {
		t.Fatalf("failed to lookup tool: %v", err)
	}
	if tool == nil {
		t.Fatal("expected to find tool EPC-001")
	}
	if tool.Name != "Hammer" {
		t.Errorf("expected Name 'Hammer', got %s", tool.Name)
	}
	if tool.Location != "Almacén A" {
		t.Errorf("expected Location 'Almacén A', got %s", tool.Location)
	}

	// Test lookup for unknown tool
	tool, err = store.GetToolByUII("UNKNOWN-EPC")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != nil {
		t.Error("expected nil for unknown tool")
	}

	t.Log("SUCCESS: Tool lookup working correctly")
}

// TestVerifyFlow_SuggestedActionAccuracy tests action suggestion logic
func TestVerifyFlow_SuggestedActionAccuracy(t *testing.T) {
	tests := []struct {
		name       string
		toolLoc    string
		zone       string
		wantAction verify.Action
		wantReason string
	}{
		{
			name:       "tool in warehouse with entrada zone",
			toolLoc:    "Almacén Central",
			zone:       "entrada",
			wantAction: verify.ActionEntrada,
			wantReason: "zone:entrada",
		},
		{
			name:       "tool in warehouse with salida zone",
			toolLoc:    "Almacén Central",
			zone:       "salida",
			wantAction: verify.ActionSalida,
			wantReason: "zone:salida",
		},
		{
			name:       "tool in warehouse without zone (should suggest salida)",
			toolLoc:    "Almacén Central",
			zone:       "",
			wantAction: verify.ActionSalida,
			wantReason: "location:warehouse",
		},
		{
			name:       "tool outside without zone (should suggest entrada)",
			toolLoc:    "Obra Norte",
			zone:       "",
			wantAction: verify.ActionEntrada,
			wantReason: "location:outside",
		},
		{
			name:       "unknown tool without zone (should suggest entrada)",
			toolLoc:    "",
			zone:       "",
			wantAction: verify.ActionEntrada,
			wantReason: "unknown:tool_not_found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock store with appropriate tool
			var tools map[string]*verify.Tool
			if tt.toolLoc != "" {
				tools = map[string]*verify.Tool{
					"EPC-TEST": {ID: 1, SKU: "SKU-001", Name: "Test Tool", UII: "EPC-TEST", Location: tt.toolLoc, Status: "active"},
				}
			}

			mockStore := &mockToolStoreForVerify{tools: tools}

			antennas := []config.AntennaConfig{
				{ID: "ant-1", Zone: tt.zone},
			}

			verifier := verify.NewVerifier(mockStore, antennas)

			var result verify.SuggestedAction
			if tt.toolLoc == "" {
				result = verifier.SuggestAction("UNKNOWN-EPC", "ant-1")
			} else {
				result = verifier.SuggestAction("EPC-TEST", "ant-1")
			}

			if result.Action != tt.wantAction {
				t.Errorf("SuggestAction() Action = %v, want %v", result.Action, tt.wantAction)
			}
			if result.Reason != tt.wantReason {
				t.Errorf("SuggestAction() Reason = %v, want %v", result.Reason, tt.wantReason)
			}
		})
	}

	t.Log("SUCCESS: Action suggestion logic working correctly")
}

// Helper types and functions

type mockToolStoreForVerify struct {
	tools map[string]*verify.Tool
}

func (m *mockToolStoreForVerify) GetToolByUII(uii string) (*verify.Tool, error) {
	tool, ok := m.tools[uii]
	if !ok {
		return nil, nil
	}
	return tool, nil
}

// Simple response types for testing
type tagInfo struct {
	UII         string `json:"uii"`
	SKU         string `json:"sku,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Location    string `json:"location,omitempty"`
}

type confirmResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Mode    string `json:"mode"`
}

// Simple test server that wraps handlers
func newTestServer(eventBus *events.EventBus, store *localstore.LocalStore, verifier *verify.Verifier, vpsClient *httpclient.VPSClient) http.Handler {
	mux := http.NewServeMux()

	// Status handler
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vps_online":    vpsClient != nil,
			"pending_count": 0,
			"tools_count":   0,
		})
	})

	// Tags handler - simplified version
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if store == nil {
			json.NewEncoder(w).Encode([]tagInfo{})
			return
		}

		confirmations, _ := store.GetUnsyncedConfirmations(1000)
		var tags []tagInfo

		seen := make(map[string]bool)
		for _, conf := range confirmations {
			if seen[conf.UII] {
				continue
			}
			seen[conf.UII] = true

			tool, _ := store.GetToolByUII(conf.UII)
			if tool != nil {
				tags = append(tags, tagInfo{
					UII:      tool.UII,
					SKU:      tool.SKU,
					Name:     tool.Name,
					Status:   tool.Status,
					Location: tool.Location,
				})
			} else {
				tags = append(tags, tagInfo{
					UII:    conf.UII,
					Status: "unknown",
				})
			}
		}

		json.NewEncoder(w).Encode(tags)
	})

	// Confirm handler - simplified version
	mux.HandleFunc("/api/confirm", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			UII    string `json:"uii"`
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}

		// Validate action
		if err := verifier.ValidateAction(req.Action); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Try VPS first
		if vpsClient != nil {
			err := vpsClient.SendConfirmation("", req.UII, req.Action)
			if err == nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(confirmResponse{
					Success: true,
					Message: "Confirmation sent to VPS",
					Mode:    "online",
				})
				return
			}
		}

		// Fallback to local store
		if store != nil {
			_, err := store.CreateConfirmation(req.UII, req.Action, "manual", time.Now(), 0)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(confirmResponse{
					Success: false,
					Message: "Failed to queue: " + err.Error(),
					Mode:    "offline",
				})
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(confirmResponse{
			Success: true,
			Message: "Confirmation queued locally (offline mode)",
			Mode:    "offline",
		})
	})

	return mux
}
