package store

import (
	"cmp"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Brainstorm quality metric (task #4901, spec §4; task #5019): how often the
// orchestrator's recommendation is taken in a storm. Every answer counts,
// attributed to its author — the human or a persistent agent — so the two are
// never summed into one figure. Computed on the fly from the brainstorm
// threads, the storm exit gates and the skill each task started with — there
// is no aggregate table to keep in sync.

// SkillUnknown is the skill a storm is filed under when its task started
// before tasks remembered their brainstorm skill.
const SkillUnknown = "unknown"

// BrainstormAnswers are the answer counters; Answered = Accepted + Corrected +
// WrongTurn.
type BrainstormAnswers struct {
	Answered            int
	Accepted            int
	AcceptedWithComment int
	Corrected           int
	WrongTurn           int
}

// BrainstormWeek is the metric of one ISO week for one brainstorm skill and
// one answerer: ParticipantHuman or a persistent agent's id.
type BrainstormWeek struct {
	Week       string // ISO week, "2026-W40"
	Skill      string
	AnsweredBy string
	BrainstormAnswers
}

// BrainstormCounts are a storm's counters. Questions counts every brainstorm
// thread of the task; the answer counters every answered one, whoever
// answered it.
type BrainstormCounts struct {
	Questions int
	BrainstormAnswers
	SpecChanges int // gates decided "changes" before the first Go
}

// BrainstormAnswererCounts are one answerer's share of a storm's answers.
type BrainstormAnswererCounts struct {
	AnsweredBy string
	BrainstormAnswers
}

// BrainstormStorm is the metric of one task's storm over its whole life.
// AnsweredBy lists who answered, each once, in order of first answer, and
// ByAnswerer splits the answer counters the same way; both are non-nil.
// GoAt is when its gate got Go (nil before that); FirstTryGo is a Go without
// spec changes before it; HasGate tells a storm whose gate was ever requested.
// LastActivity is the latest question or gate time, which orders storms and
// decides whether a storm falls into a stats window.
type BrainstormStorm struct {
	TaskID    int64
	Title     string
	ProjectID string
	Skill     string
	BrainstormCounts
	AnsweredBy   []string
	ByAnswerer   []BrainstormAnswererCounts
	GoAt         *int64
	FirstTryGo   bool
	HasGate      bool
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
	id           int64
	taskID       int64
	status       string
	resolution   string
	outcome      string
	comment      string
	askedAt      int64
	resolvedAt   int64
	answerAuthor sql.NullString // author of the latest answer entry
}

// answerer is the participant whose answer closed q, canonical ("" is the
// human), and whether q counts in the metric at all: answered with an outcome.
func (q stormQuestion) answerer() (string, bool) {
	if q.status != "resolved" || q.resolution != "answered" || q.outcome == "" || !q.answerAuthor.Valid {
		return "", false
	}
	return canonicalParticipant(q.answerAuthor.String), true
}

// count adds q's outcome to the answer counters.
func (c *BrainstormAnswers) count(q stormQuestion) {
	c.Answered++
	switch q.outcome {
	case OutcomeAccepted:
		c.Accepted++
		if strings.TrimSpace(q.comment) != "" {
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
	qSQL := `SELECT q.id, q.task_id, q.status, COALESCE(q.resolution, ''), q.outcome, q.answer_comment,
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
		if err := rows.Scan(&q.id, &q.taskID, &q.status, &q.resolution, &q.outcome, &q.comment,
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

// stormTaskBatch caps how many task ids stormTasks binds into one query, well
// under SQLite's host-parameter limit.
const stormTaskBatch = 500

// stormTasks loads the storm-relevant fields of the tasks named in ids, in a
// few batched queries rather than one per storm.
func (s *Store) stormTasks(ids []int64) (map[int64]Task, error) {
	out := make(map[int64]Task, len(ids))
	for start := 0; start < len(ids); start += stormTaskBatch {
		batch := ids[start:min(start+stormTaskBatch, len(ids))]
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := s.db.Query(`SELECT id, title, project_id, brainstorm_skill FROM tasks
			WHERE id IN (`+placeholders+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("query storm tasks: %w", err)
		}
		for rows.Next() {
			var t Task
			if err := rows.Scan(&t.ID, &t.Title, &t.ProjectID, &t.BrainstormSkill); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan storm task: %w", err)
			}
			out[t.ID] = t
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// newStorm is the empty storm of task t.
func newStorm(t Task) *BrainstormStorm {
	return &BrainstormStorm{TaskID: t.ID, Title: t.Title, ProjectID: t.ProjectID, Skill: stormSkill(t),
		AnsweredBy: []string{}, ByAnswerer: []BrainstormAnswererCounts{}}
}

// countAnswer adds q, answered by who, to the storm totals and to who's share,
// appending who on its first answer.
func (st *BrainstormStorm) countAnswer(q stormQuestion, who string) {
	st.count(q)
	for i := range st.ByAnswerer {
		if st.ByAnswerer[i].AnsweredBy == who {
			st.ByAnswerer[i].count(q)
			return
		}
	}
	c := BrainstormAnswererCounts{AnsweredBy: who}
	c.count(q)
	st.ByAnswerer = append(st.ByAnswerer, c)
	st.AnsweredBy = append(st.AnsweredBy, who)
}

// buildStorms folds questions and gates into one storm per task that has
// any of them; tasks holds every task they reference.
func buildStorms(qs []stormQuestion, gs []stormGate, tasks map[int64]Task) (map[int64]*BrainstormStorm, error) {
	storms := map[int64]*BrainstormStorm{}
	storm := func(taskID int64) (*BrainstormStorm, error) {
		if st, ok := storms[taskID]; ok {
			return st, nil
		}
		t, ok := tasks[taskID]
		if !ok {
			return nil, fmt.Errorf("storm task %d: %w", taskID, ErrNotFound)
		}
		st := newStorm(t)
		storms[taskID] = st
		return st, nil
	}
	// Fold in answer order so AnsweredBy and ByAnswerer list each answerer
	// at its first answer.
	qs = slices.Clone(qs)
	slices.SortStableFunc(qs, func(a, b stormQuestion) int {
		return cmp.Or(cmp.Compare(a.resolvedAt, b.resolvedAt), cmp.Compare(a.id, b.id))
	})
	for _, q := range qs {
		st, err := storm(q.taskID)
		if err != nil {
			return nil, err
		}
		st.Questions++
		if who, ok := q.answerer(); ok {
			st.countAnswer(q, who)
		}
		st.LastActivity = max(st.LastActivity, q.askedAt, q.resolvedAt)
	}
	for _, g := range gs {
		st, err := storm(g.taskID)
		if err != nil {
			return nil, err
		}
		st.HasGate = true
		// The storm exited at its first Go.
		if g.status == "go" && g.decidedAt.Valid && (st.GoAt == nil || g.decidedAt.Int64 < *st.GoAt) {
			v := g.decidedAt.Int64
			st.GoAt = &v
		}
		st.LastActivity = max(st.LastActivity, g.requestedAt, g.decidedAt.Int64)
	}
	// Spec changes count up to the first Go only, which is known once every
	// gate is folded. A gate superseded by a new spec version is no change:
	// only the human's explicit "changes" is.
	for _, g := range gs {
		st := storms[g.taskID]
		if g.status == "changes" && (st.GoAt == nil || (g.decidedAt.Valid && g.decidedAt.Int64 < *st.GoAt)) {
			st.SpecChanges++
		}
	}
	for _, st := range storms {
		st.FirstTryGo = st.GoAt != nil && st.SpecChanges == 0
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
	seen := map[int64]bool{}
	var ids []int64
	for _, q := range qs {
		if !seen[q.taskID] {
			seen[q.taskID] = true
			ids = append(ids, q.taskID)
		}
	}
	for _, g := range gs {
		if !seen[g.taskID] {
			seen[g.taskID] = true
			ids = append(ids, g.taskID)
		}
	}
	tasks, err := s.stormTasks(ids)
	if err != nil {
		return BrainstormStats{}, err
	}
	storms, err := buildStorms(qs, gs, tasks)
	if err != nil {
		return BrainstormStats{}, err
	}
	start := BrainstormWindowStart(now, weeks).Unix()
	loc := now.Location()

	type weekKey struct{ week, skill, answeredBy string }
	byWeek := map[weekKey]*BrainstormWeek{}
	for _, q := range qs {
		who, ok := q.answerer()
		if !ok || q.resolvedAt < start {
			continue
		}
		k := weekKey{ISOWeekLabel(time.Unix(q.resolvedAt, 0).In(loc)), storms[q.taskID].Skill, who}
		w, ok := byWeek[k]
		if !ok {
			w = &BrainstormWeek{Week: k.week, Skill: k.skill, AnsweredBy: k.answeredBy}
			byWeek[k] = w
		}
		w.count(q)
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
		if a.Skill != b.Skill {
			return a.Skill < b.Skill
		}
		// The human first, then the agents by id.
		if (a.AnsweredBy == ParticipantHuman) != (b.AnsweredBy == ParticipantHuman) {
			return a.AnsweredBy == ParticipantHuman
		}
		return a.AnsweredBy < b.AnsweredBy
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
	storms, err := buildStorms(qs, gs, map[int64]Task{t.ID: t})
	if err != nil {
		return BrainstormStorm{}, err
	}
	if st, ok := storms[taskID]; ok {
		return *st, nil
	}
	return *newStorm(t), nil
}
