package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// statsLoc is a fixed non-UTC zone standing in for the daemon's local time,
// so the tests pin that weeks are cut in local time, not in UTC.
var statsLoc = time.FixedZone("UTC+3", 3*3600)

func at(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, statsLoc)
}

func TestISOWeekLabel(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"thursday", at(2026, 10, 1, 12, 0), "2026-W40"},
		{"monday midnight starts the week", at(2026, 9, 28, 0, 0), "2026-W40"},
		{"sunday last minute ends the previous week", at(2026, 9, 27, 23, 59), "2026-W39"},
		{"single digit week is padded", at(2026, 1, 5, 9, 0), "2026-W02"},
		{"dec 31 of a 53-week year", at(2026, 12, 31, 12, 0), "2026-W53"},
		{"jan 1 belongs to the previous ISO year", at(2027, 1, 1, 12, 0), "2026-W53"},
		{"first monday of the next ISO year", at(2027, 1, 4, 0, 0), "2027-W01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ISOWeekLabel(tt.t); got != tt.want {
				t.Errorf("ISOWeekLabel(%v) = %q, want %q", tt.t, got, tt.want)
			}
		})
	}
}

func TestBrainstormWindowStart(t *testing.T) {
	now := at(2026, 10, 1, 12, 0) // Thursday of 2026-W40
	tests := []struct {
		weeks int
		want  time.Time
	}{
		{1, at(2026, 9, 28, 0, 0)},
		{2, at(2026, 9, 21, 0, 0)},
		{12, at(2026, 7, 13, 0, 0)},
	}
	for _, tt := range tests {
		if got := BrainstormWindowStart(now, tt.weeks); !got.Equal(tt.want) {
			t.Errorf("BrainstormWindowStart(%d) = %v, want %v", tt.weeks, got, tt.want)
		}
	}
}

// statsFixture builds storms on a fresh store. Times are set by hand so the
// tests do not depend on the wall clock.
type statsFixture struct {
	t *testing.T
	s *Store
}

func newStatsFixture(t *testing.T) statsFixture {
	t.Helper()
	s := openTestStore(t)
	mustAddTaskSession(t, s, "orch-1")
	return statsFixture{t: t, s: s}
}

func (f statsFixture) task(title, skill string) int64 {
	f.t.Helper()
	id, err := f.s.AddTask(Task{Title: title, ProjectID: "billing", SessionID: "orch-1", Status: "brainstorm"})
	if err != nil {
		f.t.Fatalf("AddTask: %v", err)
	}
	if skill != "" {
		if err := f.s.SetTaskBrainstormSkill(id, skill); err != nil {
			f.t.Fatalf("SetTaskBrainstormSkill: %v", err)
		}
	}
	return id
}

// ask opens a brainstorm thread recommending option 2 of three.
func (f statsFixture) ask(taskID int64, askedAt time.Time) int64 {
	f.t.Helper()
	id, err := f.s.AddQuestion(Question{
		TaskID: taskID, AskedBy: "orch-1", Body: "Q?", AskedAt: askedAt.Unix(),
		Type: QuestionTypeBrainstorm, Options: []string{"A", "B", "C"}, RecommendedOption: 2,
	})
	if err != nil {
		f.t.Fatalf("AddQuestion: %v", err)
	}
	return id
}

// answer resolves qid as author would: chosen option (0 = own text), comment,
// the answer entry, and resolved_at moved to when.
func (f statsFixture) answer(qid int64, chosen int, comment, author string, when time.Time) {
	f.t.Helper()
	if _, err := f.s.ResolveBrainstormQuestion(qid, BrainstormAnswer{
		ChosenOption: chosen, Comment: comment, Source: AnswerSourceUI,
	}); err != nil {
		f.t.Fatalf("ResolveBrainstormQuestion: %v", err)
	}
	if _, err := f.s.AddQuestionMessage(QuestionMessage{
		QuestionID: qid, Author: author, Kind: "answer", Body: "answer",
	}); err != nil {
		f.t.Fatalf("AddQuestionMessage: %v", err)
	}
	if _, err := f.s.db.Exec(`UPDATE questions SET resolved_at = ? WHERE id = ?`, when.Unix(), qid); err != nil {
		f.t.Fatalf("set resolved_at: %v", err)
	}
}

// gate inserts a gate of taskID with the given status and times.
func (f statsFixture) gate(taskID int64, status string, requested time.Time, decided *time.Time) {
	f.t.Helper()
	var dec any
	if decided != nil {
		dec = decided.Unix()
	}
	if _, err := f.s.db.Exec(`INSERT INTO task_gates (task_id, spec_version, status, requested_at, decided_at)
		VALUES (?, 1, ?, ?, ?)`, taskID, status, requested.Unix(), dec); err != nil {
		f.t.Fatalf("insert gate: %v", err)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// wk is the weekly row of one answerer.
func wk(week, skill, answeredBy string, c BrainstormAnswers) BrainstormWeek {
	return BrainstormWeek{Week: week, Skill: skill, AnsweredBy: answeredBy, BrainstormAnswers: c}
}

func TestBrainstormStats(t *testing.T) {
	f := newStatsFixture(t)
	now := at(2026, 10, 1, 12, 0) // Thursday, 2026-W40
	w40 := at(2026, 9, 29, 10, 0) // Tuesday, 2026-W40
	w39 := at(2026, 9, 27, 23, 59)
	w40start := at(2026, 9, 28, 0, 0)

	// Storm A: the custom skill, every kind of answer.
	a := f.task("Storm A", "orchestrator-brainstorming")
	f.answer(f.ask(a, w39), 2, "да, но с кэшем", "human", w39) // accepted with comment, W39
	f.answer(f.ask(a, w40), 2, "", "human", w40)               // accepted, W40
	f.answer(f.ask(a, w40), 3, "лучше C", "human", w40start)   // corrected, W40 (boundary)
	f.answer(f.ask(a, w40), 0, "ни то ни другое", "", w40)     // wrong turn, legacy "" author = human
	f.answer(f.ask(a, w40), 2, "", "cto", w40)                 // persistent agent: counts under its own id
	overridden := f.ask(a, w40)
	f.answer(overridden, 2, "", "human", w40) // accepted, overridden to corrected
	if err := f.s.SetQuestionOutcome(overridden, OutcomeCorrected); err != nil {
		t.Fatalf("SetQuestionOutcome: %v", err)
	}
	reopened := f.ask(a, w40)
	f.answer(reopened, 2, "", "human", w40)
	if err := f.s.ReopenQuestion(reopened); err != nil {
		t.Fatalf("ReopenQuestion: %v", err)
	}
	f.ask(a, w40) // still open
	dismissed := f.ask(a, w40)
	if err := f.s.ResolveQuestion(dismissed, "dismissed"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	if _, err := f.s.db.Exec(`UPDATE questions SET resolved_at = ? WHERE id = ?`, w40.Unix(), dismissed); err != nil {
		t.Fatalf("set resolved_at: %v", err)
	}
	f.gate(a, "changes", w39, ptrTime(w39))
	f.gate(a, "go", w40, ptrTime(at(2026, 9, 30, 15, 0)))

	// Storm B: started before brainstorm_skill existed.
	b := f.task("Storm B", "")
	f.answer(f.ask(b, w40), 2, "", "human", at(2026, 9, 30, 9, 0))

	// Storm C: gates only, latest activity of all.
	c := f.task("Storm C", "superpowers:brainstorming")
	f.gate(c, "changes", w40, ptrTime(at(2026, 10, 1, 8, 0)))
	f.gate(c, "pending", at(2026, 10, 1, 9, 0), nil)

	// Storm D: everything happened before a one-week window.
	d := f.task("Storm D", "superpowers:brainstorming")
	f.answer(f.ask(d, at(2026, 9, 1, 10, 0)), 1, "", "human", at(2026, 9, 1, 11, 0))

	// A plain decision thread is no storm at all.
	plain := f.task("Plain", "superpowers:brainstorming")
	if _, err := f.s.AddQuestion(Question{TaskID: plain, AskedBy: "orch-1", Body: "decide?"}); err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}

	goAt := at(2026, 9, 30, 15, 0).Unix()
	stormA := BrainstormStorm{TaskID: a, Title: "Storm A", ProjectID: "billing", Skill: "orchestrator-brainstorming",
		// The overridden answer counts as corrected, not accepted.
		BrainstormCounts: BrainstormCounts{Questions: 9, SpecChanges: 1, BrainstormAnswers: BrainstormAnswers{
			Answered: 6, Accepted: 3, AcceptedWithComment: 1, Corrected: 2, WrongTurn: 1}},
		// The legacy "" author is the human, never a participant of its own.
		AnsweredBy: []string{"human", "cto"},
		ByAnswerer: []BrainstormAnswererCounts{
			{AnsweredBy: "human", BrainstormAnswers: BrainstormAnswers{Answered: 5, Accepted: 2, AcceptedWithComment: 1, Corrected: 2, WrongTurn: 1}},
			{AnsweredBy: "cto", BrainstormAnswers: BrainstormAnswers{Answered: 1, Accepted: 1}},
		},
		HasGate: true, GoAt: &goAt, LastActivity: at(2026, 9, 30, 15, 0).Unix()}
	stormB := BrainstormStorm{TaskID: b, Title: "Storm B", ProjectID: "billing", Skill: SkillUnknown,
		BrainstormCounts: BrainstormCounts{Questions: 1, BrainstormAnswers: BrainstormAnswers{Answered: 1, Accepted: 1}},
		AnsweredBy:       []string{"human"},
		ByAnswerer:       []BrainstormAnswererCounts{{AnsweredBy: "human", BrainstormAnswers: BrainstormAnswers{Answered: 1, Accepted: 1}}},
		LastActivity:     at(2026, 9, 30, 9, 0).Unix()}
	stormC := BrainstormStorm{TaskID: c, Title: "Storm C", ProjectID: "billing", Skill: "superpowers:brainstorming",
		BrainstormCounts: BrainstormCounts{SpecChanges: 1},
		AnsweredBy:       []string{}, ByAnswerer: []BrainstormAnswererCounts{},
		HasGate: true, LastActivity: at(2026, 10, 1, 9, 0).Unix()}
	stormD := BrainstormStorm{TaskID: d, Title: "Storm D", ProjectID: "billing", Skill: "superpowers:brainstorming",
		BrainstormCounts: BrainstormCounts{Questions: 1, BrainstormAnswers: BrainstormAnswers{Answered: 1, Corrected: 1}},
		AnsweredBy:       []string{"human"},
		ByAnswerer:       []BrainstormAnswererCounts{{AnsweredBy: "human", BrainstormAnswers: BrainstormAnswers{Answered: 1, Corrected: 1}}},
		LastActivity:     at(2026, 9, 1, 11, 0).Unix()}

	tests := []struct {
		name   string
		weeks  int
		want   []BrainstormWeek
		storms []BrainstormStorm
	}{
		{
			name:  "one week",
			weeks: 1,
			want: []BrainstormWeek{
				wk("2026-W40", "orchestrator-brainstorming", "human", BrainstormAnswers{Answered: 4, Accepted: 1, Corrected: 2, WrongTurn: 1}),
				wk("2026-W40", "orchestrator-brainstorming", "cto", BrainstormAnswers{Answered: 1, Accepted: 1}),
				wk("2026-W40", SkillUnknown, "human", BrainstormAnswers{Answered: 1, Accepted: 1}),
			},
			storms: []BrainstormStorm{stormC, stormA, stormB},
		},
		{
			name:  "twelve weeks",
			weeks: 12,
			want: []BrainstormWeek{
				wk("2026-W36", "superpowers:brainstorming", "human", BrainstormAnswers{Answered: 1, Corrected: 1}),
				wk("2026-W39", "orchestrator-brainstorming", "human", BrainstormAnswers{Answered: 1, Accepted: 1, AcceptedWithComment: 1}),
				wk("2026-W40", "orchestrator-brainstorming", "human", BrainstormAnswers{Answered: 4, Accepted: 1, Corrected: 2, WrongTurn: 1}),
				wk("2026-W40", "orchestrator-brainstorming", "cto", BrainstormAnswers{Answered: 1, Accepted: 1}),
				wk("2026-W40", SkillUnknown, "human", BrainstormAnswers{Answered: 1, Accepted: 1}),
			},
			storms: []BrainstormStorm{stormC, stormA, stormB, stormD},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.s.BrainstormStats(now, tt.weeks)
			if err != nil {
				t.Fatalf("BrainstormStats: %v", err)
			}
			if !reflect.DeepEqual(got.Weeks, tt.want) {
				t.Errorf("weeks:\n got %+v\nwant %+v", got.Weeks, tt.want)
			}
			if !reflect.DeepEqual(got.Storms, tt.storms) {
				t.Errorf("storms:\n got %+v\nwant %+v", got.Storms, tt.storms)
			}
		})
	}
}

func TestBrainstormStatsEmpty(t *testing.T) {
	f := newStatsFixture(t)
	got, err := f.s.BrainstormStats(at(2026, 10, 1, 12, 0), 12)
	if err != nil {
		t.Fatalf("BrainstormStats: %v", err)
	}
	if got.Weeks == nil || got.Storms == nil || len(got.Weeks) != 0 || len(got.Storms) != 0 {
		t.Errorf("empty store: got %+v, want empty non-nil lists", got)
	}
}

func TestTaskBrainstormStats(t *testing.T) {
	f := newStatsFixture(t)
	w40 := at(2026, 9, 29, 10, 0)

	storm := f.task("Storm", "orchestrator-brainstorming")
	f.answer(f.ask(storm, w40), 2, "ok", "human", w40)
	f.answer(f.ask(storm, w40), 2, "", "cto", w40)
	f.answer(f.ask(storm, w40), 2, "  \n\t", "human", w40) // whitespace is no comment
	// Go gates inserted out of order: the storm exited at the first Go.
	f.gate(storm, "go", w40, ptrTime(w40))
	f.gate(storm, "go", w40, ptrTime(w40.Add(time.Hour)))
	other := f.task("Other", "superpowers:brainstorming")
	f.answer(f.ask(other, w40), 1, "", "human", w40)

	got, err := f.s.TaskBrainstormStats(storm)
	if err != nil {
		t.Fatalf("TaskBrainstormStats: %v", err)
	}
	goAt := w40.Unix()
	want := BrainstormStorm{TaskID: storm, Title: "Storm", ProjectID: "billing", Skill: "orchestrator-brainstorming",
		BrainstormCounts: BrainstormCounts{Questions: 3, BrainstormAnswers: BrainstormAnswers{Answered: 3, Accepted: 3, AcceptedWithComment: 1}},
		// Same resolved_at: first-answer order falls back to the question id.
		AnsweredBy: []string{"human", "cto"},
		ByAnswerer: []BrainstormAnswererCounts{
			{AnsweredBy: "human", BrainstormAnswers: BrainstormAnswers{Answered: 2, Accepted: 2, AcceptedWithComment: 1}},
			{AnsweredBy: "cto", BrainstormAnswers: BrainstormAnswers{Answered: 1, Accepted: 1}},
		},
		FirstTryGo: true, HasGate: true, GoAt: &goAt, LastActivity: w40.Add(time.Hour).Unix()}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("storm:\n got %+v\nwant %+v", got, want)
	}
	if got.GoAt == nil || *got.GoAt != goAt {
		t.Errorf("go_at = %v, want the first Go %d", got.GoAt, goAt)
	}

	quiet := f.task("Quiet", "")
	got, err = f.s.TaskBrainstormStats(quiet)
	if err != nil {
		t.Fatalf("TaskBrainstormStats quiet: %v", err)
	}
	if want := (BrainstormStorm{TaskID: quiet, Title: "Quiet", ProjectID: "billing", Skill: SkillUnknown,
		AnsweredBy: []string{}, ByAnswerer: []BrainstormAnswererCounts{}}); !reflect.DeepEqual(got, want) {
		t.Errorf("quiet task:\n got %+v\nwant %+v", got, want)
	}

	if _, err := f.s.TaskBrainstormStats(99999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown task: err = %v, want ErrNotFound", err)
	}
}

// storm is TaskBrainstormStats of taskID or a fatal error.
func (f statsFixture) storm(taskID int64) BrainstormStorm {
	f.t.Helper()
	st, err := f.s.TaskBrainstormStats(taskID)
	if err != nil {
		f.t.Fatalf("TaskBrainstormStats: %v", err)
	}
	return st
}

func TestStormAgentAnswersCount(t *testing.T) {
	f := newStatsFixture(t)
	now := at(2026, 10, 1, 12, 0)
	w40 := at(2026, 9, 29, 10, 0)
	task := f.task("Agent storm", "orchestrator-brainstorming")
	for i := 0; i < 3; i++ {
		f.answer(f.ask(task, w40), 2, "", "cto", w40)
	}

	st := f.storm(task)
	if st.Answered != 3 || st.Accepted != 3 {
		t.Errorf("storm counters = %+v, want 3 answered, 3 accepted", st.BrainstormAnswers)
	}
	if want := []string{"cto"}; !reflect.DeepEqual(st.AnsweredBy, want) {
		t.Errorf("AnsweredBy = %v, want %v", st.AnsweredBy, want)
	}
	wantBy := []BrainstormAnswererCounts{{AnsweredBy: "cto", BrainstormAnswers: BrainstormAnswers{Answered: 3, Accepted: 3}}}
	if !reflect.DeepEqual(st.ByAnswerer, wantBy) {
		t.Errorf("ByAnswerer = %+v, want %+v", st.ByAnswerer, wantBy)
	}

	got, err := f.s.BrainstormStats(now, 1)
	if err != nil {
		t.Fatalf("BrainstormStats: %v", err)
	}
	wantWeeks := []BrainstormWeek{wk("2026-W40", "orchestrator-brainstorming", "cto", BrainstormAnswers{Answered: 3, Accepted: 3})}
	if !reflect.DeepEqual(got.Weeks, wantWeeks) {
		t.Errorf("weeks:\n got %+v\nwant %+v", got.Weeks, wantWeeks)
	}
}

func TestStormMixedAnswerers(t *testing.T) {
	f := newStatsFixture(t)
	now := at(2026, 10, 1, 12, 0)
	t1, t2, t3 := at(2026, 9, 29, 10, 0), at(2026, 9, 29, 11, 0), at(2026, 9, 29, 12, 0)
	task := f.task("Mixed", "orchestrator-brainstorming")
	// Asked in reverse so the question ids disagree with the answer order:
	// first-answer order must follow resolved_at, not the id.
	q3, q2, q1 := f.ask(task, t1), f.ask(task, t1), f.ask(task, t1)
	f.answer(q1, 2, "", "human", t1)        // accepted
	f.answer(q2, 3, "", "cto", t2)          // corrected
	f.answer(q3, 0, "не туда", "human", t3) // wrong turn

	st := f.storm(task)
	if want := []string{"human", "cto"}; !reflect.DeepEqual(st.AnsweredBy, want) {
		t.Errorf("AnsweredBy = %v, want %v", st.AnsweredBy, want)
	}
	wantBy := []BrainstormAnswererCounts{
		{AnsweredBy: "human", BrainstormAnswers: BrainstormAnswers{Answered: 2, Accepted: 1, WrongTurn: 1}},
		{AnsweredBy: "cto", BrainstormAnswers: BrainstormAnswers{Answered: 1, Corrected: 1}},
	}
	if !reflect.DeepEqual(st.ByAnswerer, wantBy) {
		t.Errorf("ByAnswerer = %+v, want %+v", st.ByAnswerer, wantBy)
	}
	if want := (BrainstormAnswers{Answered: 3, Accepted: 1, Corrected: 1, WrongTurn: 1}); st.BrainstormAnswers != want {
		t.Errorf("storm totals = %+v, want %+v", st.BrainstormAnswers, want)
	}

	got, err := f.s.BrainstormStats(now, 1)
	if err != nil {
		t.Fatalf("BrainstormStats: %v", err)
	}
	wantWeeks := []BrainstormWeek{
		wk("2026-W40", "orchestrator-brainstorming", "human", BrainstormAnswers{Answered: 2, Accepted: 1, WrongTurn: 1}),
		wk("2026-W40", "orchestrator-brainstorming", "cto", BrainstormAnswers{Answered: 1, Corrected: 1}),
	}
	if !reflect.DeepEqual(got.Weeks, wantWeeks) {
		t.Errorf("weeks:\n got %+v\nwant %+v", got.Weeks, wantWeeks)
	}
}

// TestStormWeeksOrderHumanFirst pins the weekly sort inside one week and
// skill: the human first, then the agents by id.
func TestStormWeeksOrderHumanFirst(t *testing.T) {
	f := newStatsFixture(t)
	w40 := at(2026, 9, 29, 10, 0)
	task := f.task("Order", "orchestrator-brainstorming")
	f.answer(f.ask(task, w40), 2, "", "cto", w40)
	f.answer(f.ask(task, w40), 2, "", "architect", w40)
	f.answer(f.ask(task, w40), 2, "", "human", w40)

	got, err := f.s.BrainstormStats(at(2026, 10, 1, 12, 0), 1)
	if err != nil {
		t.Fatalf("BrainstormStats: %v", err)
	}
	var order []string
	for _, w := range got.Weeks {
		order = append(order, w.AnsweredBy)
	}
	if want := []string{"human", "architect", "cto"}; !reflect.DeepEqual(order, want) {
		t.Errorf("weekly answerers = %v, want %v", order, want)
	}
}

func TestStormSpecChangesBeforeGo(t *testing.T) {
	base := at(2026, 9, 29, 10, 0)
	tm := func(min int) time.Time { return base.Add(time.Duration(min) * time.Minute) }

	t.Run("changes after go do not count", func(t *testing.T) {
		f := newStatsFixture(t)
		task := f.task("Gate", "orchestrator-brainstorming")
		f.gate(task, "changes", tm(10), ptrTime(tm(10)))
		// The later change is inserted before the Go: the first Go is only
		// known once all gates are folded.
		f.gate(task, "changes", tm(30), ptrTime(tm(30)))
		f.gate(task, "go", tm(20), ptrTime(tm(20)))
		st := f.storm(task)
		if st.SpecChanges != 1 || st.GoAt == nil || *st.GoAt != tm(20).Unix() || st.FirstTryGo || !st.HasGate {
			t.Errorf("got SpecChanges=%d GoAt=%v FirstTryGo=%v HasGate=%v, want 1, %d, false, true",
				st.SpecChanges, st.GoAt, st.FirstTryGo, st.HasGate, tm(20).Unix())
		}
	})
	t.Run("go on the first try stays so after later changes", func(t *testing.T) {
		f := newStatsFixture(t)
		task := f.task("Gate", "orchestrator-brainstorming")
		f.gate(task, "go", tm(20), ptrTime(tm(20)))
		f.gate(task, "changes", tm(30), ptrTime(tm(30)))
		st := f.storm(task)
		if st.SpecChanges != 0 || !st.FirstTryGo || !st.HasGate {
			t.Errorf("got SpecChanges=%d FirstTryGo=%v HasGate=%v, want 0, true, true", st.SpecChanges, st.FirstTryGo, st.HasGate)
		}
	})
}

func TestStormSupersededIsNotAChange(t *testing.T) {
	f := newStatsFixture(t)
	base := at(2026, 9, 29, 10, 0)
	task := f.task("Superseded", "orchestrator-brainstorming")
	f.gate(task, "superseded", base, ptrTime(base.Add(10*time.Minute)))
	f.gate(task, "go", base.Add(15*time.Minute), ptrTime(base.Add(20*time.Minute)))
	st := f.storm(task)
	if st.SpecChanges != 0 || !st.FirstTryGo || !st.HasGate {
		t.Errorf("got SpecChanges=%d FirstTryGo=%v HasGate=%v, want 0, true, true", st.SpecChanges, st.FirstTryGo, st.HasGate)
	}
}

func TestStormNoGoCountsAllChanges(t *testing.T) {
	f := newStatsFixture(t)
	base := at(2026, 9, 29, 10, 0)
	task := f.task("No go", "orchestrator-brainstorming")
	f.gate(task, "changes", base, ptrTime(base.Add(10*time.Minute)))
	f.gate(task, "changes", base, ptrTime(base.Add(20*time.Minute)))
	f.gate(task, "pending", base.Add(30*time.Minute), nil)
	st := f.storm(task)
	if st.SpecChanges != 2 || st.GoAt != nil || st.FirstTryGo || !st.HasGate {
		t.Errorf("got SpecChanges=%d GoAt=%v FirstTryGo=%v HasGate=%v, want 2, nil, false, true",
			st.SpecChanges, st.GoAt, st.FirstTryGo, st.HasGate)
	}
}

func TestStormQuestionsWithoutAnswers(t *testing.T) {
	f := newStatsFixture(t)
	task := f.task("Open", "orchestrator-brainstorming")
	f.ask(task, at(2026, 9, 29, 10, 0))
	st := f.storm(task)
	if st.AnsweredBy == nil || st.ByAnswerer == nil || len(st.AnsweredBy) != 0 || len(st.ByAnswerer) != 0 {
		t.Errorf("AnsweredBy=%#v ByAnswerer=%#v, want non-nil empty slices", st.AnsweredBy, st.ByAnswerer)
	}
	if st.HasGate || st.FirstTryGo {
		t.Errorf("HasGate=%v FirstTryGo=%v, want false, false", st.HasGate, st.FirstTryGo)
	}
}
