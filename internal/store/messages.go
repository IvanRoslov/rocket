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
	Status      string // queued|delivering|accepted|delivered|failed
	Attempts    int
	CreatedAt   int64
	DeliveredAt int64  // 0 = not delivered
	Reason      string // empty = none; set on status "failed" (e.g. "timeout", "delivery_failed", "recipient_gone", "not_confirmed")
	// InjectAt is the unix-millisecond moment right before a send whose agent
	// confirms delivery from its journal (status "accepted"); 0 otherwise.
	InjectAt          int64
	DeliveryTransport string
	DeliveryProofAt   int64
	DeliveryProofText string
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
		`INSERT INTO messages (from_session, to_session, body, status, attempts, created_at, delivered_at, delivery_transport)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		nullIfEmpty(m.FromSession), m.ToSession, m.Body, m.Status, m.Attempts, m.CreatedAt, nullIfZero(m.DeliveredAt), m.DeliveryTransport,
	)
	if err != nil {
		return 0, fmt.Errorf("insert message: %w", err)
	}
	return res.LastInsertId()
}

// GetMessage returns the message with the given id, or ErrNotFound.
func (s *Store) GetMessage(id int64) (Message, error) {
	row := s.db.QueryRow(
		`SELECT id, from_session, to_session, body, status, attempts, created_at, delivered_at, reason, inject_at, delivery_transport, delivery_proof_at, delivery_proof_text
		 FROM messages WHERE id = ?`, id,
	)
	return scanMessage(row)
}

// ListMessages returns the last limit messages where to_session = sessionID
// or from_session = sessionID, in ascending id order. A limit <= 0 means no
// limit.
func (s *Store) ListMessages(sessionID string, limit int) ([]Message, error) {
	query := `SELECT id, from_session, to_session, body, status, attempts, created_at, delivered_at, reason, inject_at, delivery_transport, delivery_proof_at, delivery_proof_text
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
		`SELECT id, from_session, to_session, body, status, attempts, created_at, delivered_at, reason, inject_at, delivery_transport, delivery_proof_at, delivery_proof_text
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

func (s *Store) ClaimNativeMessage(id int64, transport string, injectAt int64) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE messages SET status = 'delivering', delivery_transport = ?, inject_at = ? WHERE id = ? AND status = 'queued'`,
		transport, injectAt, id,
	)
	if err != nil {
		return false, fmt.Errorf("claim native message: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim native message rows affected: %w", err)
	}
	return n == 1, nil
}

func (s *Store) SetMessageNativeDelivery(id int64, transport string, injectAt int64) error {
	res, err := s.db.Exec(
		`UPDATE messages SET delivery_transport = ?, inject_at = ? WHERE id = ? AND status = 'delivering'`,
		transport, injectAt, id,
	)
	if err != nil {
		return fmt.Errorf("set native delivery: %w", err)
	}
	return checkRowsAffected(res)
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

// SetMessageInjectAt records the unix-millisecond moment right before the
// text of message id is pasted (see Message.InjectAt).
func (s *Store) SetMessageInjectAt(id int64, injectAt int64) error {
	if _, err := s.db.Exec(`UPDATE messages SET inject_at = ? WHERE id = ?`, injectAt, id); err != nil {
		return fmt.Errorf("set message inject_at: %w", err)
	}
	return nil
}

// ListAcceptedMessages returns every message in status "accepted", ordered
// by recipient and then by id, so a confirmer can walk one recipient's
// messages oldest-first in a single pass.
func (s *Store) ListAcceptedMessages() ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT id, from_session, to_session, body, status, attempts, created_at, delivered_at, reason, inject_at, delivery_transport, delivery_proof_at, delivery_proof_text
		 FROM messages WHERE status = 'accepted' ORDER BY to_session, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query accepted messages: %w", err)
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
	return out, rows.Err()
}

// ConfirmAccepted atomically moves message id from "accepted" to
// "delivered" with delivered_at = deliveredAt. It reports false when the
// message was no longer accepted (already requeued, failed or confirmed by a
// concurrent pass), so a stale decision never overwrites a newer status.
func (s *Store) ConfirmAccepted(id, deliveredAt, proofAt int64, proofText string) (bool, error) {
	return s.casAccepted(id,
		`UPDATE messages SET status = 'delivered', delivered_at = ?, delivery_proof_at = ?, delivery_proof_text = ? WHERE id = ? AND status = 'accepted'`,
		deliveredAt, proofAt, proofText, id)
}

// RequeueAccepted atomically moves message id from "accepted" back to
// "queued" and clears inject_at so the next paste takes a fresh one.
// Attempts are left alone: they count pastes, and the worker increments
// them when it pastes again. False means the message was no longer accepted.
func (s *Store) RequeueAccepted(id int64) (bool, error) {
	return s.casAccepted(id,
		`UPDATE messages SET status = 'queued', inject_at = 0 WHERE id = ? AND status = 'accepted'`,
		id)
}

// FailAccepted atomically moves message id from "accepted" to "failed" with
// the given reason. False means the message was no longer accepted.
func (s *Store) FailAccepted(id int64, reason string) (bool, error) {
	return s.casAccepted(id,
		`UPDATE messages SET status = 'failed', reason = ? WHERE id = ? AND status = 'accepted'`,
		nullIfEmpty(reason), id)
}

func (s *Store) casAccepted(id int64, query string, args ...any) (bool, error) {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return false, fmt.Errorf("update accepted message %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("update accepted message %d rows affected: %w", id, err)
	}
	return n == 1, nil
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
		 RETURNING id, from_session, to_session, body, status, attempts, created_at, delivered_at, reason, inject_at, delivery_transport, delivery_proof_at, delivery_proof_text`, ts,
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

// ResetDelivering recovers messages orphaned in status "delivering" by a
// daemon crash. Legacy rows become queued for another attempt; native rows
// become accepted so their journal, not a blind re-send, settles delivery.
func (s *Store) ResetDelivering() (int, error) {
	res, err := s.db.Exec(`UPDATE messages SET status = CASE WHEN delivery_transport = 'pi-uds-v1' THEN 'accepted' ELSE 'queued' END WHERE status = 'delivering'`)
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
		&m.ID, &fromSession, &m.ToSession, &m.Body, &m.Status, &m.Attempts, &m.CreatedAt, &deliveredAt, &reason, &m.InjectAt, &m.DeliveryTransport,
		&m.DeliveryProofAt, &m.DeliveryProofText,
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
