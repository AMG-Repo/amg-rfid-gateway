// Package localstore provides local SQLite storage operations for tools, users, and confirmations.
package localstore

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"
)

// Tool represents a tool from VPS cached locally (legacy flat schema).
// Deprecated: Use ToolRecord and ToolTagRecord for normalized schema.
type Tool struct {
	ID           int64     `json:"id"`
	CompanyID    string    `json:"company_id"`
	SKU          string    `json:"sku"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	UII          string    `json:"uii"`
	Status       string    `json:"status"`
	Location     string    `json:"location"`
	LastSyncedAt time.Time `json:"last_synced_at"`
}

// ToolRecord represents a normalized tool master record (SKU level).
type ToolRecord struct {
	ID                 int64     `json:"id"`
	CompanyID          string    `json:"company_id"`
	SKU                string    `json:"sku"`
	Name               string    `json:"name"`
	Description        *string   `json:"description,omitempty"`
	DefaultDestination *string   `json:"default_destination,omitempty"`
	LastSyncedAt       time.Time `json:"last_synced_at"`
}

// ToolTagRecord represents a normalized tool tag record (individual RFID instance).
type ToolTagRecord struct {
	ID           int64     `json:"id"`
	ToolID       int64     `json:"tool_id"`
	UII          string    `json:"uii"`
	UnitNumber   *string   `json:"unit_number,omitempty"`
	Status       string    `json:"status"`
	Location     *string   `json:"location,omitempty"`
	LocationID   *int64    `json:"location_id,omitempty"`
	DisplayName  *string   `json:"display_name,omitempty"`
	Notes        *string   `json:"notes,omitempty"`
	Active       bool      `json:"active"`
	KanbanZone   *string   `json:"kanban_zone,omitempty"`
	LastSyncedAt time.Time `json:"last_synced_at"`
}

// SyncDataItem represents a flattened tool+tag row from gateway sync-data endpoint.
// This mirrors the VPS normalized schema (tool_tags JOIN tools).
type SyncDataItem struct {
	ID              int64   `json:"id"`
	ToolID          int64   `json:"tool_id"`
	UII             string  `json:"uii"`
	SKU             string  `json:"sku"`
	Name            string  `json:"name"`
	Description     *string `json:"description,omitempty"`
	Status          string  `json:"status"`
	Location        *string `json:"location,omitempty"`
	LocationID      *int64  `json:"location_id,omitempty"`
	UnitNumber      *string `json:"unit_number,omitempty"`
	DisplayName     *string `json:"display_name,omitempty"`
	Notes           *string `json:"notes,omitempty"`
	Active          bool    `json:"active"`
	KanbanZone      *string `json:"kanban_zone,omitempty"`
	ToolDestination *string `json:"tool_destination,omitempty"`
	// Kanban is the kanban the tag is assigned to (nil when unassigned or when
	// the VPS predates kanbans). Optional: old payloads without it keep working.
	Kanban *Kanban `json:"kanban,omitempty"`
}

// User represents a user from VPS cached locally.
type User struct {
	ID           int64     `json:"id"`
	CompanyID    string    `json:"company_id"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Department   string    `json:"department"`
	RFIDTag      string    `json:"rfid_tag"`
	Active       bool      `json:"active"`
	LastSyncedAt time.Time `json:"last_synced_at"`
}

// ConfirmationRetention is how long synced confirmations are kept locally.
// Pending (unsynced) confirmations are never purged.
const ConfirmationRetention = 30 * 24 * time.Hour

// sqliteTimestamp is the layout of CURRENT_TIMESTAMP, shared by created_at and
// synced_at so retention comparisons are plain string comparisons.
const sqliteTimestamp = "2006-01-02 15:04:05"

// PendingConfirmation represents a confirmation awaiting VPS sync.
type PendingConfirmation struct {
	ID         int64     `json:"id"`
	UII        string    `json:"uii"`
	Action     string    `json:"action"` // "entrada", "salida"
	AntennaID  string    `json:"antenna_id"`
	Timestamp  time.Time `json:"timestamp"`
	Synced     bool      `json:"synced"`
	RetryCount int       `json:"retry_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// LocalStore provides SQLite operations for local verification.
type LocalStore struct {
	db *sql.DB
}

// New creates a new LocalStore using existing DB connection.
func New(db *sql.DB) *LocalStore {
	return &LocalStore{db: db}
}

// toNullString converts a *string to sql.NullString.
// nil -> SQL NULL, non-nil -> the string value (including empty string).
func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *s, Valid: true}
}

// scanNullStringPtr scans a sql.NullString into a *string.
// Valid=false -> nil, Valid=true -> pointer to the string value.
func scanNullStringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

// GetToolByUII retrieves a tool by UII (EPC) using normalized schema.
// Performs a JOIN between tool_tags and tools to get complete metadata.
// Legacy adapter: NULL values are mapped to empty strings for backward compatibility.
func (s *LocalStore) GetToolByUII(uii string) (*Tool, error) {
	row := s.db.QueryRow(`
		SELECT tt.id, t.company_id, t.sku, t.name, t.description, tt.uii, tt.status, tt.location, tt.last_synced_at
		FROM tool_tags tt
		JOIN tools t ON tt.tool_id = t.id
		WHERE tt.uii = ?
	`, uii)

	var tool Tool
	var description, location sql.NullString
	err := row.Scan(
		&tool.ID,
		&tool.CompanyID,
		&tool.SKU,
		&tool.Name,
		&description,
		&tool.UII,
		&tool.Status,
		&location,
		&tool.LastSyncedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Adapter boundary: NULL -> ""
	tool.Description = description.String // Empty string if NULL
	tool.Location = location.String       // Empty string if NULL

	return &tool, nil
}

// GetUserByRFIDTag retrieves a user by RFID tag.
func (s *LocalStore) GetUserByRFIDTag(rfidTag string) (*User, error) {
	row := s.db.QueryRow(`
		SELECT id, company_id, name, role, department, rfid_tag, active, last_synced_at
		FROM users
		WHERE rfid_tag = ?
	`, rfidTag)

	var user User
	err := row.Scan(
		&user.ID,
		&user.CompanyID,
		&user.Name,
		&user.Role,
		&user.Department,
		&user.RFIDTag,
		&user.Active,
		&user.LastSyncedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UpsertTools batch upserts tools from VPS sync.
// Uses normalized schema: upserts tool_tags (tag data) and tools (master data).
func (s *LocalStore) UpsertTools(tools []Tool) error {
	if len(tools) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Prepare statements for normalized schema
	toolStmt, err := tx.Prepare(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			company_id = excluded.company_id,
			sku = excluded.sku,
			name = excluded.name,
			description = excluded.description,
			default_destination = excluded.default_destination,
			last_synced_at = excluded.last_synced_at
	`)
	if err != nil {
		return err
	}
	defer toolStmt.Close()

	tagStmt, err := tx.Prepare(`
		INSERT INTO tool_tags (id, tool_id, uii, status, location, active, last_synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(uii) DO UPDATE SET
			tool_id = excluded.tool_id,
			status = excluded.status,
			location = excluded.location,
			active = excluded.active,
			last_synced_at = excluded.last_synced_at
	`)
	if err != nil {
		return err
	}
	defer tagStmt.Close()

	now := time.Now()
	for _, tool := range tools {
		// Insert/update tool master record (using tool.ID as provisional tool_id)
		_, err := toolStmt.Exec(
			tool.ID,
			tool.CompanyID,
			tool.SKU,
			tool.Name,
			tool.Description,
			tool.Location, // Use location as default_destination
			now,
		)
		if err != nil {
			return err
		}

		// Insert/update tool tag record
		_, err = tagStmt.Exec(
			tool.ID, // Use same ID for tag
			tool.ID, // tool_id references tools.id
			tool.UII,
			tool.Status,
			tool.Location,
			true, // active
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpsertToolsFromSync batch upserts normalized tools and tool_tags from gateway sync-data.
// Uses normalized schema: upserts tool_records (SKU master) and tool_tags (per-tag state).
func (s *LocalStore) UpsertToolsFromSync(rows []SyncDataItem) error {
	if len(rows) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Prepare statements for tools (SKU master) and tool_tags (per-tag state)
	toolStmt, err := tx.Prepare(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			company_id = excluded.company_id,
			sku = excluded.sku,
			name = excluded.name,
			description = excluded.description,
			default_destination = excluded.default_destination,
			last_synced_at = excluded.last_synced_at
	`)
	if err != nil {
		return err
	}
	defer toolStmt.Close()

	tagStmt, err := tx.Prepare(`
		INSERT INTO tool_tags (id, tool_id, uii, unit_number, status, location, location_id, display_name, notes, active, kanban_zone, last_synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(uii) DO UPDATE SET
			tool_id = excluded.tool_id,
			unit_number = excluded.unit_number,
			status = excluded.status,
			location = excluded.location,
			location_id = excluded.location_id,
			display_name = excluded.display_name,
			notes = excluded.notes,
			active = excluded.active,
			kanban_zone = excluded.kanban_zone,
			last_synced_at = excluded.last_synced_at
	`)
	if err != nil {
		return err
	}
	defer tagStmt.Close()

	now := time.Now()
	for _, row := range rows {
		// Upsert tool master record (using ToolID from sync data)
		defaultDest := row.ToolDestination
		if defaultDest == nil && row.Location != nil && *row.Location != "" {
			defaultDest = row.Location
		}

		_, err := toolStmt.Exec(
			row.ToolID,
			"", // company_id not provided in sync data
			row.SKU,
			row.Name,
			toNullString(row.Description),
			toNullString(defaultDest),
			now,
		)
		if err != nil {
			return err
		}

		// Upsert tool tag record
		var locationID sql.NullInt64
		if row.LocationID != nil {
			locationID.Int64 = *row.LocationID
			locationID.Valid = true
		}

		_, err = tagStmt.Exec(
			row.ID,     // tag id
			row.ToolID, // references tools.id
			row.UII,
			toNullString(row.UnitNumber),
			row.Status,
			toNullString(row.Location),
			locationID,
			toNullString(row.DisplayName),
			row.Notes,
			row.Active,
			row.KanbanZone,
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpsertUsers batch upserts users from VPS sync.
func (s *LocalStore) UpsertUsers(users []User) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO users (id, company_id, name, role, department, rfid_tag, active, last_synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			company_id = excluded.company_id,
			name = excluded.name,
			role = excluded.role,
			department = excluded.department,
			rfid_tag = excluded.rfid_tag,
			active = excluded.active,
			last_synced_at = excluded.last_synced_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for _, user := range users {
		_, err := stmt.Exec(
			user.ID,
			user.CompanyID,
			user.Name,
			user.Role,
			user.Department,
			user.RFIDTag,
			user.Active,
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ErrDuplicateTagUII is returned by UpsertToolTags when one batch carries the
// same UII for two tags; the batch is rejected and nothing is written.
var ErrDuplicateTagUII = errors.New("duplicate tag UII in sync batch")

// ErrQueueFull is returned by CreateConfirmation when the pending queue is at
// its cap. Nothing stored is discarded: the new confirmation is rejected.
var ErrQueueFull = errors.New("pending confirmation queue is full")

// CreateConfirmation stores a new pending confirmation.
// If maxPending is > 0 and the unsynced count has reached it, the confirmation
// is rejected with ErrQueueFull and the queue is left unchanged.
func (s *LocalStore) CreateConfirmation(uii, action, antennaID string, timestamp time.Time, maxPending int) (*PendingConfirmation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if maxPending > 0 {
		var count int
		err := tx.QueryRow(`SELECT COUNT(*) FROM pending_confirmations WHERE synced = 0`).Scan(&count)
		if err != nil {
			return nil, err
		}
		if count >= maxPending {
			log.Printf("[LocalStore] Queue cap reached (%d), rejected confirmation: UII=%s", maxPending, uii)
			return nil, ErrQueueFull
		}
	}

	result, err := tx.Exec(`
		INSERT INTO pending_confirmations (uii, action, antenna_id, timestamp, synced, retry_count)
		VALUES (?, ?, ?, ?, 0, 0)
	`, uii, action, antennaID, timestamp)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &PendingConfirmation{
		ID:         id,
		UII:        uii,
		Action:     action,
		AntennaID:  antennaID,
		Timestamp:  timestamp,
		Synced:     false,
		RetryCount: 0,
		CreatedAt:  time.Now(),
	}, nil
}

// GetRetryableConfirmations returns the oldest unsynced confirmations that have
// fewer than maxRetries send attempts and are due (no backoff pending), so
// stuck or waiting ones never fill the batch.
func (s *LocalStore) GetRetryableConfirmations(limit, maxRetries int) ([]PendingConfirmation, error) {
	return s.queryUnsyncedConfirmations(`AND retry_count < ?
		AND (next_attempt_at IS NULL OR next_attempt_at <= strftime('%Y-%m-%d %H:%M:%S', 'now'))`, limit, maxRetries)
}

// GetUnsyncedConfirmations retrieves all unsynced confirmations.
func (s *LocalStore) GetUnsyncedConfirmations(limit int) ([]PendingConfirmation, error) {
	return s.queryUnsyncedConfirmations("", limit)
}

// queryUnsyncedConfirmations lists unsynced confirmations oldest first, with an
// optional extra WHERE condition whose args precede the limit.
func (s *LocalStore) queryUnsyncedConfirmations(extraWhere string, limit int, args ...any) ([]PendingConfirmation, error) {
	rows, err := s.db.Query(`
		SELECT id, uii, action, antenna_id, timestamp, retry_count, created_at
		FROM pending_confirmations
		WHERE synced = 0 `+extraWhere+`
		ORDER BY created_at ASC
		LIMIT ?
	`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var confirmations []PendingConfirmation
	for rows.Next() {
		var pc PendingConfirmation
		err := rows.Scan(
			&pc.ID,
			&pc.UII,
			&pc.Action,
			&pc.AntennaID,
			&pc.Timestamp,
			&pc.RetryCount,
			&pc.CreatedAt,
		)
		if err != nil {
			continue
		}
		pc.Synced = false
		confirmations = append(confirmations, pc)
	}

	return confirmations, rows.Err()
}

// MarkConfirmationSynced marks a confirmation as synced, stamping synced_at.
func (s *LocalStore) MarkConfirmationSynced(id int64) error {
	_, err := s.db.Exec(`
		UPDATE pending_confirmations
		SET synced = 1, synced_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, id)
	return err
}

// RecordSyncedConfirmation stores a confirmation the VPS already accepted, as
// synced with the send time, so the local history has every confirmation.
func (s *LocalStore) RecordSyncedConfirmation(uii, action, antennaID string, sentAt time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO pending_confirmations (uii, action, antenna_id, timestamp, synced, retry_count, synced_at)
		VALUES (?, ?, ?, ?, 1, 0, ?)
	`, uii, action, antennaID, sentAt, sentAt.UTC().Format(sqliteTimestamp))
	return err
}

// GetStuckConfirmationsCount counts pending confirmations that reached
// minRetries send attempts; the sync worker keeps them queued but skips them.
func (s *LocalStore) GetStuckConfirmationsCount(minRetries int) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM pending_confirmations WHERE synced = 0 AND retry_count >= ?`, minRetries).Scan(&count)
	return count, err
}

// RequeueStuckConfirmations resets the retry count and the backoff of every
// pending confirmation that reached minRetries send attempts so the sync worker
// sends it again right away, and returns how many were reset. Synced and still-retrying rows
// are untouched.
func (s *LocalStore) RequeueStuckConfirmations(minRetries int) (int64, error) {
	res, err := s.db.Exec(`
		UPDATE pending_confirmations
		SET retry_count = 0, next_attempt_at = NULL
		WHERE synced = 0 AND retry_count >= ?
	`, minRetries)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PurgeSyncedConfirmations deletes synced confirmations older than cutoff
// (by synced_at, or created_at for rows synced before synced_at existed) and
// returns how many were removed. Unsynced confirmations are never deleted, and
// the most recently synced row is always kept so the last successful send to
// the VPS (GetLastSyncedAt) survives retention.
func (s *LocalStore) PurgeSyncedConfirmations(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(`
		DELETE FROM pending_confirmations
		WHERE synced = 1 AND COALESCE(synced_at, created_at) < ?
		  AND id <> COALESCE((
			SELECT id FROM pending_confirmations
			WHERE synced = 1 AND synced_at IS NOT NULL
			ORDER BY synced_at DESC, id DESC LIMIT 1
		  ), -1)
	`, cutoff.UTC().Format(sqliteTimestamp))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// IncrementRetryCount increments retry count for a confirmation.
func (s *LocalStore) IncrementRetryCount(id int64) error {
	_, err := s.db.Exec(`
		UPDATE pending_confirmations
		SET retry_count = retry_count + 1
		WHERE id = ?
	`, id)
	return err
}

// MaxRetryBackoff caps the wait between two send attempts of a confirmation.
const MaxRetryBackoff = 30 * time.Minute

// RetryBackoff is the wait after the retryCount-th failed send:
// base * 2^(retryCount-1), capped at MaxRetryBackoff.
func RetryBackoff(base time.Duration, retryCount int) time.Duration {
	delay := base
	for i := 1; i < retryCount && delay < MaxRetryBackoff; i++ {
		delay *= 2
	}
	if delay > MaxRetryBackoff {
		return MaxRetryBackoff
	}
	return delay
}

// ScheduleConfirmationRetry sets when a confirmation whose send just failed may
// be sent again: now + RetryBackoff(base, its retry count). Call it after
// IncrementRetryCount so the count includes the failed attempt.
func (s *LocalStore) ScheduleConfirmationRetry(id int64, base time.Duration, now time.Time) error {
	var retryCount int
	if err := s.db.QueryRow(`SELECT retry_count FROM pending_confirmations WHERE id = ?`, id).Scan(&retryCount); err != nil {
		return err
	}
	if retryCount == 0 {
		return nil // requeued meanwhile: stays due now
	}
	next := now.Add(RetryBackoff(base, retryCount)).UTC().Format(sqliteTimestamp)
	// Only if the count is still the one read: a requeue in between reset it,
	// and its "due now" must not be overwritten by this stale schedule.
	_, err := s.db.Exec(`UPDATE pending_confirmations SET next_attempt_at = ? WHERE id = ? AND retry_count = ?`, next, id, retryCount)
	return err
}

// GetToolsCount returns count of cached tool tags (individual RFID instances).
// For normalized schema, this counts tool_tags table which represents actual
// trackable items, not the tools table which only contains SKU master records.
func (s *LocalStore) GetToolsCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tool_tags WHERE active = 1`).Scan(&count)
	return count, err
}

// GetUsersCount returns count of cached users.
func (s *LocalStore) GetUsersCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// GetPendingConfirmationsCount returns count of unsynced confirmations.
func (s *LocalStore) GetPendingConfirmationsCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM pending_confirmations WHERE synced = 0`).Scan(&count)
	return count, err
}

// GetOldestPendingAt returns when the oldest unsynced confirmation was queued,
// or nil when nothing is pending.
func (s *LocalStore) GetOldestPendingAt() (*time.Time, error) {
	return s.queryOptionalTime(`
		SELECT created_at FROM pending_confirmations
		WHERE synced = 0 AND created_at IS NOT NULL
		ORDER BY created_at ASC LIMIT 1
	`)
}

// GetLastSyncedAt returns when the most recent confirmation reached the VPS,
// or nil when none has been recorded as synced yet.
func (s *LocalStore) GetLastSyncedAt() (*time.Time, error) {
	return s.queryOptionalTime(`
		SELECT synced_at FROM pending_confirmations
		WHERE synced_at IS NOT NULL
		ORDER BY synced_at DESC LIMIT 1
	`)
}

// queryOptionalTime runs a single-column, single-row time query; no row is nil.
func (s *LocalStore) queryOptionalTime(query string) (*time.Time, error) {
	var t time.Time
	err := s.db.QueryRow(query).Scan(&t)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// DeleteOldestUnsyncedConfirmation deletes the oldest unsynced confirmation by created_at.
// Returns the UII of the deleted confirmation and any error.
func (s *LocalStore) DeleteOldestUnsyncedConfirmation() (string, error) {
	var uii sql.NullString
	err := s.db.QueryRow(`
		DELETE FROM pending_confirmations
		WHERE id = (SELECT id FROM pending_confirmations WHERE synced = 0 ORDER BY created_at ASC LIMIT 1)
		RETURNING uii
	`).Scan(&uii)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // Nothing to delete
		}
		return "", err
	}

	if uii.Valid {
		return uii.String, nil
	}
	return "", nil
}

// === Normalized Schema Methods ===

// UpsertToolTags batch upserts tool tags from VPS sync.
// Uses normalized schema with tool_tags table.
func (s *LocalStore) UpsertToolTags(tags []ToolTagRecord) error {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]int64, len(tags))
	for _, tag := range tags {
		if other, dup := seen[tag.UII]; dup && other != tag.ID {
			return fmt.Errorf("%w: %s (tags %d and %d)", ErrDuplicateTagUII, tag.UII, other, tag.ID)
		}
		seen[tag.UII] = tag.ID
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO tool_tags (id, tool_id, uii, unit_number, status, location, location_id, display_name, notes, active, kanban_zone, last_synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			uii = excluded.uii,
			tool_id = excluded.tool_id,
			unit_number = excluded.unit_number,
			status = excluded.status,
			location = excluded.location,
			location_id = excluded.location_id,
			display_name = excluded.display_name,
			notes = excluded.notes,
			active = excluded.active,
			kanban_zone = excluded.kanban_zone,
			last_synced_at = excluded.last_synced_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// The VPS is the source of truth by tag id: its UII can be renormalized, and
	// a UII can move to another tag. Drop any other row holding the UII first so
	// the id upsert never hits the UNIQUE(uii) constraint.
	releaseUII, err := tx.Prepare(`DELETE FROM tool_tags WHERE uii = ? AND id <> ?`)
	if err != nil {
		return err
	}
	defer releaseUII.Close()

	// A tag whose UII the VPS renormalized (same id, new UII) drops the kanban
	// link of its old UII, which no tag carries any more; the sync relinks the
	// new UII. Links of UIIs that are not tags yet (a just-accepted assignment)
	// are never touched here.
	dropOldLink, err := tx.Prepare(`
		DELETE FROM tag_kanban_links
		WHERE uii = (SELECT uii FROM tool_tags WHERE id = ? AND uii <> ?)
	`)
	if err != nil {
		return err
	}
	defer dropOldLink.Close()

	now := time.Now()
	for _, tag := range tags {
		if _, err := dropOldLink.Exec(tag.ID, tag.UII); err != nil {
			return err
		}
		if _, err := releaseUII.Exec(tag.UII, tag.ID); err != nil {
			return err
		}
		_, err := stmt.Exec(
			tag.ID,
			tag.ToolID,
			tag.UII,
			toNullString(tag.UnitNumber),
			tag.Status,
			toNullString(tag.Location),
			tag.LocationID,
			toNullString(tag.DisplayName),
			tag.Notes,
			tag.Active,
			tag.KanbanZone,
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetToolTagByUII retrieves a tool tag by UII (EPC) using normalized schema.
func (s *LocalStore) GetToolTagByUII(uii string) (*ToolTagRecord, error) {
	row := s.db.QueryRow(`
		SELECT id, tool_id, uii, unit_number, status, location, location_id, display_name, notes, active, kanban_zone, last_synced_at
		FROM tool_tags
		WHERE uii = ?
	`, uii)

	var tag ToolTagRecord
	var unitNumber, location, displayName sql.NullString
	err := row.Scan(
		&tag.ID,
		&tag.ToolID,
		&tag.UII,
		&unitNumber,
		&tag.Status,
		&location,
		&tag.LocationID,
		&displayName,
		&tag.Notes,
		&tag.Active,
		&tag.KanbanZone,
		&tag.LastSyncedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	tag.UnitNumber = scanNullStringPtr(unitNumber)
	tag.Location = scanNullStringPtr(location)
	tag.DisplayName = scanNullStringPtr(displayName)

	return &tag, nil
}

// GetToolTagsByToolID retrieves all tool tags for a given tool ID.
func (s *LocalStore) GetToolTagsByToolID(toolID int64) ([]ToolTagRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, tool_id, uii, unit_number, status, location, location_id, display_name, notes, active, kanban_zone, last_synced_at
		FROM tool_tags
		WHERE tool_id = ?
	`, toolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []ToolTagRecord
	for rows.Next() {
		var tag ToolTagRecord
		var unitNumber, location, displayName sql.NullString
		err := rows.Scan(
			&tag.ID,
			&tag.ToolID,
			&tag.UII,
			&unitNumber,
			&tag.Status,
			&location,
			&tag.LocationID,
			&displayName,
			&tag.Notes,
			&tag.Active,
			&tag.KanbanZone,
			&tag.LastSyncedAt,
		)
		if err != nil {
			continue
		}

		tag.UnitNumber = scanNullStringPtr(unitNumber)
		tag.Location = scanNullStringPtr(location)
		tag.DisplayName = scanNullStringPtr(displayName)

		tags = append(tags, tag)
	}

	return tags, rows.Err()
}

// UpsertToolsRecords batch upserts tool master records from VPS sync.
// Uses normalized schema with tools table (no uii column).
func (s *LocalStore) UpsertToolsRecords(tools []ToolRecord) error {
	if len(tools) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO tools (id, company_id, sku, name, description, default_destination, last_synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			company_id = excluded.company_id,
			sku = excluded.sku,
			name = excluded.name,
			description = excluded.description,
			default_destination = excluded.default_destination,
			last_synced_at = excluded.last_synced_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for _, tool := range tools {
		_, err := stmt.Exec(
			tool.ID,
			tool.CompanyID,
			tool.SKU,
			tool.Name,
			toNullString(tool.Description),
			tool.DefaultDestination,
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetToolRecordByID retrieves a tool master record by ID.
func (s *LocalStore) GetToolRecordByID(id int64) (*ToolRecord, error) {
	row := s.db.QueryRow(`
		SELECT id, company_id, sku, name, description, default_destination, last_synced_at
		FROM tools
		WHERE id = ?
	`, id)

	var tool ToolRecord
	var description sql.NullString
	err := row.Scan(
		&tool.ID,
		&tool.CompanyID,
		&tool.SKU,
		&tool.Name,
		&description,
		&tool.DefaultDestination,
		&tool.LastSyncedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	tool.Description = scanNullStringPtr(description)
	return &tool, nil
}
