package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// CreativeBinding records the exact scope and shown immutable versions of a
// discussion. It never follows an artifact's current-version pointer.
type CreativeBinding struct {
	RunID              int64   `json:"run_id"`
	StageID            int64   `json:"stage_id,omitempty"`
	ItemID             int64   `json:"item_id,omitempty"`
	ArtifactVersionIDs []int64 `json:"artifact_version_ids,omitempty"`
}

func (s *Store) ValidateCreativeBinding(b CreativeBinding) (CreativeContext, error) {
	c, err := s.ResolveCreativeContext(b.RunID, b.StageID, b.ItemID)
	if err != nil {
		return c, err
	}
	if err := s.ValidateCreativeArtifactVersions(b.RunID, b.StageID, b.ItemID, b.ArtifactVersionIDs); err != nil {
		return c, err
	}
	return c, nil
}
func (s *Store) BindCreativeSession(id string, b CreativeBinding) error {
	c, err := s.ValidateCreativeBinding(b)
	if err != nil {
		return err
	}
	sess, err := s.GetSession(id)
	if err != nil {
		return err
	}
	if sess.ProjectID != c.ProjectID || sess.TaskID != 0 || sess.SubtaskID != 0 {
		return creativeInvalid("creative session must belong to the run project and have no development task")
	}
	tx, err := s.creativeWrite(b.RunID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO creative_session_contexts(session_id,run_id,stage_id,item_id) VALUES(?,?,?,?)`, id, b.RunID, nullIfZero(b.StageID), nullIfZero(b.ItemID))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) GetCreativeSessionContext(id string) (CreativeBinding, error) {
	var b CreativeBinding
	err := s.db.QueryRow(`SELECT run_id,COALESCE(stage_id,0),COALESCE(item_id,0) FROM creative_session_contexts WHERE session_id=?`, id).Scan(&b.RunID, &b.StageID, &b.ItemID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return b, err
}
func (s *Store) ListCreativeSessionIDs(runID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT session_id FROM creative_session_contexts WHERE run_id=? ORDER BY session_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func (s *Store) GetCreativeDiscussionContext(kind string, id int64) (CreativeBinding, error) {
	column, err := discussionColumn(kind)
	if err != nil {
		return CreativeBinding{}, err
	}
	var b CreativeBinding
	var versions string
	err = s.db.QueryRow(`SELECT run_id,COALESCE(stage_id,0),COALESCE(item_id,0),artifact_version_ids FROM creative_discussion_contexts WHERE `+column+`=?`, id).Scan(&b.RunID, &b.StageID, &b.ItemID, &versions)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	err = json.Unmarshal([]byte(versions), &b.ArtifactVersionIDs)
	return b, err
}
func discussionColumn(kind string) (string, error) {
	switch kind {
	case "message":
		return "message_id", nil
	case "question":
		return "question_id", nil
	}
	return "", fmt.Errorf("unknown discussion kind")
}
func insertCreativeDiscussion(tx *sql.Tx, kind string, id int64, b CreativeBinding) error {
	column, err := discussionColumn(kind)
	if err != nil {
		return err
	}
	ids := b.ArtifactVersionIDs
	if ids == nil {
		ids = []int64{}
	}
	data, _ := json.Marshal(ids)
	_, err = tx.Exec(`INSERT INTO creative_discussion_contexts(`+column+`,run_id,stage_id,item_id,artifact_version_ids) VALUES(?,?,?,?,?)`, id, b.RunID, nullIfZero(b.StageID), nullIfZero(b.ItemID), string(data))
	return err
}
func (s *Store) ListCreativeDiscussionIDs(kind string, b CreativeBinding) ([]int64, error) {
	column, err := discussionColumn(kind)
	if err != nil {
		return nil, err
	}
	return creativeIDs(s.db, `SELECT `+column+` FROM creative_discussion_contexts WHERE `+column+` IS NOT NULL AND run_id=? AND COALESCE(stage_id,0)=? AND COALESCE(item_id,0)=? ORDER BY `+column, b.RunID, b.StageID, b.ItemID)
}
func (s *Store) AddCreativeMessage(m Message, b CreativeBinding) (int64, error) {
	if _, err := s.ValidateCreativeBinding(b); err != nil {
		return 0, err
	}
	tx, err := s.creativeWrite(b.RunID)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO messages(from_session,to_session,body,status,attempts,created_at) VALUES(?,?,?,'queued',0,?)`, nullIfEmpty(m.FromSession), m.ToSession, m.Body, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := insertCreativeDiscussion(tx, "message", id, b); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
func (s *Store) AddCreativeQuestion(q Question, b CreativeBinding) (int64, error) {
	if _, err := s.ValidateCreativeBinding(b); err != nil {
		return 0, err
	}
	if q.Title == "" {
		q.Title = DeriveTitle(q.Body)
	}
	tx, err := s.creativeWrite(b.RunID)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	status, typ, resolution, resolvedAt := "open", QuestionTypeDecision, "", int64(0)
	if q.Type == QuestionTypeFYI {
		status, typ, resolution, resolvedAt = "resolved", QuestionTypeFYI, QuestionResolutionFYI, now
	}
	res, err := tx.Exec(`INSERT INTO questions(asked_by,title,brief,body,status,type,options,asked_at,resolution,resolved_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, q.AskedBy, q.Title, q.Brief, q.Body, status, typ, encodeOptions(q.Options), now, nullIfEmpty(resolution), nullIfZero(resolvedAt))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := insertCreativeDiscussion(tx, "question", id, b); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
