package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// Device is a paired client allowed onto the TCP listeners.
type Device struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	CreatedAt  int64  `json:"created_at"`
	LastSeenAt *int64 `json:"last_seen_at"`
}

// ErrInvalidCode: the pairing code is unknown, expired or already used.
// The three cases are deliberately indistinguishable to callers.
var ErrInvalidCode = errors.New("invalid pairing code")

// CreatePairingCode stores a new code hash and prunes codes that are
// expired or used as of now.
func (s *Store) CreatePairingCode(codeHash string, now, expiresAt int64) error {
	if _, err := s.db.Exec(`DELETE FROM pairing_codes WHERE expires_at <= ? OR used_at IS NOT NULL`, now); err != nil {
		return fmt.Errorf("prune pairing codes: %w", err)
	}
	if _, err := s.db.Exec(`INSERT INTO pairing_codes (code_hash, expires_at) VALUES (?, ?)`, codeHash, expiresAt); err != nil {
		return fmt.Errorf("create pairing code: %w", err)
	}
	return nil
}

// RedeemPairingCode atomically burns the code and creates the device.
func (s *Store) RedeemPairingCode(codeHash string, now int64, name, kind, tokenHash string) (Device, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Device{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE pairing_codes SET used_at = ?
		WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?`, now, codeHash, now)
	if err != nil {
		return Device{}, fmt.Errorf("redeem pairing code: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Device{}, ErrInvalidCode
	}
	res, err = tx.Exec(`INSERT INTO devices (name, kind, token_hash, created_at) VALUES (?, ?, ?, ?)`,
		name, kind, tokenHash, now)
	if err != nil {
		return Device{}, fmt.Errorf("create device: %w", err)
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return Device{}, err
	}
	return Device{ID: id, Name: name, Kind: kind, CreatedAt: now}, nil
}

const deviceCols = `id, name, kind, created_at, last_seen_at`

func scanDevice(sc interface{ Scan(...any) error }) (Device, error) {
	var d Device
	var seen sql.NullInt64
	if err := sc.Scan(&d.ID, &d.Name, &d.Kind, &d.CreatedAt, &seen); err != nil {
		return Device{}, err
	}
	if seen.Valid {
		v := seen.Int64
		d.LastSeenAt = &v
	}
	return d, nil
}

// DeviceByTokenHash returns the active device owning tokenHash.
func (s *Store) DeviceByTokenHash(tokenHash string) (Device, error) {
	d, err := scanDevice(s.db.QueryRow(
		`SELECT `+deviceCols+` FROM devices WHERE token_hash = ? AND revoked_at IS NULL`, tokenHash))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("device by token: %w", err)
	}
	return d, nil
}

// ListDevices returns active devices ordered by id.
func (s *Store) ListDevices() ([]Device, error) {
	rows, err := s.db.Query(`SELECT ` + deviceCols + ` FROM devices WHERE revoked_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RevokeDevice marks an active device revoked; ErrNotFound if none.
func (s *Store) RevokeDevice(id, now int64) error {
	res, err := s.db.Exec(`UPDATE devices SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, now, id)
	if err != nil {
		return fmt.Errorf("revoke device: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchDevice records the device's last activity.
func (s *Store) TouchDevice(id, now int64) error {
	if _, err := s.db.Exec(`UPDATE devices SET last_seen_at = ? WHERE id = ?`, now, id); err != nil {
		return fmt.Errorf("touch device: %w", err)
	}
	return nil
}
