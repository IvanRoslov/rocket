package store

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Brainstorm quality metric (task #4901, spec §4): how often the human takes
// the orchestrator's recommendation in a storm. Computed on the fly from the
// brainstorm threads, the storm exit gates and the skill each task started
// with — there is no aggregate table to keep in sync.

// SkillUnknown is the skill a storm is filed under when its task started
// before tasks remembered their brainstorm skill.
const SkillUnknown = "unknown"

// BrainstormWeek is the metric of one ISO week for one brainstorm skill. Only
// the human's answers count; Answered = Accepted + Corrected + WrongTurn.
type BrainstormWeek struct {
	Week                string // ISO week, "2026-W40"
	Skill               string
	Answered            int
	Accepted            int
	AcceptedWithComment int
	Corrected           int
	WrongTurn           int
}

// BrainstormCounts are a storm's counters. Questions counts every brainstorm
// thread of the task; the answer counters only those the human answered.
type BrainstormCounts struct {
	Questions           int
	Answered            int
	Accepted            int
	AcceptedWithComment int
	Corrected           int
	WrongTurn           int
	SpecChanges         int // gates decided "changes"
}

// BrainstormStorm is the metric of one task's storm over its whole life.
// GoAt is when its gate got Go (nil before that); LastActivity is the latest
// question or gate time, which orders storms and decides whether a storm
// falls into a stats window.
type BrainstormStorm struct {
	TaskID    int64
	Title     string
	ProjectID string
	Skill     string
	BrainstormCounts
	GoAt         *int64
	LastActivity int64
}

// BrainstormStats is the metric over a window of weeks: per-week rows
// (oldest week first, then by skill) and the storms active in the window
// (most recently active first). Both lists are non-nil.
type BrainstormStats struct {
	Weeks  []BrainstormWeek
	Storms []BrainstormStorm
}

// ISOWeekLabel renders t's ISO week in t's own location, e.g. "2026-W40".
func ISOWeekLabel(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// BrainstormWindowStart is the start of a window of weeks ISO weeks ending
// with now's: Monday 00:00, in now's location, weeks-1 weeks before the
// Monday of now's week.
func BrainstormWindowStart(now time.Time, weeks int) time.Time {
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	y, m, d := now.Date()
	return time.Date(y, m, d-daysSinceMonday-7*(weeks-1), 0, 0, 0, 0, now.Location())
}

// stormQuestion is one brainstorm thread as the metric reads it.
type stormQuestion struct {
	taskID       int64
	status       string
	resolution   string
	outcome      string
	comment      string
	askedAt      int64
	resolvedAt   int64
	answerAuthor sql.NullString // author of the latest answer entry
}

// humanAnswer reports whether q counts in the metric: answered with an
// outcome, and the answer entry that closed it is the human's — a persistent
// agent answering a storm question says nothing about the recommendation's
// quality for the human.
func (q stormQuestion) humanAnswer() bool {
	return q.status == "resolved" && q.resolution == "answered" && q.outcome != "" &&
		q.answerAuthor.Valid && IsHuman(q.answerAuthor.String)
}

// count adds q's outcome to the answer counters.
func (c *BrainstormCounts) count(q stormQuestion) {
	c.Answered++
	switch q.outcome {
	case OutcomeAccepted:
		c.Accepted++
		if q.comment != "" {
			c.AcceptedWithComment++
		}
	case OutcomeCorrected:
		c.Corrected++
	case OutcomeWrongTurn:
		c.WrongTurn++
	}
}

type stormGate struct {
	taskID      int64
	status      string
	requestedAt int64
	decidedAt   sql.NullInt64
}

// loadStormData reads the brainstorm threads and the gates, of one task when
// taskID != 0 or of all tasks.
func (s *Store) loadStormData(taskID int64) ([]stormQuestion, []stormGate, error) {
	qSQL := `SELECT q.task_id, q.status, COALESCE(q.resolution, ''), q.outcome, q.answer_comment,
		       q.asked_at, COALESCE(q.resolved_at, 0),
		       (SELECT m.author FROM question_messages m
		         WHERE m.question_id = q.id AND m.kind = 'answer' ORDER BY m.id DESC LIMIT 1)
		  FROM questions q
		 WHERE q.type = ? AND q.task_id IS NOT NULL`
	gSQL := `SELECT task_id, status, requested_at, decided_at FROM task_gates`
	qArgs := []any{QuestionTypeBrainstorm}
	var gArgs []any
	if taskID != 0 {
		qSQL += ` AND q.task_id = ?`
		qArgs = append(qArgs, taskID)
		gSQL += ` WHERE task_id = ?`
		gArgs = append(gArgs, taskID)
	}

	rows, err := s.db.Query(qSQL, qArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("query brainstorm questions: %w", err)
	}
	var qs []stormQuestion
	for rows.Next() {
		var q stormQuestion
		if err := rows.Scan(&q.taskID, &q.status, &q.resolution, &q.outcome, &q.comment,
			&q.askedAt, &q.resolvedAt, &q.answerAuthor); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("scan brainstorm question: %w", err)
		}
		qs = append(qs, q)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}

	rows, err = s.db.Query(gSQL, gArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("query gates: %w", err)
	}
	var gs []stormGate
	for rows.Next() {
		var g stormGate
		if err := rows.Scan(&g.taskID, &g.status, &g.requestedAt, &g.decidedAt); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("scan gate: %w", err)
		}
		gs = append(gs, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	return qs, gs, nil
}

// stormSkill is the task's brainstorm skill as the metric files it.
func stormSkill(t Task) string {
	if t.BrainstormSkill == "" {
		return SkillUnknown
	}
	return t.BrainstormSkill
}

// buildStorms folds questions and gates into one storm per task that has
// any of them.
func (s *Store) buildStorms(qs []stormQuestion, gs []stormGate) (map[int64]*BrainstormStorm, error) {
	storms := map[int64]*BrainstormStorm{}
	storm := func(taskID int64) (*BrainstormStorm, error) {
		if st, ok := storms[taskID]; ok {
			return st, nil
		}
		t, err := s.GetTask(taskID)
		if err != nil {
			return nil, fmt.Errorf("get storm task %d: %w", taskID, err)
		}
		st := &BrainstormStorm{TaskID: t.ID, Title: t.Title, ProjectID: t.ProjectID, Skill: stormSkill(t)}
		storms[taskID] = st
		return st, nil
	}
	for _, q := range qs {
		st, err := storm(q.taskID)
		if err != nil {
			return nil, err
		}
		st.Questions++
		if q.humanAnswer() {
			st.count(q)
		}
		st.LastActivity = max(st.LastActivity, q.askedAt, q.resolvedAt)
	}
	for _, g := range gs {
		st, err := storm(g.taskID)
		if err != nil {
			return nil, err
		}
		switch g.status {
		case "changes":
			st.SpecChanges++
		case "go":
			if g.decidedAt.Valid {
				v := g.decidedAt.Int64
				st.GoAt = &v
			}
		}
		st.LastActivity = max(st.LastActivity, g.requestedAt, g.decidedAt.Int64)
	}
	return storms, nil
}

// BrainstormStats computes the metric over the weeks ISO weeks ending with
// now's, cut in now's location (the daemon's local time). A question falls
// into the week it was answered in; weeks without answers are left out.
func (s *Store) BrainstormStats(now time.Time, weeks int) (BrainstormStats, error) {
	qs, gs, err := s.loadStormData(0)
	if err != nil {
		return BrainstormStats{}, err
	}
	storms, err := s.buildStorms(qs, gs)
	if err != nil {
		return BrainstormStats{}, err
	}
	start := BrainstormWindowStart(now, weeks).Unix()
	loc := now.Location()

	type weekKey struct{ week, skill string }
	byWeek := map[weekKey]*BrainstormWeek{}
	for _, q := range qs {
		if !q.humanAnswer() || q.resolvedAt < start {
			continue
		}
		k := weekKey{ISOWeekLabel(time.Unix(q.resolvedAt, 0).In(loc)), storms[q.taskID].Skill}
		w, ok := byWeek[k]
		if !ok {
			w = &BrainstormWeek{Week: k.week, Skill: k.skill}
			byWeek[k] = w
		}
		c := BrainstormCounts{}
		c.count(q)
		w.Answered += c.Answered
		w.Accepted += c.Accepted
		w.AcceptedWithComment += c.AcceptedWithComment
		w.Corrected += c.Corrected
		w.WrongTurn += c.WrongTurn
	}

	out := BrainstormStats{Weeks: []BrainstormWeek{}, Storms: []BrainstormStorm{}}
	for _, w := range byWeek {
		out.Weeks = append(out.Weeks, *w)
	}
	sort.Slice(out.Weeks, func(i, j int) bool {
		a, b := out.Weeks[i], out.Weeks[j]
		if a.Week != b.Week {
			return a.Week < b.Week
		}
		return a.Skill < b.Skill
	})
	for _, st := range storms {
		if st.LastActivity >= start {
			out.Storms = append(out.Storms, *st)
		}
	}
	sort.Slice(out.Storms, func(i, j int) bool {
		a, b := out.Storms[i], out.Storms[j]
		if a.LastActivity != b.LastActivity {
			return a.LastActivity > b.LastActivity
		}
		return a.TaskID > b.TaskID
	})
	return out, nil
}

// TaskBrainstormStats computes one task's storm; a task without a storm gets
// zeros. ErrNotFound for an unknown task.
func (s *Store) TaskBrainstormStats(taskID int64) (BrainstormStorm, error) {
	t, err := s.GetTask(taskID)
	if err != nil {
		return BrainstormStorm{}, err
	}
	qs, gs, err := s.loadStormData(taskID)
	if err != nil {
		return BrainstormStorm{}, err
	}
	storms, err := s.buildStorms(qs, gs)
	if err != nil {
		return BrainstormStorm{}, err
	}
	if st, ok := storms[taskID]; ok {
		return *st, nil
	}
	return BrainstormStorm{TaskID: t.ID, Title: t.Title, ProjectID: t.ProjectID, Skill: stormSkill(t)}, nil
}
