package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Message represents an entry in the inter-session message delivery queue.
type Message struct {
	ID          int64
	FromSession string // empty = human/system
	ToSession   string
	Body        string
	Status      string // queued|delivering|delivered|failed
	Attempts    int
	CreatedAt   int64
	DeliveredAt int64 // 0 = not delivered
}

// AddMessage inserts a new message with status "queued" (unless already set)
// and CreatedAt defaulted to now (unless already set). Returns the assigned id.
func (s *Store) AddMessage(m Message) (int64, error) {
	if m.CreatedAt == 0 {
		m.CreatedAt = time.Now().Unix()
	}
	if m.Status == "" {
		m.Status = "queued"
	}

	res, err := s.db.Exec(
		`INSERT INTO messages (from_session, to_session, body, status, attempts, created_at, delivered_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nullIfEmpty(m.FromSession), m.ToSession, m.Body, m.Status, m.Attempts, m.CreatedAt, nullIfZero(m.DeliveredAt),
	)
	if err != nil {
		return 0, fmt.Errorf("insert message: %w", err)
	}
	return res.LastInsertId()
}

// GetMessage returns the message with the given id, or ErrNotFound.
func (s *Store) GetMessage(id int64) (Message, error) {
	row := s.db.QueryRow(
		`SELECT id, from_session, to_session, body, status, attempts, created_at, delivered_at
		 FROM messages WHERE id = ?`, id,
	)
	return scanMessage(row)
}

// ListMessages returns the last limit messages where to_session = sessionID
// or from_session = sessionID, in ascending id order. A limit <= 0 means no
// limit.
func (s *Store) ListMessages(sessionID string, limit int) ([]Message, error) {
	query := `SELECT id, from_session, to_session, body, status, attempts, created_at, delivered_at
	          FROM messages WHERE to_session = ? OR from_session = ? ORDER BY id DESC`
	args := []any{sessionID, sessionID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse to ascending order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// NextQueuedMessage returns the queued message with the smallest id for
// recipient to. The second return value is false if there is no queued
// message for to.
func (s *Store) NextQueuedMessage(to string) (Message, bool, error) {
	row := s.db.QueryRow(
		`SELECT id, from_session, to_session, body, status, attempts, created_at, delivered_at
		 FROM messages WHERE to_session = ? AND status = 'queued' ORDER BY id LIMIT 1`, to,
	)
	m, err := scanMessage(row)
	if errors.Is(err, ErrNotFound) {
		return Message{}, false, nil
	}
	if err != nil {
		return Message{}, false, err
	}
	return m, true, nil
}

// UpdateMessageStatus updates a message's status, attempts, and delivered_at
// (0 stored as NULL). Returns ErrNotFound if the message doesn't exist.
func (s *Store) UpdateMessageStatus(id int64, status string, attempts int, deliveredAt int64) error {
	res, err := s.db.Exec(
		`UPDATE messages SET status = ?, attempts = ?, delivered_at = ? WHERE id = ?`,
		status, attempts, nullIfZero(deliveredAt), id,
	)
	if err != nil {
		return fmt.Errorf("update message status: %w", err)
	}
	return checkRowsAffected(res)
}

// ExpireQueuedBefore sets status = 'failed' on all queued messages with
// created_at < ts, and returns the affected messages (post-update, i.e. with
// status "failed").
func (s *Store) ExpireQueuedBefore(ts int64) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT id, from_session, to_session, body, status, attempts, created_at, delivered_at
		 FROM messages WHERE status = 'queued' AND created_at < ?`, ts,
	)
	if err != nil {
		return nil, fmt.Errorf("query expiring messages: %w", err)
	}

	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		m.Status = "failed"
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if len(out) == 0 {
		return nil, nil
	}

	if _, err := s.db.Exec(
		`UPDATE messages SET status = 'failed' WHERE status = 'queued' AND created_at < ?`, ts,
	); err != nil {
		return nil, fmt.Errorf("expire messages: %w", err)
	}

	return out, nil
}

// ListQueuedRecipients returns the distinct to_session values of messages
// currently in status "queued".
func (s *Store) ListQueuedRecipients() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT to_session FROM messages WHERE status = 'queued'`)
	if err != nil {
		return nil, fmt.Errorf("query queued recipients: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var to string
		if err := rows.Scan(&to); err != nil {
			return nil, fmt.Errorf("scan recipient: %w", err)
		}
		out = append(out, to)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// PurgeOld deletes messages and events with created_at/ts < before.
func (s *Store) PurgeOld(before int64) error {
	if _, err := s.db.Exec(`DELETE FROM messages WHERE created_at < ?`, before); err != nil {
		return fmt.Errorf("purge messages: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM events WHERE ts < ?`, before); err != nil {
		return fmt.Errorf("purge events: %w", err)
	}
	return nil
}

func scanMessage(row interface{ Scan(...any) error }) (Message, error) {
	var m Message
	var fromSession sql.NullString
	var deliveredAt sql.NullInt64

	err := row.Scan(
		&m.ID, &fromSession, &m.ToSession, &m.Body, &m.Status, &m.Attempts, &m.CreatedAt, &deliveredAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, fmt.Errorf("scan message: %w", err)
	}

	m.FromSession = fromSession.String
	m.DeliveredAt = deliveredAt.Int64

	return m, nil
}
