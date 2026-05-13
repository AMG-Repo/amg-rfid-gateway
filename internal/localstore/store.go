// Package localstore provides local SQLite storage operations for tools, users, and confirmations.
package localstore

import (
	"database/sql"
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
	Description        string    `json:"description"`
	DefaultDestination *string   `json:"default_destination,omitempty"`
	LastSyncedAt       time.Time `json:"last_synced_at"`
}

// ToolTagRecord represents a normalized tool tag record (individual RFID instance).
type ToolTagRecord struct {
	ID           int64     `json:"id"`
	ToolID       int64     `json:"tool_id"`
	UII          string    `json:"uii"`
	UnitNumber   string    `json:"unit_number"`
	Status       string    `json:"status"`
	Location     string    `json:"location"`
	LocationID   *int64    `json:"location_id,omitempty"`
	DisplayName  string    `json:"display_name"`
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
	Description     string  `json:"description"`
	Status          string  `json:"status"`
	Location        string  `json:"location"`
	LocationID      *int64  `json:"location_id,omitempty"`
	UnitNumber      string  `json:"unit_number"`
	DisplayName     string  `json:"display_name"`
	Notes           *string `json:"notes,omitempty"`
	Active          bool    `json:"active"`
	KanbanZone      *string `json:"kanban_zone,omitempty"`
	ToolDestination *string `json:"tool_destination,omitempty"`
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

// GetToolByUII retrieves a tool by UII (EPC) using normalized schema.
// Performs a JOIN between tool_tags and tools to get complete metadata.
func (s *LocalStore) GetToolByUII(uii string) (*Tool, error) {
	row := s.db.QueryRow(`
		SELECT tt.id, t.company_id, t.sku, t.name, t.description, tt.uii, tt.status, tt.location, tt.last_synced_at
		FROM tool_tags tt
		JOIN tools t ON tt.tool_id = t.id
		WHERE tt.uii = ?
	`, uii)

	var tool Tool
	err := row.Scan(
		&tool.ID,
		&tool.CompanyID,
		&tool.SKU,
		&tool.Name,
		&tool.Description,
		&tool.UII,
		&tool.Status,
		&tool.Location,
		&tool.LastSyncedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
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
			tool.ID,    // Use same ID for tag
			tool.ID,    // tool_id references tools.id
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
		if defaultDest == nil || *defaultDest == "" {
			defaultDest = &row.Location
		}

		_, err := toolStmt.Exec(
			row.ToolID,
			"", // company_id not provided in sync data
			row.SKU,
			row.Name,
			row.Description,
			defaultDest,
			now,
		)
		if err != nil {
			return err
		}

		// Upsert tool tag record
		location := row.Location
		if location == "" {
			location = "Almacén General"
		}

		var locationID sql.NullInt64
		if row.LocationID != nil {
			locationID.Int64 = *row.LocationID
			locationID.Valid = true
		}

		_, err = tagStmt.Exec(
			row.ID,        // tag id
			row.ToolID,    // references tools.id
			row.UII,
			row.UnitNumber,
			row.Status,
			location,
			locationID,
			row.DisplayName,
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
		ON CONFLICT(rfid_tag) DO UPDATE SET
			company_id = excluded.company_id,
			name = excluded.name,
			role = excluded.role,
			department = excluded.department,
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

// CreateConfirmation stores a new pending confirmation.
// If maxPending is > 0 and the count exceeds it, the oldest unsynced confirmation is deleted first.
func (s *LocalStore) CreateConfirmation(uii, action, antennaID string, timestamp time.Time, maxPending int) (*PendingConfirmation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Check queue cap if maxPending > 0
	if maxPending > 0 {
		var count int
		err := tx.QueryRow(`SELECT COUNT(*) FROM pending_confirmations WHERE synced = 0`).Scan(&count)
		if err != nil {
			return nil, err
		}

		// If at or over cap, delete oldest atomically
		if count >= maxPending {
			var deletedUII sql.NullString
			err := tx.QueryRow(`
				DELETE FROM pending_confirmations
				WHERE id = (SELECT id FROM pending_confirmations WHERE synced = 0 ORDER BY created_at ASC LIMIT 1)
				RETURNING uii
			`).Scan(&deletedUII)
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
			if deletedUII.Valid {
				log.Printf("[LocalStore] Queue cap reached (%d), discarded oldest confirmation: UII=%s", maxPending, deletedUII.String)
			}
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

// GetUnsyncedConfirmations retrieves all unsynced confirmations.
func (s *LocalStore) GetUnsyncedConfirmations(limit int) ([]PendingConfirmation, error) {
	rows, err := s.db.Query(`
		SELECT id, uii, action, antenna_id, timestamp, retry_count, created_at
		FROM pending_confirmations
		WHERE synced = 0
		ORDER BY created_at ASC
		LIMIT ?
	`, limit)
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

// MarkConfirmationSynced marks a confirmation as synced.
func (s *LocalStore) MarkConfirmationSynced(id int64) error {
	_, err := s.db.Exec(`
		UPDATE pending_confirmations
		SET synced = 1
		WHERE id = ?
	`, id)
	return err
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

// GetToolsCount returns count of cached tools.
func (s *LocalStore) GetToolsCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tools`).Scan(&count)
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

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
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
	defer stmt.Close()

	now := time.Now()
	for _, tag := range tags {
		_, err := stmt.Exec(
			tag.ID,
			tag.ToolID,
			tag.UII,
			tag.UnitNumber,
			tag.Status,
			tag.Location,
			tag.LocationID,
			tag.DisplayName,
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
	err := row.Scan(
		&tag.ID,
		&tag.ToolID,
		&tag.UII,
		&tag.UnitNumber,
		&tag.Status,
		&tag.Location,
		&tag.LocationID,
		&tag.DisplayName,
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
		err := rows.Scan(
			&tag.ID,
			&tag.ToolID,
			&tag.UII,
			&tag.UnitNumber,
			&tag.Status,
			&tag.Location,
			&tag.LocationID,
			&tag.DisplayName,
			&tag.Notes,
			&tag.Active,
			&tag.KanbanZone,
			&tag.LastSyncedAt,
		)
		if err != nil {
			continue
		}
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
			tool.Description,
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
	err := row.Scan(
		&tool.ID,
		&tool.CompanyID,
		&tool.SKU,
		&tool.Name,
		&tool.Description,
		&tool.DefaultDestination,
		&tool.LastSyncedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tool, nil
}
