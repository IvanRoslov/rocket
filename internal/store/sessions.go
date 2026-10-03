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
	TaskID       int64 // 0 = no linked root task
	SubtaskID    int64 // 0 = orchestrator or no linked subtask
	Agent        string
	Branch       string
	WorktreePath string
	TmuxName     string
	State        string
	Activity     string
	Prompt       string
	ActivityTS   int64
	PRNumber     int
	PRState      string
	CIState      string
	// PRCheckedAt is the unix time of the last successful GitHub poll for
	// this session (0 = never polled). It answers "how much can I trust
	// PRState/CIState", which pr_state alone cannot: a session that stopped
	// being polled keeps its last known state forever.
	PRCheckedAt int64
	PendingQuiz string
	// Profile, Model and Effort snapshot the model profile the session was
	// launched with (task #5026). Restore relaunches from this snapshot, so
	// later edits to the registry never change a running session.
	Profile   string
	Model     string
	Effort    string
	CreatedAt int64
	UpdatedAt int64
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
			worktree_path, tmux_name, state, activity, activity_ts, pr_number, pr_state,
			ci_state, prompt, pending_quiz, pr_checked_at, profile, model, effort, created_at, updated_at,
			task_id, subtask_id
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Kind, sess.ProjectID, sess.RepoID, sess.FeatureSlug,
		nullIfEmpty(sess.ParentID), sess.Agent, sess.Branch, sess.WorktreePath,
		sess.TmuxName, sess.State, nullIfEmpty(sess.Activity), nullIfZero(sess.ActivityTS),
		nullIfZero(int64(sess.PRNumber)), nullIfEmpty(sess.PRState), nullIfEmpty(sess.CIState),
		nullIfEmpty(sess.Prompt), nullIfEmpty(sess.PendingQuiz), nullIfZero(sess.PRCheckedAt),
		sess.Profile, sess.Model, sess.Effort, sess.CreatedAt, sess.UpdatedAt,
		nullIfZero(sess.TaskID), nullIfZero(sess.SubtaskID),
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
		        worktree_path, tmux_name, state, activity, activity_ts, pr_number, pr_state,
		        ci_state, prompt, pending_quiz, pr_checked_at, profile, model, effort, created_at, updated_at,
		        task_id, subtask_id
		 FROM sessions WHERE id = ?`, id,
	)
	return scanSession(row)
}

// ListSessions returns sessions matching the filter, ordered by created_at.
func (s *Store) ListSessions(f SessionFilter) ([]Session, error) {
	query := `SELECT id, kind, project_id, repo_id, feature_slug, parent_id, agent, branch,
	                  worktree_path, tmux_name, state, activity, activity_ts, pr_number, pr_state,
	                  ci_state, prompt, pending_quiz, pr_checked_at, profile, model, effort, created_at, updated_at,
	                  task_id, subtask_id
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
			activity = ?, activity_ts = ?, pr_number = ?, pr_state = ?, ci_state = ?,
			prompt = ?, pending_quiz = ?, pr_checked_at = ?, profile = ?, model = ?, effort = ?,
			task_id = ?, subtask_id = ?,
			updated_at = ?
		 WHERE id = ?`,
		sess.Kind, sess.ProjectID, sess.RepoID, sess.FeatureSlug, nullIfEmpty(sess.ParentID),
		sess.Agent, sess.Branch, sess.WorktreePath, sess.TmuxName, sess.State,
		nullIfEmpty(sess.Activity), nullIfZero(sess.ActivityTS),
		nullIfZero(int64(sess.PRNumber)), nullIfEmpty(sess.PRState), nullIfEmpty(sess.CIState),
		nullIfEmpty(sess.Prompt), nullIfEmpty(sess.PendingQuiz), nullIfZero(sess.PRCheckedAt),
		sess.Profile, sess.Model, sess.Effort, nullIfZero(sess.TaskID), nullIfZero(sess.SubtaskID),
		time.Now().Unix(), sess.ID,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return checkRowsAffected(res)
}

// UpdateSessionPR updates a session's PR/CI fields and refreshes updated_at
// and pr_checked_at. The freshness stamp belongs here because the only writer
// is the GitHub poller: a PR/CI write is by construction the result of a
// successful poll. Polls that find nothing changed never reach this method and
// stamp freshness through MarkSessionPRChecked instead.
// Returns ErrNotFound if the session doesn't exist.
func (s *Store) UpdateSessionPR(id string, number int, prState, ciState string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET pr_number = ?, pr_state = ?, ci_state = ?,
		        pr_checked_at = ?, updated_at = ? WHERE id = ?`,
		nullIfZero(int64(number)), nullIfEmpty(prState), nullIfEmpty(ciState),
		time.Now().Unix(), time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("update session pr: %w", err)
	}
	return checkRowsAffected(res)
}

// terminalPRStates are the pr_state values after which there is nothing left
// to poll: a merged or closed-unmerged PR does not change again in a way that
// matters for a session nobody is working in.
const terminalPRStates = `('merged', 'closed')`

// ListSessionsForPRPoll returns the worker sessions the GitHub poller must
// visit: live ones (state spawning/running) PLUS any worker still holding a PR
// in a non-terminal state, whatever the session's own state.
//
// The second half is the fix for the stale-status bug (task #1087): polling
// only live sessions meant that killing a worker froze its pr_state forever,
// so already-merged PRs kept being reported as open for hours.
func (s *Store) ListSessionsForPRPoll() ([]Session, error) {
	rows, err := s.db.Query(
		`SELECT id, kind, project_id, repo_id, feature_slug, parent_id, agent, branch,
		        worktree_path, tmux_name, state, activity, activity_ts, pr_number, pr_state,
		        ci_state, prompt, pending_quiz, pr_checked_at, profile, model, effort, created_at, updated_at,
		        task_id, subtask_id
		 FROM sessions
		 WHERE kind = 'worker'
		   AND (state IN ('spawning', 'running')
		        OR (COALESCE(pr_number, 0) != 0
		            AND COALESCE(pr_state, '') NOT IN ` + terminalPRStates + `))
		 ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("query sessions for pr poll: %w", err)
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

// MarkSessionPRChecked stamps a session with the unix time of a successful
// GitHub poll. It deliberately does NOT touch updated_at: a poll that found
// nothing new is not a change to the session, and bumping updated_at every
// couple of minutes would make every session look perpetually active.
// Returns ErrNotFound if the session doesn't exist.
func (s *Store) MarkSessionPRChecked(id string, ts int64) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET pr_checked_at = ? WHERE id = ?`, nullIfZero(ts), id,
	)
	if err != nil {
		return fmt.Errorf("mark session pr checked: %w", err)
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

// SetPendingQuiz stores the JSON-encoded pending AskUserQuestion quiz for a
// session and refreshes updated_at. Returns ErrNotFound if the session
// doesn't exist.
func (s *Store) SetPendingQuiz(id string, quizJSON string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET pending_quiz = ?, updated_at = ? WHERE id = ?`,
		nullIfEmpty(quizJSON), time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("set pending quiz: %w", err)
	}
	return checkRowsAffected(res)
}

// ClearPendingQuiz removes any pending quiz for a session and refreshes
// updated_at. Returns ErrNotFound if the session doesn't exist. Clearing a
// session that has no pending quiz is a no-op (idempotent).
func (s *Store) ClearPendingQuiz(id string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET pending_quiz = NULL, updated_at = ? WHERE id = ?`,
		time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("clear pending quiz: %w", err)
	}
	return checkRowsAffected(res)
}

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var sess Session
	var parentID, activity, prState, ciState, prompt, pendingQuiz sql.NullString
	var activityTS, prNumber, prCheckedAt, taskID, subtaskID sql.NullInt64

	err := row.Scan(
		&sess.ID, &sess.Kind, &sess.ProjectID, &sess.RepoID, &sess.FeatureSlug,
		&parentID, &sess.Agent, &sess.Branch, &sess.WorktreePath, &sess.TmuxName,
		&sess.State, &activity, &activityTS, &prNumber, &prState, &ciState, &prompt,
		&pendingQuiz, &prCheckedAt, &sess.Profile, &sess.Model, &sess.Effort,
		&sess.CreatedAt, &sess.UpdatedAt, &taskID, &subtaskID,
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
	sess.PRNumber = int(prNumber.Int64)
	sess.PRState = prState.String
	sess.CIState = ciState.String
	sess.PendingQuiz = pendingQuiz.String
	sess.PRCheckedAt = prCheckedAt.Int64
	sess.TaskID = taskID.Int64
	sess.SubtaskID = subtaskID.Int64

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

// CompareAndSwapPendingQuiz sets a session's pending quiz to newJSON only if
// it currently equals oldJSON ("" meaning none, on either side), refreshing
// updated_at. It reports whether the swap happened; a mismatch is not an
// error. The monitor writes permission quizzes this way so it never
// overwrites a hook quiz that landed between its read and its write.
func (s *Store) CompareAndSwapPendingQuiz(id, oldJSON, newJSON string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE sessions SET pending_quiz = ?, updated_at = ? WHERE id = ? AND IFNULL(pending_quiz, '') = ?`,
		nullIfEmpty(newJSON), time.Now().Unix(), id, oldJSON,
	)
	if err != nil {
		return false, fmt.Errorf("swap pending quiz: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("swap pending quiz: rows affected: %w", err)
	}
	return n == 1, nil
}
