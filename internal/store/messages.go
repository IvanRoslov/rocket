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
	DeliveredAt int64  // 0 = not delivered
	Reason      string // empty = none; set on status "failed" (e.g. "timeout", "delivery_failed", "recipient_gone")
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
		`SELECT id, from_session, to_session, body, status, attempts, created_at, delivered_at, reason
		 FROM messages WHERE id = ?`, id,
	)
	return scanMessage(row)
}

// ListMessages returns the last limit messages where to_session = sessionID
// or from_session = sessionID, in ascending id order. A limit <= 0 means no
// limit.
func (s *Store) ListMessages(sessionID string, limit int) ([]Message, error) {
	query := `SELECT id, from_session, to_session, body, status, attempts, created_at, delivered_at, reason
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
		`SELECT id, from_session, to_session, body, status, attempts, created_at, delivered_at, reason
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

// ClaimMessage atomically transitions message id from status "queued" to
// "delivering" via a compare-and-swap UPDATE. It returns true if this call
// won the claim (RowsAffected == 1); false means the message was no longer
// "queued" (e.g. it was already claimed, or expired/failed concurrently by
// housekeeping) and the caller must not proceed with delivery.
//
// This exists to close a race between a live delivery worker and
// expireTimedOut: without a CAS, a worker that read a message as "queued"
// could still inject and mark it "delivered" after housekeeping had already
// marked the same message "failed" for timing out, silently reviving a
// message that should have stayed failed.
func (s *Store) ClaimMessage(id int64) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE messages SET status = 'delivering' WHERE id = ? AND status = 'queued'`, id,
	)
	if err != nil {
		return false, fmt.Errorf("claim message: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim message rows affected: %w", err)
	}
	return n == 1, nil
}

// UpdateMessageStatus updates a message's status, attempts, delivered_at
// (0 stored as NULL), and reason (empty string stored as NULL). Returns
// ErrNotFound if the message doesn't exist.
func (s *Store) UpdateMessageStatus(id int64, status string, attempts int, deliveredAt int64, reason string) error {
	res, err := s.db.Exec(
		`UPDATE messages SET status = ?, attempts = ?, delivered_at = ?, reason = ? WHERE id = ?`,
		status, attempts, nullIfZero(deliveredAt), nullIfEmpty(reason), id,
	)
	if err != nil {
		return fmt.Errorf("update message status: %w", err)
	}
	return checkRowsAffected(res)
}

// ExpireQueuedBefore sets status = 'failed' on all queued messages with
// created_at < ts, and returns the affected messages (post-update, i.e. with
// status "failed").
//
// The select-and-update is wrapped in a single transaction (using
// UPDATE ... RETURNING) so that a message which transitions from "queued" to
// "delivering" concurrently (between a separate SELECT and UPDATE) can never
// be spuriously expired: the WHERE clause is evaluated atomically against the
// same row state that gets updated.
func (s *Store) ExpireQueuedBefore(ts int64) ([]Message, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin expire tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op if committed

	rows, err := tx.Query(
		`UPDATE messages SET status = 'failed', reason = 'timeout'
		 WHERE status = 'queued' AND created_at < ?
		 RETURNING id, from_session, to_session, body, status, attempts, created_at, delivered_at, reason`, ts,
	)
	if err != nil {
		return nil, fmt.Errorf("expire messages: %w", err)
	}

	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit expire tx: %w", err)
	}

	return out, nil
}

// ResetDelivering resets every message stuck in status "delivering" back to
// "queued". It exists to recover messages orphaned by a daemon crash: since
// no query anywhere reads status = 'delivering' back into the live queue,
// such messages would otherwise be silently lost forever, breaking FIFO for
// anything queued behind them. It is intended to be called once, at
// Queue.Run startup, before any delivery workers are spawned.
//
// Tradeoff: if the crash happened after the recipient actually received the
// injected text but before the status update was persisted, this causes a
// duplicate re-delivery of that one message. That is accepted as the lesser
// evil versus silently losing the message forever.
func (s *Store) ResetDelivering() (int, error) {
	res, err := s.db.Exec(`UPDATE messages SET status = 'queued' WHERE status = 'delivering'`)
	if err != nil {
		return 0, fmt.Errorf("reset delivering messages: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reset delivering messages rows affected: %w", err)
	}
	return int(n), nil
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

// CountMessagesByStatus returns the number of messages in each status
// (queued, delivering, delivered, failed), keyed by status. Statuses with
// no messages are omitted from the map (i.e. absent means zero).
func (s *Store) CountMessagesByStatus() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM messages GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("count messages by status: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan message status count: %w", err)
		}
		out[status] = count
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
	var reason sql.NullString

	err := row.Scan(
		&m.ID, &fromSession, &m.ToSession, &m.Body, &m.Status, &m.Attempts, &m.CreatedAt, &deliveredAt, &reason,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, fmt.Errorf("scan message: %w", err)
	}

	m.FromSession = fromSession.String
	m.DeliveredAt = deliveredAt.Int64
	m.Reason = reason.String

	return m, nil
}
