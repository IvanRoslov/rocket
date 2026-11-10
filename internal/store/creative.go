package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// creativeReader lets snapshots be read consistently inside a transaction.
type creativeReader interface {
	QueryRow(string, ...any) *sql.Row
	Query(string, ...any) (*sql.Rows, error)
}

func creativeInvalid(message string) error { return fmt.Errorf("%w: %s", ErrCreativeInvalid, message) }
func creativeConflict(message string) error {
	return fmt.Errorf("%w: %s", ErrCreativeConflict, message)
}
func creativeNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func validateCreativePipeline(in CreativePipelineInput) error {
	if strings.TrimSpace(in.Name) == "" || len(in.Stages) == 0 {
		return creativeInvalid("name and at least one stage are required")
	}
	for _, stage := range in.Stages {
		if strings.TrimSpace(stage.Name) == "" {
			return creativeInvalid("stage name is required")
		}
		if stage.Approval != "auto" && stage.Approval != "stage" && stage.Approval != "item" {
			return creativeInvalid("approval must be auto, stage or item")
		}
		if stage.Approval == "item" && !stage.Repeated {
			return creativeInvalid("item approval requires repeated stage")
		}
	}
	return nil
}
func (s *Store) CreateCreativePipeline(in CreativePipelineInput) (CreativePipeline, error) {
	if err := validateCreativePipeline(in); err != nil {
		return CreativePipeline{}, err
	}
	data, _ := json.Marshal(in.Stages)
	now := time.Now().Unix()
	res, err := s.db.Exec(`INSERT INTO creative_pipelines(name,stages,created_at,updated_at) VALUES(?,?,?,?)`, in.Name, string(data), now, now)
	if err != nil {
		return CreativePipeline{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return CreativePipeline{}, err
	}
	return s.GetCreativePipeline(id)
}
func (s *Store) UpdateCreativePipeline(id int64, in CreativePipelineInput) (CreativePipeline, error) {
	if err := validateCreativePipeline(in); err != nil {
		return CreativePipeline{}, err
	}
	data, _ := json.Marshal(in.Stages)
	res, err := s.db.Exec(`UPDATE creative_pipelines SET name=?,stages=?,updated_at=? WHERE id=?`, in.Name, string(data), time.Now().Unix(), id)
	if err != nil {
		return CreativePipeline{}, err
	}
	if err = checkRowsAffected(res); err != nil {
		return CreativePipeline{}, err
	}
	return s.GetCreativePipeline(id)
}
func getCreativePipeline(q creativeReader, id int64) (CreativePipeline, error) {
	var p CreativePipeline
	var stages string
	err := q.QueryRow(`SELECT id,name,stages,created_at,updated_at FROM creative_pipelines WHERE id=?`, id).Scan(&p.ID, &p.Name, &stages, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, creativeNotFound(err)
	}
	err = json.Unmarshal([]byte(stages), &p.Stages)
	return p, err
}
func (s *Store) GetCreativePipeline(id int64) (CreativePipeline, error) {
	return getCreativePipeline(s.db, id)
}
func creativeIDs(q creativeReader, query string, args ...any) ([]int64, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Store) ListCreativePipelines() ([]CreativePipeline, error) {
	ids, err := creativeIDs(s.db, `SELECT id FROM creative_pipelines ORDER BY id`)
	if err != nil {
		return nil, err
	}
	out := []CreativePipeline{}
	for _, id := range ids {
		p, err := s.GetCreativePipeline(id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
func (s *Store) CreateCreativeRun(in CreativeRunInput) (CreativeRun, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Brief) == "" || in.PipelineID <= 0 || in.ProjectID == "" {
		return CreativeRun{}, creativeInvalid("project_id, pipeline_id, name and brief required")
	}
	if _, err := s.GetProject(in.ProjectID); err != nil {
		return CreativeRun{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return CreativeRun{}, err
	}
	defer tx.Rollback()
	// Acquire the write lock before reading the template; concurrent edits cannot
	// create a mixed snapshot or a deferred-transaction upgrade failure.
	if _, err = tx.Exec(`UPDATE creative_pipelines SET id=id WHERE id=?`, in.PipelineID); err != nil {
		return CreativeRun{}, err
	}
	p, err := getCreativePipeline(tx, in.PipelineID)
	if err != nil {
		return CreativeRun{}, err
	}
	now := time.Now().Unix()
	res, err := tx.Exec(`INSERT INTO creative_runs(project_id,pipeline_id,pipeline_name,name,brief,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, in.ProjectID, in.PipelineID, p.Name, in.Name, in.Brief, now, now)
	if err != nil {
		return CreativeRun{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return CreativeRun{}, err
	}
	for pos, stage := range p.Stages {
		data, _ := json.Marshal(stage)
		if _, err = tx.Exec(`INSERT INTO creative_stages(run_id,position,definition) VALUES(?,?,?)`, id, pos, string(data)); err != nil {
			return CreativeRun{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return CreativeRun{}, err
	}
	return s.GetCreativeRun(id)
}
func getCreativeRun(q creativeReader, id int64) (CreativeRun, error) {
	var r CreativeRun
	var allowed string
	err := q.QueryRow(`SELECT id,project_id,pipeline_id,pipeline_name,name,brief,COALESCE(orchestrator_session_id,''),created_at,updated_at,allowed_profiles FROM creative_runs WHERE id=?`, id).Scan(&r.ID, &r.ProjectID, &r.PipelineID, &r.PipelineName, &r.Name, &r.Brief, &r.OrchestratorSessionID, &r.CreatedAt, &r.UpdatedAt, &allowed)
	if err != nil {
		return r, creativeNotFound(err)
	}
	if err := json.Unmarshal([]byte(allowed), &r.AllowedProfiles); err != nil {
		return r, fmt.Errorf("decode creative allowed profiles: %w", err)
	}
	if r.AllowedProfiles == nil {
		r.AllowedProfiles = []string{}
	}
	ids, err := creativeIDs(q, `SELECT id FROM creative_stages WHERE run_id=? ORDER BY position`, id)
	if err != nil {
		return r, err
	}
	r.Stages = []CreativeStage{}
	r.Status = "completed"
	for _, stageID := range ids {
		stage, err := getCreativeStage(q, stageID)
		if err != nil {
			return r, err
		}
		r.Stages = append(r.Stages, stage)
		if r.Status == "completed" && stage.Status != "completed" {
			r.Status = stage.Status
		}
	}
	if r.Status == "pending" {
		for _, stage := range r.Stages {
			if stage.Status != "pending" {
				r.Status = "in_progress"
				break
			}
		}
	}
	return r, nil
}
func (s *Store) GetCreativeRun(id int64) (CreativeRun, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return CreativeRun{}, err
	}
	defer tx.Rollback()
	r, err := getCreativeRun(tx, id)
	if err != nil {
		return r, err
	}
	return r, tx.Commit()
}
func (s *Store) ListCreativeRuns(projectID string) ([]CreativeRun, error) {
	ids, err := creativeIDs(s.db, `SELECT id FROM creative_runs WHERE (?='' OR project_id=?) ORDER BY id`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	out := []CreativeRun{}
	for _, id := range ids {
		r, err := s.GetCreativeRun(id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
func getCreativeStage(q creativeReader, id int64) (CreativeStage, error) {
	var st CreativeStage
	var definition, versions string
	err := q.QueryRow(`SELECT id,run_id,position,definition,status,progress,progress_note,review_revision,artifact_version_ids FROM creative_stages WHERE id=?`, id).Scan(&st.ID, &st.RunID, &st.Position, &definition, &st.Status, &st.Progress, &st.ProgressNote, &st.ReviewRevision, &versions)
	if err != nil {
		return st, creativeNotFound(err)
	}
	if err = json.Unmarshal([]byte(definition), &st.CreativeStageDefinition); err != nil {
		return st, err
	}
	if err = json.Unmarshal([]byte(versions), &st.ArtifactVersionIDs); err != nil {
		return st, err
	}
	ids, err := creativeIDs(q, `SELECT id FROM creative_items WHERE stage_id=? ORDER BY position`, id)
	if err != nil {
		return st, err
	}
	st.Items = []CreativeItem{}
	for _, itemID := range ids {
		item, err := getCreativeItem(q, itemID)
		if err != nil {
			return st, err
		}
		st.Items = append(st.Items, item)
	}
	return st, nil
}
func (s *Store) GetCreativeStage(id int64) (CreativeStage, error) { return getCreativeStage(s.db, id) }
func getCreativeItem(q creativeReader, id int64) (CreativeItem, error) {
	var i CreativeItem
	var versions string
	err := q.QueryRow(`SELECT id,run_id,stage_id,position,name,description,expected_artifacts,status,progress,progress_note,review_revision,artifact_version_ids FROM creative_items WHERE id=?`, id).Scan(&i.ID, &i.RunID, &i.StageID, &i.Position, &i.Name, &i.Description, &i.ExpectedArtifacts, &i.Status, &i.Progress, &i.ProgressNote, &i.ReviewRevision, &versions)
	if err != nil {
		return i, creativeNotFound(err)
	}
	err = json.Unmarshal([]byte(versions), &i.ArtifactVersionIDs)
	return i, err
}
func (s *Store) GetCreativeItem(id int64) (CreativeItem, error) { return getCreativeItem(s.db, id) }
func resolveCreativeContext(q creativeReader, runID, stageID, itemID int64) (CreativeContext, error) {
	c := CreativeContext{RunID: runID, StageID: stageID, ItemID: itemID}
	if runID <= 0 || stageID < 0 || itemID < 0 || (itemID != 0 && stageID == 0) {
		return c, creativeInvalid("invalid run/stage/item context")
	}
	err := q.QueryRow(`SELECT project_id,COALESCE(orchestrator_session_id,'') FROM creative_runs WHERE id=?`, runID).Scan(&c.ProjectID, &c.OrchestratorSessionID)
	if err != nil {
		return c, creativeNotFound(err)
	}
	if stageID != 0 {
		var found int
		err = q.QueryRow(`SELECT 1 FROM creative_stages WHERE id=? AND run_id=?`, stageID, runID).Scan(&found)
		if err != nil {
			return c, creativeNotFound(err)
		}
	}
	if itemID != 0 {
		var found int
		err = q.QueryRow(`SELECT 1 FROM creative_items WHERE id=? AND stage_id=? AND run_id=?`, itemID, stageID, runID).Scan(&found)
		if err != nil {
			return c, creativeNotFound(err)
		}
	}
	return c, nil
}
func (s *Store) ResolveCreativeContext(runID, stageID, itemID int64) (CreativeContext, error) {
	return resolveCreativeContext(s.db, runID, stageID, itemID)
}
func (s *Store) SetCreativeRunOrchestrator(runID int64, sessionID string) error {
	tx, err := s.creativeWrite(runID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c, err := resolveCreativeContext(tx, runID, 0, 0)
	if err != nil {
		return err
	}
	var project, kind string
	err = tx.QueryRow(`SELECT project_id,kind FROM sessions WHERE id=?`, sessionID).Scan(&project, &kind)
	if err != nil {
		return creativeNotFound(err)
	}
	if project != c.ProjectID || (kind != "orchestrator" && kind != "agent") {
		return creativeInvalid("orchestrator must be a same-project orchestrator or agent session")
	}
	_, err = tx.Exec(`UPDATE creative_runs SET orchestrator_session_id=? WHERE id=?`, sessionID, runID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// All mutable run operations obtain the SQLite write lock before loading state.
func (s *Store) creativeWrite(runID int64) (*sql.Tx, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	res, err := tx.Exec(`UPDATE creative_runs SET updated_at=? WHERE id=?`, time.Now().Unix(), runID)
	if err == nil {
		err = checkRowsAffected(res)
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// SetCreativeRunAllowedProfiles replaces a run's worker allowlist. Empty means
// all enabled profiles. The API validates names against the model registry.
func (s *Store) SetCreativeRunAllowedProfiles(runID int64, names []string) error {
	if names == nil {
		names = []string{}
	}
	value, err := json.Marshal(names)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE creative_runs SET allowed_profiles=?, updated_at=? WHERE id=?`, string(value), time.Now().Unix(), runID)
	if err != nil {
		return err
	}
	return checkRowsAffected(res)
}
