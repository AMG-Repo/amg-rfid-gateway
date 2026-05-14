package localstore

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// setupTestDB creates an in-memory SQLite database for testing with normalized schema
func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	// Create normalized schema tables (tools + tool_tags)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS tools (
			id INTEGER PRIMARY KEY,
			company_id TEXT NOT NULL,
			sku TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			default_destination TEXT,
			last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS tool_tags (
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

		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY,
			company_id TEXT NOT NULL,
			name TEXT NOT NULL,
			role TEXT,
			department TEXT,
			rfid_tag TEXT UNIQUE,
			active BOOLEAN DEFAULT 1,
			last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS pending_confirmations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uii TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('entrada', 'salida')),
			antenna_id TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced BOOLEAN DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_tool_tags_uii ON tool_tags(uii);
		CREATE INDEX IF NOT EXISTS idx_tool_tags_tool_id ON tool_tags(tool_id);
		CREATE INDEX IF NOT EXISTS idx_tools_sku ON tools(sku);
		CREATE INDEX IF NOT EXISTS idx_users_rfid ON users(rfid_tag);
		CREATE INDEX IF NOT EXISTS idx_pending_synced ON pending_confirmations(synced);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	return db
}

// Test New creates a LocalStore
func TestNew(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)
	if store == nil {
		t.Fatal("expected store to be non-nil")
	}
	if store.db != db {
		t.Error("expected store.db to match input db")
	}
}

// Test GetToolByUII with existing tool
func TestGetToolByUII_Existing(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert normalized data: tool master + tool tag
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (1, 'comp-001', 'SKU-001', 'Test Tool', 'A test tool', 'Warehouse A', ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert tool: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, unit_number, status, location, active, last_synced_at)
		VALUES (1, 1, 'E200341502001080', '001', 'active', 'Warehouse A', 1, ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert tool tag: %v", err)
	}

	// Get the tool
	tool, err := store.GetToolByUII("E200341502001080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool == nil {
		t.Fatal("expected tool to be found")
	}

	if tool.ID != 1 {
		t.Errorf("expected ID 1, got %d", tool.ID)
	}
	if tool.CompanyID != "comp-001" {
		t.Errorf("expected CompanyID 'comp-001', got %s", tool.CompanyID)
	}
	if tool.SKU != "SKU-001" {
		t.Errorf("expected SKU 'SKU-001', got %s", tool.SKU)
	}
	if tool.Name != "Test Tool" {
		t.Errorf("expected Name 'Test Tool', got %s", tool.Name)
	}
	if tool.Description != "A test tool" {
		t.Errorf("expected Description 'A test tool', got %s", tool.Description)
	}
	if tool.UII != "E200341502001080" {
		t.Errorf("expected UII 'E200341502001080', got %s", tool.UII)
	}
	if tool.Status != "active" {
		t.Errorf("expected Status 'active', got %s", tool.Status)
	}
	if tool.Location != "Warehouse A" {
		t.Errorf("expected Location 'Warehouse A', got %s", tool.Location)
	}
}

// Test GetToolByUII with non-existent tool
func TestGetToolByUII_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	tool, err := store.GetToolByUII("NON-EXISTENT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != nil {
		t.Error("expected tool to be nil for non-existent UII")
	}
}

// Test GetToolByUII with empty UII
func TestGetToolByUII_Empty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	tool, err := store.GetToolByUII("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != nil {
		t.Error("expected tool to be nil for empty UII")
	}
}

// Test UpsertTools inserts new tools
func TestUpsertTools_Insert(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	tools := []Tool{
		{
			ID:        1,
			CompanyID: "comp-001",
			SKU:       "SKU-001",
			Name:      "Tool One",
			UII:       "E200341502001080",
			Status:    "active",
			Location:  "Warehouse A",
		},
		{
			ID:        2,
			CompanyID: "comp-001",
			SKU:       "SKU-002",
			Name:      "Tool Two",
			UII:       "E200341502001081",
			Status:    "inactive",
			Location:  "Warehouse B",
		},
	}

	err := store.UpsertTools(tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tools were inserted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM tools").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count tools: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 tools, got %d", count)
	}

	// Verify tool data
	tool, err := store.GetToolByUII("E200341502001080")
	if err != nil {
		t.Fatalf("failed to get tool: %v", err)
	}
	if tool.Name != "Tool One" {
		t.Errorf("expected Name 'Tool One', got %s", tool.Name)
	}
}

// Test UpsertTools updates existing tools
func TestUpsertTools_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert initial normalized data
	_, err := db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (1, 'comp-001', 'SKU-001', 'Old Name', 'Desc', 'Old Location', ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert initial tool: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, status, location, active, last_synced_at)
		VALUES (1, 1, 'E200341502001080', 'active', 'Old Location', 1, ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert initial tool tag: %v", err)
	}

	// Upsert with updated values
	updatedTools := []Tool{
		{
			ID:        1,
			CompanyID: "comp-002", // Changed
			SKU:       "SKU-001",
			Name:      "New Name", // Changed
			UII:       "E200341502001080",
			Status:    "inactive",     // Changed
			Location:  "New Location", // Changed
		},
	}

	err = store.UpsertTools(updatedTools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tool was updated
	tool, err := store.GetToolByUII("E200341502001080")
	if err != nil {
		t.Fatalf("failed to get tool: %v", err)
	}
	if tool.Name != "New Name" {
		t.Errorf("expected Name 'New Name', got %s", tool.Name)
	}
	if tool.CompanyID != "comp-002" {
		t.Errorf("expected CompanyID 'comp-002', got %s", tool.CompanyID)
	}
	if tool.Status != "inactive" {
		t.Errorf("expected Status 'inactive', got %s", tool.Status)
	}
	if tool.Location != "New Location" {
		t.Errorf("expected Location 'New Location', got %s", tool.Location)
	}
}

// Test UpsertTools with empty slice
func TestUpsertTools_Empty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	err := store.UpsertTools([]Tool{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM tools").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count tools: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 tools, got %d", count)
	}
}

// Test GetUserByRFIDTag with existing user
func TestGetUserByRFIDTag_Existing(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert a user
	_, err := db.Exec(`
		INSERT INTO users (id, company_id, name, role, department, rfid_tag, active, last_synced_at)
		VALUES (1, 'comp-001', 'John Doe', 'operator', 'Production', 'TAG-001', 1, ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert user: %v", err)
	}

	// Get the user
	user, err := store.GetUserByRFIDTag("TAG-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user == nil {
		t.Fatal("expected user to be found")
	}

	if user.ID != 1 {
		t.Errorf("expected ID 1, got %d", user.ID)
	}
	if user.Name != "John Doe" {
		t.Errorf("expected Name 'John Doe', got %s", user.Name)
	}
	if user.Role != "operator" {
		t.Errorf("expected Role 'operator', got %s", user.Role)
	}
	if user.Department != "Production" {
		t.Errorf("expected Department 'Production', got %s", user.Department)
	}
	if user.RFIDTag != "TAG-001" {
		t.Errorf("expected RFIDTag 'TAG-001', got %s", user.RFIDTag)
	}
	if !user.Active {
		t.Error("expected Active to be true")
	}
}

// Test GetUserByRFIDTag with non-existent user
func TestGetUserByRFIDTag_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	user, err := store.GetUserByRFIDTag("NON-EXISTENT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != nil {
		t.Error("expected user to be nil for non-existent tag")
	}
}

// Test UpsertUsers inserts new users
func TestUpsertUsers_Insert(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	users := []User{
		{
			ID:         1,
			CompanyID:  "comp-001",
			Name:       "John Doe",
			Role:       "operator",
			Department: "Production",
			RFIDTag:    "TAG-001",
			Active:     true,
		},
		{
			ID:         2,
			CompanyID:  "comp-001",
			Name:       "Jane Smith",
			Role:       "supervisor",
			Department: "Management",
			RFIDTag:    "TAG-002",
			Active:     false,
		},
	}

	err := store.UpsertUsers(users)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify users were inserted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count users: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 users, got %d", count)
	}
}

// Test UpsertUsers updates existing users
func TestUpsertUsers_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert initial user
	_, err := db.Exec(`
		INSERT INTO users (id, company_id, name, role, department, rfid_tag, active)
		VALUES (1, 'comp-001', 'Old Name', 'old-role', 'old-dept', 'TAG-001', 1)
	`)
	if err != nil {
		t.Fatalf("failed to insert initial user: %v", err)
	}

	// Upsert with updated values
	updatedUsers := []User{
		{
			ID:         1,
			CompanyID:  "comp-002",
			Name:       "New Name",
			Role:       "new-role",
			Department: "new-dept",
			RFIDTag:    "TAG-001",
			Active:     false,
		},
	}

	err = store.UpsertUsers(updatedUsers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify user was updated
	user, err := store.GetUserByRFIDTag("TAG-001")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if user.Name != "New Name" {
		t.Errorf("expected Name 'New Name', got %s", user.Name)
	}
	if user.Role != "new-role" {
		t.Errorf("expected Role 'new-role', got %s", user.Role)
	}
	if user.Active {
		t.Error("expected Active to be false")
	}
}

// Test CreateConfirmation
func TestCreateConfirmation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	timestamp := time.Now()
	conf, err := store.CreateConfirmation("E200341502001080", "entrada", "ANT-01", timestamp, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conf == nil {
		t.Fatal("expected confirmation to be created")
	}

	if conf.ID == 0 {
		t.Error("expected ID to be set")
	}
	if conf.UII != "E200341502001080" {
		t.Errorf("expected UII 'E200341502001080', got %s", conf.UII)
	}
	if conf.Action != "entrada" {
		t.Errorf("expected Action 'entrada', got %s", conf.Action)
	}
	if conf.AntennaID != "ANT-01" {
		t.Errorf("expected AntennaID 'ANT-01', got %s", conf.AntennaID)
	}
	if conf.Synced {
		t.Error("expected Synced to be false")
	}
	if conf.RetryCount != 0 {
		t.Errorf("expected RetryCount 0, got %d", conf.RetryCount)
	}
}

// Test CreateConfirmation with invalid action
func TestCreateConfirmation_InvalidAction(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	_, err := store.CreateConfirmation("E200341502001080", "invalid-action", "ANT-01", time.Now(), 0)
	if err == nil {
		t.Error("expected error for invalid action")
	}
}

// Test GetUnsyncedConfirmations
func TestGetUnsyncedConfirmations(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Create confirmations
	now := time.Now()
	store.CreateConfirmation("EPC-001", "entrada", "ANT-01", now, 0)
	store.CreateConfirmation("EPC-002", "salida", "ANT-02", now.Add(1*time.Second), 0)
	store.CreateConfirmation("EPC-003", "entrada", "ANT-01", now.Add(2*time.Second), 0)

	// Mark one as synced
	_, err := db.Exec("UPDATE pending_confirmations SET synced = 1 WHERE uii = 'EPC-002'")
	if err != nil {
		t.Fatalf("failed to mark synced: %v", err)
	}

	// Get unsynced confirmations
	confirmations, err := store.GetUnsyncedConfirmations(100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(confirmations) != 2 {
		t.Errorf("expected 2 unsynced confirmations, got %d", len(confirmations))
	}

	// Verify order (should be by created_at ASC)
	if confirmations[0].UII != "EPC-001" {
		t.Errorf("expected first UII 'EPC-001', got %s", confirmations[0].UII)
	}
	if confirmations[1].UII != "EPC-003" {
		t.Errorf("expected second UII 'EPC-003', got %s", confirmations[1].UII)
	}
}

// Test GetUnsyncedConfirmations with limit
func TestGetUnsyncedConfirmations_WithLimit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Create 5 confirmations
	now := time.Now()
	for i := 0; i < 5; i++ {
		_, err := store.CreateConfirmation("EPC-00"+string(rune('1'+i)), "entrada", "ANT-01", now.Add(time.Duration(i)*time.Second), 0)
		if err != nil {
			t.Fatalf("failed to create confirmation: %v", err)
		}
	}

	// Get with limit 2
	confirmations, err := store.GetUnsyncedConfirmations(2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(confirmations) != 2 {
		t.Errorf("expected 2 confirmations with limit, got %d", len(confirmations))
	}
}

// Test MarkConfirmationSynced
func TestMarkConfirmationSynced(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Create confirmation
	conf, err := store.CreateConfirmation("EPC-001", "entrada", "ANT-01", time.Now(), 0)
	if err != nil {
		t.Fatalf("failed to create confirmation: %v", err)
	}

	// Mark as synced
	err = store.MarkConfirmationSynced(conf.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's marked as synced
	confirmations, err := store.GetUnsyncedConfirmations(100)
	if err != nil {
		t.Fatalf("failed to get unsynced: %v", err)
	}

	for _, c := range confirmations {
		if c.ID == conf.ID {
			t.Error("confirmation should not be in unsynced list")
		}
	}
}

// Test IncrementRetryCount
func TestIncrementRetryCount(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Create confirmation
	conf, err := store.CreateConfirmation("EPC-001", "entrada", "ANT-01", time.Now(), 0)
	if err != nil {
		t.Fatalf("failed to create confirmation: %v", err)
	}

	// Increment retry count twice
	err = store.IncrementRetryCount(conf.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = store.IncrementRetryCount(conf.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify retry count
	var retryCount int
	err = db.QueryRow("SELECT retry_count FROM pending_confirmations WHERE id = ?", conf.ID).Scan(&retryCount)
	if err != nil {
		t.Fatalf("failed to query retry count: %v", err)
	}

	if retryCount != 2 {
		t.Errorf("expected retry_count 2, got %d", retryCount)
	}
}

// Test GetToolsCount
func TestGetToolsCount(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Initially should be 0
	count, err := store.GetToolsCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 tools initially, got %d", count)
	}

	// Insert master tool records first (required for FK constraint)
	now := time.Now()
	_, err = db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES 
			(1, 'comp-001', 'SKU-001', 'Tool One', 'Desc 1', 'Loc 1', ?),
			(2, 'comp-001', 'SKU-002', 'Tool Two', 'Desc 2', 'Loc 2', ?)
	`, now, now)
	if err != nil {
		t.Fatalf("failed to insert tools: %v", err)
	}

	// Insert tool tags (actual RFID instances) - only active ones count
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, unit_number, status, location, active, last_synced_at)
		VALUES 
			(1, 1, 'EPC-001', 'UNIT-001', 'active', 'Almacén General', 1, ?),
			(2, 1, 'EPC-002', 'UNIT-002', 'active', 'Almacén General', 1, ?),
			(3, 2, 'EPC-003', 'UNIT-003', 'active', 'Línea 1', 1, ?),
			(4, 2, 'EPC-004', 'UNIT-004', 'inactive', 'Línea 2', 0, ?)
	`, now, now, now, now)
	if err != nil {
		t.Fatalf("failed to insert tool tags: %v", err)
	}

	// Should be 3 (only active tool_tags count, excluding inactive EPC-004)
	count, err = store.GetToolsCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 active tool tags, got %d", count)
	}
}

// Test GetUsersCount
func TestGetUsersCount(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Initially should be 0
	count, err := store.GetUsersCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 users initially, got %d", count)
	}

	// Insert users
	_, err = db.Exec(`
		INSERT INTO users (id, company_id, name, rfid_tag, active)
		VALUES 
			(1, 'comp-001', 'User One', 'TAG-001', 1),
			(2, 'comp-001', 'User Two', 'TAG-002', 1)
	`)
	if err != nil {
		t.Fatalf("failed to insert users: %v", err)
	}

	// Should be 2
	count, err = store.GetUsersCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 users, got %d", count)
	}
}

// Test GetPendingConfirmationsCount
func TestGetPendingConfirmationsCount(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Initially should be 0
	count, err := store.GetPendingConfirmationsCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 pending confirmations initially, got %d", count)
	}

	// Create confirmations
	now := time.Now()
	_, err = store.CreateConfirmation("EPC-001", "entrada", "ANT-01", now, 0)
	if err != nil {
		t.Fatalf("failed to create confirmation: %v", err)
	}
	_, err = store.CreateConfirmation("EPC-002", "salida", "ANT-02", now.Add(1*time.Second), 0)
	if err != nil {
		t.Fatalf("failed to create confirmation: %v", err)
	}
	_, err = store.CreateConfirmation("EPC-003", "entrada", "ANT-01", now.Add(2*time.Second), 0)
	if err != nil {
		t.Fatalf("failed to create confirmation: %v", err)
	}

	// Should be 3
	count, err = store.GetPendingConfirmationsCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 pending confirmations, got %d", count)
	}

	// Mark one as synced
	_, err = db.Exec("UPDATE pending_confirmations SET synced = 1 WHERE uii = 'EPC-002'")
	if err != nil {
		t.Fatalf("failed to mark synced: %v", err)
	}

	// Should be 2
	count, err = store.GetPendingConfirmationsCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 pending confirmations, got %d", count)
	}
}

// Test GetPendingConfirmationsCount with no synced column set (edge case)
func TestGetPendingConfirmationsCount_OnlyUnsynced(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert directly with synced=0
	_, err := db.Exec(`
		INSERT INTO pending_confirmations (uii, action, antenna_id, timestamp, synced, retry_count)
		VALUES 
			('EPC-001', 'entrada', 'ANT-01', ?, 0, 0),
			('EPC-002', 'salida', 'ANT-02', ?, 0, 0)
	`, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("failed to insert confirmations: %v", err)
	}

	count, err := store.GetPendingConfirmationsCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 pending confirmations, got %d", count)
	}
}

// Test CreateConfirmation with queue cap enforcement
func TestCreateConfirmation_QueueCapEnforcement(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Create 3 confirmations with cap of 3
	now := time.Now()
	_, err := store.CreateConfirmation("EPC-001", "entrada", "ANT-01", now, 3)
	if err != nil {
		t.Fatalf("failed to create confirmation 1: %v", err)
	}
	_, err = store.CreateConfirmation("EPC-002", "salida", "ANT-02", now.Add(1*time.Second), 3)
	if err != nil {
		t.Fatalf("failed to create confirmation 2: %v", err)
	}
	_, err = store.CreateConfirmation("EPC-003", "entrada", "ANT-01", now.Add(2*time.Second), 3)
	if err != nil {
		t.Fatalf("failed to create confirmation 3: %v", err)
	}

	// Verify 3 confirmations exist
	count, err := store.GetPendingConfirmationsCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 confirmations, got %d", count)
	}

	// Create 4th confirmation with cap of 3 - should discard oldest (EPC-001)
	_, err = store.CreateConfirmation("EPC-004", "salida", "ANT-02", now.Add(3*time.Second), 3)
	if err != nil {
		t.Fatalf("failed to create confirmation 4: %v", err)
	}

	// Verify still 3 confirmations (oldest was discarded)
	count, err = store.GetPendingConfirmationsCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 confirmations after cap enforcement, got %d", count)
	}

	// Verify oldest was discarded - EPC-001 should not exist
	confirmations, err := store.GetUnsyncedConfirmations(100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, conf := range confirmations {
		if conf.UII == "EPC-001" {
			t.Error("EPC-001 should have been discarded due to queue cap")
		}
	}

	// Verify EPC-002, EPC-003, EPC-004 exist
	found := make(map[string]bool)
	for _, conf := range confirmations {
		found[conf.UII] = true
	}
	if !found["EPC-002"] || !found["EPC-003"] || !found["EPC-004"] {
		t.Errorf("expected EPC-002, EPC-003, EPC-004 to exist, got: %v", found)
	}
}

// Test CreateConfirmation with queue cap disabled (0 = unlimited)
func TestCreateConfirmation_QueueCapDisabled(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Create 5 confirmations with cap of 0 (unlimited)
	now := time.Now()
	for i := 1; i <= 5; i++ {
		_, err := store.CreateConfirmation(fmt.Sprintf("EPC-%03d", i), "entrada", "ANT-01", now.Add(time.Duration(i)*time.Second), 0)
		if err != nil {
			t.Fatalf("failed to create confirmation %d: %v", i, err)
		}
	}

	// Verify all 5 confirmations exist
	count, err := store.GetPendingConfirmationsCount()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 confirmations with unlimited cap, got %d", count)
	}
}

// Test DeleteOldestUnsyncedConfirmation
func TestDeleteOldestUnsyncedConfirmation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Create test confirmations
	_, err := store.CreateConfirmation("EPC-001", "entrada", "", time.Now(), 0)
	if err != nil {
		t.Fatalf("failed to create confirmation: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	_, err = store.CreateConfirmation("EPC-002", "salida", "", time.Now(), 0)
	if err != nil {
		t.Fatalf("failed to create confirmation: %v", err)
	}

	// Delete oldest
	deletedUII, err := store.DeleteOldestUnsyncedConfirmation()
	if err != nil {
		t.Fatalf("failed to delete oldest: %v", err)
	}

	if deletedUII != "EPC-001" {
		t.Errorf("expected oldest UII 'EPC-001', got %q", deletedUII)
	}

	// Verify only one remains
	count, err := store.GetPendingConfirmationsCount()
	if err != nil {
		t.Fatalf("failed to get count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 pending confirmation, got %d", count)
	}
}

func TestUpsertToolsFromSync_Insert(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert tools from sync data
	rows := []SyncDataItem{
		{
			ID:       1,
			ToolID:   10,
			UII:      "EPC-001",
			SKU:      "SKU-001",
			Name:     "Tool One",
			Status:   "available",
			Location: "Almacén General",
			Active:   true,
		},
		{
			ID:       2,
			ToolID:   10,
			UII:      "EPC-002",
			SKU:      "SKU-001",
			Name:     "Tool One",
			Status:   "in_use",
			Location: "Línea 1",
			Active:   true,
		},
	}

	err := store.UpsertToolsFromSync(rows)
	if err != nil {
		t.Fatalf("failed to upsert tools from sync: %v", err)
	}

	// Verify tool master record was created
	toolRecord, err := store.GetToolRecordByID(10)
	if err != nil {
		t.Fatalf("failed to get tool record: %v", err)
	}
	if toolRecord == nil {
		t.Fatal("expected tool record to exist")
	}
	if toolRecord.SKU != "SKU-001" {
		t.Errorf("expected SKU 'SKU-001', got %q", toolRecord.SKU)
	}
	if toolRecord.Name != "Tool One" {
		t.Errorf("expected Name 'Tool One', got %q", toolRecord.Name)
	}

	// Verify tool tags were created
	tag1, err := store.GetToolTagByUII("EPC-001")
	if err != nil {
		t.Fatalf("failed to get tag: %v", err)
	}
	if tag1 == nil {
		t.Fatal("expected tag 1 to exist")
	}
	if tag1.Status != "available" {
		t.Errorf("expected Status 'available', got %q", tag1.Status)
	}
	if tag1.Location != "Almacén General" {
		t.Errorf("expected Location 'Almacén General', got %q", tag1.Location)
	}

	tag2, err := store.GetToolTagByUII("EPC-002")
	if err != nil {
		t.Fatalf("failed to get tag: %v", err)
	}
	if tag2 == nil {
		t.Fatal("expected tag 2 to exist")
	}
	if tag2.Location != "Línea 1" {
		t.Errorf("expected Location 'Línea 1', got %q", tag2.Location)
	}

	// Verify legacy GetToolByUII still works via JOIN
	tool1, err := store.GetToolByUII("EPC-001")
	if err != nil {
		t.Fatalf("failed to get tool via legacy method: %v", err)
	}
	if tool1 == nil {
		t.Fatal("expected tool 1 to exist via legacy method")
	}
	if tool1.SKU != "SKU-001" {
		t.Errorf("expected SKU 'SKU-001', got %q", tool1.SKU)
	}
}

func TestUpsertToolsFromSync_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert initial normalized data
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (10, 'comp-001', 'SKU-001', 'Old Name', 'Desc', 'Old Location', ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert initial tool: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, status, location, active, last_synced_at)
		VALUES (1, 10, 'EPC-001', 'available', 'Old Location', 1, ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert initial tag: %v", err)
	}

	// Update via sync data
	rows := []SyncDataItem{
		{
			ID:       1,
			ToolID:   10,
			UII:      "EPC-001",
			SKU:      "SKU-001",
			Name:     "New Name",
			Status:   "in_use",
			Location: "New Location",
			Active:   true,
		},
	}

	err = store.UpsertToolsFromSync(rows)
	if err != nil {
		t.Fatalf("failed to upsert tools from sync: %v", err)
	}

	// Verify tool master was updated
	toolRecord, err := store.GetToolRecordByID(10)
	if err != nil {
		t.Fatalf("failed to get tool record: %v", err)
	}
	if toolRecord.Name != "New Name" {
		t.Errorf("expected Name 'New Name', got %q", toolRecord.Name)
	}

	// Verify tag was updated
	tag, err := store.GetToolTagByUII("EPC-001")
	if err != nil {
		t.Fatalf("failed to get tag: %v", err)
	}
	if tag.Status != "in_use" {
		t.Errorf("expected Status 'in_use', got %q", tag.Status)
	}
	if tag.Location != "New Location" {
		t.Errorf("expected Location 'New Location', got %q", tag.Location)
	}

	// Verify legacy GetToolByUII still works
	tool, err := store.GetToolByUII("EPC-001")
	if err != nil {
		t.Fatalf("failed to get tool via legacy method: %v", err)
	}
	if tool.Name != "New Name" {
		t.Errorf("expected Name 'New Name' via legacy method, got %q", tool.Name)
	}
	if tool.Location != "New Location" {
		t.Errorf("expected Location 'New Location' via legacy method, got %q", tool.Location)
	}
}

func TestUpsertToolsFromSync_EmptyLocationFallback(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert with empty location (should default to "Almacén General")
	rows := []SyncDataItem{
		{
			ID:       1,
			ToolID:   10,
			UII:      "EPC-001",
			SKU:      "SKU-001",
			Name:     "Tool One",
			Status:   "available",
			Location: "", // Empty location
			Active:   true,
		},
	}

	err := store.UpsertToolsFromSync(rows)
	if err != nil {
		t.Fatalf("failed to upsert tools from sync: %v", err)
	}

	// Verify tag location defaults to "Almacén General"
	tag, err := store.GetToolTagByUII("EPC-001")
	if err != nil {
		t.Fatalf("failed to get tag: %v", err)
	}
	if tag.Location != "Almacén General" {
		t.Errorf("expected Location 'Almacén General' for empty input, got %q", tag.Location)
	}

	// Verify legacy GetToolByUII also gets the defaulted location
	tool, err := store.GetToolByUII("EPC-001")
	if err != nil {
		t.Fatalf("failed to get tool: %v", err)
	}
	if tool.Location != "Almacén General" {
		t.Errorf("expected Location 'Almacén General' via legacy method, got %q", tool.Location)
	}
}

func TestUpsertToolsFromSync_EmptySlice(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Upsert empty slice
	err := store.UpsertToolsFromSync([]SyncDataItem{})
	if err != nil {
		t.Fatalf("failed to upsert empty sync data: %v", err)
	}

	// Verify no tools exist
	count, err := store.GetToolsCount()
	if err != nil {
		t.Fatalf("failed to get count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 tools, got %d", count)
	}
}

// === Normalized Schema Tests ===

// Test UpsertToolTags inserts new tags
func TestUpsertToolTags_Insert(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert a tool master record first
	_, err := db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (10, 'comp-001', 'SKU-001', 'Test Tool', 'Description', 'Warehouse A', ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert tool: %v", err)
	}

	tags := []ToolTagRecord{
		{
			ID:          1,
			ToolID:      10,
			UII:         "EPC-001",
			UnitNumber:  "001",
			Status:      "available",
			Location:    "Warehouse A",
			DisplayName: "Tool One",
			Active:      true,
		},
		{
			ID:          2,
			ToolID:      10,
			UII:         "EPC-002",
			UnitNumber:  "002",
			Status:      "in_use",
			Location:    "Line 1",
			DisplayName: "Tool Two",
			Active:      true,
		},
	}

	err = store.UpsertToolTags(tags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tags were inserted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM tool_tags").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count tags: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 tags, got %d", count)
	}
}

// Test UpsertToolTags updates existing tags
func TestUpsertToolTags_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert tool master and initial tag
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (10, 'comp-001', 'SKU-001', 'Test Tool', 'Description', 'Warehouse A', ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert tool: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, unit_number, status, location, display_name, active, last_synced_at)
		VALUES (1, 10, 'EPC-001', '001', 'available', 'Warehouse A', 'Old Name', 1, ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert tag: %v", err)
	}

	// Update via UpsertToolTags
	updatedTags := []ToolTagRecord{
		{
			ID:          1,
			ToolID:      10,
			UII:         "EPC-001",
			UnitNumber:  "001",
			Status:      "in_use",       // Changed
			Location:    "Line 1",       // Changed
			DisplayName: "Updated Name", // Changed
			Active:      false,          // Changed
		},
	}

	err = store.UpsertToolTags(updatedTags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tag was updated
	tag, err := store.GetToolTagByUII("EPC-001")
	if err != nil {
		t.Fatalf("failed to get tag: %v", err)
	}
	if tag.Status != "in_use" {
		t.Errorf("expected Status 'in_use', got %s", tag.Status)
	}
	if tag.Location != "Line 1" {
		t.Errorf("expected Location 'Line 1', got %s", tag.Location)
	}
	if tag.DisplayName != "Updated Name" {
		t.Errorf("expected DisplayName 'Updated Name', got %s", tag.DisplayName)
	}
	if tag.Active {
		t.Error("expected Active to be false")
	}
}

// Test GetToolTagByUII with existing tag
func TestGetToolTagByUII_Existing(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert tool master and tag
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (10, 'comp-001', 'SKU-001', 'Test Tool', 'Description', 'Warehouse A', ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert tool: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, unit_number, status, location, display_name, active, last_synced_at)
		VALUES (1, 10, 'EPC-001', '001', 'available', 'Warehouse A', 'Test Tag', 1, ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert tag: %v", err)
	}

	// Get the tag
	tag, err := store.GetToolTagByUII("EPC-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag == nil {
		t.Fatal("expected tag to be found")
	}

	if tag.ID != 1 {
		t.Errorf("expected ID 1, got %d", tag.ID)
	}
	if tag.ToolID != 10 {
		t.Errorf("expected ToolID 10, got %d", tag.ToolID)
	}
	if tag.UII != "EPC-001" {
		t.Errorf("expected UII 'EPC-001', got %s", tag.UII)
	}
	if tag.UnitNumber != "001" {
		t.Errorf("expected UnitNumber '001', got %s", tag.UnitNumber)
	}
	if tag.Status != "available" {
		t.Errorf("expected Status 'available', got %s", tag.Status)
	}
	if tag.Location != "Warehouse A" {
		t.Errorf("expected Location 'Warehouse A', got %s", tag.Location)
	}
	if tag.DisplayName != "Test Tag" {
		t.Errorf("expected DisplayName 'Test Tag', got %s", tag.DisplayName)
	}
	if !tag.Active {
		t.Error("expected Active to be true")
	}
}

// Test GetToolTagByUII with non-existent tag
func TestGetToolTagByUII_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	tag, err := store.GetToolTagByUII("NON-EXISTENT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != nil {
		t.Error("expected tag to be nil for non-existent UII")
	}
}

// Test GetToolTagsByToolID
func TestGetToolTagsByToolID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert tool masters
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES 
			(10, 'comp-001', 'SKU-001', 'Tool A', 'Description', 'Warehouse A', ?),
			(20, 'comp-001', 'SKU-002', 'Tool B', 'Description', 'Warehouse B', ?)
	`, now, now)
	if err != nil {
		t.Fatalf("failed to insert tools: %v", err)
	}

	// Insert tags for tool 10
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, unit_number, status, location, display_name, active, last_synced_at)
		VALUES 
			(1, 10, 'EPC-001', '001', 'available', 'Warehouse A', 'Tag 1', 1, ?),
			(2, 10, 'EPC-002', '002', 'in_use', 'Line 1', 'Tag 2', 1, ?),
			(3, 20, 'EPC-003', '001', 'available', 'Warehouse B', 'Tag 3', 1, ?)
	`, now, now, now)
	if err != nil {
		t.Fatalf("failed to insert tags: %v", err)
	}

	// Get tags for tool 10
	tags, err := store.GetToolTagsByToolID(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("expected 2 tags for tool 10, got %d", len(tags))
	}

	// Verify we got the right tags
	uiis := make(map[string]bool)
	for _, tag := range tags {
		uiis[tag.UII] = true
	}
	if !uiis["EPC-001"] || !uiis["EPC-002"] {
		t.Errorf("expected EPC-001 and EPC-002, got: %v", uiis)
	}
	if uiis["EPC-003"] {
		t.Error("EPC-003 should not be in results for tool 10")
	}
}

// Test UpsertToolsRecords inserts new records
func TestUpsertToolsRecords_Insert(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	tools := []ToolRecord{
		{
			ID:          1,
			CompanyID:   "comp-001",
			SKU:         "SKU-001",
			Name:        "Tool One",
			Description: "Description 1",
		},
		{
			ID:          2,
			CompanyID:   "comp-001",
			SKU:         "SKU-002",
			Name:        "Tool Two",
			Description: "Description 2",
		},
	}

	err := store.UpsertToolsRecords(tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify records were inserted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM tools").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count tools: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 tool records, got %d", count)
	}
}

// Test UpsertToolsRecords updates existing records
func TestUpsertToolsRecords_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert initial tool record
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (1, 'comp-001', 'SKU-001', 'Old Name', 'Old Desc', 'Old Location', ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert tool: %v", err)
	}

	// Update via UpsertToolsRecords
	updatedTools := []ToolRecord{
		{
			ID:                 1,
			CompanyID:          "comp-002",     // Changed
			SKU:                "SKU-001",
			Name:               "New Name",     // Changed
			Description:        "New Desc",     // Changed
			DefaultDestination: strPtr("New Location"), // Changed
		},
	}

	err = store.UpsertToolsRecords(updatedTools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify record was updated
	tool, err := store.GetToolRecordByID(1)
	if err != nil {
		t.Fatalf("failed to get tool: %v", err)
	}
	if tool.CompanyID != "comp-002" {
		t.Errorf("expected CompanyID 'comp-002', got %s", tool.CompanyID)
	}
	if tool.Name != "New Name" {
		t.Errorf("expected Name 'New Name', got %s", tool.Name)
	}
	if tool.Description != "New Desc" {
		t.Errorf("expected Description 'New Desc', got %s", tool.Description)
	}
	if tool.DefaultDestination == nil || *tool.DefaultDestination != "New Location" {
		t.Errorf("expected DefaultDestination 'New Location', got %v", tool.DefaultDestination)
	}
}

// Test GetToolRecordByID with existing record
func TestGetToolRecordByID_Existing(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	// Insert tool record
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (1, 'comp-001', 'SKU-001', 'Test Tool', 'Description', 'Warehouse A', ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert tool: %v", err)
	}

	// Get the record
	tool, err := store.GetToolRecordByID(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool == nil {
		t.Fatal("expected tool record to be found")
	}

	if tool.ID != 1 {
		t.Errorf("expected ID 1, got %d", tool.ID)
	}
	if tool.CompanyID != "comp-001" {
		t.Errorf("expected CompanyID 'comp-001', got %s", tool.CompanyID)
	}
	if tool.SKU != "SKU-001" {
		t.Errorf("expected SKU 'SKU-001', got %s", tool.SKU)
	}
	if tool.Name != "Test Tool" {
		t.Errorf("expected Name 'Test Tool', got %s", tool.Name)
	}
	if tool.Description != "Description" {
		t.Errorf("expected Description 'Description', got %s", tool.Description)
	}
	if tool.DefaultDestination == nil || *tool.DefaultDestination != "Warehouse A" {
		t.Errorf("expected DefaultDestination 'Warehouse A', got %v", tool.DefaultDestination)
	}
}

// Test GetToolRecordByID with non-existent record
func TestGetToolRecordByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := New(db)

	tool, err := store.GetToolRecordByID(999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != nil {
		t.Error("expected tool record to be nil for non-existent ID")
	}
}

// Helper function for string pointers
func strPtr(s string) *string {
	return &s
}
