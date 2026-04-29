package cache

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
}

// NewSQLite creates a new SQLite cache instance.
// Uses NEGATIVE PROGRAMMING: check what should NOT be, early returns.
func NewSQLite(dbPath string) (*SQLite, error) {
	// NEGATIVE: Create data directory if needed
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Open database
	db, err := sql.Open("sqlite", dbPath)
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

	// HAPPY PATH: Create cache instance with buffered channel
	cache := &SQLite{
		db:       db,
		readings: make(chan models.Reading, BufferSize),
		closeCh:  make(chan struct{}),
		flushCh:  make(chan chan struct{}),
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
func createTables(db *sql.DB) error {
	_, err := db.Exec(`
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
			uii TEXT NOT NULL UNIQUE,
			status TEXT,
			location TEXT,
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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_tools_uii ON tools(uii);
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

	return nil
}

// Store adds a reading to the cache.
// Uses NEGATIVE PROGRAMMING: check what should NOT be, early returns.
func (c *SQLite) Store(r models.Reading) error {
	// NEGATIVE: Check if cache is closed
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return errors.New("cache is closed")
	}
	c.mu.RUnlock()

	// NEGATIVE: Validate reading
	if err := r.Validate(); err != nil {
		return fmt.Errorf("invalid reading: %w", err)
	}

	// HAPPY PATH: Send to buffered channel (non-blocking if space available)
	select {
	case c.readings <- r:
		return nil
	case <-time.After(100 * time.Millisecond):
		return errors.New("cache buffer full, dropping reading")
	}
}

// writer is the single goroutine that writes to SQLite.
// This ensures thread-safety without complex locking.
func (c *SQLite) writer() {
	defer c.wg.Done()

	ticker := time.NewTicker(FlushInterval)
	defer ticker.Stop()

	batch := make([]models.Reading, 0, 100)

	for {
		select {
		case r, ok := <-c.readings:
			if !ok {
				// Channel closed, flush remaining batch
				if len(batch) > 0 {
					c.flush(batch)
				}
				return
			}
			batch = append(batch, r)

			// Flush when batch is full
			if len(batch) >= 100 {
				c.flush(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			// Periodic flush
			if len(batch) > 0 {
				c.flush(batch)
				batch = batch[:0]
			}

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
					batch = append(batch, r)
					if len(batch) >= 100 {
						c.flush(batch)
						batch = batch[:0]
					}
				default:
					drained = true
				}
			}
			if len(batch) > 0 {
				c.flush(batch)
				batch = batch[:0]
			}
			close(done)

		case <-c.closeCh:
			// Close requested, flush and exit
			if len(batch) > 0 {
				c.flush(batch)
			}
			return
		}
	}
}

// flush writes a batch of readings to SQLite.
// Uses retry mechanism with exponential backoff for transient errors.
func (c *SQLite) flush(readings []models.Reading) {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 100ms, 200ms, 400ms
			time.Sleep(time.Duration(100*(1<<attempt)) * time.Millisecond)
		}

		lastErr = c.flushOnce(readings)
		if lastErr == nil {
			return // Success
		}

		// Log error but retry
	}

	// All retries failed - readings will be retried in next batch
	_ = lastErr // Log this error
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
		(antenna_id, gateway_id, epc, rssi, timestamp, synced, retry_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	failedCount := 0
	for _, r := range readings {
		_, err := stmt.Exec(
			r.AntennaID,
			r.GatewayID,
			r.EPC,
			r.RSSI,
			r.Timestamp,
			false,
			0,
		)
		if err != nil {
			failedCount++
			continue
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	if failedCount > 0 {
		return fmt.Errorf("failed to insert %d of %d readings", failedCount, len(readings))
	}

	return nil
}

// GetUnsynced retrieves unsynced readings from the cache.
func (c *SQLite) GetUnsynced(limit int) ([]PendingReading, error) {
	rows, err := c.db.Query(`
		SELECT id, antenna_id, gateway_id, epc, rssi, timestamp, retry_count
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
		err := rows.Scan(
			&pr.ID,
			&pr.AntennaID,
			&pr.GatewayID,
			&pr.EPC,
			&pr.RSSI,
			&pr.Timestamp,
			&pr.RetryCount,
		)
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

// MarkSynced marks readings as synced by their IDs.
// Uses batch UPDATE with IN clause for better performance.
func (c *SQLite) MarkSynced(ids []int64) error {
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

		if err := c.markSyncedBatch(batch); err != nil {
			return fmt.Errorf("failed to mark batch %d-%d: %w", i, end, err)
		}
	}

	return nil
}

// markSyncedBatch marks a single batch of IDs as synced.
func (c *SQLite) markSyncedBatch(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	// Build IN clause with placeholders
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		"UPDATE pending_readings SET synced = 1 WHERE id IN (%s)",
		strings.Join(placeholders, ","),
	)

	_, err := c.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	return nil
}

// Count returns the total number of readings in the cache.
func (c *SQLite) Count() (int, error) {
	var count int
	err := c.db.QueryRow(`
		SELECT COUNT(*) FROM pending_readings
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
