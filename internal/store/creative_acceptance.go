package store

import (
	"database/sql"
	"errors"
	"time"
)

func checkCreativeMutable(q creativeReader, runID int64) error {
	var state string
	if err := q.QueryRow(`SELECT closure_state FROM creative_runs WHERE id=?`, runID).Scan(&state); err != nil {
		return creativeNotFound(err)
	}
	if state != "open" {
		return creativeConflict("work is permanently closed for changes; closure may only be retried")
	}
	return nil
}
func (s *Store) CheckCreativeRunMutable(runID int64) error { return checkCreativeMutable(s.db, runID) }
func (s *Store) CheckCreativeSessionMutable(id string) error {
	b, err := s.GetCreativeSessionContext(id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.CheckCreativeRunMutable(b.RunID)
}

// BeginCreativeAcceptance records the human decision and obtains the SQLite
// write lock before checking eligibility. A closing run never becomes writable
// again: retries finish the same decision even after a partial runtime stop.
func (s *Store) BeginCreativeAcceptance(runID int64, human bool) (CreativeRun, error) {
	if !human {
		return CreativeRun{}, ErrCreativeForbidden
	}
	tx, err := s.db.Begin()
	if err != nil {
		return CreativeRun{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE creative_runs SET id=id WHERE id=?`, runID); err != nil {
		return CreativeRun{}, err
	}
	r, err := getCreativeRun(tx, runID)
	if err != nil {
		return r, err
	}
	if r.ClosureState == "open" {
		if len(r.Stages) == 0 || r.Status != "completed" {
			return r, creativeConflict("all stages must complete before final acceptance")
		}
		now := time.Now().Unix()
		if _, err = tx.Exec(`UPDATE creative_runs SET closure_state='closing',acceptance_requested_at=?,updated_at=? WHERE id=?`, now, now, runID); err != nil {
			return r, err
		}
	}
	r, err = getCreativeRun(tx, runID)
	if err != nil {
		return r, err
	}
	return r, tx.Commit()
}
func (s *Store) RecordCreativeClosureError(runID int64, message string) error {
	res, err := s.db.Exec(`UPDATE creative_runs SET closure_error=?,updated_at=? WHERE id=? AND closure_state='closing'`, message, time.Now().Unix(), runID)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}
func (s *Store) FinishCreativeAcceptance(runID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE creative_runs SET id=id WHERE id=?`, runID); err != nil {
		return err
	}
	var active int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM sessions s JOIN creative_session_contexts c ON c.session_id=s.id WHERE c.run_id=? AND s.state NOT IN ('killed','done','errored')`, runID).Scan(&active); err != nil {
		return err
	}
	if active != 0 {
		return creativeConflict("run still has active sessions")
	}
	var res sql.Result
	res, err = tx.Exec(`UPDATE creative_runs SET closure_state='accepted',accepted_at=?,closure_error='',updated_at=? WHERE id=? AND closure_state='closing'`, time.Now().Unix(), time.Now().Unix(), runID)
	if err != nil {
		return err
	}
	if err = checkRowsAffected(res); err != nil {
		return err
	}
	return tx.Commit()
}
