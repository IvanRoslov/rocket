package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func validateCreativeItem(in CreativeItemInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return creativeInvalid("item name is required")
	}
	return nil
}
func mutableCreativeItems(stage CreativeStage) error {
	if !stage.Repeated {
		return creativeInvalid("stage is not repeated")
	}
	if stage.Status != "pending" && stage.Status != "in_progress" {
		return creativeConflict("stage items are frozen during review and after completion")
	}
	return nil
}
func (s *Store) CreateCreativeItem(runID, stageID int64, in CreativeItemInput) (CreativeItem, error) {
	if err := validateCreativeItem(in); err != nil {
		return CreativeItem{}, err
	}
	tx, err := s.creativeWrite(runID)
	if err != nil {
		return CreativeItem{}, err
	}
	defer tx.Rollback()
	if _, err = resolveCreativeContext(tx, runID, stageID, 0); err != nil {
		return CreativeItem{}, err
	}
	stage, err := getCreativeStage(tx, stageID)
	if err != nil {
		return CreativeItem{}, err
	}
	if err = mutableCreativeItems(stage); err != nil {
		return CreativeItem{}, err
	}
	res, err := tx.Exec(`INSERT INTO creative_items(run_id,stage_id,position,name,description,expected_artifacts) SELECT ?,?,COALESCE(MAX(position),-1)+1,?,?,? FROM creative_items WHERE stage_id=?`, runID, stageID, in.Name, in.Description, in.ExpectedArtifacts, stageID)
	if err != nil {
		return CreativeItem{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return CreativeItem{}, err
	}
	item, err := getCreativeItem(tx, id)
	if err != nil {
		return item, err
	}
	return item, tx.Commit()
}
func (s *Store) UpdateCreativeItem(runID, stageID, itemID int64, in CreativeItemInput) (CreativeItem, error) {
	if err := validateCreativeItem(in); err != nil {
		return CreativeItem{}, err
	}
	tx, err := s.creativeWrite(runID)
	if err != nil {
		return CreativeItem{}, err
	}
	defer tx.Rollback()
	if err = editableCreativeItem(tx, runID, stageID, itemID); err != nil {
		return CreativeItem{}, err
	}
	_, err = tx.Exec(`UPDATE creative_items SET name=?,description=?,expected_artifacts=? WHERE id=?`, in.Name, in.Description, in.ExpectedArtifacts, itemID)
	if err != nil {
		return CreativeItem{}, err
	}
	item, err := getCreativeItem(tx, itemID)
	if err != nil {
		return item, err
	}
	return item, tx.Commit()
}
func editableCreativeItem(tx *sql.Tx, runID, stageID, itemID int64) error {
	if itemID <= 0 || stageID <= 0 {
		return creativeInvalid("stage and item IDs required")
	}
	if _, err := resolveCreativeContext(tx, runID, stageID, itemID); err != nil {
		return err
	}
	stage, err := getCreativeStage(tx, stageID)
	if err != nil {
		return err
	}
	if err = mutableCreativeItems(stage); err != nil {
		return err
	}
	item, err := getCreativeItem(tx, itemID)
	if err != nil {
		return err
	}
	if item.Status != "pending" {
		return creativeConflict("only pending items can be edited or removed")
	}
	return nil
}
func (s *Store) DeleteCreativeItem(runID, stageID, itemID int64) error {
	tx, err := s.creativeWrite(runID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = editableCreativeItem(tx, runID, stageID, itemID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM creative_items WHERE id=?`, itemID); err != nil {
		return err
	}
	return tx.Commit()
}
func activeCreativeStage(run CreativeRun, stageID int64) error {
	for _, st := range run.Stages {
		if st.Status != "completed" {
			if st.ID == stageID {
				return nil
			}
			break
		}
	}
	return creativeConflict("only the first unfinished stage can progress")
}
func (s *Store) UpdateCreativeProgress(runID, stageID, itemID int64, in CreativeProgress) (CreativeRun, error) {
	if in.Progress < 0 || in.Progress > 100 {
		return CreativeRun{}, creativeInvalid("progress must be 0..100")
	}
	tx, err := s.creativeWrite(runID)
	if err != nil {
		return CreativeRun{}, err
	}
	defer tx.Rollback()
	if _, err = resolveCreativeContext(tx, runID, stageID, itemID); err != nil {
		return CreativeRun{}, err
	}
	run, err := getCreativeRun(tx, runID)
	if err != nil {
		return run, err
	}
	if err = activeCreativeStage(run, stageID); err != nil {
		return run, err
	}
	stage, err := getCreativeStage(tx, stageID)
	if err != nil {
		return run, err
	}
	if stage.Status != "in_progress" {
		return run, creativeConflict("stage is not in progress")
	}
	table, id, status := "creative_stages", stageID, stage.Status
	if itemID != 0 {
		item, e := getCreativeItem(tx, itemID)
		if e != nil {
			return run, e
		}
		table, id, status = "creative_items", itemID, item.Status
	}
	if status != "in_progress" {
		return run, creativeConflict("target is not in progress")
	}
	if _, err = tx.Exec(`UPDATE `+table+` SET progress=?,progress_note=? WHERE id=?`, in.Progress, in.ProgressNote, id); err != nil {
		return run, err
	}
	run, err = getCreativeRun(tx, runID)
	if err != nil {
		return run, err
	}
	return run, tx.Commit()
}

// validateCreativeReviewVersions deliberately fails closed when the assets
// migration is not installed. This SQL contract is owned with deliverable 5988;
// immutable version rows make stored review snapshots independent of current.
func validateCreativeReviewVersions(tx *sql.Tx, runID, stageID, itemID int64, ids []int64) error {
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return creativeInvalid("artifact version IDs must be positive and unique")
		}
		seen[id] = true
		var exists int
		err := tx.QueryRow(`SELECT 1 FROM creative_artifact_versions v JOIN creative_artifacts a ON a.id=v.artifact_id WHERE v.id=? AND a.run_id=? AND COALESCE(a.stage_id,0)=? AND COALESCE(a.item_id,0)=?`, id, runID, stageID, itemID).Scan(&exists)
		if err != nil {
			return fmt.Errorf("%w: artifact version %d is unavailable in this context: %v", ErrCreativeInvalid, id, err)
		}
	}
	return nil
}

// ApplyCreativeAction is the sole completion/review transition path. human is
// derived from authenticated caller identity by the API, never a request field.
func (s *Store) ApplyCreativeAction(runID, stageID, itemID int64, action CreativeAction, human bool) (CreativeRun, error) {
	if stageID <= 0 {
		return CreativeRun{}, creativeInvalid("stage ID required")
	}
	if (action.Action == "approve" || action.Action == "reject" || action.Action == "reopen") && !human {
		return CreativeRun{}, ErrCreativeForbidden
	}
	tx, err := s.creativeWrite(runID)
	if err != nil {
		return CreativeRun{}, err
	}
	defer tx.Rollback()
	if _, err = resolveCreativeContext(tx, runID, stageID, itemID); err != nil {
		return CreativeRun{}, err
	}
	run, err := getCreativeRun(tx, runID)
	if err != nil {
		return run, err
	}
	stage, err := getCreativeStage(tx, stageID)
	if err != nil {
		return run, err
	}
	table, id, status, progress, review := "creative_stages", stageID, stage.Status, stage.Progress, stage.CreativeReview
	var item CreativeItem
	if itemID != 0 {
		item, err = getCreativeItem(tx, itemID)
		if err != nil {
			return run, err
		}
		table, id, status, progress, review = "creative_items", itemID, item.Status, item.Progress, item.CreativeReview
	}
	if action.Action != "reopen" {
		if err = activeCreativeStage(run, stageID); err != nil {
			return run, err
		}
		if itemID != 0 && stage.Status != "in_progress" {
			return run, creativeConflict("stage must be in progress for item actions")
		}
	}
	if action.Action != "submit" && len(action.ArtifactVersionIDs) > 0 {
		return run, creativeInvalid("artifact_version_ids only allowed on submit")
	}
	switch action.Action {
	case "start":
		if status != "pending" {
			return run, creativeConflict("only pending work can start")
		}
		if itemID != 0 && stage.Approval == "item" {
			for _, prior := range stage.Items {
				if prior.Position < item.Position && prior.Status != "completed" {
					return run, creativeConflict("previous item requires completion/approval")
				}
			}
		}
		status = "in_progress"
	case "submit":
		if status != "in_progress" {
			return run, creativeConflict("only in-progress work can submit")
		}
		if itemID == 0 && stage.Repeated {
			if len(stage.Items) == 0 {
				return run, creativeConflict("repeated stage requires at least one item")
			}
			for _, i := range stage.Items {
				if i.Status != "completed" {
					return run, creativeConflict("all items must complete before stage submission")
				}
			}
		}
		if err = validateCreativeReviewVersions(tx, runID, stageID, itemID, action.ArtifactVersionIDs); err != nil {
			return run, err
		}
		review.ReviewRevision++
		review.ArtifactVersionIDs = append([]int64{}, action.ArtifactVersionIDs...)
		status = "completed"
		progress = 100
		if (itemID == 0 && stage.Approval == "stage") || (itemID != 0 && stage.Approval == "item") {
			status = "awaiting_approval"
		}
	case "approve", "reject":
		if status != "awaiting_approval" || action.ReviewRevision <= 0 || action.ReviewRevision != review.ReviewRevision {
			return run, creativeConflict("approval requires the current pending review revision")
		}
		if action.Action == "approve" {
			status = "completed"
			progress = 100
		} else {
			status = "in_progress"
		}
	case "reopen":
		if status != "completed" {
			return run, creativeConflict("only completed work can be reopened")
		}
		status = "in_progress"
		progress = 0
		if itemID != 0 {
			// Reopening invalidates the enclosing review while keeping its snapshot
			// as history until resubmission; downstream stages are never reset.
			if _, err = tx.Exec(`UPDATE creative_stages SET status='in_progress',progress=0 WHERE id=?`, stageID); err != nil {
				return run, err
			}
		}
	default:
		return run, creativeInvalid("unknown action")
	}
	if review.ArtifactVersionIDs == nil {
		review.ArtifactVersionIDs = []int64{}
	}
	versions, _ := json.Marshal(review.ArtifactVersionIDs)
	_, err = tx.Exec(`UPDATE `+table+` SET status=?,progress=?,review_revision=?,artifact_version_ids=? WHERE id=?`, status, progress, review.ReviewRevision, string(versions), id)
	if err != nil {
		return run, err
	}
	run, err = getCreativeRun(tx, runID)
	if err != nil {
		return run, err
	}
	return run, tx.Commit()
}
