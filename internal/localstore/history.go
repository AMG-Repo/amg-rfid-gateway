package localstore

import (
	"database/sql"
	"sort"
	"time"
)

// HistoryEntry is one recorded confirmation with the kanban its tag is linked
// to today. The kanban fields are nil when the tag has no active link.
type HistoryEntry struct {
	UII          string
	Action       string
	AntennaID    string
	Timestamp    time.Time
	Synced       bool
	KanbanCode   *string
	Description  *string
	StandardPack *float64
	Unit         *string
}

// ListConfirmationsBetween returns the confirmations whose confirmation time is
// in [start, end), newest first, synced and pending alike.
func (s *LocalStore) ListConfirmationsBetween(start, end time.Time) ([]HistoryEntry, error) {
	// created_at never precedes the confirmation time, so it is a safe, index-free
	// prefilter that avoids reading the whole 30-day retention window.
	rows, err := s.db.Query(`
		SELECT c.id, c.uii, c.action, c.antenna_id, c.timestamp, c.synced,
		       k.code, k.description, k.standard_pack, k.unit
		FROM pending_confirmations c
		LEFT JOIN tag_kanban_links l ON l.uii = c.uii
		LEFT JOIN kanbans k ON k.code = l.kanban_code
		WHERE c.created_at >= ?
	`, start.UTC().Format(sqliteTimestamp))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		id    int64
		entry HistoryEntry
	}
	var found []row
	for rows.Next() {
		var r row
		var antenna, code, description, unit sql.NullString
		var pack sql.NullFloat64
		e := &r.entry
		if err := rows.Scan(&r.id, &e.UII, &e.Action, &antenna, &e.Timestamp, &e.Synced, &code, &description, &pack, &unit); err != nil {
			return nil, err
		}
		if e.Timestamp.Before(start) || !e.Timestamp.Before(end) {
			continue
		}
		e.AntennaID = antenna.String
		e.KanbanCode = scanNullStringPtr(code)
		e.Description = scanNullStringPtr(description)
		e.Unit = scanNullStringPtr(unit)
		if pack.Valid {
			e.StandardPack = &pack.Float64
		}
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(found, func(i, j int) bool {
		if !found[i].entry.Timestamp.Equal(found[j].entry.Timestamp) {
			return found[i].entry.Timestamp.After(found[j].entry.Timestamp)
		}
		return found[i].id > found[j].id
	})
	entries := make([]HistoryEntry, len(found))
	for i, r := range found {
		entries[i] = r.entry
	}
	return entries, nil
}
