package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// SetSetting upserts key's value in the settings table.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
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
