package store

import (
	"context"
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
	// ErrGateSuperseded is returned by DecideTaskGate when a newer spec
	// version exists than the one the gate was requested for; the gate is
	// moved to superseded on the way.
	ErrGateSuperseded = errors.New("gate is superseded by a newer spec")
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

// immediateTx is a write transaction opened with BEGIN IMMEDIATE on a
// dedicated connection: it takes SQLite's write lock up front, so concurrent
// writers queue on busy_timeout instead of failing to upgrade a read lock
// (SQLITE_BUSY) halfway through. database/sql has no per-transaction lock
// mode, and the DSN-wide _txlock would change every transaction in the store.
type immediateTx struct {
	ctx  context.Context
	conn *sql.Conn
	done bool
}

func (s *Store) beginImmediate() (*immediateTx, error) {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		conn.Close()
		return nil, err
	}
	return &immediateTx{ctx: ctx, conn: conn}, nil
}

func (t *immediateTx) QueryRow(q string, args ...any) *sql.Row {
	return t.conn.QueryRowContext(t.ctx, q, args...)
}

func (t *immediateTx) Query(q string, args ...any) (*sql.Rows, error) {
	return t.conn.QueryContext(t.ctx, q, args...)
}

func (t *immediateTx) Exec(q string, args ...any) (sql.Result, error) {
	return t.conn.ExecContext(t.ctx, q, args...)
}

func (t *immediateTx) Commit() error {
	_, err := t.conn.ExecContext(t.ctx, `COMMIT`)
	if err == nil {
		t.done = true
		t.conn.Close()
	}
	return err
}

// Rollback is a no-op after a successful Commit, so it can be deferred.
func (t *immediateTx) Rollback() {
	if t.done {
		return
	}
	t.done = true
	t.conn.ExecContext(t.ctx, `ROLLBACK`) //nolint:errcheck // best effort
	t.conn.Close()
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
	tx, err := s.beginImmediate()
	if err != nil {
		return TaskGate{}, nil, fmt.Errorf("begin request gate tx: %w", err)
	}
	defer tx.Rollback()

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
// unknown id with ErrNotFound. A gate whose spec version is no longer the
// task's latest is moved to superseded and fails with ErrGateSuperseded — the
// check is part of the conditional UPDATE, so a spec written concurrently (or
// one whose on-put supersede failed) can never be approved by a stale gate.
func (s *Store) DecideTaskGate(id int64, status, comment, decidedBy string) (TaskGate, error) {
	if status != "go" && status != "changes" {
		return TaskGate{}, fmt.Errorf("invalid gate decision %q", status)
	}
	tx, err := s.beginImmediate()
	if err != nil {
		return TaskGate{}, fmt.Errorf("begin decide gate tx: %w", err)
	}
	defer tx.Rollback()

	g, err := scanTaskGate(tx.QueryRow(`SELECT `+gateColumns+` FROM task_gates WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return TaskGate{}, ErrNotFound
	}
	if err != nil {
		return TaskGate{}, fmt.Errorf("get gate: %w", err)
	}
	if g.Status != "pending" {
		return g, ErrGateNotPending
	}
	spec, _, err := latestDocVersion(tx, g.TaskID, "spec")
	if err != nil {
		return TaskGate{}, err
	}
	if spec != g.SpecVersion {
		if _, err := tx.Exec(`UPDATE task_gates SET status = 'superseded' WHERE id = ?`, id); err != nil {
			return TaskGate{}, fmt.Errorf("supersede stale gate: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return TaskGate{}, fmt.Errorf("commit supersede stale gate: %w", err)
		}
		g.Status = "superseded"
		return g, ErrGateSuperseded
	}

	now := time.Now().Unix()
	if _, err := tx.Exec(`UPDATE task_gates SET status = ?, comment = ?, decided_by = ?, decided_at = ?
		WHERE id = ?`, status, comment, decidedBy, now, id); err != nil {
		return TaskGate{}, fmt.Errorf("decide gate: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TaskGate{}, fmt.Errorf("commit decide gate: %w", err)
	}
	g.Status, g.Comment, g.DecidedBy, g.DecidedAt = status, comment, decidedBy, &now
	return g, nil
}

// ReopenTaskGate rolls a decided gate back to a clean pending state. It undoes
// a decision whose follow-up (the task's status move) failed, so the human's
// retry is a fresh decision rather than a 409. Only go/changes gates move.
func (s *Store) ReopenTaskGate(id int64) error {
	_, err := s.db.Exec(`UPDATE task_gates SET status = 'pending', comment = '', decided_by = '', decided_at = NULL
		WHERE id = ? AND status IN ('go', 'changes')`, id)
	if err != nil {
		return fmt.Errorf("reopen gate: %w", err)
	}
	return nil
}
