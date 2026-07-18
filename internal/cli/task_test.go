package cli

import (
	"bytes"
	"errors"
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

// TestRenderTaskBoardEmpty tests rendering an empty board.
func TestRenderTaskBoardEmpty(t *testing.T) {
	now := time.Now()
	board := map[string][]taskRow{}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, now)

	output := w.String()
	if len(strings.TrimSpace(output)) > 0 {
		t.Errorf("expected empty output for empty board, got: %q", output)
	}
}

// TestRenderTaskBoardSingleStatus tests rendering a board with one status.
func TestRenderTaskBoardSingleStatus(t *testing.T) {
	now := time.Now()
	board := map[string][]taskRow{
		"backlog": []taskRow{
			{ID: 1, Title: "Task 1", Status: "backlog"},
			{ID: 2, Title: "Task 2", Status: "backlog"},
		},
	}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, now)

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
	now := time.Now()
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
	renderTaskBoard(board, w, now)

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
	now := time.Now()
	board := map[string][]taskRow{
		"backlog": []taskRow{
			{ID: 1, Title: "Task 1", Status: "backlog"},
		},
		"in_progress": []taskRow{},
		"done":        []taskRow{},
	}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, now)

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
	now := time.Now()
	board := map[string][]taskRow{
		"backlog": []taskRow{
			{ID: 1, Title: "Task 1", Status: "backlog", FeatureSlug: "my-feature", SessionID: "sess-123"},
		},
	}

	w := &bytes.Buffer{}
	renderTaskBoard(board, w, now)

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
