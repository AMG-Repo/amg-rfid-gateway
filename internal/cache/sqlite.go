package cache

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/uuidutil"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
	_ "modernc.org/sqlite"
)

const (
	// BufferSize is the capacity of the buffered channel for async writes
	BufferSize = 1000

	// FlushInterval is how often to flush pending writes to SQLite
	FlushInterval = 1 * time.Second
)

// PendingReading extends models.Reading with cache-specific fields
type PendingReading struct {
	ID int64
	models.Reading
	Synced     bool
	RetryCount int
	CreatedAt  time.Time
}

// SQLite implements a thread-safe cache using SQLite with buffered channel
type SQLite struct {
	db       *sql.DB
	readings chan models.Reading
	closed   bool
	closeCh  chan struct{}
	flushCh  chan chan struct{}
	wg       sync.WaitGroup
	mu       sync.RWMutex

	// maxCarried bounds the readings the writer keeps after failed writes.
	maxCarried int
	// dropped counts readings lost in the writer; rejected counts Store
	// calls that timed out on a full buffer.
	dropped    atomic.Int64
	dropEvents atomic.Int64
	rejected   atomic.Int64
}

// MaxCarriedReadings is how many unwritten readings the writer keeps while
// SQLite writes fail; beyond it the oldest are dropped and counted.
const MaxCarriedReadings = 10 * BufferSize

// dropLogEvery rate-limits loss logs: the first event, then every Nth.
const dropLogEvery = 100

// DroppedReadings returns how many readings the writer lost.
func (c *SQLite) DroppedReadings() int64 { return c.dropped.Load() }

// BufferFullRejections returns how many Store calls failed on a full buffer.
func (c *SQLite) BufferFullRejections() int64 { return c.rejected.Load() }

// countDropped records lost readings and logs rate-limited.
func (c *SQLite) countDropped(n int, reason string) {
	if n <= 0 {
		return
	}
	total := c.dropped.Add(int64(n))
	if ev := c.dropEvents.Add(1); ev == 1 || ev%dropLogEvery == 0 {
		log.Printf("[cache] dropped %d reading(s): %s (total dropped: %d)", n, reason, total)
	}
}

// sqliteBusyTimeoutMs is how long a connection waits for a locked database.
const sqliteBusyTimeoutMs = 5000

// sqliteDSN adds per-connection pragmas to a file path; in-memory and URI
// paths are left as given.
func sqliteDSN(dbPath string) string {
	if dbPath == ":memory:" || strings.HasPrefix(dbPath, "file:") {
		return dbPath
	}
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)", dbPath, sqliteBusyTimeoutMs)
}

// NewSQLite creates a new SQLite cache instance.
// Uses NEGATIVE PROGRAMMING: check what should NOT be, early returns.
func NewSQLite(dbPath string) (*SQLite, error) {
	// NEGATIVE: Create data directory if needed
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Open database. busy_timeout goes in the DSN so every pooled connection
	// waits for a held write lock instead of failing with SQLITE_BUSY: the
	// reading cache, the unassigned inbox and the sync write concurrently.
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure SQLite for performance
	if _, err := db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;
		PRAGMA cache_size = -64000;
		PRAGMA temp_store = MEMORY;
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to configure database: %w", err)
	}

	// Create tables
	if err := createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	// Run migrations to ensure schema is up to date
	if err := migratePendingConfirmations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate pending_confirmations: %w", err)
	}

	// Run normalized schema migration for gateway sync contract fix
	if err := migrateNormalizedSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate normalized schema: %w", err)
	}

	// Run nullability migration: convert empty strings to NULL for optional fields
	if err := migrateNullableFields(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate nullable fields: %w", err)
	}

	// Kanban catalog + tag links (additive tables, rebuilt from sync)
	if err := migrateKanbanTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate kanban tables: %w", err)
	}

	// Reading sessions: last_seen and read_count on queued readings (additive)
	if err := migrateReadingSessionColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate reading session columns: %w", err)
	}

	// RSSI as signed dBm: convert positive magnitudes queued by older gateways
	if err := migrateSignedRSSI(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate rssi to signed dBm: %w", err)
	}

	// Gateway settings persisted across restarts (additive table)
	if err := migrateSettingsTable(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate settings table: %w", err)
	}

	// HAPPY PATH: Create cache instance with buffered channel
	cache := &SQLite{
		db:       db,
		readings: make(chan models.Reading, BufferSize),
		closeCh:  make(chan struct{}),
		flushCh:  make(chan chan struct{}),

		maxCarried: MaxCarriedReadings,
	}

	// Start background writer goroutine
	cache.wg.Add(1)
	go cache.writer()

	return cache, nil
}

// GetDB returns the underlying *sql.DB for direct access (e.g., localstore).
func (s *SQLite) GetDB() *sql.DB {
	return s.db
}

// createTables creates the database schema.
// Uses normalized schema (tools + tool_tags) for new databases.
func createTables(db *sql.DB) error {
	// Check if tools table already exists (could be legacy or normalized)
	var toolsExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='tools'
	`).Scan(&toolsExists)
	if err != nil {
		return fmt.Errorf("failed to check tools table existence: %w", err)
	}

	// If tools table exists, it might be legacy schema - let migrateNormalizedSchema handle it
	// Otherwise, create the normalized schema for new databases
	if toolsExists {
		// Table exists, migration will handle normalization if needed
		// Just ensure other tables exist
		_, err = db.Exec(`
			CREATE TABLE IF NOT EXISTS pending_readings (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				antenna_id TEXT NOT NULL,
				gateway_id TEXT NOT NULL,
				epc TEXT NOT NULL,
				rssi INTEGER NOT NULL,
				timestamp DATETIME NOT NULL,
				synced BOOLEAN DEFAULT 0,
				retry_count INTEGER DEFAULT 0,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);

			CREATE INDEX IF NOT EXISTS idx_pending_readings_synced 
			ON pending_readings(synced) WHERE synced = 0;

			CREATE INDEX IF NOT EXISTS idx_pending_readings_epc 
			ON pending_readings(epc);

			CREATE TABLE IF NOT EXISTS users (
				id INTEGER PRIMARY KEY,
				company_id TEXT NOT NULL,
				name TEXT NOT NULL,
				role TEXT,
				department TEXT,
				rfid_tag TEXT,
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
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				synced_at DATETIME,
				next_attempt_at DATETIME,
				confirmation_id TEXT,
				occurred_at TEXT,
				auto INTEGER NOT NULL DEFAULT 0
			);

			CREATE INDEX IF NOT EXISTS idx_users_rfid ON users(rfid_tag);
			CREATE INDEX IF NOT EXISTS idx_pending_synced ON pending_confirmations(synced);
		`)
		return err
	}

	// New database - create normalized schema directly
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS pending_readings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			antenna_id TEXT NOT NULL,
			gateway_id TEXT NOT NULL,
			epc TEXT NOT NULL,
			rssi INTEGER NOT NULL,
			timestamp DATETIME NOT NULL,
			synced BOOLEAN DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_pending_readings_synced 
		ON pending_readings(synced) WHERE synced = 0;

		CREATE INDEX IF NOT EXISTS idx_pending_readings_epc 
		ON pending_readings(epc);

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
			rfid_tag TEXT,
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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced_at DATETIME,
			next_attempt_at DATETIME,
			confirmation_id TEXT,
			occurred_at TEXT,
			auto INTEGER NOT NULL DEFAULT 0
		);

		CREATE INDEX IF NOT EXISTS idx_tool_tags_uii ON tool_tags(uii);
		CREATE INDEX IF NOT EXISTS idx_tool_tags_tool_id ON tool_tags(tool_id);
		CREATE INDEX IF NOT EXISTS idx_tools_sku ON tools(sku);
		CREATE INDEX IF NOT EXISTS idx_users_rfid ON users(rfid_tag);
		CREATE INDEX IF NOT EXISTS idx_pending_synced ON pending_confirmations(synced);
	`)
	return err
}

// migratePendingConfirmations adds missing columns to pending_confirmations table.
// It is idempotent - safe to run multiple times.
func migratePendingConfirmations(db *sql.DB) error {
	// Check if antenna_id column exists
	var antennaIDExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0 FROM pragma_table_info('pending_confirmations') WHERE name = 'antenna_id'
	`).Scan(&antennaIDExists)
	if err != nil {
		return fmt.Errorf("failed to check antenna_id column: %w", err)
	}

	// Add antenna_id column if missing
	if !antennaIDExists {
		_, err = db.Exec(`ALTER TABLE pending_confirmations ADD COLUMN antenna_id TEXT`)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				// Another process already added the column, that's fine
			} else {
				return fmt.Errorf("failed to add antenna_id column: %w", err)
			}
		}
	}

	// Check if created_at column exists
	var createdAtExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0 FROM pragma_table_info('pending_confirmations') WHERE name = 'created_at'
	`).Scan(&createdAtExists)
	if err != nil {
		return fmt.Errorf("failed to check created_at column: %w", err)
	}

	// Add created_at column if missing
	// Note: SQLite doesn't allow DEFAULT with CURRENT_TIMESTAMP in ALTER TABLE,
	// so we add without default, then update existing rows
	if !createdAtExists {
		_, err = db.Exec(`ALTER TABLE pending_confirmations ADD COLUMN created_at DATETIME`)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				// Another process already added the column, that's fine
			} else {
				return fmt.Errorf("failed to add created_at column: %w", err)
			}
		}
		// Set created_at to CURRENT_TIMESTAMP for existing rows
		_, err = db.Exec(`UPDATE pending_confirmations SET created_at = CURRENT_TIMESTAMP WHERE created_at IS NULL`)
		if err != nil {
			return fmt.Errorf("failed to update created_at values: %w", err)
		}
	}

	// Check if synced_at column exists (when the VPS accepted the confirmation)
	var syncedAtExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0 FROM pragma_table_info('pending_confirmations') WHERE name = 'synced_at'
	`).Scan(&syncedAtExists)
	if err != nil {
		return fmt.Errorf("failed to check synced_at column: %w", err)
	}

	// Add synced_at column if missing; existing synced rows keep it NULL and
	// are retained by created_at.
	if !syncedAtExists {
		_, err = db.Exec(`ALTER TABLE pending_confirmations ADD COLUMN synced_at DATETIME`)
		if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("failed to add synced_at column: %w", err)
		}
	}

	// Check if next_attempt_at column exists (retry backoff: when the sync worker
	// may send a failed confirmation again)
	var nextAttemptAtExists bool
	err = db.QueryRow(`
		SELECT COUNT(*) > 0 FROM pragma_table_info('pending_confirmations') WHERE name = 'next_attempt_at'
	`).Scan(&nextAttemptAtExists)
	if err != nil {
		return fmt.Errorf("failed to check next_attempt_at column: %w", err)
	}

	// Add next_attempt_at column if missing; existing rows keep it NULL, which
	// means eligible now.
	if !nextAttemptAtExists {
		_, err = db.Exec(`ALTER TABLE pending_confirmations ADD COLUMN next_attempt_at DATETIME`)
		if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("failed to add next_attempt_at column: %w", err)
		}
	}

	if err := migrateConfirmationIdempotency(db); err != nil {
		return err
	}
	return migrateAutoConfirmation(db)
}

// migrateAutoConfirmation adds auto (1 when the gateway confirmed the movement
// by itself, 0 for operator confirmations) to pending_confirmations. Existing
// rows were all confirmed by an operator. It is idempotent - safe to run
// multiple times.
func migrateAutoConfirmation(db *sql.DB) error {
	var exists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0 FROM pragma_table_info('pending_confirmations') WHERE name = 'auto'
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check auto column: %w", err)
	}
	if !exists {
		_, err = db.Exec(`ALTER TABLE pending_confirmations ADD COLUMN auto INTEGER NOT NULL DEFAULT 0`)
		if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("failed to add auto column: %w", err)
		}
	}
	return nil
}

// migrateConfirmationIdempotency adds confirmation_id (the idempotency key the
// VPS dedupes replays by) and occurred_at (when the operator confirmed, RFC3339
// UTC) to pending_confirmations. Unsynced rows queued before the upgrade get a
// random UUID once, so every resend carries the same id; synced rows and
// occurred_at stay NULL. It is idempotent - safe to run multiple times.
func migrateConfirmationIdempotency(db *sql.DB) error {
	for _, col := range []string{"confirmation_id", "occurred_at"} {
		var exists bool
		err := db.QueryRow(`
			SELECT COUNT(*) > 0 FROM pragma_table_info('pending_confirmations') WHERE name = ?
		`, col).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check %s column: %w", col, err)
		}
		if !exists {
			_, err = db.Exec(`ALTER TABLE pending_confirmations ADD COLUMN ` + col + ` TEXT`)
			if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("failed to add %s column: %w", col, err)
			}
		}
	}

	rows, err := db.Query(`SELECT id FROM pending_confirmations WHERE synced = 0 AND confirmation_id IS NULL`)
	if err != nil {
		return fmt.Errorf("failed to list confirmations without id: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan confirmation id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to list confirmations without id: %w", err)
	}
	for _, id := range ids {
		// The IS NULL guard keeps an id another process assigned meanwhile.
		_, err := db.Exec(`UPDATE pending_confirmations SET confirmation_id = ? WHERE id = ? AND confirmation_id IS NULL`, uuidutil.NewV4(), id)
		if err != nil {
			return fmt.Errorf("failed to assign confirmation_id to %d: %w", id, err)
		}
	}

	_, err = db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_pending_confirmation_id
		ON pending_confirmations(confirmation_id) WHERE confirmation_id IS NOT NULL
	`)
	if err != nil {
		return fmt.Errorf("failed to create confirmation_id index: %w", err)
	}
	return nil
}

// migrateNormalizedSchema migrates from flat tools table to normalized tools + tool_tags schema.
// This is idempotent and safe to run multiple times.
func migrateNormalizedSchema(db *sql.DB) error {
	// Check if tool_tags table already exists
	var toolTagsExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='tool_tags'
	`).Scan(&toolTagsExists)
	if err != nil {
		return fmt.Errorf("failed to check tool_tags table existence: %w", err)
	}

	// If tool_tags already exists, migration is complete
	if toolTagsExists {
		return nil
	}

	// Start transaction for migration
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Step 1: Check if we need to migrate from legacy flat schema
	var legacyToolsExists bool
	err = tx.QueryRow(`
		SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='tools'
	`).Scan(&legacyToolsExists)
	if err != nil {
		return fmt.Errorf("failed to check legacy tools table: %w", err)
	}

	var hasUIIColumn bool
	if legacyToolsExists {
		// Check if legacy table has uii column (flat schema)
		err = tx.QueryRow(`
			SELECT COUNT(*) > 0 FROM pragma_table_info('tools') WHERE name = 'uii'
		`).Scan(&hasUIIColumn)
		if err != nil {
			return fmt.Errorf("failed to check uii column: %w", err)
		}
	}

	// Step 2: If legacy flat schema exists, migrate it
	if legacyToolsExists && hasUIIColumn {
		// Rename old table to preserve data
		_, err = tx.Exec(`ALTER TABLE tools RENAME TO tools_legacy`)
		if err != nil {
			return fmt.Errorf("failed to rename legacy tools table: %w", err)
		}

		// Create normalized tools table (SKU master, no uii column)
		_, err = tx.Exec(`
			CREATE TABLE tools (
				id INTEGER PRIMARY KEY,
				company_id TEXT NOT NULL,
				sku TEXT NOT NULL,
				name TEXT NOT NULL,
				description TEXT,
				default_destination TEXT,
				last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create normalized tools table: %w", err)
		}

		// Create index on sku (not unique - multiple tools can share same SKU)
		_, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tools_sku ON tools(sku)`)
		if err != nil {
			return fmt.Errorf("failed to create sku index: %w", err)
		}

		// Migrate distinct SKU data from legacy to normalized tools
		_, err = tx.Exec(`
			INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
			SELECT DISTINCT id, company_id, sku, name, description, location, last_synced_at 
			FROM tools_legacy
		`)
		if err != nil {
			return fmt.Errorf("failed to migrate tools data: %w", err)
		}

		// Create tool_tags table
		_, err = tx.Exec(`
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
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create tool_tags table: %w", err)
		}

		// Create indexes on tool_tags
		_, err = tx.Exec(`
			CREATE UNIQUE INDEX idx_tool_tags_uii ON tool_tags(uii);
			CREATE INDEX idx_tool_tags_tool_id ON tool_tags(tool_id)
		`)
		if err != nil {
			return fmt.Errorf("failed to create tool_tags indexes: %w", err)
		}

		// Migrate tag data from legacy to tool_tags
		_, err = tx.Exec(`
			INSERT INTO tool_tags (id, tool_id, uii, unit_number, status, location, active, last_synced_at)
			SELECT id, id, uii, NULL, status, location, 1, last_synced_at 
			FROM tools_legacy
			WHERE uii IS NOT NULL AND uii != ''
		`)
		if err != nil {
			return fmt.Errorf("failed to migrate legacy data to tool_tags: %w", err)
		}

		// Drop legacy table
		_, err = tx.Exec(`DROP TABLE tools_legacy`)
		if err != nil {
			return fmt.Errorf("failed to drop legacy tools table: %w", err)
		}
	} else {
		// No legacy data to migrate, just create the new tables if they don't exist

		// Create normalized tools table if it doesn't exist
		if !legacyToolsExists {
			_, err = tx.Exec(`
				CREATE TABLE IF NOT EXISTS tools (
					id INTEGER PRIMARY KEY,
					company_id TEXT NOT NULL,
					sku TEXT NOT NULL,
					name TEXT NOT NULL,
					description TEXT,
					default_destination TEXT,
					last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
				)
			`)
			if err != nil {
				return fmt.Errorf("failed to create tools table: %w", err)
			}

			// Create index on sku (not unique - multiple tools can share same SKU)
			_, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_tools_sku ON tools(sku)`)
			if err != nil {
				return fmt.Errorf("failed to create sku index: %w", err)
			}
		}

		// Create tool_tags table
		_, err = tx.Exec(`
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
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create tool_tags table: %w", err)
		}

		// Create indexes on tool_tags
		_, err = tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_tool_tags_uii ON tool_tags(uii);
			CREATE INDEX IF NOT EXISTS idx_tool_tags_tool_id ON tool_tags(tool_id)
		`)
		if err != nil {
			return fmt.Errorf("failed to create tool_tags indexes: %w", err)
		}
	}

	return tx.Commit()
}

// migrateNullableFields converts empty strings to NULL for nullable fields.
// This is idempotent and safe to run multiple times.
func migrateNullableFields(db *sql.DB) error {
	// Check if tools table exists
	var toolsExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='tools'
	`).Scan(&toolsExists)
	if err != nil {
		return fmt.Errorf("failed to check tools table existence: %w", err)
	}

	if !toolsExists {
		return nil // No tables yet, nothing to migrate
	}

	// Run migration inside a transaction
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Convert empty strings to NULL in tools.description
	_, err = tx.Exec(`UPDATE tools SET description = NULL WHERE description = ''`)
	if err != nil {
		return fmt.Errorf("failed to normalize tools.description: %w", err)
	}

	// Convert empty strings to NULL in tool_tags.location
	_, err = tx.Exec(`UPDATE tool_tags SET location = NULL WHERE location = ''`)
	if err != nil {
		return fmt.Errorf("failed to normalize tool_tags.location: %w", err)
	}

	// Convert empty strings to NULL in tool_tags.unit_number
	_, err = tx.Exec(`UPDATE tool_tags SET unit_number = NULL WHERE unit_number = ''`)
	if err != nil {
		return fmt.Errorf("failed to normalize tool_tags.unit_number: %w", err)
	}

	// Convert empty strings to NULL in tool_tags.display_name
	_, err = tx.Exec(`UPDATE tool_tags SET display_name = NULL WHERE display_name = ''`)
	if err != nil {
		return fmt.Errorf("failed to normalize tool_tags.display_name: %w", err)
	}

	return tx.Commit()
}

// migrateKanbanTables creates the kanban cache tables. They are additive and
// independent from tool_tags, so databases created before kanbans existed
// upgrade in place without touching tool_tags or pending_confirmations.
// kanbans and tag_kanban_links are a read-only copy of VPS data rebuilt on
// every sync; unassigned_tags is the local inbox of reads with no kanban.
// It is idempotent - safe to run multiple times.
// migrateReadingSessionColumns adds last_seen and read_count to
// pending_readings when missing. Existing rows keep NULL / 0: plain readings.
func migrateReadingSessionColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(pending_readings)`)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for rows.Next() {
		var (
			cid, notNull, pk int
			name, typ        string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		have[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if !have["last_seen"] {
		if _, err := db.Exec(`ALTER TABLE pending_readings ADD COLUMN last_seen DATETIME`); err != nil {
			return err
		}
	}
	if !have["read_count"] {
		if _, err := db.Exec(`ALTER TABLE pending_readings ADD COLUMN read_count INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	return nil
}

// migrateSignedRSSI converts pending_readings rows queued before rssi became
// signed dBm (larger = stronger, 0 = unknown) from the old positive magnitude.
// Assumption: queued rows come from Chainway, whose old rssi was the dBm
// magnitude (66 means -66 dBm); the only deployed gateway is a Chainway UR4.
// It is idempotent: new rows are always <= 0, so a second run matches nothing,
// and unknown (0) or already signed rows are never touched.
func migrateSignedRSSI(db *sql.DB) error {
	res, err := db.Exec(`UPDATE pending_readings SET rssi = -rssi WHERE rssi > 0`)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		fmt.Fprintf(os.Stderr, "[cache] converted %d queued readings to signed dBm rssi\n", n)
	}
	return nil
}

// migrateSettingsTable creates the key/value table for gateway settings changed
// at runtime (e.g. the automatic confirmation mode). It is additive.
func migrateSettingsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create settings table: %w", err)
	}
	return nil
}

func migrateKanbanTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS kanbans (
			code TEXT PRIMARY KEY,
			description TEXT,
			standard_pack REAL,
			unit TEXT,
			zones TEXT,
			extrusora TEXT,
			position TEXT,
			area TEXT,
			customer TEXT,
			card_type TEXT,
			sku TEXT,
			last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS tag_kanban_links (
			uii TEXT PRIMARY KEY,
			kanban_code TEXT NOT NULL,
			last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_tag_kanban_links_code ON tag_kanban_links(kanban_code);

		CREATE TABLE IF NOT EXISTS unassigned_tags (
			epc TEXT PRIMARY KEY,
			antenna_id TEXT,
			first_seen DATETIME NOT NULL,
			last_seen DATETIME NOT NULL,
			read_count INTEGER NOT NULL DEFAULT 0
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create kanban tables: %w", err)
	}
	return nil
}

// Store adds a reading to the cache.
// Uses NEGATIVE PROGRAMMING: check what should NOT be, early returns.
func (c *SQLite) Store(r models.Reading) error {
	// NEGATIVE: Validate reading
	if err := r.Validate(); err != nil {
		return fmt.Errorf("invalid reading: %w", err)
	}

	// Hold the read lock through the send: Close takes the write lock, so it
	// waits for in-flight Stores and no reading is sent after the writer's
	// final drain (lost) or after the channel is closed (panic).
	c.mu.RLock()
	defer c.mu.RUnlock()
	// NEGATIVE: Check if cache is closed
	if c.closed {
		return errors.New("cache is closed")
	}

	// HAPPY PATH: Send to buffered channel (non-blocking if space available)
	select {
	case c.readings <- r:
		return nil
	case <-time.After(100 * time.Millisecond):
		// Callers such as the session aggregator may retry, so this is a
		// rejection, not necessarily a loss.
		if n := c.rejected.Add(1); n == 1 || n%dropLogEvery == 0 {
			log.Printf("[cache] buffer full, rejected reading (total rejections: %d)", n)
		}
		return errors.New("cache buffer full, dropping reading")
	}
}

// writer is the single goroutine that writes to SQLite.
// This ensures thread-safety without complex locking. A batch that fails to
// write is kept and retried on the next tick or flush, bounded by maxCarried.
func (c *SQLite) writer() {
	defer c.wg.Done()

	ticker := time.NewTicker(FlushInterval)
	defer ticker.Stop()

	batch := make([]models.Reading, 0, 100)
	// failing pauses size-triggered flushes while writes fail, so the
	// backoff runs once per tick instead of once per reading.
	failing := false

	add := func(r models.Reading) {
		batch = append(batch, r)
		if over := len(batch) - c.maxCarried; over > 0 {
			c.countDropped(over, "write retry buffer full, oldest discarded")
			batch = append(batch[:0], batch[over:]...)
		}
	}
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if c.flush(batch) {
			batch = batch[:0]
			failing = false
			return
		}
		failing = true
	}
	final := func() {
		flush()
		c.countDropped(len(batch), "write failed at shutdown")
	}

	for {
		select {
		case r, ok := <-c.readings:
			if !ok {
				final()
				return
			}
			add(r)

			// Flush when batch is full
			if !failing && len(batch) >= 100 {
				flush()
			}

		case <-ticker.C:
			flush()

		case done := <-c.flushCh:
			// Explicit flush requested, drain channel and flush all pending readings
			drained := false
			for !drained {
				select {
				case r, ok := <-c.readings:
					if !ok {
						drained = true
						break
					}
					add(r)
					if !failing && len(batch) >= 100 {
						flush()
					}
				default:
					drained = true
				}
			}
			flush()
			close(done)

		case <-c.closeCh:
			// Take readings still buffered so they are written or counted.
			for drained := false; !drained; {
				select {
				case r, ok := <-c.readings:
					if !ok {
						drained = true
						break
					}
					add(r)
				default:
					drained = true
				}
			}
			final()
			return
		}
	}
}

// flush writes a batch of readings to SQLite and reports whether it was
// written. Retries with exponential backoff for transient errors; on failure
// the caller keeps the batch.
func (c *SQLite) flush(readings []models.Reading) bool {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 200ms, 400ms
			time.Sleep(time.Duration(100*(1<<attempt)) * time.Millisecond)
		}

		lastErr = c.flushOnce(readings)
		if lastErr == nil {
			return true
		}
	}

	log.Printf("[cache] failed to write %d reading(s), keeping them for retry: %v", len(readings), lastErr)
	return false
}

// flushOnce performs a single flush attempt.
func (c *SQLite) flushOnce(readings []models.Reading) error {
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO pending_readings 
		(antenna_id, gateway_id, epc, rssi, timestamp, synced, retry_count, last_seen, read_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// All-or-nothing: a failed row aborts the transaction so a retry does not
	// duplicate the rows that did insert.
	for _, r := range readings {
		_, err := stmt.Exec(
			r.AntennaID,
			r.GatewayID,
			r.EPC,
			r.RSSI,
			r.Timestamp,
			false,
			0,
			sessionLastSeen(r),
			r.ReadCount,
		)
		if err != nil {
			return fmt.Errorf("failed to insert reading %s: %w", r.EPC, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

// GetUnsynced retrieves unsynced readings from the cache.
func (c *SQLite) GetUnsynced(limit int) ([]PendingReading, error) {
	rows, err := c.db.Query(`
		SELECT id, antenna_id, gateway_id, epc, rssi, timestamp, retry_count, last_seen, read_count
		FROM pending_readings
		WHERE synced = 0
		ORDER BY timestamp ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query unsynced: %w", err)
	}
	defer rows.Close()

	var readings []PendingReading
	for rows.Next() {
		var pr PendingReading
		var lastSeen sql.NullTime
		err := rows.Scan(
			&pr.ID,
			&pr.AntennaID,
			&pr.GatewayID,
			&pr.EPC,
			&pr.RSSI,
			&pr.Timestamp,
			&pr.RetryCount,
			&lastSeen,
			&pr.ReadCount,
		)
		if lastSeen.Valid {
			pr.LastSeen = lastSeen.Time
		}
		if err != nil {
			// Log scan error but continue processing other rows
			// We use fmt.Printf here since we don't have access to a logger in this context
			fmt.Fprintf(os.Stderr, "[cache] failed to scan pending_reading row: %v\n", err)
			continue
		}
		pr.Synced = false
		readings = append(readings, pr)
	}

	return readings, rows.Err()
}

// pending_readings.synced values.
const (
	readingPending   = 0 // not yet delivered; returned by GetUnsynced and counted by Count
	readingDelivered = 1 // stored by the VPS; purged after ReadingRetention
	readingRejected  = 2 // rejected terminally by the VPS; never resent nor purged
)

// MarkSynced marks readings as synced by their IDs.
// Uses batch UPDATE with IN clause for better performance.
func (c *SQLite) MarkSynced(ids []int64) error {
	return c.markReadings(ids, readingDelivered)
}

// MarkRejected marks readings the VPS rejected terminally. They leave the
// queue but are kept locally: the purge never deletes them.
func (c *SQLite) MarkRejected(ids []int64) error {
	return c.markReadings(ids, readingRejected)
}

// markReadings sets synced to state for the given IDs in batches.
func (c *SQLite) markReadings(ids []int64, state int) error {
	if len(ids) == 0 {
		return nil
	}

	// SQLite has limit of 999 variables per query, so batch if needed
	const batchSize = 500

	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]

		if err := c.markBatch(batch, state); err != nil {
			return fmt.Errorf("failed to mark batch %d-%d: %w", i, end, err)
		}
	}

	return nil
}

// markBatch sets synced to state for a single batch of IDs.
func (c *SQLite) markBatch(ids []int64, state int) error {
	if len(ids) == 0 {
		return nil
	}

	// Build IN clause with placeholders
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, state)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(
		"UPDATE pending_readings SET synced = ? WHERE id IN (%s)",
		strings.Join(placeholders, ","),
	)

	_, err := c.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	return nil
}

// ReadingRetention is how long readings already synced to the VPS are kept locally.
const ReadingRetention = 7 * 24 * time.Hour

// purgeBatchSize bounds each purge DELETE so a large backlog does not hold the
// write lock for long.
const purgeBatchSize = 5000

// PurgeSyncedReadings deletes synced readings stored before cutoff and returns
// how many were removed. Unsynced and rejected readings are never deleted.
func (c *SQLite) PurgeSyncedReadings(cutoff time.Time) (int64, error) {
	// created_at uses the CURRENT_TIMESTAMP layout, so a string comparison works.
	cutoffText := cutoff.UTC().Format("2006-01-02 15:04:05")
	var total int64
	for {
		res, err := c.db.Exec(`
			DELETE FROM pending_readings
			WHERE id IN (
				SELECT id FROM pending_readings
				WHERE synced = 1 AND created_at < ?
				LIMIT ?
			)
		`, cutoffText, purgeBatchSize)
		if err != nil {
			return total, fmt.Errorf("failed to purge synced readings: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("failed to purge synced readings: %w", err)
		}
		total += n
		if n < purgeBatchSize {
			return total, nil
		}
	}
}

// Count returns the number of readings not yet synced to the VPS.
func (c *SQLite) Count() (int, error) {
	var count int
	err := c.db.QueryRow(`
		SELECT COUNT(*) FROM pending_readings WHERE synced = 0
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count: %w", err)
	}
	return count, nil
}

// Flush forces any pending writes to be committed immediately.
func (c *SQLite) Flush() error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return errors.New("cache is closed")
	}
	c.mu.RUnlock()

	// Signal writer to flush and wait for completion
	done := make(chan struct{})
	select {
	case c.flushCh <- done:
		<-done
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("flush timeout")
	}
}

// Close shuts down the cache gracefully.
func (c *SQLite) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	// Signal writer to stop
	close(c.closeCh)

	// Wait for writer to finish
	c.wg.Wait()

	// Close channel and database
	close(c.readings)

	return c.db.Close()
}

// sessionLastSeen stores NULL for a plain reading, so it reads back as zero.
func sessionLastSeen(r models.Reading) any {
	if r.LastSeen.IsZero() {
		return nil
	}
	return r.LastSeen
}
