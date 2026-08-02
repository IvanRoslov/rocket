package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Inbox message statuses. An inbox message is written when a message is
// addressed to an agent whose tmux session is not alive; the agent drains it
// itself with `rocket inbox next` (see docs/10-agents.md).
const (
	InboxUnread = "unread"
	InboxRead   = "read"
)

// InboxMessage is one message waiting in an agent's inbox.
type InboxMessage struct {
	ID      int64
	AgentID string
	// From is the sender's session id; empty for the human/UI.
	From      string
	Body      string
	Status    string // unread|read
	CreatedAt int64
	ReadAt    int64
}

const inboxColumns = `id, agent_id, from_id, body, status, created_at, read_at`

// AddInboxMessage appends a message to an agent's inbox and returns its id.
// Status defaults to unread.
func (s *Store) AddInboxMessage(m InboxMessage) (int64, error) {
	if m.Status == "" {
		m.Status = InboxUnread
	}
	if m.CreatedAt == 0 {
		m.CreatedAt = time.Now().Unix()
	}

	res, err := s.db.Exec(
		`INSERT INTO agent_inbox (agent_id, from_id, body, status, created_at, read_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		m.AgentID, m.From, m.Body, m.Status, m.CreatedAt, nullIfZero(m.ReadAt),
	)
	if err != nil {
		return 0, fmt.Errorf("insert inbox message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("inbox message id: %w", err)
	}
	return id, nil
}

// ListInboxMessages returns an agent's messages oldest-first. An empty status
// returns every message; limit <= 0 means no limit.
func (s *Store) ListInboxMessages(agentID, status string, limit int) ([]InboxMessage, error) {
	query := `SELECT ` + inboxColumns + ` FROM agent_inbox WHERE agent_id = ?`
	args := []any{agentID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY id`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query inbox messages: %w", err)
	}
	defer rows.Close()

	var out []InboxMessage
	for rows.Next() {
		m, err := scanInboxMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetInboxMessage returns one message by id, or ErrNotFound.
func (s *Store) GetInboxMessage(id int64) (InboxMessage, error) {
	row := s.db.QueryRow(`SELECT `+inboxColumns+` FROM agent_inbox WHERE id = ?`, id)
	return scanInboxMessage(row)
}

// NextUnreadInboxMessage takes an agent's oldest unread message and marks it
// read, atomically: two concurrent `rocket inbox next` calls never hand out the
// same message twice. ok is false when the inbox holds nothing unread.
func (s *Store) NextUnreadInboxMessage(agentID string) (InboxMessage, bool, error) {
	for {
		tx, err := s.db.Begin()
		if err != nil {
			return InboxMessage{}, false, fmt.Errorf("begin inbox next: %w", err)
		}

		row := tx.QueryRow(
			`SELECT `+inboxColumns+` FROM agent_inbox
			 WHERE agent_id = ? AND status = ? ORDER BY id LIMIT 1`,
			agentID, InboxUnread,
		)
		m, err := scanInboxMessage(row)
		if errors.Is(err, ErrNotFound) {
			tx.Rollback()
			return InboxMessage{}, false, nil
		}
		if err != nil {
			tx.Rollback()
			return InboxMessage{}, false, err
		}

		now := time.Now().Unix()
		res, err := tx.Exec(
			`UPDATE agent_inbox SET status = ?, read_at = ? WHERE id = ? AND status = ?`,
			InboxRead, now, m.ID, InboxUnread,
		)
		if err != nil {
			tx.Rollback()
			return InboxMessage{}, false, fmt.Errorf("mark inbox message read: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			tx.Rollback()
			return InboxMessage{}, false, fmt.Errorf("mark inbox message read: %w", err)
		}
		if n == 0 {
			// Someone else took it between the select and the update; retry.
			tx.Rollback()
			continue
		}
		if err := tx.Commit(); err != nil {
			return InboxMessage{}, false, fmt.Errorf("commit inbox next: %w", err)
		}

		m.Status = InboxRead
		m.ReadAt = now
		return m, true, nil
	}
}

// CountUnreadInbox returns how many unread messages an agent has.
func (s *Store) CountUnreadInbox(agentID string) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM agent_inbox WHERE agent_id = ? AND status = ?`,
		agentID, InboxUnread,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count unread inbox: %w", err)
	}
	return n, nil
}

// MaxUnreadInboxID returns the highest id among an agent's unread messages, or
// 0 when it has none. The unread notifier uses it to tell "the same unread pile
// I already announced" from "something new arrived since".
func (s *Store) MaxUnreadInboxID(agentID string) (int64, error) {
	var id sql.NullInt64
	err := s.db.QueryRow(
		`SELECT MAX(id) FROM agent_inbox WHERE agent_id = ? AND status = ?`,
		agentID, InboxUnread,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("max unread inbox id: %w", err)
	}
	if !id.Valid {
		return 0, nil
	}
	return id.Int64, nil
}

func scanInboxMessage(row interface{ Scan(...any) error }) (InboxMessage, error) {
	var m InboxMessage
	var from sql.NullString
	var readAt sql.NullInt64

	err := row.Scan(&m.ID, &m.AgentID, &from, &m.Body, &m.Status, &m.CreatedAt, &readAt)
	if errors.Is(err, sql.ErrNoRows) {
		return InboxMessage{}, ErrNotFound
	}
	if err != nil {
		return InboxMessage{}, fmt.Errorf("scan inbox message: %w", err)
	}

	m.From = from.String
	m.ReadAt = readAt.Int64
	return m, nil
}
