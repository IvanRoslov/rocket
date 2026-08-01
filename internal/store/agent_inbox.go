package store

import (
	"fmt"
	"strings"
	"time"
)

// Inbox event kinds and statuses. Kinds are validated at the API/CLI edge
// (see internal/api/agents.go); the store keeps them as free strings so a
// later event source can be added without a migration.
const (
	InboxStatusQueued    = "queued"
	InboxStatusDelivered = "delivered"
	InboxStatusDone      = "done"
)

// AgentInboxEvent is one durable event in a role's inbox. Inbox events are
// what wakes a role instance (task #642); this layer only stores them.
type AgentInboxEvent struct {
	ID        int64
	RoleID    string
	Kind      string
	Payload   string // JSON object, "{}" when absent
	Status    string // queued|delivered|done
	CreatedAt int64
	UpdatedAt int64
}

const inboxColumns = `id, role_id, kind, payload, status, created_at, updated_at`

// EnqueueInboxEvent appends an event to a role's inbox and returns its id.
// Status defaults to queued and payload to an empty JSON object.
func (s *Store) EnqueueInboxEvent(e AgentInboxEvent) (int64, error) {
	if e.Status == "" {
		e.Status = InboxStatusQueued
	}
	if strings.TrimSpace(e.Payload) == "" {
		e.Payload = "{}"
	}
	now := time.Now().Unix()
	if e.CreatedAt == 0 {
		e.CreatedAt = now
	}
	if e.UpdatedAt == 0 {
		e.UpdatedAt = e.CreatedAt
	}

	res, err := s.db.Exec(
		`INSERT INTO agent_inbox (role_id, kind, payload, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.RoleID, e.Kind, e.Payload, e.Status, e.CreatedAt, e.UpdatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert inbox event: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("inbox event id: %w", err)
	}
	return id, nil
}

// ListInboxEvents returns a role's events oldest-first. An empty status
// returns every event; limit <= 0 means no limit.
func (s *Store) ListInboxEvents(roleID, status string, limit int) ([]AgentInboxEvent, error) {
	query := `SELECT ` + inboxColumns + ` FROM agent_inbox WHERE role_id = ?`
	args := []any{roleID}
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
		return nil, fmt.Errorf("query inbox events: %w", err)
	}
	defer rows.Close()

	var out []AgentInboxEvent
	for rows.Next() {
		var e AgentInboxEvent
		if err := rows.Scan(&e.ID, &e.RoleID, &e.Kind, &e.Payload, &e.Status,
			&e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan inbox event: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// QueuedInboxEvents returns a role's undelivered events, oldest first.
func (s *Store) QueuedInboxEvents(roleID string) ([]AgentInboxEvent, error) {
	return s.ListInboxEvents(roleID, InboxStatusQueued, 0)
}

// CountQueuedInboxEvents returns how many events are waiting in a role's inbox.
func (s *Store) CountQueuedInboxEvents(roleID string) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM agent_inbox WHERE role_id = ? AND status = ?`,
		roleID, InboxStatusQueued,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count queued inbox events: %w", err)
	}
	return n, nil
}

// MarkInboxDelivered moves events to the delivered status.
func (s *Store) MarkInboxDelivered(ids []int64) error {
	return s.setInboxStatus(ids, InboxStatusDelivered)
}

// MarkInboxDone moves events to the done status (the role has processed them).
func (s *Store) MarkInboxDone(ids []int64) error {
	return s.setInboxStatus(ids, InboxStatusDone)
}

func (s *Store) setInboxStatus(ids []int64, status string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+2)
	args = append(args, status, time.Now().Unix())
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.db.Exec(
		`UPDATE agent_inbox SET status = ?, updated_at = ? WHERE id IN (`+placeholders+`)`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("update inbox status: %w", err)
	}
	return nil
}
