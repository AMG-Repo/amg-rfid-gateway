package localstore

import (
	"database/sql"
	"errors"
)

const (
	settingAutoConfirmMode = "auto_confirm_mode"
	autoConfirmAutomatic   = "automatic"
	autoConfirmManual      = "manual"
)

// AutoConfirmEnabled reports whether the persisted confirmation mode is
// automatic. A gateway that never stored a mode is manual.
func (s *LocalStore) AutoConfirmEnabled() (bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, settingAutoConfirmMode).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == autoConfirmAutomatic, nil
}

// SetAutoConfirmEnabled persists the confirmation mode (automatic or manual).
func (s *LocalStore) SetAutoConfirmEnabled(enabled bool) error {
	value := autoConfirmManual
	if enabled {
		value = autoConfirmAutomatic
	}
	_, err := s.db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, settingAutoConfirmMode, value)
	return err
}
