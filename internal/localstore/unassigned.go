package localstore

import "time"

// UnassignedTag is a tag that was read but has no active kanban link.
// It stays in the local inbox until the operator assigns it (online only).
type UnassignedTag struct {
	EPC       string    `json:"epc"`
	AntennaID string    `json:"antenna_id"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	ReadCount int       `json:"read_count"`
}

// UpsertUnassignedTag records reads of an unassigned EPC: one row per EPC,
// first_seen is kept, last_seen/antenna_id follow the latest read and
// read_count accumulates.
func (s *LocalStore) UpsertUnassignedTag(epc, antennaID string, seenAt time.Time, reads int) error {
	_, err := s.db.Exec(`
		INSERT INTO unassigned_tags (epc, antenna_id, first_seen, last_seen, read_count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(epc) DO UPDATE SET
			antenna_id = excluded.antenna_id,
			last_seen = excluded.last_seen,
			read_count = unassigned_tags.read_count + excluded.read_count
	`, epc, antennaID, seenAt, seenAt, reads)
	return err
}

// ListUnassignedTags returns the unassigned tags, most recently seen first.
// Tags that a later sync linked to a kanban are not listed.
func (s *LocalStore) ListUnassignedTags() ([]UnassignedTag, error) {
	rows, err := s.db.Query(`
		SELECT epc, antenna_id, first_seen, last_seen, read_count
		FROM unassigned_tags
		WHERE epc NOT IN (SELECT uii FROM tag_kanban_links)
		ORDER BY last_seen DESC, epc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := []UnassignedTag{}
	for rows.Next() {
		var tag UnassignedTag
		var antennaID *string
		if err := rows.Scan(&tag.EPC, &antennaID, &tag.FirstSeen, &tag.LastSeen, &tag.ReadCount); err != nil {
			return nil, err
		}
		if antennaID != nil {
			tag.AntennaID = *antennaID
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// DeleteUnassignedTag removes an EPC from the inbox (no-op when absent).
func (s *LocalStore) DeleteUnassignedTag(epc string) error {
	_, err := s.db.Exec(`DELETE FROM unassigned_tags WHERE epc = ?`, epc)
	return err
}
