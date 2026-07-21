package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Attachment is an uploaded dashboard file (pasted screenshot). Bytes live
// on disk (see internal/api/attachments.go); this row is identity+metadata.
type Attachment struct {
	ID        int64
	MIME      string
	Size      int64
	CreatedAt int64
}

// AddAttachment inserts a new attachment row, defaulting CreatedAt to now.
// Returns the assigned id (which also names the file on disk).
func (s *Store) AddAttachment(a Attachment) (int64, error) {
	if a.CreatedAt == 0 {
		a.CreatedAt = time.Now().Unix()
	}
	res, err := s.db.Exec(
		`INSERT INTO attachments (mime, size, created_at) VALUES (?, ?, ?)`,
		a.MIME, a.Size, a.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert attachment: %w", err)
	}
	return res.LastInsertId()
}

// GetAttachment returns the attachment with the given id, or ErrNotFound.
func (s *Store) GetAttachment(id int64) (Attachment, error) {
	var a Attachment
	err := s.db.QueryRow(
		`SELECT id, mime, size, created_at FROM attachments WHERE id = ?`, id,
	).Scan(&a.ID, &a.MIME, &a.Size, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Attachment{}, ErrNotFound
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("scan attachment: %w", err)
	}
	return a, nil
}
