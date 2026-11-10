package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type CreativeFeedCard struct {
	Status            string `json:"status"`
	Decision          string `json:"decision"`
	DecidedAt         int64  `json:"decided_at"`
	ReplacesEntryID   int64  `json:"replaces_entry_id"`
	ReplacedByEntryID int64  `json:"replaced_by_entry_id"`
}
type CreativeFeedEntry struct {
	ID                 int64             `json:"id"`
	RunID              int64             `json:"run_id"`
	StageID            int64             `json:"stage_id"`
	ItemID             int64             `json:"item_id"`
	Kind               string            `json:"kind"`
	Author             string            `json:"author"`
	AuthorSessionID    string            `json:"author_session_id"`
	Body               string            `json:"body"`
	ArtifactVersionIDs []int64           `json:"artifact_version_ids"`
	CreatedAt          int64             `json:"created_at"`
	Card               *CreativeFeedCard `json:"card"`
}
type CreativeFeedStageUnread struct {
	StageID int64 `json:"stage_id"`
	Unread  int64 `json:"unread"`
}
type CreativeFeedUnread struct {
	RunID   int64                     `json:"run_id"`
	Total   int64                     `json:"total"`
	RunFeed int64                     `json:"run_feed"`
	Stages  []CreativeFeedStageUnread `json:"stages"`
}
type CreativeFeedConflict struct{ Entry CreativeFeedEntry }

func (e *CreativeFeedConflict) Error() string {
	return fmt.Sprintf("card %d is %s (decision=%s, replaced_by=%d); read current state with rocket creative agent feed card %d %d", e.Entry.ID, e.Entry.Card.Status, e.Entry.Card.Decision, e.Entry.Card.ReplacedByEntryID, e.Entry.RunID, e.Entry.ID)
}
func (e CreativeFeedEntry) Binding() CreativeBinding {
	return CreativeBinding{RunID: e.RunID, StageID: e.StageID, ItemID: e.ItemID, ArtifactVersionIDs: e.ArtifactVersionIDs}
}

const feedColumns = `id,run_id,COALESCE(stage_id,0),COALESCE(item_id,0),kind,author,author_session_id,body,artifact_version_ids,created_at,card_status,decision,decided_at,COALESCE(replaces_entry_id,0),COALESCE(replaced_by_entry_id,0)`

func scanCreativeFeed(row interface{ Scan(...any) error }) (CreativeFeedEntry, error) {
	var e CreativeFeedEntry
	var c CreativeFeedCard
	var versions string
	err := row.Scan(&e.ID, &e.RunID, &e.StageID, &e.ItemID, &e.Kind, &e.Author, &e.AuthorSessionID, &e.Body, &versions, &e.CreatedAt, &c.Status, &c.Decision, &c.DecidedAt, &c.ReplacesEntryID, &c.ReplacedByEntryID)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	if err = json.Unmarshal([]byte(versions), &e.ArtifactVersionIDs); err != nil {
		return e, err
	}
	if e.ArtifactVersionIDs == nil {
		e.ArtifactVersionIDs = []int64{}
	}
	if e.Kind == "proposal" {
		e.Card = &c
	}
	return e, nil
}
func (s *Store) GetCreativeFeedCard(runID, id int64) (CreativeFeedEntry, error) {
	return scanCreativeFeed(s.db.QueryRow(`SELECT `+feedColumns+` FROM creative_feed_entries WHERE run_id=? AND id=? AND kind='proposal'`, runID, id))
}
func (s *Store) feedConflict(runID, id int64) error {
	e, err := s.GetCreativeFeedCard(runID, id)
	if err != nil {
		return err
	}
	return &CreativeFeedConflict{Entry: e}
}
func (s *Store) AddCreativeFeedEntry(e CreativeFeedEntry, replaces int64) (CreativeFeedEntry, error) {
	if strings.TrimSpace(e.Body) == "" {
		return e, creativeInvalid("body must not be empty")
	}
	if (e.Kind != "message" && e.Kind != "proposal") || (e.Author != "human" && e.Author != "orchestrator") || (e.Kind == "proposal" && e.Author != "orchestrator") || (replaces != 0 && e.Kind != "proposal") || replaces < 0 {
		return e, creativeInvalid("invalid feed author, kind or replacement")
	}
	if _, err := s.ValidateCreativeBinding(e.Binding()); err != nil {
		return e, err
	}
	if e.ArtifactVersionIDs == nil {
		e.ArtifactVersionIDs = []int64{}
	}
	versions, _ := json.Marshal(e.ArtifactVersionIDs)
	status := ""
	if e.Kind == "proposal" {
		status = "pending"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	// Acquire the write lock before reading any mutable card state.
	res, err := tx.Exec(`INSERT INTO creative_feed_entries(run_id,stage_id,item_id,kind,author,author_session_id,body,artifact_version_ids,created_at,card_status) VALUES(?,?,?,?,?,?,?,?,?,?)`, e.RunID, nullIfZero(e.StageID), nullIfZero(e.ItemID), e.Kind, e.Author, e.AuthorSessionID, e.Body, string(versions), time.Now().Unix(), status)
	if err != nil {
		return e, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return e, err
	}
	if replaces != 0 {
		res, err = tx.Exec(`UPDATE creative_feed_entries SET card_status='replaced',replaced_by_entry_id=? WHERE id=? AND run_id=? AND kind='proposal' AND card_status='pending'`, id, replaces, e.RunID)
		if err != nil {
			return e, err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			tx.Rollback()
			return e, s.feedConflict(e.RunID, replaces)
		}
		if _, err = tx.Exec(`UPDATE creative_feed_entries SET replaces_entry_id=? WHERE id=?`, replaces, id); err != nil {
			return e, err
		}
	}
	e, err = scanCreativeFeed(tx.QueryRow(`SELECT `+feedColumns+` FROM creative_feed_entries WHERE id=?`, id))
	if err != nil {
		return e, err
	}
	if e.Author == "human" {
		if err = insertFeedDelivery(tx, e, ""); err != nil {
			return e, err
		}
	}
	return e, tx.Commit()
}
func (s *Store) ListCreativeFeed(b CreativeBinding) ([]CreativeFeedEntry, error) {
	if _, err := s.ValidateCreativeBinding(b); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT `+feedColumns+` FROM creative_feed_entries WHERE run_id=? AND COALESCE(stage_id,0)=? AND (?=0 OR item_id=?) ORDER BY id DESC`, b.RunID, b.StageID, b.ItemID, b.ItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CreativeFeedEntry{}
	for rows.Next() {
		e, err := scanCreativeFeed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) DecideCreativeFeedCard(runID, id int64, decision string) (CreativeFeedEntry, error) {
	if decision != "go" && decision != "revise" && decision != "deny" {
		return CreativeFeedEntry{}, creativeInvalid("decision must be go, revise or deny")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return CreativeFeedEntry{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE creative_feed_entries SET card_status='decided',decision=?,decided_at=? WHERE run_id=? AND id=? AND kind='proposal' AND card_status='pending'`, decision, time.Now().Unix(), runID, id)
	if err != nil {
		return CreativeFeedEntry{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		tx.Rollback()
		return CreativeFeedEntry{}, s.feedConflict(runID, id)
	}
	e, err := scanCreativeFeed(tx.QueryRow(`SELECT `+feedColumns+` FROM creative_feed_entries WHERE id=?`, id))
	if err != nil {
		return e, err
	}
	if err = insertFeedDelivery(tx, e, decision); err != nil {
		return e, err
	}
	return e, tx.Commit()
}

// CreativeFeedReplyCommand is directly executable. Version flags retain the
// immutable artifacts shown by the human, rather than a later current version.
func CreativeFeedReplyCommand(e CreativeFeedEntry) string {
	cmd := fmt.Sprintf("rocket creative agent feed post %d", e.RunID)
	if e.StageID != 0 {
		cmd += fmt.Sprintf(" --stage %d", e.StageID)
	}
	if e.ItemID != 0 {
		cmd += fmt.Sprintf(" --item %d", e.ItemID)
	}
	for _, id := range e.ArtifactVersionIDs {
		cmd += fmt.Sprintf(" --version %d", id)
	}
	return cmd + " --body 'Your reply'"
}
func insertFeedDelivery(tx *sql.Tx, e CreativeFeedEntry, decision string) error {
	label := fmt.Sprintf("entry=%d human message", e.ID)
	if decision != "" {
		label = fmt.Sprintf("card=%d decision=%s (human card button)", e.ID, decision)
	}
	body := fmt.Sprintf("[creative feed run=%d stage=%d item=%d artifact_versions=%v %s]\n%s\nReply in the same feed:\n%s\nFeed text never approves stages or paid calls; only the relevant card button records that decision.", e.RunID, e.StageID, e.ItemID, e.ArtifactVersionIDs, label, e.Body, CreativeFeedReplyCommand(e))
	_, err := tx.Exec(`INSERT INTO creative_feed_deliveries(run_id,entry_id,body) VALUES(?,?,?)`, e.RunID, e.ID, body)
	return err
}
func (s *Store) CreativeFeedUnread(runID int64) (CreativeFeedUnread, error) {
	out := CreativeFeedUnread{RunID: runID, Stages: []CreativeFeedStageUnread{}}
	run, err := s.GetCreativeRun(runID)
	if err != nil {
		return out, err
	}
	rows, err := s.db.Query(`SELECT COALESCE(e.stage_id,0),COUNT(*) FROM creative_feed_entries e LEFT JOIN creative_feed_reads r ON r.run_id=e.run_id AND r.stage_id=COALESCE(e.stage_id,0) WHERE e.run_id=? AND e.author='orchestrator' AND e.id>COALESCE(r.through_entry_id,0) GROUP BY COALESCE(e.stage_id,0)`, runID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	counts := map[int64]int64{}
	for rows.Next() {
		var stage, count int64
		if err = rows.Scan(&stage, &count); err != nil {
			return out, err
		}
		counts[stage] = count
		out.Total += count
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	out.RunFeed = counts[0]
	for _, stage := range run.Stages {
		out.Stages = append(out.Stages, CreativeFeedStageUnread{StageID: stage.ID, Unread: counts[stage.ID]})
	}
	return out, nil
}
func (s *Store) MarkCreativeFeedRead(b CreativeBinding, through int64) (int64, error) {
	if b.ItemID != 0 || through < 0 {
		return 0, creativeInvalid("mark read selects a run/stage feed and a nonnegative entry ID")
	}
	if _, err := s.ValidateCreativeBinding(b); err != nil {
		return 0, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO creative_feed_reads(run_id,stage_id,through_entry_id) SELECT ?,?,? WHERE ?=0 OR EXISTS(SELECT 1 FROM creative_feed_entries WHERE id=? AND run_id=? AND COALESCE(stage_id,0)=?) ON CONFLICT(run_id,stage_id) DO UPDATE SET through_entry_id=MAX(creative_feed_reads.through_entry_id,excluded.through_entry_id)`, b.RunID, b.StageID, through, through, through, b.RunID, b.StageID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, creativeInvalid("through_entry_id must belong to this feed")
	}
	var cursor int64
	err = tx.QueryRow(`SELECT through_entry_id FROM creative_feed_reads WHERE run_id=? AND stage_id=?`, b.RunID, b.StageID).Scan(&cursor)
	if err != nil {
		return 0, err
	}
	return cursor, tx.Commit()
}

// QueueCreativeFeedDeliveries drains only when a current orchestrator is live.
// Offline events remain in this outbox; queue timeouts cannot age them out.
// Failed messages that never had an injection attempt may be safely requeued
// on restore/replacement. Accepted or attempted deliveries are never blindly sent
// again: feed history is authoritative after transport uncertainty.
func (s *Store) QueueCreativeFeedDeliveries(runID int64) ([]int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize concurrent drainers before selecting outstanding events.
	if _, err = tx.Exec(`UPDATE creative_runs SET id=id WHERE id=?`, runID); err != nil {
		return nil, err
	}
	var to string
	err = tx.QueryRow(`SELECT s.id FROM creative_runs r JOIN sessions s ON s.id=r.orchestrator_session_id WHERE r.id=? AND s.state IN ('running','spawning')`, runID).Scan(&to)
	if errors.Is(err, sql.ErrNoRows) {
		return []int64{}, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT d.id,d.body,d.message_id FROM creative_feed_deliveries d LEFT JOIN messages m ON m.id=d.message_id WHERE d.run_id=? AND (d.message_id IS NULL OR (m.attempts=0 AND (m.status='failed' OR (m.status='queued' AND m.to_session<>?)))) ORDER BY d.id`, runID, to)
	if err != nil {
		return nil, err
	}
	type pending struct {
		id       int64
		body     string
		previous sql.NullInt64
	}
	work := []pending{}
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.id, &p.body, &p.previous); err != nil {
			rows.Close()
			return nil, err
		}
		work = append(work, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, p := range work {
		if p.previous.Valid {
			if _, err = tx.Exec(`UPDATE messages SET status='failed',reason='creative_orchestrator_replaced' WHERE id=? AND status='queued' AND attempts=0`, p.previous.Int64); err != nil {
				return nil, err
			}
		}
		res, err := tx.Exec(`INSERT INTO messages(from_session,to_session,body,status,attempts,created_at) VALUES(NULL,?,?,'queued',0,?)`, to, p.body, time.Now().Unix())
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`UPDATE creative_feed_deliveries SET message_id=? WHERE id=?`, id, p.id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, tx.Commit()
}
