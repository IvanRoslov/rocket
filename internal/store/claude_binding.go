package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"math"
	"path/filepath"
	"time"
)

type claudeBindingReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readClaudeBinding(ctx context.Context, q claudeBindingReader, id string) (claudeinstance.Launch, *claudeinstance.Identity, int64, error) {
	var l claudeinstance.Launch
	var raw, generation string
	var identity sql.NullString
	var at int64
	e := q.QueryRowContext(ctx, `SELECT generation,launch_json,identity_json,last_event_at FROM claude_launches WHERE session_id=?`, id).Scan(&generation, &raw, &identity, &at)
	if errors.Is(e, sql.ErrNoRows) {
		return l, nil, 0, ErrNotFound
	}
	if e != nil {
		return l, nil, 0, e
	}
	if json.Unmarshal([]byte(raw), &l) != nil || l.RocketSessionID != id || l.Generation != generation || at < 0 {
		return l, nil, 0, fmt.Errorf("invalid Claude binding")
	}
	var target *claudeinstance.Identity
	if identity.Valid {
		target = new(claudeinstance.Identity)
		if json.Unmarshal([]byte(identity.String), target) != nil || target.RocketSessionID != id || target.LaunchGeneration != generation {
			return l, nil, 0, fmt.Errorf("invalid Claude identity")
		}
	}
	return l, target, at, nil
}
func (s *Store) GetClaudeBinding(ctx context.Context, id string) (claudeinstance.Launch, *claudeinstance.Identity, int64, error) {
	return readClaudeBinding(ctx, s.db, id)
}
func claudeSameRoot(a, b claudeinstance.Identity) bool {
	old := a
	old.ClaudeSessionID = b.ClaudeSessionID
	old.JournalPath = b.JournalPath
	return claudeinstance.SameIdentity(old, b) && filepath.Dir(a.JournalPath) == filepath.Dir(b.JournalPath) && filepath.Base(b.JournalPath) == b.ClaudeSessionID+".jsonl"
}
func (s *Store) AdvanceClaudeBinding(ctx context.Context, previous, next claudeinstance.Identity, revision uint64, observed, at int64) (bool, error) {
	if at <= 0 || at <= observed || at > time.Now().UnixNano() || revision == math.MaxUint64 || !claudeSameRoot(previous, next) || claudeinstance.SameIdentity(previous, next) {
		return false, nil
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE sessions SET updated_at=updated_at WHERE id=?`, previous.RocketSessionID); e != nil {
		return false, e
	}
	l, current, event, e := readClaudeBinding(ctx, tx, previous.RocketSessionID)
	if e != nil {
		return false, e
	}
	if current == nil || !claudeinstance.SameIdentity(*current, previous) || l.BindingRevision != revision || event != observed {
		return false, nil
	}
	l.BindingRevision++
	raw, e := json.Marshal(l)
	if e != nil {
		return false, e
	}
	target, e := json.Marshal(next)
	if e != nil {
		return false, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE claude_launches SET launch_json=?,identity_json=?,last_event_at=? WHERE session_id=?`, string(raw), string(target), at, l.RocketSessionID); e != nil {
		return false, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE sessions SET activity=NULL,activity_ts=NULL WHERE id=?`, l.RocketSessionID); e != nil {
		return false, e
	}
	if e = ctx.Err(); e != nil {
		return false, e
	}
	e = tx.Commit()
	return e == nil, e
}
func (s *Store) AcceptClaudeHookForBinding(ctx context.Context, target claudeinstance.Identity, revision uint64, at int64, state string, activityTS int64) (bool, error) {
	if at <= 0 || at > time.Now().UnixNano() || state == "exited" || (state != "" && !activity.State(state).Valid()) {
		return false, nil
	}
	return s.writeClaudeActivity(ctx, target, revision, nil, at, state, activityTS)
}
func (s *Store) ApplyClaudeActivityForBinding(ctx context.Context, target claudeinstance.Identity, revision uint64, observed int64, state string, activityTS int64) (bool, error) {
	if state != "" && !activity.State(state).Valid() {
		return false, nil
	}
	return s.writeClaudeActivity(ctx, target, revision, &observed, 0, state, activityTS)
}
func (s *Store) writeClaudeActivity(ctx context.Context, target claudeinstance.Identity, revision uint64, observed *int64, at int64, state string, activityTS int64) (bool, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE sessions SET updated_at=updated_at WHERE id=?`, target.RocketSessionID); e != nil {
		return false, e
	}
	l, current, event, e := readClaudeBinding(ctx, tx, target.RocketSessionID)
	if e != nil {
		return false, e
	}
	if current == nil || !claudeinstance.SameIdentity(*current, target) || l.BindingRevision != revision || (observed != nil && event != *observed) || (at != 0 && at <= event) {
		return false, nil
	}
	if at != 0 {
		if _, e = tx.ExecContext(ctx, `UPDATE claude_launches SET last_event_at=? WHERE session_id=?`, at, target.RocketSessionID); e != nil {
			return false, e
		}
	}
	// Empty hook state only records ordering (SessionEnd); empty poll state clears uncertainty.
	if observed != nil || state != "" {
		_, e = tx.ExecContext(ctx, `UPDATE sessions SET activity=?,activity_ts=? WHERE id=? AND (activity_ts IS NULL OR activity_ts<=? OR ?='exited' OR activity IS NULL OR activity='' OR activity='exited')`, nullIfEmpty(state), nullIfZero(activityTS), target.RocketSessionID, activityTS, state)
		if e != nil {
			return false, e
		}
	}
	if e = ctx.Err(); e != nil {
		return false, e
	}
	e = tx.Commit()
	return e == nil, e
}
