package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AgentItem is one entry in a role's dossier: an issue, task or ping the role
// is tracking, plus the state it is in. States are free strings on purpose —
// the dossier is the role's notebook, not a daemon state machine. The
// canonical set is new|triaged|taken|deferred|waiting_team|in_work|resolved|closed.
type AgentItem struct {
	ID          int64
	RoleID      string
	Kind        string // issue|task|ping
	ExternalRef string // owner/repo#123 | task:45 | msg:<id>
	State       string
	Note        string
	TaskID      int64 // 0 = no rocket task attached
	SnoozeUntil int64 // unix seconds, 0 = not snoozed
	CreatedAt   int64
	UpdatedAt   int64
}

const agentItemColumns = `id, role_id, kind, external_ref, state, note, task_id, snooze_until, created_at, updated_at`

// UpsertAgentItem inserts or updates the dossier entry identified by
// (role_id, kind, external_ref) and returns the stored row. On update the id
// and created_at are preserved.
func (s *Store) UpsertAgentItem(it AgentItem) (AgentItem, error) {
	if it.State == "" {
		it.State = "new"
	}
	now := time.Now().Unix()
	if it.CreatedAt == 0 {
		it.CreatedAt = now
	}
	it.UpdatedAt = now

	_, err := s.db.Exec(
		`INSERT INTO agent_items (role_id, kind, external_ref, state, note, task_id,
		        snooze_until, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (role_id, kind, external_ref) DO UPDATE SET
		        state = excluded.state,
		        note = excluded.note,
		        task_id = excluded.task_id,
		        snooze_until = excluded.snooze_until,
		        updated_at = excluded.updated_at`,
		it.RoleID, it.Kind, it.ExternalRef, it.State, it.Note,
		nullIfZero(it.TaskID), nullIfZero(it.SnoozeUntil), it.CreatedAt, it.UpdatedAt,
	)
	if err != nil {
		return AgentItem{}, fmt.Errorf("upsert agent item: %w", err)
	}
	return s.GetAgentItem(it.RoleID, it.Kind, it.ExternalRef)
}

// GetAgentItem returns one dossier entry, or ErrNotFound.
func (s *Store) GetAgentItem(roleID, kind, ref string) (AgentItem, error) {
	row := s.db.QueryRow(
		`SELECT `+agentItemColumns+` FROM agent_items
		 WHERE role_id = ? AND kind = ? AND external_ref = ?`,
		roleID, kind, ref,
	)
	return scanAgentItem(row)
}

// ListAgentItems returns a role's dossier ordered by most recently updated.
// An empty state returns every entry.
func (s *Store) ListAgentItems(roleID, state string) ([]AgentItem, error) {
	query := `SELECT ` + agentItemColumns + ` FROM agent_items WHERE role_id = ?`
	args := []any{roleID}
	if state != "" {
		query += ` AND state = ?`
		args = append(args, state)
	}
	query += ` ORDER BY updated_at DESC, id DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query agent items: %w", err)
	}
	defer rows.Close()

	var out []AgentItem
	for rows.Next() {
		it, err := scanAgentItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanAgentItem(row interface{ Scan(...any) error }) (AgentItem, error) {
	var it AgentItem
	var taskID, snooze sql.NullInt64

	err := row.Scan(&it.ID, &it.RoleID, &it.Kind, &it.ExternalRef, &it.State, &it.Note,
		&taskID, &snooze, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentItem{}, ErrNotFound
	}
	if err != nil {
		return AgentItem{}, fmt.Errorf("scan agent item: %w", err)
	}
	it.TaskID = taskID.Int64
	it.SnoozeUntil = snooze.Int64
	return it, nil
}

// DueSnoozedItems returns dossier entries whose snooze_until has come due at
// or before now, oldest deadline first. The scheduler turns each into a
// snooze_expired inbox event and then clears the deadline with
// ClearAgentItemSnooze so it fires exactly once.
func (s *Store) DueSnoozedItems(now int64) ([]AgentItem, error) {
	rows, err := s.db.Query(
		`SELECT `+agentItemColumns+` FROM agent_items
		 WHERE snooze_until IS NOT NULL AND snooze_until > 0 AND snooze_until <= ?
		 ORDER BY snooze_until, id`,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("query due snoozed items: %w", err)
	}
	defer rows.Close()

	var out []AgentItem
	for rows.Next() {
		it, err := scanAgentItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ClearAgentItemSnooze drops an entry's snooze deadline (leaving its state
// and note untouched) and stamps updated_at. Returns ErrNotFound if the entry
// doesn't exist.
func (s *Store) ClearAgentItemSnooze(id int64) error {
	res, err := s.db.Exec(
		`UPDATE agent_items SET snooze_until = NULL, updated_at = ? WHERE id = ?`,
		time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("clear agent item snooze: %w", err)
	}
	return checkRowsAffected(res)
}
