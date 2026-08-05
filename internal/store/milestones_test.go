package store

import "testing"

// TestMilestoneActivity pins what counts as a trace of the holding agent:
// its own journal entries (except the take/assign bookkeeping), its docs and
// its thread entries — and nothing written by anybody else.
func TestMilestoneActivity(t *testing.T) {
	s := openTestStore(t)

	milestone, err := s.AddTask(Task{Title: "Improve external agents", Milestone: true})
	if err != nil {
		t.Fatalf("AddTask milestone: %v", err)
	}
	if err := s.SetTaskAssignedRole(milestone, "cto"); err != nil {
		t.Fatalf("SetTaskAssignedRole: %v", err)
	}

	// Bookkeeping and other authors must not count as the agent's work.
	if _, err := s.AddTaskLog(TaskLogEntry{
		TaskID: milestone, Kind: "status", Body: "milestone taken by cto",
		Author: "cto", CreatedAt: 9000,
	}); err != nil {
		t.Fatalf("AddTaskLog status: %v", err)
	}
	if _, err := s.AddTaskLog(TaskLogEntry{
		TaskID: milestone, Kind: "note", Body: "written by somebody else",
		Author: "orch1", CreatedAt: 9500,
	}); err != nil {
		t.Fatalf("AddTaskLog other author: %v", err)
	}

	// Three real traces; the newest of them is the answer.
	if _, err := s.AddTaskLog(TaskLogEntry{
		TaskID: milestone, Kind: "decision", Body: "chose A over B",
		Author: "cto", CreatedAt: 1000,
	}); err != nil {
		t.Fatalf("AddTaskLog decision: %v", err)
	}
	if _, err := s.PutTaskDoc(TaskDoc{
		TaskID: milestone, Kind: "doc", Title: "notes", Body: "…",
		Author: "cto", CreatedAt: 2000,
	}); err != nil {
		t.Fatalf("PutTaskDoc: %v", err)
	}
	qid, err := s.AddQuestion(Question{TaskID: milestone, AskedBy: "cto", Body: "ship?", AskedAt: 1500})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if _, err := s.AddQuestionMessage(QuestionMessage{
		QuestionID: qid, Author: "cto", Body: "still thinking", CreatedAt: 3000,
	}); err != nil {
		t.Fatalf("AddQuestionMessage: %v", err)
	}

	// A regular task with the same kind of activity: never a milestone row.
	plain, err := s.AddTask(Task{Title: "regular", ProjectID: "billing"})
	if err != nil {
		t.Fatalf("AddTask regular: %v", err)
	}
	if _, err := s.AddTaskLog(TaskLogEntry{
		TaskID: plain, Kind: "note", Body: "x", Author: "cto", CreatedAt: 4000,
	}); err != nil {
		t.Fatalf("AddTaskLog regular: %v", err)
	}

	// An untaken milestone has no holder, so no trace can be attributed.
	untaken, err := s.AddTask(Task{Title: "untaken", Milestone: true})
	if err != nil {
		t.Fatalf("AddTask untaken: %v", err)
	}
	if _, err := s.AddTaskLog(TaskLogEntry{
		TaskID: untaken, Kind: "note", Body: "x", Author: "cto", CreatedAt: 5000,
	}); err != nil {
		t.Fatalf("AddTaskLog untaken: %v", err)
	}

	got, err := s.MilestoneActivity()
	if err != nil {
		t.Fatalf("MilestoneActivity: %v", err)
	}
	if got[milestone] != 3000 {
		t.Errorf("activity[%d] = %d, want 3000 (the thread reply)", milestone, got[milestone])
	}
	if _, ok := got[plain]; ok {
		t.Errorf("regular task %d is in the milestone activity map", plain)
	}
	if _, ok := got[untaken]; ok {
		t.Errorf("untaken milestone %d is in the activity map", untaken)
	}
}

// TestMilestoneActivity_NoTraces: a milestone the agent has not touched is
// absent from the map rather than present with a zero — the caller falls back
// to the assignment time itself.
func TestMilestoneActivity_NoTraces(t *testing.T) {
	s := openTestStore(t)

	id, err := s.AddTask(Task{Title: "silent", Milestone: true})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := s.SetTaskAssignedRole(id, "cto"); err != nil {
		t.Fatalf("SetTaskAssignedRole: %v", err)
	}

	got, err := s.MilestoneActivity()
	if err != nil {
		t.Fatalf("MilestoneActivity: %v", err)
	}
	if _, ok := got[id]; ok {
		t.Errorf("milestone without traces is in the map: %v", got)
	}
}
