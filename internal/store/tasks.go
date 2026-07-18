package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// validTaskStatuses is the set of allowed values for tasks.status.
var validTaskStatuses = map[string]bool{
	"backlog":     true,
	"in_progress": true,
	"review":      true,
	"done":        true,
	"cancelled":   true,
}

// Task represents a unit of work, optionally a subtask of another task.
type Task struct {
	ID          int64
	ParentID    int64 // 0 = root task
	Title       string
	Description string
	ProjectID   string
	RepoID      string
	Status      string
	FeatureSlug string
	SessionID   string
	CreatedBy   string
	CreatedAt   int64
	UpdatedAt   int64
	CompletedAt int64 // 0 = not completed
}

// TaskFilter narrows the results of ListTasks. Empty Project/Status mean "no
// filter" on that field. ParentSet controls whether Parent is applied: when
// false, no filter is applied on parent_id; when true and Parent == 0, only
// root tasks (parent_id IS NULL) are returned; when true and Parent != 0,
// only direct children of Parent are returned.
type TaskFilter struct {
	Project   string
	Status    string
	Parent    int64
	ParentSet bool
}

// AddTask inserts a new task. Status defaults to "backlog" and CreatedBy
// defaults to "user" when empty. CreatedAt/UpdatedAt default to now. Returns
// an error if Status is set to a value outside the allowed set.
func (s *Store) AddTask(t Task) (int64, error) {
	if t.Status == "" {
		t.Status = "backlog"
	}
	if !validTaskStatuses[t.Status] {
		return 0, fmt.Errorf("invalid status %q", t.Status)
	}
	if t.CreatedBy == "" {
		t.CreatedBy = "user"
	}
	now := time.Now().Unix()
	if t.CreatedAt == 0 {
		t.CreatedAt = now
	}
	if t.UpdatedAt == 0 {
		t.UpdatedAt = t.CreatedAt
	}

	res, err := s.db.Exec(
		`INSERT INTO tasks (
			parent_id, title, description, project_id, repo_id, status, feature_slug,
			session_id, created_by, created_at, updated_at, completed_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullIfZero(t.ParentID), t.Title, t.Description, t.ProjectID, nullIfEmpty(t.RepoID),
		t.Status, nullIfEmpty(t.FeatureSlug), nullIfEmpty(t.SessionID), t.CreatedBy,
		t.CreatedAt, t.UpdatedAt, nullIfZero(t.CompletedAt),
	)
	if err != nil {
		return 0, fmt.Errorf("insert task: %w", err)
	}
	return res.LastInsertId()
}

// GetTask returns the task with the given id, or ErrNotFound.
func (s *Store) GetTask(id int64) (Task, error) {
	row := s.db.QueryRow(
		`SELECT id, parent_id, title, description, project_id, repo_id, status, feature_slug,
		        session_id, created_by, created_at, updated_at, completed_at
		 FROM tasks WHERE id = ?`, id,
	)
	return scanTask(row)
}

// ListTasks returns tasks matching the filter, ordered by id.
func (s *Store) ListTasks(f TaskFilter) ([]Task, error) {
	query := `SELECT id, parent_id, title, description, project_id, repo_id, status, feature_slug,
	                  session_id, created_by, created_at, updated_at, completed_at
	          FROM tasks`

	var conds []string
	var args []any

	if f.Project != "" {
		conds = append(conds, `project_id = ?`)
		args = append(args, f.Project)
	}
	if f.Status != "" {
		conds = append(conds, `status = ?`)
		args = append(args, f.Status)
	}
	if f.ParentSet {
		if f.Parent == 0 {
			conds = append(conds, `parent_id IS NULL`)
		} else {
			conds = append(conds, `parent_id = ?`)
			args = append(args, f.Parent)
		}
	}

	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY id"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateTaskStatus updates a task's status and refreshes updated_at. When
// status is "done" or "cancelled", completed_at is set to now; otherwise it
// is cleared. Returns ErrNotFound if the task doesn't exist.
func (s *Store) UpdateTaskStatus(id int64, status string) error {
	if !validTaskStatuses[status] {
		return fmt.Errorf("invalid status %q", status)
	}
	var completedAt any
	if status == "done" || status == "cancelled" {
		completedAt = time.Now().Unix()
	}

	res, err := s.db.Exec(
		`UPDATE tasks SET status = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
		status, completedAt, time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	return checkRowsAffected(res)
}

// UpdateTask updates the title, description, feature_slug, session_id, and
// repo_id of a task, and refreshes updated_at. Status, parent, project, and
// created_* fields are not modified. Returns ErrNotFound if the task doesn't
// exist.
func (s *Store) UpdateTask(t Task) error {
	res, err := s.db.Exec(
		`UPDATE tasks SET title = ?, description = ?, feature_slug = ?, session_id = ?, repo_id = ?, updated_at = ?
		 WHERE id = ?`,
		t.Title, t.Description, nullIfEmpty(t.FeatureSlug), nullIfEmpty(t.SessionID), nullIfEmpty(t.RepoID),
		time.Now().Unix(), t.ID,
	)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	return checkRowsAffected(res)
}

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var parentID, completedAt sql.NullInt64
	var repoID, featureSlug, sessionID sql.NullString

	err := row.Scan(
		&t.ID, &parentID, &t.Title, &t.Description, &t.ProjectID, &repoID, &t.Status, &featureSlug,
		&sessionID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("scan task: %w", err)
	}

	t.ParentID = parentID.Int64
	t.RepoID = repoID.String
	t.FeatureSlug = featureSlug.String
	t.SessionID = sessionID.String
	t.CompletedAt = completedAt.Int64

	return t, nil
}

// TaskDoc represents a versioned markdown document (spec/plan/report/doc)
// attached to a task.
type TaskDoc struct {
	ID        int64
	TaskID    int64
	Version   int64
	Kind      string
	Title     string
	Body      string
	Author    string // session id, or empty for the user
	CreatedAt int64
}

// PutTaskDoc inserts a new version of a doc. The version is one greater than
// the max existing version for the same (task_id, kind, title); if none
// exist, version 1 is used. CreatedAt defaults to now if unset. Returns the
// stored doc, including its assigned ID and Version.
func (s *Store) PutTaskDoc(d TaskDoc) (TaskDoc, error) {
	if d.CreatedAt == 0 {
		d.CreatedAt = time.Now().Unix()
	}

	tx, err := s.db.Begin()
	if err != nil {
		return TaskDoc{}, fmt.Errorf("begin put task doc tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op if committed

	var maxVersion int64
	err = tx.QueryRow(
		`SELECT COALESCE(MAX(version), 0) FROM task_docs WHERE task_id = ? AND kind = ? AND title = ?`,
		d.TaskID, d.Kind, d.Title,
	).Scan(&maxVersion)
	if err != nil {
		return TaskDoc{}, fmt.Errorf("query max doc version: %w", err)
	}
	d.Version = maxVersion + 1

	res, err := tx.Exec(
		`INSERT INTO task_docs (task_id, kind, title, body, version, author, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.TaskID, d.Kind, d.Title, d.Body, d.Version, nullIfEmpty(d.Author), d.CreatedAt,
	)
	if err != nil {
		return TaskDoc{}, fmt.Errorf("insert task doc: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return TaskDoc{}, fmt.Errorf("insert task doc last id: %w", err)
	}
	d.ID = id

	if err := tx.Commit(); err != nil {
		return TaskDoc{}, fmt.Errorf("commit put task doc tx: %w", err)
	}

	return d, nil
}

// ListTaskDocs returns docs for taskID. If history is true, every version of
// every (kind, title) is returned, ascending by id. If false, only the
// highest-version row for each (kind, title) is returned.
func (s *Store) ListTaskDocs(taskID int64, history bool) ([]TaskDoc, error) {
	query := `SELECT id, task_id, kind, title, body, version, author, created_at
	          FROM task_docs WHERE task_id = ?`
	if !history {
		query += ` AND version = (
			SELECT MAX(version) FROM task_docs AS t2
			WHERE t2.task_id = task_docs.task_id AND t2.kind = task_docs.kind AND t2.title = task_docs.title
		)`
	}
	query += ` ORDER BY id`

	rows, err := s.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("query task docs: %w", err)
	}
	defer rows.Close()

	var out []TaskDoc
	for rows.Next() {
		d, err := scanTaskDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanTaskDoc(row interface{ Scan(...any) error }) (TaskDoc, error) {
	var d TaskDoc
	var author sql.NullString

	err := row.Scan(&d.ID, &d.TaskID, &d.Kind, &d.Title, &d.Body, &d.Version, &author, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskDoc{}, ErrNotFound
	}
	if err != nil {
		return TaskDoc{}, fmt.Errorf("scan task doc: %w", err)
	}

	d.Author = author.String

	return d, nil
}

// TaskLogEntry represents a single entry in a task's append-only log
// (decision/problem/note/status).
type TaskLogEntry struct {
	ID        int64
	TaskID    int64
	Kind      string
	Body      string
	Author    string // session id, or empty for the user
	CreatedAt int64
}

// AddTaskLog inserts a new log entry, defaulting CreatedAt to now if unset.
// Returns the assigned id.
func (s *Store) AddTaskLog(e TaskLogEntry) (int64, error) {
	if e.CreatedAt == 0 {
		e.CreatedAt = time.Now().Unix()
	}

	res, err := s.db.Exec(
		`INSERT INTO task_log (task_id, kind, body, author, created_at) VALUES (?, ?, ?, ?, ?)`,
		e.TaskID, e.Kind, e.Body, nullIfEmpty(e.Author), e.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert task log: %w", err)
	}
	return res.LastInsertId()
}

// ListTaskLog returns log entries for taskID, ascending by id. An empty kind
// means no filter on kind.
func (s *Store) ListTaskLog(taskID int64, kind string) ([]TaskLogEntry, error) {
	query := `SELECT id, task_id, kind, body, author, created_at FROM task_log WHERE task_id = ?`
	args := []any{taskID}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY id`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query task log: %w", err)
	}
	defer rows.Close()

	var out []TaskLogEntry
	for rows.Next() {
		var e TaskLogEntry
		var author sql.NullString
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Kind, &e.Body, &author, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan task log: %w", err)
		}
		e.Author = author.String
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
