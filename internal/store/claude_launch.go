package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
)

func (s *Store) PutClaudeLaunch(l claudeinstance.Launch) error {
	if l.RocketSessionID == "" || l.Generation == "" || l.StartedAt.IsZero() {
		return fmt.Errorf("incomplete Claude launch")
	}
	l.StartedAt = l.StartedAt.UTC()
	l.PaneStart = l.PaneStart.UTC()
	raw, e := json.Marshal(l)
	if e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE sessions SET updated_at=updated_at WHERE id=?`, l.RocketSessionID); e != nil {
		return e
	}
	var old, oldRaw string
	err := tx.QueryRow(`SELECT generation,launch_json FROM claude_launches WHERE session_id=?`, l.RocketSessionID).Scan(&old, &oldRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		var prior claudeinstance.Launch
		if json.Unmarshal([]byte(oldRaw), &prior) != nil {
			return fmt.Errorf("invalid Claude launch")
		}
		if old != l.Generation && !l.StartedAt.After(prior.StartedAt) && l.PaneIdentity != "" {
			return fmt.Errorf("stale Claude pane completion")
		}
		if old != l.Generation && l.StartedAt.Before(prior.StartedAt) {
			return fmt.Errorf("stale Claude launch")
		}
	}
	_, e = tx.Exec(`INSERT INTO claude_launches(session_id,generation,launch_json) VALUES(?,?,?)
 ON CONFLICT(session_id) DO UPDATE SET generation=excluded.generation,launch_json=excluded.launch_json,
 identity_json=CASE WHEN claude_launches.generation=excluded.generation THEN claude_launches.identity_json ELSE NULL END,
 last_event_at=CASE WHEN claude_launches.generation=excluded.generation THEN claude_launches.last_event_at ELSE 0 END
 WHERE claude_launches.generation<>excluded.generation OR claude_launches.identity_json IS NULL`, l.RocketSessionID, l.Generation, string(raw))
	if e != nil {
		return e
	}
	if old != l.Generation {
		if _, e = tx.Exec(`UPDATE sessions SET activity=NULL,activity_ts=NULL WHERE id=?`, l.RocketSessionID); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) GetClaudeLaunch(id string) (claudeinstance.Launch, *claudeinstance.Identity, error) {
	var l claudeinstance.Launch
	var raw string
	var identity sql.NullString
	e := s.db.QueryRow(`SELECT launch_json,identity_json FROM claude_launches WHERE session_id=?`, id).Scan(&raw, &identity)
	if errors.Is(e, sql.ErrNoRows) {
		return l, nil, ErrNotFound
	}
	if e != nil {
		return l, nil, e
	}
	if e = json.Unmarshal([]byte(raw), &l); e != nil || l.RocketSessionID != id {
		return l, nil, fmt.Errorf("invalid Claude launch snapshot")
	}
	var pinned *claudeinstance.Identity
	if identity.Valid {
		pinned = new(claudeinstance.Identity)
		if e = json.Unmarshal([]byte(identity.String), pinned); e != nil || pinned.LaunchGeneration != l.Generation || pinned.RocketSessionID != id {
			return l, nil, fmt.Errorf("invalid Claude identity snapshot")
		}
	}
	return l, pinned, nil
}
func (s *Store) BindClaudeIdentity(id claudeinstance.Identity) (bool, error) {
	id.ProcessStart = id.ProcessStart.UTC()
	raw, e := json.Marshal(id)
	if e != nil {
		return false, e
	}
	res, e := s.db.Exec(`UPDATE claude_launches SET identity_json=? WHERE session_id=? AND generation=? AND (identity_json IS NULL OR identity_json=?)`, string(raw), id.RocketSessionID, id.LaunchGeneration, string(raw))
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	return n == 1, e
}
