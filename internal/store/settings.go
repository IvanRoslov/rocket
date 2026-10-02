package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// SetSetting upserts key's value in the settings table.
func (s *Store) SetSetting(key, value string) error {
	return setSettingOn(s.db, key, value)
}

// GetSetting returns the value stored under key, or ErrNotFound if unset.
func (s *Store) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get setting %s: %w", key, err)
	}
	return value, nil
}

// DeleteSetting removes key from the settings table. It is idempotent:
// deleting an unset key is not an error.
func (s *Store) DeleteSetting(key string) error {
	if _, err := s.db.Exec(`DELETE FROM settings WHERE key = ?`, key); err != nil {
		return fmt.Errorf("delete setting %s: %w", key, err)
	}
	return nil
}

// SettingOrchestratorBrainstormCustom is the settings key of the
// "custom orchestrator brainstorm" toggle: "true" makes newly started tasks'
// orchestrators run rocket's own orchestrator-brainstorming skill instead of
// superpowers:brainstorming. Unset means off.
const SettingOrchestratorBrainstormCustom = "orchestrator_brainstorm_custom"

// OrchestratorBrainstormCustom reports whether the custom orchestrator
// brainstorm toggle is on. Only the exact value "true" turns it on.
func (s *Store) OrchestratorBrainstormCustom() (bool, error) {
	v, err := s.GetSetting(SettingOrchestratorBrainstormCustom)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return v == "true", nil
}
