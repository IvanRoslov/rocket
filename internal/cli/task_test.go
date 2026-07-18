package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestTaskAddUsage tests usage violations for task add.
func TestTaskAddUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"too many args", []string{"title", "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskAddCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestNeedsProjectDefault verifies that `task add --parent` skips default
// project resolution (leaving the project empty for the API to inherit from
// the parent task), while a bare `task add` still resolves a default.
func TestNeedsProjectDefault(t *testing.T) {
	tests := []struct {
		name      string
		projectID string
		parentID  int64
		want      bool
	}{
		{"no project, no parent", "", 0, true},
		{"no project, with parent", "", 5, false},
		{"project given, no parent", "proj1", 0, false},
		{"project given, with parent", "proj1", 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsProjectDefault(tt.projectID, tt.parentID); got != tt.want {
				t.Errorf("needsProjectDefault(%q, %d) = %v, want %v", tt.projectID, tt.parentID, got, tt.want)
			}
		})
	}
}

// TestTaskAddMutuallyExclusive tests that --desc and --desc-file are mutually exclusive.
func TestTaskAddDescMutuallyExclusive(t *testing.T) {
	cmd := newTaskAddCmd()
	cmd.SetArgs([]string{"title", "--desc", "text", "--desc-file", "file.md"})
	err := cmd.Execute()
	if err == nil {
		t.Errorf("expected error for mutually exclusive flags")
	}
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Errorf("expected usageError, got %T: %v", err, err)
	}
}

// TestTaskStartUsage tests usage violations for task start.
func TestTaskStartUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"too many args", []string{"1", "extra"}},
		{"non-numeric id", []string{"abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskStartCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestTaskLsUsage tests usage violations for task ls.
func TestTaskLsUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"unexpected arg", []string{"arg"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskLsCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestTaskShowUsage tests usage violations for task show.
func TestTaskShowUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"too many args", []string{"1", "2"}},
		{"invalid id", []string{"not-a-number"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskShowCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestTaskMoveUsage tests usage violations for task move.
func TestTaskMoveUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"one arg", []string{"1"}},
		{"invalid id", []string{"not-a-number", "in_progress"}},
		{"cancelled status", []string{"1", "cancelled"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskMoveCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestTaskCancelUsage tests usage violations for task cancel.
func TestTaskCancelUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"too many args", []string{"1", "2"}},
		{"invalid id", []string{"not-a-number"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskCancelCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestTaskDocPutUsage tests usage violations for task doc put.
func TestTaskDocPutUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"invalid id", []string{"not-a-number", "--kind", "spec", "--title", "t", "--file", "f.md"}},
		{"missing kind", []string{"1", "--title", "t", "--file", "f.md"}},
		{"missing title", []string{"1", "--kind", "spec", "--file", "f.md"}},
		{"missing file", []string{"1", "--kind", "spec", "--title", "t"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskDocPutCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestTaskLogUsage tests usage violations for task log.
func TestTaskLogUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"one arg", []string{"1"}},
		{"invalid id", []string{"not-a-number", "text"}},
		{"missing kind", []string{"1", "text"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskLogCmd()
			cmd.SetArgs(tt.args)
			// Don't set kind flag
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestTaskAskUsage tests usage violations for task ask.
func TestTaskAskUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"one arg", []string{"1"}},
		{"too many args", []string{"1", "q", "extra"}},
		{"invalid id", []string{"not-a-number", "q"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskAskCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestTaskQuestionsUsage tests usage violations for task questions.
func TestTaskQuestionsUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"too many args", []string{"1", "2"}},
		{"invalid id", []string{"not-a-number"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskQuestionsCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestTaskReplyUsage tests usage violations for task reply.
func TestTaskReplyUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"one arg", []string{"1"}},
		{"too many args", []string{"1", "text", "extra"}},
		{"invalid id", []string{"not-a-number", "text"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskReplyCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestTaskAnswerUsage tests usage violations for task answer, including the
// XOR between a body argument and --dismiss.
func TestTaskAnswerUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"too many args", []string{"1", "a", "b"}},
		{"invalid id", []string{"not-a-number", "answer"}},
		{"neither body nor dismiss", []string{"1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskAnswerCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}

	t.Run("both body and dismiss", func(t *testing.T) {
		cmd := newTaskAnswerCmd()
		cmd.SetArgs([]string{"1", "answer", "--dismiss"})
		err := cmd.Execute()
		if err == nil {
			t.Errorf("expected error when both body and --dismiss given")
		}
		var usageErr *usageError
		if !errors.As(err, &usageErr) {
			t.Errorf("expected usageError, got %T: %v", err, err)
		}
	})
}

// TestRenderQuestionsEmpty tests that an empty question list renders nothing.
func TestRenderQuestionsEmpty(t *testing.T) {
	out := renderQuestions(1, nil)
	if out != "" {
		t.Errorf("expected empty output for no questions, got %q", out)
	}
}

// TestRenderQuestionsWhoseTurnUser tests the "waits for user" arrow.
func TestRenderQuestionsWhoseTurnUser(t *testing.T) {
	qs := []questionRow{
		{ID: 5, Ordinal: 1, Status: "open", WhoseTurn: "user", Body: "Which approach?"},
	}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "task #42") {
		t.Errorf("expected task header, got: %q", out)
	}
	if !strings.Contains(out, "Q1 (#5) [open]") {
		t.Errorf("expected question header, got: %q", out)
	}
	if !strings.Contains(out, "ждёт ответа пользователя") {
		t.Errorf("expected user-turn arrow, got: %q", out)
	}
	if !strings.Contains(out, "Which approach?") {
		t.Errorf("expected body, got: %q", out)
	}
}

// TestRenderQuestionsWhoseTurnOrchestrator tests the "waits for orchestrator" arrow.
func TestRenderQuestionsWhoseTurnOrchestrator(t *testing.T) {
	qs := []questionRow{
		{ID: 6, Ordinal: 2, Status: "open", WhoseTurn: "orchestrator", Body: "Question body"},
	}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "ждёт оркестратора") {
		t.Errorf("expected orchestrator-turn arrow, got: %q", out)
	}
}

// TestRenderQuestionsResolvedNoArrow tests that a resolved question shows no arrow.
func TestRenderQuestionsResolvedNoArrow(t *testing.T) {
	qs := []questionRow{
		{ID: 7, Ordinal: 3, Status: "resolved", WhoseTurn: "", Body: "Done question"},
	}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "Q3 (#7) [resolved]") {
		t.Errorf("expected resolved header, got: %q", out)
	}
	if strings.Contains(out, "ждёт") {
		t.Errorf("expected no turn arrow for resolved question, got: %q", out)
	}
}

// TestRenderQuestionsWithContext tests that context renders when present.
func TestRenderQuestionsWithContext(t *testing.T) {
	qs := []questionRow{
		{ID: 8, Ordinal: 1, Status: "open", WhoseTurn: "user", Body: "Body", Context: "extra context here"},
	}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "extra context here") {
		t.Errorf("expected context in output, got: %q", out)
	}
}

// TestRenderQuestionsThreadLines tests that thread messages render with
// author prefixes: "[user]" for a human entry, "[<session>]" for an agent.
func TestRenderQuestionsThreadLines(t *testing.T) {
	qs := []questionRow{
		{
			ID: 9, Ordinal: 1, Status: "open", WhoseTurn: "orchestrator", Body: "Q body",
			Messages: []questionMessageRow{
				{ID: 1, Author: "", Kind: "reply", Body: "human reply text"},
				{ID: 2, Author: "demo-orch-abc", Kind: "reply", Body: "orch reply text"},
			},
		},
	}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "[user] human reply text") {
		t.Errorf("expected user thread line, got: %q", out)
	}
	if !strings.Contains(out, "[demo-orch-abc] orch reply text") {
		t.Errorf("expected session thread line, got: %q", out)
	}
}

// TestRenderTaskBoardEmpty tests rendering an empty board.
func TestRenderTaskBoardEmpty(t *testing.T) {
	board := map[string][]taskRow{}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, false)

	output := w.String()
	if len(strings.TrimSpace(output)) > 0 {
		t.Errorf("expected empty output for empty board, got: %q", output)
	}
}

// TestRenderTaskBoardSingleStatus tests rendering a board with one status.
func TestRenderTaskBoardSingleStatus(t *testing.T) {
	board := map[string][]taskRow{
		"backlog": []taskRow{
			{ID: 1, Title: "Task 1", Status: "backlog"},
			{ID: 2, Title: "Task 2", Status: "backlog"},
		},
	}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, false)

	output := w.String()
	if !strings.Contains(output, "BACKLOG") {
		t.Errorf("expected BACKLOG header in output")
	}
	if !strings.Contains(output, "#1 Task 1") {
		t.Errorf("expected '#1 Task 1' in output")
	}
	if !strings.Contains(output, "#2 Task 2") {
		t.Errorf("expected '#2 Task 2' in output")
	}
}

// TestRenderTaskBoardMultipleStatuses tests rendering a board with multiple statuses.
func TestRenderTaskBoardMultipleStatuses(t *testing.T) {
	board := map[string][]taskRow{
		"backlog": []taskRow{
			{ID: 1, Title: "Task 1", Status: "backlog"},
		},
		"in_progress": []taskRow{
			{ID: 2, Title: "Task 2", Status: "in_progress"},
		},
		"review": []taskRow{
			{ID: 3, Title: "Task 3", Status: "review"},
		},
		"done": []taskRow{
			{ID: 4, Title: "Task 4", Status: "done"},
		},
		"cancelled": []taskRow{
			{ID: 5, Title: "Task 5", Status: "cancelled"},
		},
	}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, false)

	output := w.String()
	headers := []string{"BACKLOG", "IN PROGRESS", "REVIEW", "DONE", "CANCELLED"}
	for _, h := range headers {
		if !strings.Contains(output, h) {
			t.Errorf("expected %q header in output", h)
		}
	}
}

// TestRenderTaskBoardSkipsEmpty tests that empty statuses are skipped.
func TestRenderTaskBoardSkipsEmpty(t *testing.T) {
	board := map[string][]taskRow{
		"backlog": []taskRow{
			{ID: 1, Title: "Task 1", Status: "backlog"},
		},
		"in_progress": []taskRow{},
		"done":        []taskRow{},
	}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, false)

	output := w.String()
	if !strings.Contains(output, "BACKLOG") {
		t.Errorf("expected BACKLOG header in output")
	}
	if strings.Contains(output, "IN PROGRESS") {
		t.Errorf("should not show IN PROGRESS header for empty status")
	}
	if strings.Contains(output, "DONE") {
		t.Errorf("should not show DONE header for empty status")
	}
}

// TestRenderTaskBoardWithFeatureAndSession tests rendering tasks with feature slug and session.
func TestRenderTaskBoardWithFeatureAndSession(t *testing.T) {
	board := map[string][]taskRow{
		"backlog": []taskRow{
			{ID: 1, Title: "Task 1", Status: "backlog", FeatureSlug: "my-feature", SessionID: "sess-123"},
		},
	}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, false)

	output := w.String()
	if !strings.Contains(output, "[my-feature]") {
		t.Errorf("expected feature slug in output")
	}
	if !strings.Contains(output, "[sess-123]") {
		t.Errorf("expected session id in output")
	}
}

// TestRenderTaskCardBasic tests rendering a basic task card.
func TestRenderTaskCardBasic(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:        1,
		Title:     "My Task",
		Status:    "in_progress",
		ProjectID: "proj-1",
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, w, now)

	output := w.String()
	if !strings.Contains(output, "#1 My Task (in_progress)") {
		t.Errorf("expected header in output")
	}
	if !strings.Contains(output, "proj-1") {
		t.Errorf("expected project id in output")
	}
}

// TestRenderTaskCardWithDescription tests rendering a card with description.
func TestRenderTaskCardWithDescription(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:          1,
		Title:       "My Task",
		Status:      "backlog",
		ProjectID:   "proj-1",
		Description: "This is a description",
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, w, now)

	output := w.String()
	if !strings.Contains(output, "Description") {
		t.Errorf("expected Description section in output")
	}
	if !strings.Contains(output, "This is a description") {
		t.Errorf("expected description text in output")
	}
}

// TestRenderTaskCardWithSubtasks tests rendering a card with subtasks.
func TestRenderTaskCardWithSubtasks(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:        1,
		Title:     "My Task",
		Status:    "in_progress",
		ProjectID: "proj-1",
		Subtasks: []taskRow{
			{ID: 10, Title: "Subtask 1", Status: "backlog"},
			{ID: 11, Title: "Subtask 2", Status: "in_progress"},
		},
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, w, now)

	output := w.String()
	if !strings.Contains(output, "Subtasks") {
		t.Errorf("expected Subtasks section in output")
	}
	if !strings.Contains(output, "#10") || !strings.Contains(output, "Subtask 1") {
		t.Errorf("expected subtask 1 in output")
	}
	if !strings.Contains(output, "#11") || !strings.Contains(output, "Subtask 2") {
		t.Errorf("expected subtask 2 in output")
	}
}

// TestRenderTaskCardWithDocs tests rendering a card with docs.
func TestRenderTaskCardWithDocs(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:        1,
		Title:     "My Task",
		Status:    "in_progress",
		ProjectID: "proj-1",
	}
	docs := []taskDocRow{
		{ID: 1, Kind: "spec", Title: "Spec Doc", Version: 1},
		{ID: 2, Kind: "plan", Title: "Plan Doc", Version: 2},
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, docs, []taskLogRow{}, w, now)

	output := w.String()
	if !strings.Contains(output, "Docs") {
		t.Errorf("expected Docs section in output")
	}
	if !strings.Contains(output, "spec \"Spec Doc\" v1") {
		t.Errorf("expected spec doc in output")
	}
	if !strings.Contains(output, "plan \"Plan Doc\" v2") {
		t.Errorf("expected plan doc in output")
	}
}

// TestRenderTaskCardWithLog tests rendering a card with log entries.
func TestRenderTaskCardWithLog(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:        1,
		Title:     "My Task",
		Status:    "in_progress",
		ProjectID: "proj-1",
	}
	logs := []taskLogRow{
		{ID: 1, Kind: "status", Body: "started", CreatedAt: now.Unix() - 60},
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, logs, w, now)

	output := w.String()
	if !strings.Contains(output, "Log") {
		t.Errorf("expected Log section in output")
	}
	if !strings.Contains(output, "[status]") || !strings.Contains(output, "started") {
		t.Errorf("expected log entry in output")
	}
}

// TestRenderTaskCardEmptyDocsAndLogs tests rendering a card with no docs or logs.
func TestRenderTaskCardEmptyDocsAndLogs(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:        1,
		Title:     "My Task",
		Status:    "backlog",
		ProjectID: "proj-1",
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, w, now)

	output := w.String()
	if strings.Contains(output, "Docs") {
		t.Errorf("should not show Docs section for empty docs")
	}
	if strings.Contains(output, "Log") {
		t.Errorf("should not show Log section for empty log")
	}
}

// TestRenderTaskCardWithSession tests rendering a card with session info.
func TestRenderTaskCardWithSession(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:        1,
		Title:     "My Task",
		Status:    "in_progress",
		ProjectID: "proj-1",
		Session: &taskSessionDetail{
			ID:       "sess-123",
			TmuxName: "tmux-sess",
			Attach:   []string{"tmux", "attach", "-t", "tmux-sess"},
		},
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, w, now)

	output := w.String()
	if !strings.Contains(output, "sess-123") {
		t.Errorf("expected session id in output")
	}
	if !strings.Contains(output, "tmux-sess") {
		t.Errorf("expected tmux name in output")
	}
	if !strings.Contains(output, "Attach") {
		t.Errorf("expected Attach hint in output")
	}
}

// TestRenderTaskCardWithOpenQuestions tests rendering a card with open questions.
func TestRenderTaskCardWithOpenQuestions(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:            1,
		Title:         "My Task",
		Status:        "in_progress",
		ProjectID:     "proj-1",
		OpenQuestions: 3,
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, w, now)

	output := w.String()
	if !strings.Contains(output, "Open Questions") {
		t.Errorf("expected Open Questions section in output")
	}
	if !strings.Contains(output, "3") {
		t.Errorf("expected open questions count in output")
	}
}

// TestRenderTaskCardLogTail tests that log shows only last 10 entries.
func TestRenderTaskCardLogTail(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:        1,
		Title:     "My Task",
		Status:    "in_progress",
		ProjectID: "proj-1",
	}
	// Create 15 log entries
	logs := make([]taskLogRow, 15)
	for i := 0; i < 15; i++ {
		logs[i] = taskLogRow{
			ID:        int64(i + 1),
			Kind:      "status",
			Body:      "entry " + string(rune(i+'0')),
			CreatedAt: now.Unix() - int64(15-i)*60,
		}
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, logs, w, now)

	output := w.String()
	// Should show entry 5-14 (last 10), not entry 0-4 (first 5)
	if strings.Contains(output, "entry 0") {
		t.Errorf("should not show first entries in log tail")
	}
	if !strings.Contains(output, "entry 9") {
		t.Errorf("expected to see entry 9 in log tail")
	}
}

// TestFormatAttach tests the formatAttach helper.
func TestFormatAttach(t *testing.T) {
	tests := []struct {
		name   string
		attach []string
		want   string
	}{
		{"empty", []string{}, ""},
		{"single", []string{"cmd"}, "cmd"},
		{"multiple", []string{"tmux", "attach", "-t", "sess"}, "tmux"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAttach(tt.attach)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRenderTaskBoardTrimsDonesUnfiltered tests that done tasks are trimmed to last 5 when not status-filtered.
func TestRenderTaskBoardTrimsDonesUnfiltered(t *testing.T) {
	// Create 7 done tasks (ids 1-7)
	doneTasks := make([]taskRow, 7)
	for i := 0; i < 7; i++ {
		doneTasks[i] = taskRow{
			ID:     int64(i + 1),
			Title:  fmt.Sprintf("Done Task %d", i+1),
			Status: "done",
		}
	}
	board := map[string][]taskRow{
		"done": doneTasks,
	}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, false) // statusFiltered=false

	output := w.String()
	// Should show last 5 (tasks 3-7)
	if !strings.Contains(output, "#3 Done Task 3") {
		t.Errorf("expected task 3 in output")
	}
	if !strings.Contains(output, "#7 Done Task 7") {
		t.Errorf("expected task 7 in output")
	}
	// Should NOT show first 2 (tasks 1-2)
	if strings.Contains(output, "#1 Done Task 1") {
		t.Errorf("should not show task 1 when trimmed")
	}
	if strings.Contains(output, "#2 Done Task 2") {
		t.Errorf("should not show task 2 when trimmed")
	}
	// Should show trimmed message
	if !strings.Contains(output, "… and 2 more") {
		t.Errorf("expected '… and 2 more' message in output")
	}
	if !strings.Contains(output, "use --status done") {
		t.Errorf("expected 'use --status done' in message")
	}
}

// TestRenderTaskBoardShowsAllWhenFiltered tests that all done tasks are shown when status-filtered.
func TestRenderTaskBoardShowsAllWhenFiltered(t *testing.T) {
	// Create 7 done tasks (ids 1-7)
	doneTasks := make([]taskRow, 7)
	for i := 0; i < 7; i++ {
		doneTasks[i] = taskRow{
			ID:     int64(i + 1),
			Title:  fmt.Sprintf("Done Task %d", i+1),
			Status: "done",
		}
	}
	board := map[string][]taskRow{
		"done": doneTasks,
	}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, true) // statusFiltered=true

	output := w.String()
	// Should show all 7 tasks
	for i := 1; i <= 7; i++ {
		if !strings.Contains(output, fmt.Sprintf("#%d Done Task %d", i, i)) {
			t.Errorf("expected task %d in output when filtered", i)
		}
	}
	// Should NOT show trimmed message
	if strings.Contains(output, "… and") {
		t.Errorf("should not show trimmed message when status-filtered")
	}
}

// TestRenderTaskBoardTrimmsCancelledUnfiltered tests that cancelled tasks are trimmed like done tasks.
func TestRenderTaskBoardTrimmsCancelledUnfiltered(t *testing.T) {
	// Create 7 cancelled tasks
	cancelledTasks := make([]taskRow, 7)
	for i := 0; i < 7; i++ {
		cancelledTasks[i] = taskRow{
			ID:     int64(i + 1),
			Title:  fmt.Sprintf("Cancelled Task %d", i+1),
			Status: "cancelled",
		}
	}
	board := map[string][]taskRow{
		"cancelled": cancelledTasks,
	}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, false) // statusFiltered=false

	output := w.String()
	// Should show last 5 (tasks 3-7)
	if !strings.Contains(output, "#3 Cancelled Task 3") {
		t.Errorf("expected task 3 in output")
	}
	// Should NOT show first 2
	if strings.Contains(output, "#1 Cancelled Task 1") {
		t.Errorf("should not show task 1 when trimmed")
	}
	// Should show trimmed message for cancelled
	if !strings.Contains(output, "… and 2 more") {
		t.Errorf("expected trimmed message")
	}
	if !strings.Contains(output, "use --status cancelled") {
		t.Errorf("expected 'use --status cancelled' in message")
	}
}

// TestRenderTaskCardSubtaskRepoID tests that subtask table uses RepoID column.
func TestRenderTaskCardSubtaskRepoID(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:        1,
		Title:     "My Task",
		Status:    "in_progress",
		ProjectID: "proj-1",
		Subtasks: []taskRow{
			{ID: 10, Title: "Subtask with repo", Status: "backlog", RepoID: "repo-123"},
			{ID: 11, Title: "Subtask without repo", Status: "in_progress", RepoID: ""},
		},
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, w, now)

	output := w.String()
	// Should show repo-123 for first subtask
	if !strings.Contains(output, "repo-123") {
		t.Errorf("expected repo-123 in output for subtask with RepoID")
	}
	// Should show subtask 11 with repo-id (with "-" for empty repo)
	if !strings.Contains(output, "#11") || !strings.Contains(output, "Subtask without repo") {
		t.Errorf("expected subtask 11 in output")
	}
	// Verify the REPO column contains "-" for empty repo by checking the table structure
	lines := strings.Split(output, "\n")
	subtaskSection := false
	found := false
	for _, line := range lines {
		if strings.Contains(line, "Subtasks") {
			subtaskSection = true
			continue
		}
		if subtaskSection && strings.Contains(line, "#11") && strings.Contains(line, "Subtask without repo") {
			// Just check that the line contains "-" somewhere after the status column
			if strings.Contains(line, "-") {
				found = true
			}
			break
		}
	}
	if !found {
		t.Errorf("expected '-' for empty RepoID in subtask table")
	}
}
