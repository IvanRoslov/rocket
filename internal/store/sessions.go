package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Session represents an orchestrator or worker session.
type Session struct {
	ID           string
	Kind         string
	ProjectID    string
	RepoID       string
	FeatureSlug  string
	ParentID     string
	Agent        string
	Branch       string
	WorktreePath string
	TmuxName     string
	State        string
	Activity     string
	Prompt       string
	ActivityTS   int64
	CreatedAt    int64
	UpdatedAt    int64
}

// SessionFilter narrows the results of ListSessions. If All is false, only
// sessions in state 'spawning' or 'running' are returned; otherwise every
// non-empty field is AND-ed into the query.
type SessionFilter struct {
	Kind    string
	Project string
	Feature string
	State   string
	All     bool
}

// AddSession inserts a new session. Returns ErrExists if the id is already taken.
func (s *Store) AddSession(sess Session) error {
	now := time.Now().Unix()
	if sess.CreatedAt == 0 {
		sess.CreatedAt = now
	}
	if sess.UpdatedAt == 0 {
		sess.UpdatedAt = sess.CreatedAt
	}

	_, err := s.db.Exec(
		`INSERT INTO sessions (
			id, kind, project_id, repo_id, feature_slug, parent_id, agent, branch,
			worktree_path, tmux_name, state, activity, activity_ts, prompt,
			created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Kind, sess.ProjectID, sess.RepoID, sess.FeatureSlug,
		nullIfEmpty(sess.ParentID), sess.Agent, sess.Branch, sess.WorktreePath,
		sess.TmuxName, sess.State, nullIfEmpty(sess.Activity), nullIfZero(sess.ActivityTS),
		nullIfEmpty(sess.Prompt), sess.CreatedAt, sess.UpdatedAt,
	)
	if isUniqueViolation(err) {
		return ErrExists
	}
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// GetSession returns the session with the given id, or ErrNotFound.
func (s *Store) GetSession(id string) (Session, error) {
	row := s.db.QueryRow(
		`SELECT id, kind, project_id, repo_id, feature_slug, parent_id, agent, branch,
		        worktree_path, tmux_name, state, activity, activity_ts, prompt,
		        created_at, updated_at
		 FROM sessions WHERE id = ?`, id,
	)
	return scanSession(row)
}

// ListSessions returns sessions matching the filter, ordered by created_at.
func (s *Store) ListSessions(f SessionFilter) ([]Session, error) {
	query := `SELECT id, kind, project_id, repo_id, feature_slug, parent_id, agent, branch,
	                  worktree_path, tmux_name, state, activity, activity_ts, prompt,
	                  created_at, updated_at
	          FROM sessions`

	var conds []string
	var args []any

	if !f.All {
		conds = append(conds, `state IN ('spawning', 'running')`)
	}
	if f.Kind != "" {
		conds = append(conds, `kind = ?`)
		args = append(args, f.Kind)
	}
	if f.Project != "" {
		conds = append(conds, `project_id = ?`)
		args = append(args, f.Project)
	}
	if f.Feature != "" {
		conds = append(conds, `feature_slug = ?`)
		args = append(args, f.Feature)
	}
	if f.State != "" {
		conds = append(conds, `state = ?`)
		args = append(args, f.State)
	}

	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY created_at"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateSessionState updates a session's state and refreshes updated_at.
// Returns ErrNotFound if the session doesn't exist.
func (s *Store) UpdateSessionState(id, state string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET state = ?, updated_at = ? WHERE id = ?`,
		state, time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("update session state: %w", err)
	}
	return checkRowsAffected(res)
}

// UpdateSession updates the full mutable session row and refreshes updated_at.
// Returns ErrNotFound if the session doesn't exist.
func (s *Store) UpdateSession(sess Session) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET
			kind = ?, project_id = ?, repo_id = ?, feature_slug = ?, parent_id = ?,
			agent = ?, branch = ?, worktree_path = ?, tmux_name = ?, state = ?,
			activity = ?, activity_ts = ?, prompt = ?, updated_at = ?
		 WHERE id = ?`,
		sess.Kind, sess.ProjectID, sess.RepoID, sess.FeatureSlug, nullIfEmpty(sess.ParentID),
		sess.Agent, sess.Branch, sess.WorktreePath, sess.TmuxName, sess.State,
		nullIfEmpty(sess.Activity), nullIfZero(sess.ActivityTS), nullIfEmpty(sess.Prompt),
		time.Now().Unix(), sess.ID,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return checkRowsAffected(res)
}

// UpdateSessionActivity updates a session's activity, activity_ts, and refreshes updated_at.
// Returns ErrNotFound if the session doesn't exist.
func (s *Store) UpdateSessionActivity(id, state string, ts int64) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET activity = ?, activity_ts = ?, updated_at = ? WHERE id = ?`,
		nullIfEmpty(state), nullIfZero(ts), time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("update session activity: %w", err)
	}
	return checkRowsAffected(res)
}

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var sess Session
	var parentID, activity, prompt sql.NullString
	var activityTS sql.NullInt64

	err := row.Scan(
		&sess.ID, &sess.Kind, &sess.ProjectID, &sess.RepoID, &sess.FeatureSlug,
		&parentID, &sess.Agent, &sess.Branch, &sess.WorktreePath, &sess.TmuxName,
		&sess.State, &activity, &activityTS, &prompt, &sess.CreatedAt, &sess.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("scan session: %w", err)
	}

	sess.ParentID = parentID.String
	sess.Activity = activity.String
	sess.Prompt = prompt.String
	sess.ActivityTS = activityTS.Int64

	return sess, nil
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullIfZero(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
