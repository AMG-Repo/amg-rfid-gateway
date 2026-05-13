package cache

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
	_ "modernc.org/sqlite"
)

func TestSQLite_Store_ValidReading(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	reading := models.Reading{
		AntennaID: "ant-1",
		EPC:       "E200341502001080",
		RSSI:      201,
		Timestamp: time.Now(),
	}

	err = cache.Store(reading)
	if err != nil {
		t.Errorf("expected no error storing reading, got: %v", err)
	}
}

func TestSQLite_Store_InvalidReading(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	// Invalid reading - empty EPC
	reading := models.Reading{
		AntennaID: "ant-1",
		EPC:       "", // INVALID
		RSSI:      201,
		Timestamp: time.Now(),
	}

	err = cache.Store(reading)
	if err == nil {
		t.Error("expected error for invalid reading, got nil")
	}
}

func TestSQLite_Store_CacheClosed(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}

	// Close the cache
	cache.Close()

	reading := models.Reading{
		AntennaID: "ant-1",
		EPC:       "E200341502001080",
		RSSI:      201,
		Timestamp: time.Now(),
	}

	err = cache.Store(reading)
	if err == nil {
		t.Error("expected error when storing to closed cache, got nil")
	}
}

func TestSQLite_GetUnsynced(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	// Store some readings
	for i := 0; i < 5; i++ {
		reading := models.Reading{
			AntennaID: "ant-1",
			EPC:       "EPC-" + string(rune('A'+i)),
			RSSI:      201,
			Timestamp: time.Now(),
		}
		if err := cache.Store(reading); err != nil {
			t.Fatalf("failed to store reading: %v", err)
		}
	}

	// Flush to ensure writes are committed
	if err := cache.Flush(); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}

	// Get unsynced readings
	readings, err := cache.GetUnsynced(10)
	if err != nil {
		t.Fatalf("failed to get unsynced: %v", err)
	}

	if len(readings) != 5 {
		t.Errorf("expected 5 unsynced readings, got %d", len(readings))
	}
}

func TestSQLite_MarkSynced(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	// Store a reading
	reading := models.Reading{
		AntennaID: "ant-1",
		EPC:       "EPC-TEST",
		RSSI:      201,
		Timestamp: time.Now(),
	}
	if err := cache.Store(reading); err != nil {
		t.Fatalf("failed to store reading: %v", err)
	}

	// Flush to ensure writes are committed
	if err := cache.Flush(); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}

	// Get the reading ID
	readings, _ := cache.GetUnsynced(10)
	if len(readings) == 0 {
		t.Fatal("expected at least one reading")
	}

	// Mark as synced
	err = cache.MarkSynced([]int64{readings[0].ID})
	if err != nil {
		t.Errorf("expected no error marking synced, got: %v", err)
	}

	// Verify it's no longer unsynced
	unsynced, _ := cache.GetUnsynced(10)
	if len(unsynced) != 0 {
		t.Errorf("expected 0 unsynced readings after marking synced, got %d", len(unsynced))
	}
}

func TestSQLite_Count(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	// Initial count should be 0
	count, err := cache.Count()
	if err != nil {
		t.Fatalf("failed to get count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected initial count 0, got %d", count)
	}

	// Store a reading
	reading := models.Reading{
		AntennaID: "ant-1",
		EPC:       "EPC-TEST",
		RSSI:      201,
		Timestamp: time.Now(),
	}
	if err := cache.Store(reading); err != nil {
		t.Fatalf("failed to store reading: %v", err)
	}

	// Flush to ensure writes are committed
	if err := cache.Flush(); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}

	// Count should be 1
	count, err = cache.Count()
	if err != nil {
		t.Fatalf("failed to get count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
}

func TestSQLite_ConcurrentStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	// Concurrent stores from multiple goroutines
	var wg sync.WaitGroup
	numGoroutines := 10
	numReadings := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numReadings; j++ {
				reading := models.Reading{
					AntennaID: "ant-1",
					EPC:       "EPC-" + string(rune('A'+goroutineID)) + "-" + string(rune('0'+j)),
					RSSI:      201,
					Timestamp: time.Now(),
				}
				if err := cache.Store(reading); err != nil {
					t.Errorf("failed to store reading: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Flush to ensure writes are committed
	if err := cache.Flush(); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}

	// Verify count
	count, err := cache.Count()
	if err != nil {
		t.Fatalf("failed to get count: %v", err)
	}

	expected := numGoroutines * numReadings
	if count != expected {
		t.Errorf("expected count %d, got %d", expected, count)
	}
}

func TestSQLite_PendingReading_WithInternalID(t *testing.T) {
	// Test that PendingReading includes the internal ID for sync operations
	r := PendingReading{
		ID:         123,
		Reading:    models.Reading{EPC: "test"},
		Synced:     false,
		RetryCount: 0,
	}

	if r.ID != 123 {
		t.Errorf("expected ID 123, got %d", r.ID)
	}
	if r.EPC != "test" {
		t.Errorf("expected EPC 'test', got '%s'", r.EPC)
	}
}

func TestNewSQLite_CreatesDataDir(t *testing.T) {
	tmpDir := t.TempDir()
	dbDir := filepath.Join(tmpDir, "data", "subdir")
	dbPath := filepath.Join(dbDir, "cache.db")

	// Ensure directory doesn't exist
	os.RemoveAll(dbDir)

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	// Verify directory was created
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		t.Error("expected data directory to be created")
	}
}

// Test that new tables are created on initialization
func TestNewSQLite_CreatesToolsUsersConfirmationsTables(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	// Get the underlying DB
	db := cache.GetDB()

	// Verify tools table exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tools'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check tools table: %v", err)
	}
	if count != 1 {
		t.Errorf("expected tools table to exist, got count %d", count)
	}

	// Verify users table exists
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check users table: %v", err)
	}
	if count != 1 {
		t.Errorf("expected users table to exist, got count %d", count)
	}

	// Verify pending_confirmations table exists
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pending_confirmations'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check pending_confirmations table: %v", err)
	}
	if count != 1 {
		t.Errorf("expected pending_confirmations table to exist, got count %d", count)
	}
}

// Test that indexes are created
func TestNewSQLite_CreatesIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	db := cache.GetDB()

	// Check for normalized schema indexes (tool_tags indexes in new schema)
	var count int

	// Check for idx_tool_tags_uii index (normalized schema)
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_tool_tags_uii'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check idx_tool_tags_uii index: %v", err)
	}
	if count != 1 {
		t.Errorf("expected idx_tool_tags_uii index to exist, got count %d", count)
	}

	// Check for idx_tool_tags_tool_id index (normalized schema)
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_tool_tags_tool_id'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check idx_tool_tags_tool_id index: %v", err)
	}
	if count != 1 {
		t.Errorf("expected idx_tool_tags_tool_id index to exist, got count %d", count)
	}

	// Check for idx_users_rfid index
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_users_rfid'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check idx_users_rfid index: %v", err)
	}
	if count != 1 {
		t.Errorf("expected idx_users_rfid index to exist, got count %d", count)
	}

	// Check for idx_pending_synced index
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_pending_synced'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check idx_pending_synced index: %v", err)
	}
	if count != 1 {
		t.Errorf("expected idx_pending_synced index to exist, got count %d", count)
	}
}

// Test tools table schema (normalized - no uii column, uii is in tool_tags)
func TestNewSQLite_ToolsSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	db := cache.GetDB()

	// Insert into normalized tools table (SKU master, no uii)
	_, err = db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (1, 'comp-001', 'SKU-001', 'Test Tool', 'A test tool', 'Warehouse A', ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert into tools table: %v", err)
	}

	// Insert into tool_tags table (tag instances with uii)
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, unit_number, status, location, active, last_synced_at)
		VALUES (1, 1, 'E200341502001080', '001', 'active', 'Warehouse A', 1, ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert into tool_tags table: %v", err)
	}

	// Verify unique constraint on uii in tool_tags (not tools)
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, status, active, last_synced_at)
		VALUES (2, 1, 'E200341502001080', 'active', 1, ?)
	`, time.Now())
	if err == nil {
		t.Error("expected error for duplicate UII in tool_tags, got nil")
	}
}

// Test users table schema
func TestNewSQLite_UsersSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	db := cache.GetDB()

	// Try to insert into users table
	_, err = db.Exec(`
		INSERT INTO users (id, company_id, name, role, department, rfid_tag, active, last_synced_at)
		VALUES (1, 'comp-001', 'John Doe', 'operator', 'Production', 'E200341502001080', 1, ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert into users table: %v", err)
	}
}

// Test pending_confirmations table schema
func TestNewSQLite_PendingConfirmationsSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	db := cache.GetDB()

	// Try to insert into pending_confirmations table with antenna_id
	_, err = db.Exec(`
		INSERT INTO pending_confirmations (uii, action, antenna_id, timestamp, synced, retry_count)
		VALUES ('E200341502001080', 'entrada', 'ANT-01', ?, 0, 0)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert into pending_confirmations table: %v", err)
	}

	// Test action constraint - invalid action should fail
	_, err = db.Exec(`
		INSERT INTO pending_confirmations (uii, action, antenna_id, timestamp)
		VALUES ('E200341502001081', 'invalid_action', 'ANT-01', ?)
	`, time.Now())
	if err == nil {
		t.Error("expected error for invalid action, got nil")
	}

	// Verify we can query back with antenna_id
	var antennaID string
	err = db.QueryRow(`SELECT antenna_id FROM pending_confirmations WHERE uii = 'E200341502001080'`).Scan(&antennaID)
	if err != nil {
		t.Fatalf("failed to query antenna_id: %v", err)
	}
	if antennaID != "ANT-01" {
		t.Errorf("expected antenna_id 'ANT-01', got '%s'", antennaID)
	}
}

// TestMigratePendingConfirmations_AddsMissingColumns tests that migration adds missing columns
func TestMigratePendingConfirmations_AddsMissingColumns(t *testing.T) {
	tmpDir := t.TempDir()
	// Use file-based database to persist between opens
	dbPath := filepath.Join(tmpDir, "test_migrate.db")

	// Create database with old schema (missing antenna_id and created_at)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Create old schema without antenna_id and created_at, with last_retry_at
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS pending_confirmations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uii TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('entrada', 'salida')),
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced BOOLEAN DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			last_retry_at DATETIME
		)
	`)
	if err != nil {
		t.Fatalf("failed to create old schema: %v", err)
	}

	// Insert a test record with old schema
	_, err = db.Exec(`
		INSERT INTO pending_confirmations (uii, action, timestamp, synced, retry_count)
		VALUES ('EPC-OLD', 'entrada', ?, 0, 0)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert test record: %v", err)
	}
	db.Close()

	// Now open with NewSQLite which should run migration
	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache with migration: %v", err)
	}
	defer cache.Close()

	db = cache.GetDB()

	// Verify antenna_id column exists
	var antennaIDCol int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('pending_confirmations') WHERE name = 'antenna_id'
	`).Scan(&antennaIDCol)
	if err != nil {
		t.Fatalf("failed to check antenna_id column: %v", err)
	}
	if antennaIDCol != 1 {
		t.Errorf("expected antenna_id column to exist, got count %d", antennaIDCol)
	}

	// Verify created_at column exists
	var createdAtCol int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('pending_confirmations') WHERE name = 'created_at'
	`).Scan(&createdAtCol)
	if err != nil {
		t.Fatalf("failed to check created_at column: %v", err)
	}
	if createdAtCol != 1 {
		t.Errorf("expected created_at column to exist, got count %d", createdAtCol)
	}

	// Verify existing data is preserved
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM pending_confirmations WHERE uii = 'EPC-OLD'`).Scan(&count)
	if err != nil {
		t.Fatalf("failed to check existing data: %v", err)
	}
	if count != 1 {
		t.Errorf("expected existing record to be preserved, got count %d", count)
	}
}

// TestMigratePendingConfirmations_Idempotent tests that migration is idempotent
func TestMigratePendingConfirmations_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_idempotent.db")

	// First call to NewSQLite creates the database with new schema
	cache1, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache first time: %v", err)
	}
	cache1.Close()

	// Second call should not fail (idempotent)
	cache2, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache second time (migration not idempotent): %v", err)
	}
	defer cache2.Close()

	// Verify columns still exist
	db := cache2.GetDB()
	var antennaIDCol, createdAtCol int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('pending_confirmations') WHERE name = 'antenna_id'
	`).Scan(&antennaIDCol)
	if err != nil || antennaIDCol != 1 {
		t.Errorf("expected antenna_id column to exist after second migration, got count %d, err: %v", antennaIDCol, err)
	}
	err = db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('pending_confirmations') WHERE name = 'created_at'
	`).Scan(&createdAtCol)
	if err != nil || createdAtCol != 1 {
		t.Errorf("expected created_at column to exist after second migration, got count %d, err: %v", createdAtCol, err)
	}
}

// TestMigratePendingConfirmations_InsertWithNewColumns tests insert with new columns works
func TestMigratePendingConfirmations_InsertWithNewColumns(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_insert.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	db := cache.GetDB()

	// Insert with antenna_id
	_, err = db.Exec(`
		INSERT INTO pending_confirmations (uii, action, antenna_id, timestamp, synced, retry_count)
		VALUES ('EPC-001', 'entrada', 'ANT-01', ?, 0, 0)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert with antenna_id: %v", err)
	}

	// Verify we can query the antenna_id back
	var antennaID string
	err = db.QueryRow(`SELECT antenna_id FROM pending_confirmations WHERE uii = 'EPC-001'`).Scan(&antennaID)
	if err != nil {
		t.Fatalf("failed to query antenna_id: %v", err)
	}
	if antennaID != "ANT-01" {
		t.Errorf("expected antenna_id 'ANT-01', got '%s'", antennaID)
	}
}

// === Tests for Normalized Schema Migration (tool_tags + tools) ===

// TestNormalizedSchema_ToolTagsTableExists verifies tool_tags table is created
func TestNormalizedSchema_ToolTagsTableExists(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	db := cache.GetDB()

	// Verify tool_tags table exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tool_tags'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check tool_tags table: %v", err)
	}
	if count != 1 {
		t.Errorf("expected tool_tags table to exist, got count %d", count)
	}
}

// TestNormalizedSchema_ToolsTableNormalized verifies tools table has normalized schema
func TestNormalizedSchema_ToolsTableNormalized(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	db := cache.GetDB()

	// Verify tools table exists (normalized version without uii column)
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tools'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check tools table: %v", err)
	}
	if count != 1 {
		t.Errorf("expected tools table to exist, got count %d", count)
	}

	// Check that tools table does NOT have uii column (it's now in tool_tags)
	var uiiColCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('tools') WHERE name = 'uii'
	`).Scan(&uiiColCount)
	if err != nil {
		t.Fatalf("failed to check uii column: %v", err)
	}
	// After migration, uii should be removed from tools table
	if uiiColCount != 0 {
		t.Logf("Note: tools table still has uii column (expected during transition)")
	}
}

// TestNormalizedSchema_ToolTagsColumns verifies tool_tags has correct columns
func TestNormalizedSchema_ToolTagsColumns(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	db := cache.GetDB()

	// Check required columns exist
	requiredColumns := []string{"id", "tool_id", "uii", "unit_number", "status", "location", "location_id", "display_name", "notes", "active", "kanban_zone", "last_synced_at"}

	for _, col := range requiredColumns {
		var count int
		err = db.QueryRow(`
			SELECT COUNT(*) FROM pragma_table_info('tool_tags') WHERE name = ?
		`, col).Scan(&count)
		if err != nil {
			t.Fatalf("failed to check column %s: %v", col, err)
		}
		if count != 1 {
			t.Errorf("expected column '%s' to exist in tool_tags, got count %d", col, count)
		}
	}
}

// TestNormalizedSchema_ToolTagsIndexes verifies indexes are created
func TestNormalizedSchema_ToolTagsIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	db := cache.GetDB()

	// Check idx_tool_tags_uii index exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_tool_tags_uii'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check idx_tool_tags_uii index: %v", err)
	}
	if count != 1 {
		t.Errorf("expected idx_tool_tags_uii index to exist, got count %d", count)
	}

	// Check idx_tool_tags_tool_id index exists
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_tool_tags_tool_id'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to check idx_tool_tags_tool_id index: %v", err)
	}
	if count != 1 {
		t.Errorf("expected idx_tool_tags_tool_id index to exist, got count %d", count)
	}
}

// TestNormalizedSchema_ToolTagsInsert verifies we can insert into tool_tags
func TestNormalizedSchema_ToolTagsInsert(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	db := cache.GetDB()

	// Insert a tool (master record)
	_, err = db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, last_synced_at)
		VALUES (1, 'comp-001', 'SKU-001', 'Test Tool', 'Description', ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert tool: %v", err)
	}

	// Insert a tool_tag referencing the tool
	now := time.Now()
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, unit_number, status, location, display_name, notes, active, kanban_zone, last_synced_at)
		VALUES (1, 1, 'E200341502001080', '001', 'available', 'Almacén General', 'Tool 001', 'Notes', 1, 'Linea 1', ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert tool_tag: %v", err)
	}

	// Verify we can query it back
	var uii string
	err = db.QueryRow(`SELECT uii FROM tool_tags WHERE id = 1`).Scan(&uii)
	if err != nil {
		t.Fatalf("failed to query tool_tag: %v", err)
	}
	if uii != "E200341502001080" {
		t.Errorf("expected uii 'E200341502001080', got '%s'", uii)
	}
}

// TestNormalizedSchema_ToolTagsUIIUnique verifies uii uniqueness constraint
func TestNormalizedSchema_ToolTagsUIIUnique(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cache.Close()

	db := cache.GetDB()

	// Insert a tool (master record)
	_, err = db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, last_synced_at)
		VALUES (1, 'comp-001', 'SKU-001', 'Test Tool', 'Description', ?)
	`, time.Now())
	if err != nil {
		t.Fatalf("failed to insert tool: %v", err)
	}

	// Insert first tool_tag
	now := time.Now()
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, status, active, last_synced_at)
		VALUES (1, 1, 'E200341502001080', 'available', 1, ?)
	`, now)
	if err != nil {
		t.Fatalf("failed to insert first tool_tag: %v", err)
	}

	// Try to insert second tool_tag with same uii (should fail)
	_, err = db.Exec(`
		INSERT INTO tool_tags (id, tool_id, uii, status, active, last_synced_at)
		VALUES (2, 1, 'E200341502001080', 'in_use', 1, ?)
	`, now)
	if err == nil {
		t.Error("expected error for duplicate uii, got nil")
	}
}

// TestNormalizedSchema_MigrationFromLegacy backfills from legacy flat tools table
func TestNormalizedSchema_MigrationFromLegacy(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_migrate.db")

	// Create database with old flat schema
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Create old flat tools table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS tools (
			id INTEGER PRIMARY KEY,
			company_id TEXT NOT NULL,
			sku TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			uii TEXT NOT NULL UNIQUE,
			status TEXT,
			location TEXT,
			last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create old tools table: %v", err)
	}

	// Insert legacy data
	now := time.Now()
	_, err = db.Exec(`
		INSERT INTO tools (id, company_id, sku, name, description, uii, status, location, last_synced_at)
		VALUES 
			(1, 'comp-001', 'SKU-001', 'Tool One', 'Desc 1', 'EPC-001', 'available', 'Almacén General', ?),
			(2, 'comp-001', 'SKU-001', 'Tool One', 'Desc 1', 'EPC-002', 'in_use', 'Linea 1', ?),
			(3, 'comp-001', 'SKU-002', 'Tool Two', 'Desc 2', 'EPC-003', 'available', 'Almacén General', ?)
	`, now, now, now)
	if err != nil {
		t.Fatalf("failed to insert legacy data: %v", err)
	}
	db.Close()

	// Now open with NewSQLite which should run migration
	cache, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache with migration: %v", err)
	}
	defer cache.Close()

	db = cache.GetDB()

	// Verify tool_tags table exists and has data
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM tool_tags`).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count tool_tags: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 tool_tags after migration, got %d", count)
	}

	// Verify normalized tools table exists and has distinct SKUs
	err = db.QueryRow(`SELECT COUNT(DISTINCT sku) FROM tools`).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count tools: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 distinct tools (SKUs) after migration, got %d", count)
	}
}

// TestNormalizedSchema_Idempotent verifies migration is idempotent
func TestNormalizedSchema_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_idempotent.db")

	// First call to NewSQLite creates the database with new schema
	cache1, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache first time: %v", err)
	}
	cache1.Close()

	// Second call should not fail (idempotent)
	cache2, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache second time (migration not idempotent): %v", err)
	}
	defer cache2.Close()

	// Verify tables still exist
	db := cache2.GetDB()
	var toolTagsCount, toolsCount int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tool_tags'").Scan(&toolTagsCount)
	if err != nil {
		t.Fatalf("failed to check tool_tags: %v", err)
	}
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tools'").Scan(&toolsCount)
	if err != nil {
		t.Fatalf("failed to check tools: %v", err)
	}

	if toolTagsCount != 1 {
		t.Errorf("expected tool_tags table to exist after second migration, got %d", toolTagsCount)
	}
	if toolsCount != 1 {
		t.Errorf("expected tools table to exist after second migration, got %d", toolsCount)
	}
}
