package store

import (
	"github.com/IvanRoslov/rocket/internal/activity"
	"time"
)

func (s *Store) AcceptClaudeHook(sessionID, generation string, at int64, state string, activityTS int64) (bool, error) {
	// SessionEnd is only an ordering hint; independent poll owns OS death.
	if at <= 0 || at > time.Now().UnixNano() || (state != "" && (!activity.State(state).Valid() || state == "exited")) {
		return false, nil
	}
	tx, e := s.db.Begin()
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	res, e := tx.Exec(`UPDATE claude_launches SET last_event_at=? WHERE session_id=? AND generation=? AND identity_json IS NOT NULL AND last_event_at<?`, at, sessionID, generation, at)
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	if e != nil || n != 1 {
		return false, e
	}
	if state != "" {
		if _, e = tx.Exec(`UPDATE sessions SET activity=?,activity_ts=? WHERE id=? AND EXISTS(SELECT 1 FROM claude_launches WHERE session_id=? AND generation=?)`, state, activityTS, sessionID, sessionID, generation); e != nil {
			return false, e
		}
	}
	return true, tx.Commit()
}
func (s *Store) ApplyClaudeActivity(sessionID, generation, state string, activityTS int64) (bool, error) {
	if state != "" && !activity.State(state).Valid() {
		return false, nil
	}
	res, e := s.db.Exec(`UPDATE sessions SET activity=?,activity_ts=? WHERE id=?
 AND ((?<>'' AND EXISTS(SELECT 1 FROM claude_launches WHERE session_id=? AND generation=?)) OR (?='' AND NOT EXISTS(SELECT 1 FROM claude_launches WHERE session_id=?)))
 AND (activity_ts IS NULL OR activity_ts<=? OR ?='exited' OR activity IS NULL OR activity='' OR activity='exited')`, nullIfEmpty(state), nullIfZero(activityTS), sessionID, generation, sessionID, generation, generation, sessionID, activityTS, state, state)
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	return n == 1, e
}
