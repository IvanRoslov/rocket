package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Gate-specific sentinel errors.
var (
	// ErrNoSpec is returned by RequestTaskGate when the task has no spec doc:
	// a gate is bound to a spec version, so there is nothing to gate yet.
	ErrNoSpec = errors.New("task has no spec")
	// ErrGateNotPending is returned by DecideTaskGate for a gate that was
	// already decided or superseded.
	ErrGateNotPending = errors.New("gate is not pending")
)

// TaskGate is one storm exit gate (task #4901, spec v1 §2.3): a request to
// leave brainstorm bound to the spec (and plan) versions current at request
// time. PlanVersion and DecidedAt are nil when absent.
type TaskGate struct {
	ID          int64
	TaskID      int64
	SpecVersion int64
	PlanVersion *int64
	Status      string // pending | go | changes | superseded
	Comment     string
	RequestedBy string // session id, or "user" for the human
	DecidedBy   string
	RequestedAt int64
	DecidedAt   *int64
}

const gateColumns = `id, task_id, spec_version, plan_version, status, comment,
	requested_by, decided_by, requested_at, decided_at`

func scanTaskGate(row interface{ Scan(...any) error }) (TaskGate, error) {
	var g TaskGate
	var plan, decided sql.NullInt64
	if err := row.Scan(&g.ID, &g.TaskID, &g.SpecVersion, &plan, &g.Status, &g.Comment,
		&g.RequestedBy, &g.DecidedBy, &g.RequestedAt, &decided); err != nil {
		return TaskGate{}, err
	}
	if plan.Valid {
		v := plan.Int64
		g.PlanVersion = &v
	}
	if decided.Valid {
		v := decided.Int64
		g.DecidedAt = &v
	}
	return g, nil
}

type queryer interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

// latestDocVersion returns the version of the most recently written doc of
// kind on taskID. Doc versions count per (kind, title), so "latest" is the
// newest row, not the largest number across titles.
func latestDocVersion(q queryer, taskID int64, kind string) (int64, bool, error) {
	var v int64
	err := q.QueryRow(
		`SELECT version FROM task_docs WHERE task_id = ? AND kind = ? ORDER BY id DESC LIMIT 1`,
		taskID, kind,
	).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("query latest %s version: %w", kind, err)
	}
	return v, true, nil
}

// LatestDocVersion is latestDocVersion on the store's own handle.
func (s *Store) LatestDocVersion(taskID int64, kind string) (int64, bool, error) {
	return latestDocVersion(s.db, taskID, kind)
}

// supersedePending moves every pending gate of taskID to superseded and
// returns them in their new state.
func supersedePending(q queryer, taskID int64) ([]TaskGate, error) {
	rows, err := q.Query(`SELECT `+gateColumns+` FROM task_gates
		WHERE task_id = ? AND status = 'pending' ORDER BY id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("query pending gates: %w", err)
	}
	var out []TaskGate
	for rows.Next() {
		g, err := scanTaskGate(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan pending gate: %w", err)
		}
		g.Status = "superseded"
		out = append(out, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	if _, err := q.Exec(`UPDATE task_gates SET status = 'superseded'
		WHERE task_id = ? AND status = 'pending'`, taskID); err != nil {
		return nil, fmt.Errorf("supersede pending gates: %w", err)
	}
	return out, nil
}

// RequestTaskGate opens a pending gate on taskID bound to the latest spec and
// plan versions. Any gate already pending on the task is superseded first and
// returned as superseded. Without a spec it fails with ErrNoSpec.
func (s *Store) RequestTaskGate(taskID int64, requestedBy string) (TaskGate, []TaskGate, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return TaskGate{}, nil, fmt.Errorf("begin request gate tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op if committed

	spec, ok, err := latestDocVersion(tx, taskID, "spec")
	if err != nil {
		return TaskGate{}, nil, err
	}
	if !ok {
		return TaskGate{}, nil, ErrNoSpec
	}
	g := TaskGate{
		TaskID:      taskID,
		SpecVersion: spec,
		Status:      "pending",
		RequestedBy: requestedBy,
		RequestedAt: time.Now().Unix(),
	}
	if plan, ok, err := latestDocVersion(tx, taskID, "plan"); err != nil {
		return TaskGate{}, nil, err
	} else if ok {
		g.PlanVersion = &plan
	}

	superseded, err := supersedePending(tx, taskID)
	if err != nil {
		return TaskGate{}, nil, err
	}

	var plan any
	if g.PlanVersion != nil {
		plan = *g.PlanVersion
	}
	res, err := tx.Exec(`INSERT INTO task_gates (task_id, spec_version, plan_version, status, requested_by, requested_at)
		VALUES (?, ?, ?, 'pending', ?, ?)`, taskID, g.SpecVersion, plan, g.RequestedBy, g.RequestedAt)
	if err != nil {
		return TaskGate{}, nil, fmt.Errorf("insert gate: %w", err)
	}
	if g.ID, err = res.LastInsertId(); err != nil {
		return TaskGate{}, nil, fmt.Errorf("insert gate last id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TaskGate{}, nil, fmt.Errorf("commit request gate tx: %w", err)
	}
	return g, superseded, nil
}

// SupersedePendingGates moves taskID's pending gate (if any) to superseded —
// called when a new spec version is written — and returns what it moved.
func (s *Store) SupersedePendingGates(taskID int64) ([]TaskGate, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin supersede tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op if committed
	out, err := supersedePending(tx, taskID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit supersede tx: %w", err)
	}
	return out, nil
}

// GetTaskGate returns gate id, or ErrNotFound.
func (s *Store) GetTaskGate(id int64) (TaskGate, error) {
	g, err := scanTaskGate(s.db.QueryRow(`SELECT `+gateColumns+` FROM task_gates WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return TaskGate{}, ErrNotFound
	}
	if err != nil {
		return TaskGate{}, fmt.Errorf("get gate: %w", err)
	}
	return g, nil
}

// ListTaskGates returns taskID's gate history, newest first.
func (s *Store) ListTaskGates(taskID int64) ([]TaskGate, error) {
	rows, err := s.db.Query(`SELECT `+gateColumns+` FROM task_gates WHERE task_id = ? ORDER BY id DESC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list gates: %w", err)
	}
	defer rows.Close()
	out := []TaskGate{}
	for rows.Next() {
		g, err := scanTaskGate(rows)
		if err != nil {
			return nil, fmt.Errorf("scan gate: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// DecideTaskGate records the human's decision (go | changes) on a pending
// gate. A gate that is no longer pending fails with ErrGateNotPending; an
// unknown id with ErrNotFound. The status check and the update are one
// statement, so two concurrent decisions cannot both win.
func (s *Store) DecideTaskGate(id int64, status, comment, decidedBy string) (TaskGate, error) {
	if status != "go" && status != "changes" {
		return TaskGate{}, fmt.Errorf("invalid gate decision %q", status)
	}
	res, err := s.db.Exec(`UPDATE task_gates SET status = ?, comment = ?, decided_by = ?, decided_at = ?
		WHERE id = ? AND status = 'pending'`, status, comment, decidedBy, time.Now().Unix(), id)
	if err != nil {
		return TaskGate{}, fmt.Errorf("decide gate: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return TaskGate{}, fmt.Errorf("decide gate rows: %w", err)
	}
	g, err := s.GetTaskGate(id)
	if err != nil {
		return TaskGate{}, err
	}
	if n == 0 {
		return g, ErrGateNotPending
	}
	return g, nil
}
