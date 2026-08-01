package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// AgentSubscription is one GitHub subscription of a role: a repository, an
// optional label filter, and whether only issues/comments mentioning the role
// count.
type AgentSubscription struct {
	Repo        string   `json:"repo"`
	Labels      []string `json:"labels,omitempty"`
	MentionOnly bool     `json:"mention_only,omitempty"`
}

// Agent represents a registered agent role: a durable definition whose runs
// are ephemeral sessions of kind 'agent' (see docs/10-agents.md).
type Agent struct {
	ID            string
	ProjectID     string
	PromptPath    string
	Subscriptions []AgentSubscription
	Cron          string
	// Agent is the underlying coding agent (claude-code|codex) role
	// instances are spawned with; empty means "daemon default".
	Agent     string
	Enabled   bool
	CreatedAt int64
	UpdatedAt int64
}

const agentColumns = `id, project_id, prompt_path, subscriptions, cron, agent, enabled, created_at, updated_at`

// AddAgent inserts a new role. Returns ErrExists if the id is already taken.
func (s *Store) AddAgent(a Agent) error {
	now := time.Now().Unix()
	if a.CreatedAt == 0 {
		a.CreatedAt = now
	}
	if a.UpdatedAt == 0 {
		a.UpdatedAt = a.CreatedAt
	}

	subsJSON, err := marshalOrEmpty(a.Subscriptions, "[]")
	if err != nil {
		return fmt.Errorf("marshal subscriptions: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO agents (`+agentColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ProjectID, a.PromptPath, subsJSON, a.Cron, a.Agent, boolToInt(a.Enabled),
		a.CreatedAt, a.UpdatedAt,
	)
	if isUniqueViolation(err) {
		return ErrExists
	}
	if err != nil {
		return fmt.Errorf("insert agent: %w", err)
	}
	return nil
}

// GetAgent returns the role with the given id, or ErrNotFound.
func (s *Store) GetAgent(id string) (Agent, error) {
	row := s.db.QueryRow(`SELECT `+agentColumns+` FROM agents WHERE id = ?`, id)
	return scanAgent(row)
}

// ListAgents returns roles ordered by id. An empty projectID returns every
// role; otherwise only the roles of that project.
func (s *Store) ListAgents(projectID string) ([]Agent, error) {
	query := `SELECT ` + agentColumns + ` FROM agents`
	var args []any
	if projectID != "" {
		query += ` WHERE project_id = ?`
		args = append(args, projectID)
	}
	query += ` ORDER BY id`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}
	defer rows.Close()

	var out []Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateAgent updates a role's mutable fields (everything but the id and
// created_at) and stamps updated_at. Returns ErrNotFound if it doesn't exist.
func (s *Store) UpdateAgent(a Agent) error {
	subsJSON, err := marshalOrEmpty(a.Subscriptions, "[]")
	if err != nil {
		return fmt.Errorf("marshal subscriptions: %w", err)
	}

	res, err := s.db.Exec(
		`UPDATE agents SET project_id = ?, prompt_path = ?, subscriptions = ?, cron = ?,
		        agent = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		a.ProjectID, a.PromptPath, subsJSON, a.Cron, a.Agent, boolToInt(a.Enabled),
		time.Now().Unix(), a.ID,
	)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	return checkRowsAffected(res)
}

// DeleteAgent removes a role together with its inbox events and dossier
// items. Returns ErrNotFound if the role doesn't exist. Files under the role
// home directory are deliberately left on disk.
func (s *Store) DeleteAgent(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete agent: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM agent_inbox WHERE role_id = ?`, id); err != nil {
		return fmt.Errorf("delete agent inbox: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM agent_items WHERE role_id = ?`, id); err != nil {
		return fmt.Errorf("delete agent items: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	if err := checkRowsAffected(res); err != nil {
		return err
	}
	return tx.Commit()
}

func scanAgent(row interface{ Scan(...any) error }) (Agent, error) {
	var a Agent
	var subsJSON string
	var enabled int

	err := row.Scan(&a.ID, &a.ProjectID, &a.PromptPath, &subsJSON, &a.Cron, &a.Agent,
		&enabled, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	if err != nil {
		return Agent{}, fmt.Errorf("scan agent: %w", err)
	}

	if err := json.Unmarshal([]byte(subsJSON), &a.Subscriptions); err != nil {
		return Agent{}, fmt.Errorf("unmarshal subscriptions: %w", err)
	}
	a.Enabled = enabled != 0
	return a, nil
}
