package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Agent is a registered persistent agent: a name, an optional human-facing
// description, and optional launcher fields. Rocket does not manage the agent
// itself — the id doubles as the name of the tmux session it lives in (see
// docs/10-agents.md).
type Agent struct {
	ID          string
	Description string
	// ProjectID groups agents in the UI. Empty means "no project"; it grants
	// and restricts nothing.
	ProjectID string
	// Dir and Command are the launcher pair used by `rocket agent start`:
	// the working directory and the command to run there. Both optional.
	Dir       string
	Command   string
	Enabled   bool
	CreatedAt int64
	UpdatedAt int64
}

const agentColumns = `id, description, project_id, dir, command, enabled, created_at, updated_at`

// AddAgent inserts a new agent. Returns ErrExists if the id is already taken.
func (s *Store) AddAgent(a Agent) error {
	now := time.Now().Unix()
	if a.CreatedAt == 0 {
		a.CreatedAt = now
	}
	if a.UpdatedAt == 0 {
		a.UpdatedAt = a.CreatedAt
	}

	_, err := s.db.Exec(
		`INSERT INTO agents (`+agentColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Description, a.ProjectID, a.Dir, a.Command, boolToInt(a.Enabled),
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

// GetAgent returns the agent with the given id, or ErrNotFound.
func (s *Store) GetAgent(id string) (Agent, error) {
	row := s.db.QueryRow(`SELECT `+agentColumns+` FROM agents WHERE id = ?`, id)
	return scanAgent(row)
}

// ListAgents returns agents ordered by id. An empty projectID returns every
// agent; otherwise only the agents of that project.
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

// UpdateAgent updates an agent's mutable fields (everything but the id and
// created_at) and stamps updated_at. Returns ErrNotFound if it doesn't exist.
func (s *Store) UpdateAgent(a Agent) error {
	res, err := s.db.Exec(
		`UPDATE agents SET description = ?, project_id = ?, dir = ?, command = ?,
		        enabled = ?, updated_at = ? WHERE id = ?`,
		a.Description, a.ProjectID, a.Dir, a.Command, boolToInt(a.Enabled),
		time.Now().Unix(), a.ID,
	)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	return checkRowsAffected(res)
}

// DeleteAgent removes an agent together with its inbox and its Q&A threads.
// Returns ErrNotFound if the agent doesn't exist. Nothing on disk is touched:
// the agent's directory belongs to its author, not to rocket.
func (s *Store) DeleteAgent(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete agent: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM agent_inbox WHERE agent_id = ?`, id); err != nil {
		return fmt.Errorf("delete agent inbox: %w", err)
	}
	// Role threads share the unified tables with task threads, so every delete
	// is scoped by role_id — a task thread must not be caught by it.
	if _, err := tx.Exec(
		`DELETE FROM question_participants WHERE question_id IN
		 (SELECT id FROM questions WHERE role_id = ?)`, id); err != nil {
		return fmt.Errorf("delete agent question participants: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM question_messages WHERE question_id IN
		 (SELECT id FROM questions WHERE role_id = ?)`, id); err != nil {
		return fmt.Errorf("delete agent question messages: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM questions WHERE role_id = ?`, id); err != nil {
		return fmt.Errorf("delete agent questions: %w", err)
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
	var enabled int

	err := row.Scan(&a.ID, &a.Description, &a.ProjectID, &a.Dir, &a.Command,
		&enabled, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	if err != nil {
		return Agent{}, fmt.Errorf("scan agent: %w", err)
	}

	a.Enabled = enabled != 0
	return a, nil
}
