package localstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Kanban is the customer kanban card definition maintained on the VPS.
// The gateway only holds a read-only copy; it never edits kanban data.
type Kanban struct {
	Code         string   `json:"code"`
	Description  string   `json:"description"`
	StandardPack float64  `json:"standard_pack"`
	Unit         string   `json:"unit"`
	Zones        []string `json:"zones"`
	Extrusora    string   `json:"extrusora"`
	Position     string   `json:"position"`
	Area         string   `json:"area"`
	Customer     string   `json:"customer"`
	CardType     string   `json:"card_type"`
	SKU          string   `json:"sku"`
}

const kanbanColumns = `code, description, standard_pack, unit, zones, extrusora, position, area, customer, card_type, sku`

// upsertKanbanTx stores (or refreshes) one kanban row.
func upsertKanbanTx(tx *sql.Tx, k Kanban, now time.Time) error {
	zones := k.Zones
	if zones == nil {
		zones = []string{}
	}
	zonesJSON, err := json.Marshal(zones)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO kanbans (`+kanbanColumns+`, last_synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET
			description = excluded.description,
			standard_pack = excluded.standard_pack,
			unit = excluded.unit,
			zones = excluded.zones,
			extrusora = excluded.extrusora,
			position = excluded.position,
			area = excluded.area,
			customer = excluded.customer,
			card_type = excluded.card_type,
			sku = excluded.sku,
			last_synced_at = excluded.last_synced_at
	`, k.Code, k.Description, k.StandardPack, k.Unit, string(zonesJSON), k.Extrusora, k.Position, k.Area, k.Customer, k.CardType, k.SKU, now)
	return err
}

// scanKanban reads a kanban row selected with kanbanColumns.
func scanKanban(scan func(dest ...any) error) (*Kanban, error) {
	var k Kanban
	var description, unit, zones, extrusora, position, area, customer, cardType, sku sql.NullString
	var pack sql.NullFloat64
	if err := scan(&k.Code, &description, &pack, &unit, &zones, &extrusora, &position, &area, &customer, &cardType, &sku); err != nil {
		return nil, err
	}
	k.Description = description.String
	k.StandardPack = pack.Float64
	k.Unit = unit.String
	k.Extrusora = extrusora.String
	k.Position = position.String
	k.Area = area.String
	k.Customer = customer.String
	k.CardType = cardType.String
	k.SKU = sku.String
	k.Zones = []string{}
	if zones.Valid && zones.String != "" {
		if err := json.Unmarshal([]byte(zones.String), &k.Zones); err != nil {
			return nil, err
		}
		if k.Zones == nil {
			k.Zones = []string{}
		}
	}
	return &k, nil
}

// ApplyKanbanLinks mirrors the kanban link of each sync item into the local cache.
// An active item carrying a kanban is linked (and the kanban row refreshed);
// an inactive item, or one with kanban:null, loses its link so stale
// assignments never survive a sync. Items from old payloads (no kanban
// fields) therefore leave their tags unassigned.
func (s *LocalStore) ApplyKanbanLinks(items []SyncDataItem) error {
	if len(items) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	for _, item := range items {
		if item.UII == "" {
			continue
		}
		if !item.Active || item.Kanban == nil || strings.TrimSpace(item.Kanban.Code) == "" {
			if _, err := tx.Exec(`DELETE FROM tag_kanban_links WHERE uii = ?`, item.UII); err != nil {
				return err
			}
			continue
		}
		if err := upsertKanbanTx(tx, *item.Kanban, now); err != nil {
			return err
		}
		if err := upsertLinkTx(tx, item.UII, item.Kanban.Code, now); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// upsertLinkTx links a tag to a kanban code.
func upsertLinkTx(tx *sql.Tx, uii, kanbanCode string, now time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO tag_kanban_links (uii, kanban_code, last_synced_at)
		VALUES (?, ?, ?)
		ON CONFLICT(uii) DO UPDATE SET
			kanban_code = excluded.kanban_code,
			last_synced_at = excluded.last_synced_at
	`, uii, kanbanCode, now)
	return err
}

// AssignKanban records, in the local cache, an assignment the VPS has already
// accepted: it stores the kanban returned by the VPS, links the tag and removes
// it from the unassigned inbox. It never decides an assignment by itself.
func (s *LocalStore) AssignKanban(uii string, kanban Kanban) error {
	if strings.TrimSpace(uii) == "" || strings.TrimSpace(kanban.Code) == "" {
		return errors.New("uii and kanban code are required")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	if err := upsertKanbanTx(tx, kanban, now); err != nil {
		return err
	}
	if err := upsertLinkTx(tx, uii, kanban.Code, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM unassigned_tags WHERE epc = ?`, uii); err != nil {
		return err
	}

	return tx.Commit()
}

// ReplaceKanbanCatalog refreshes the cached catalog used by the assignment picker.
// Rows missing from the new catalog are dropped unless a tag is still linked to them.
func (s *LocalStore) ReplaceKanbanCatalog(kanbans []Kanban) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	keep := make(map[string]struct{}, len(kanbans))
	for _, k := range kanbans {
		if strings.TrimSpace(k.Code) == "" {
			continue
		}
		if err := upsertKanbanTx(tx, k, now); err != nil {
			return err
		}
		keep[k.Code] = struct{}{}
	}

	rows, err := tx.Query(`
		SELECT code FROM kanbans
		WHERE code NOT IN (SELECT kanban_code FROM tag_kanban_links)
	`)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return err
		}
		if _, ok := keep[code]; !ok {
			stale = append(stale, code)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, code := range stale {
		if _, err := tx.Exec(`DELETE FROM kanbans WHERE code = ?`, code); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ListKanbans returns the cached kanban catalog ordered by code.
func (s *LocalStore) ListKanbans() ([]Kanban, error) {
	rows, err := s.db.Query(`SELECT ` + kanbanColumns + ` FROM kanbans ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	kanbans := []Kanban{}
	for rows.Next() {
		k, err := scanKanban(rows.Scan)
		if err != nil {
			return nil, err
		}
		kanbans = append(kanbans, *k)
	}
	return kanbans, rows.Err()
}

// GetKanbanByUII returns the kanban a tag is actively linked to, or nil when
// the tag has no link (unassigned or unknown).
func (s *LocalStore) GetKanbanByUII(uii string) (*Kanban, error) {
	row := s.db.QueryRow(`
		SELECT `+qualifiedKanbanColumns+`
		FROM tag_kanban_links l
		JOIN kanbans k ON k.code = l.kanban_code
		WHERE l.uii = ?
	`, uii)

	k, err := scanKanban(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return k, nil
}

var qualifiedKanbanColumns = "k." + strings.ReplaceAll(kanbanColumns, ", ", ", k.")
