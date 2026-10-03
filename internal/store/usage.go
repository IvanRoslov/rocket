package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// SessionStats records the collection state for one session. Zero task IDs
// represent SQL NULL; EndedAt is nil for a live snapshot.
type SessionStats struct {
	SessionID   string
	TaskID      int64
	SubtaskID   int64
	Status      string
	Final       bool
	StartedAt   int64
	EndedAt     *int64
	CollectedAt int64
	Error       string
	Attempts    int
}

type UsageTokens struct {
	Input, CacheWrite, CacheRead, Output, Reasoning, Messages int64
}

type ModelUsage struct {
	Model  string
	Tokens UsageTokens
}

// ReplaceSessionUsage replaces the entire per-model breakdown and collection
// state in one transaction. A failed insert leaves the previous snapshot intact.
func (s *Store) ReplaceSessionUsage(st SessionStats, models []ModelUsage) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin usage replacement: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM session_usage WHERE session_id = ?`, st.SessionID); err != nil {
		return fmt.Errorf("delete session usage: %w", err)
	}
	for _, model := range models {
		v := model.Tokens
		if _, err := tx.Exec(`INSERT INTO session_usage
			(session_id, model, input, cache_write, cache_read, output, reasoning, messages)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, st.SessionID, model.Model,
			v.Input, v.CacheWrite, v.CacheRead, v.Output, v.Reasoning, v.Messages); err != nil {
			return fmt.Errorf("insert model usage %q: %w", model.Model, err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO session_stats
		(session_id, task_id, subtask_id, status, final, started_at, ended_at, collected_at, error, attempts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
		 task_id=excluded.task_id, subtask_id=excluded.subtask_id, status=excluded.status,
		 final=excluded.final, started_at=excluded.started_at, ended_at=excluded.ended_at,
		 collected_at=excluded.collected_at, error=excluded.error, attempts=excluded.attempts`,
		st.SessionID, nullIfZero(st.TaskID), nullIfZero(st.SubtaskID), st.Status, st.Final,
		st.StartedAt, st.EndedAt, st.CollectedAt, st.Error, st.Attempts); err != nil {
		return fmt.Errorf("upsert session stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit usage replacement: %w", err)
	}
	return nil
}

// GetSessionStats returns a session's collection state and per-model rows.
func (s *Store) GetSessionStats(sessionID string) (SessionStats, []ModelUsage, error) {
	var st SessionStats
	var taskID, subtaskID, endedAt sql.NullInt64
	err := s.db.QueryRow(`SELECT session_id, task_id, subtask_id, status, final,
		started_at, ended_at, collected_at, error, attempts
		FROM session_stats WHERE session_id = ?`, sessionID).Scan(
		&st.SessionID, &taskID, &subtaskID, &st.Status, &st.Final,
		&st.StartedAt, &endedAt, &st.CollectedAt, &st.Error, &st.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionStats{}, nil, ErrNotFound
	}
	if err != nil {
		return SessionStats{}, nil, fmt.Errorf("get session stats: %w", err)
	}
	st.TaskID, st.SubtaskID = taskID.Int64, subtaskID.Int64
	if endedAt.Valid {
		st.EndedAt = &endedAt.Int64
	}
	rows, err := s.db.Query(`SELECT model, input, cache_write, cache_read, output, reasoning, messages
		FROM session_usage WHERE session_id = ? ORDER BY model`, sessionID)
	if err != nil {
		return SessionStats{}, nil, fmt.Errorf("get model usage: %w", err)
	}
	defer rows.Close()
	var models []ModelUsage
	for rows.Next() {
		var model ModelUsage
		v := &model.Tokens
		if err := rows.Scan(&model.Model, &v.Input, &v.CacheWrite, &v.CacheRead,
			&v.Output, &v.Reasoning, &v.Messages); err != nil {
			return SessionStats{}, nil, fmt.Errorf("scan model usage: %w", err)
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return SessionStats{}, nil, fmt.Errorf("iterate model usage: %w", err)
	}
	return st, models, nil
}

// SessionsNeedingUsage selects terminal sessions with no final successful or
// missing result. Errors are retried no more than three times, once per hour.
func (s *Store) SessionsNeedingUsage(limit int, now int64) ([]Session, error) {
	rows, err := s.db.Query(`SELECT s.id, s.kind, s.project_id, s.repo_id, s.feature_slug,
		s.parent_id, s.agent, s.branch, s.worktree_path, s.tmux_name, s.state,
		s.activity, s.activity_ts, s.pr_number, s.pr_state, s.ci_state, s.prompt,
		s.pending_quiz, s.pr_checked_at, s.profile, s.model, s.effort,
		s.created_at, s.updated_at, s.task_id, s.subtask_id
		FROM sessions s LEFT JOIN session_stats st ON st.session_id = s.id
		WHERE s.state IN ('done', 'killed', 'errored')
		AND (st.session_id IS NULL OR st.final = 0 OR
			(st.status = 'error' AND st.attempts < 3 AND st.collected_at < ?))
		ORDER BY s.created_at, s.id LIMIT ?`, now-3600, limit)
	if err != nil {
		return nil, fmt.Errorf("query sessions needing usage: %w", err)
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
		return nil, fmt.Errorf("iterate sessions needing usage: %w", err)
	}
	return out, nil
}

// ResolveSessionTask finds and persists the root and subtask for a session,
// including historical sessions displaced from tasks.session_id by respawn.
func (s *Store) ResolveSessionTask(sessionID string) (taskID, subtaskID int64, err error) {
	sess, err := s.GetSession(sessionID)
	if err != nil {
		return 0, 0, err
	}
	if sess.TaskID != 0 {
		return sess.TaskID, sess.SubtaskID, nil
	}
	var id int64
	var parent sql.NullInt64
	err = s.db.QueryRow(`SELECT id, parent_id FROM tasks WHERE session_id = ? LIMIT 1`, sessionID).Scan(&id, &parent)
	if err == nil {
		if parent.Valid {
			taskID, subtaskID = parent.Int64, id
		} else {
			taskID = id
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, fmt.Errorf("resolve task link: %w", err)
	} else if sess.Kind == "worker" {
		prefix := "spawned worker " + sessionID + " for subtask #"
		rows, qerr := s.db.Query(`SELECT task_id, body FROM task_log
			WHERE kind = 'status' AND instr(body, ?) = 1 ORDER BY id DESC`, prefix)
		if qerr != nil {
			return 0, 0, fmt.Errorf("resolve task log: %w", qerr)
		}
		for rows.Next() {
			var root int64
			var body string
			if qerr = rows.Scan(&root, &body); qerr != nil {
				break
			}
			sub, parseErr := strconv.ParseInt(strings.TrimPrefix(body, prefix), 10, 64)
			if parseErr != nil || sub <= 0 {
				continue
			}
			task, getErr := s.GetTask(sub)
			if getErr != nil || task.ParentID != root {
				continue
			}
			taskID, subtaskID = root, sub
			break
		}
		if qerr == nil {
			qerr = rows.Err()
		}
		rows.Close()
		if qerr != nil {
			return 0, 0, fmt.Errorf("scan task log: %w", qerr)
		}
	}
	if taskID == 0 {
		return 0, 0, nil
	}
	if _, err := s.db.Exec(`UPDATE sessions SET task_id = ?, subtask_id = ? WHERE id = ? AND task_id IS NULL`,
		taskID, nullIfZero(subtaskID), sessionID); err != nil {
		return 0, 0, fmt.Errorf("persist resolved session task: %w", err)
	}
	return taskID, subtaskID, nil
}
