package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/spf13/cobra"
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
			if got := needsProjectDefault(tt.projectID, tt.parentID, false); got != tt.want {
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

// TestTaskMoveForceFlagRegistered verifies the --force flag is registered
// and parses cleanly on task move. This deliberately stops at flag parsing
// (ParseFlags) rather than calling cmd.Execute()/RunE, since RunE calls
// connect(true) with autostart=true — which, in this environment, would
// reach and mutate the real local rocket daemon's live task database
// rather than failing cleanly.
func TestTaskMoveForceFlagRegistered(t *testing.T) {
	cmd := newTaskMoveCmd()
	if cmd.Flags().Lookup("force") == nil {
		t.Fatal("expected --force flag to be registered on task move")
	}
	if err := cmd.ParseFlags([]string{"1", "in_progress", "--force"}); err != nil {
		t.Fatalf("ParseFlags with --force: %v", err)
	}
	if !cmd.Flags().Changed("force") {
		t.Error("expected --force to be marked as changed after parsing")
	}
}

// TestRenderMoveErrorReviewBlocked verifies the review_blocked 409 is
// rendered with the open/live details and a --force hint.
func TestRenderMoveErrorReviewBlocked(t *testing.T) {
	apiErr := &client.APIError{
		Code:    "review_blocked",
		Message: "open subtasks: [3 4]; live workers: [worker-1] — retry with --force to override",
	}
	rendered := renderMoveError(apiErr)
	if rendered == nil {
		t.Fatal("expected a non-nil error")
	}
	msg := rendered.Error()
	if !strings.Contains(msg, "3 4") {
		t.Errorf("rendered message missing open subtask ids: %q", msg)
	}
	if !strings.Contains(msg, "worker-1") {
		t.Errorf("rendered message missing live worker id: %q", msg)
	}
	if !strings.Contains(msg, "--force") {
		t.Errorf("rendered message missing --force hint: %q", msg)
	}
}

// TestRenderMoveErrorPassesThroughOtherErrors verifies non-review_blocked
// errors are returned unchanged.
func TestRenderMoveErrorPassesThroughOtherErrors(t *testing.T) {
	apiErr := &client.APIError{Code: "invalid_status", Message: "bad status"}
	rendered := renderMoveError(apiErr)
	if rendered != apiErr {
		t.Errorf("expected passthrough of non-review_blocked error, got %v", rendered)
	}

	other := errors.New("boom")
	if renderMoveError(other) != other {
		t.Errorf("expected passthrough of non-APIError")
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

// TestTaskAskNoRocketSessionID tests that task ask without ROCKET_SESSION_ID
// returns an error with the ask-orch hint.
func TestTaskAskNoRocketSessionID(t *testing.T) {
	// Ensure ROCKET_SESSION_ID is not set
	oldSessionID := os.Getenv("ROCKET_SESSION_ID")
	os.Unsetenv("ROCKET_SESSION_ID")
	defer func() {
		if oldSessionID != "" {
			os.Setenv("ROCKET_SESSION_ID", oldSessionID)
		}
	}()

	cmd := newTaskAskCmd()
	cmd.SetArgs([]string{"1", "question text"})
	err := cmd.Execute()

	if err == nil {
		t.Errorf("expected error when ROCKET_SESSION_ID not set")
	}

	// Check that the error message contains the ask-orch hint
	if !strings.Contains(err.Error(), "ask-orch") {
		t.Errorf("expected error to mention ask-orch, got: %v", err)
	}
}

// TestTaskAskWithRocketSessionID tests that task ask with ROCKET_SESSION_ID
// set does not return the ROCKET_SESSION_ID guard error (though connect may fail).
func TestTaskAskWithRocketSessionID(t *testing.T) {
	// Set ROCKET_SESSION_ID
	oldSessionID := os.Getenv("ROCKET_SESSION_ID")
	os.Setenv("ROCKET_SESSION_ID", "test-session-123")
	defer func() {
		if oldSessionID != "" {
			os.Setenv("ROCKET_SESSION_ID", oldSessionID)
		} else {
			os.Unsetenv("ROCKET_SESSION_ID")
		}
	}()

	cmd := newTaskAskCmd()
	cmd.SetArgs([]string{"1", "question text"})
	err := cmd.Execute()

	// The command may fail from connect(), but should NOT return the
	// ROCKET_SESSION_ID guard error message
	if err != nil && err.Error() == "rocket task ask is for orchestrators asking the human; to ask the orchestrator a question use: rocket task ask-orch" {
		t.Errorf("should not return ROCKET_SESSION_ID guard error when env var is set, got: %v", err)
	}
}

// TestTaskAskOrchUsage tests usage violations for task ask-orch.
func TestTaskAskOrchUsage(t *testing.T) {
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
			cmd := newTaskAskOrchCmd()
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

	// Since task #1023 --dismiss may carry a reason, so body+dismiss is a
	// legal close. Naming two different resolutions at once is what is not.
	t.Run("both choose and dismiss", func(t *testing.T) {
		cmd := newTaskCloseCmd0()
		cmd.SetArgs([]string{"1", "--dismiss", "--choose", "2"})
		err := cmd.Execute()
		if err == nil {
			t.Fatalf("expected error when both --choose and --dismiss given")
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

// TestRenderQuestionsPrintsLocalRef: a thread header names the thread by the
// one id a user types back — the local ref — and no longer by the global id
// that sat next to it. Two numbers side by side is what sent replies into the
// wrong thread (task #1023, spec v1 §«Тред и его id»).
func TestRenderQuestionsPrintsLocalRef(t *testing.T) {
	qs := []questionRow{{
		ID: 372, Ordinal: 2, LocalRef: "799/Q2", Status: "open", Body: "Which approach?",
		Options: []string{"откатить", "чинить вперёд"},
	}}
	out := renderQuestions(799, qs)
	if !strings.Contains(out, "799/Q2 [open]") {
		t.Errorf("expected the local ref in the header, got: %q", out)
	}
	if strings.Contains(out, "#372") {
		t.Errorf("expected the global id to be gone from human output, got: %q", out)
	}
	if !strings.Contains(out, "  варианты: 1) откатить  2) чинить вперёд") {
		t.Errorf("expected the answer options, got: %q", out)
	}
}

// TestRenderQuestionsLocalRefFallback: a daemon that predates local_ref sends
// none, and the CLI must still print a usable ref rather than a bare "Q2".
func TestRenderQuestionsLocalRefFallback(t *testing.T) {
	out := renderQuestions(799, []questionRow{{ID: 372, Ordinal: 2, Status: "open", Body: "b"}})
	if !strings.Contains(out, "799/Q2 [open]") {
		t.Errorf("expected a ref built from task and ordinal, got: %q", out)
	}
}

// TestRenderQuestionsFyiMarked: an fyi thread is a status note nobody has to
// answer, and it says so instead of looking like a resolved decision.
func TestRenderQuestionsFyiMarked(t *testing.T) {
	out := renderQuestions(799, []questionRow{{
		ID: 1, Ordinal: 1, LocalRef: "799/Q1", Status: "resolved",
		Type: "fyi", Body: "выкатили",
	}})
	if !strings.Contains(out, "799/Q1 [fyi]") {
		t.Errorf("expected the fyi marker instead of the status, got: %q", out)
	}
}

// TestRenderQuestionsWaitingArrow tests that the header arrow names who is
// awaited, replacing the pre-participant whose_turn arrow.
func TestRenderQuestionsWaitingArrow(t *testing.T) {
	qs := []questionRow{{
		ID: 5, Ordinal: 1, Status: "open", WhoseTurn: "user", Body: "Which approach?",
		Participants: []string{"human", "cto"},
		WaitingOn:    []string{"cto", "human"},
	}}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "task #42") {
		t.Errorf("expected task header, got: %q", out)
	}
	if !strings.Contains(out, "42/Q1 [open] → ждут: cto, human") {
		t.Errorf("expected waiting-on arrow, got: %q", out)
	}
	if strings.Contains(out, "ждёт ответа") || strings.Contains(out, "ждёт оркестратора") {
		t.Errorf("expected the whose_turn arrow to be gone, got: %q", out)
	}
	if !strings.Contains(out, "  участники: human, cto") {
		t.Errorf("expected participants line, got: %q", out)
	}
	if !strings.Contains(out, "Which approach?") {
		t.Errorf("expected body, got: %q", out)
	}
}

// TestRenderQuestionsNoWaitingNoArrow tests that a thread nobody is awaited on
// -- a resolved one, or a pre-participant server -- renders no arrow and no
// participants line.
func TestRenderQuestionsNoWaitingNoArrow(t *testing.T) {
	qs := []questionRow{
		{ID: 7, Ordinal: 3, Status: "resolved", WhoseTurn: "", Body: "Done question"},
	}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "42/Q3 [resolved]") {
		t.Errorf("expected resolved header, got: %q", out)
	}
	if strings.Contains(out, "ждут") || strings.Contains(out, "участники") {
		t.Errorf("expected no arrow and no participants line, got: %q", out)
	}
}

// TestRenderQuestionsYourTurn tests that a thread waiting on the caller is
// marked, so "rocket task questions" shows what needs an answer.
func TestRenderQuestionsYourTurn(t *testing.T) {
	qs := []questionRow{{
		ID: 12, Ordinal: 1, Status: "open", Body: "Q body",
		Participants: []string{"human", "cto"},
		WaitingOn:    []string{"human"},
		YourTurn:     true,
	}}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "→ ждут: human (ваш ход)") {
		t.Errorf("expected your-turn marker, got: %q", out)
	}
}

// TestRenderQuestionsCanonicalHumanAuthor tests that the canonical "human"
// author renders like the legacy empty author: the wire still sends "" today,
// but subtask #736 flips it and the CLI must read both.
func TestRenderQuestionsCanonicalHumanAuthor(t *testing.T) {
	qs := []questionRow{{
		ID: 13, Ordinal: 1, Status: "open", Body: "Q body",
		Messages: []questionMessageRow{
			{ID: 1, Author: "human", Kind: "reply", Body: "canonical human"},
			{ID: 2, Author: "", Kind: "reply", Body: "legacy human"},
		},
	}}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "[user] canonical human") {
		t.Errorf("expected canonical human rendered as user, got: %q", out)
	}
	if !strings.Contains(out, "[user] legacy human") {
		t.Errorf("expected legacy human rendered as user, got: %q", out)
	}
}

// TestRenderQuestionsAddressedTo tests that a targeted message names its
// addressees in the frame.
func TestRenderQuestionsAddressedTo(t *testing.T) {
	qs := []questionRow{{
		ID: 14, Ordinal: 1, Status: "open", Body: "Q body",
		Messages: []questionMessageRow{
			{ID: 1, Author: "cto", Kind: "reply", Body: "targeted",
				AddressedTo: []string{"reply-answer-orch", "human"}},
		},
	}}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "[cto → reply-answer-orch, human] targeted") {
		t.Errorf("expected addressed-to frame, got: %q", out)
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
		"brainstorm": []taskRow{
			{ID: 6, Title: "Task 6", Status: "brainstorm"},
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
	headers := []string{"BACKLOG", "BRAINSTORM", "IN PROGRESS", "REVIEW", "DONE", "CANCELLED"}
	last := -1
	for _, h := range headers {
		i := strings.Index(output, h)
		if i < 0 {
			t.Errorf("expected %q header in output", h)
			continue
		}
		if i < last {
			t.Errorf("header %q is out of canonical board order", h)
		}
		last = i
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
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, nil, w, now)

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
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, nil, w, now)

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
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, nil, w, now)

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
	renderTaskCard(task, docs, []taskLogRow{}, nil, w, now)

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
	renderTaskCard(task, []taskDocRow{}, logs, nil, w, now)

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
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, nil, w, now)

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
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, nil, w, now)

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
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, nil, w, now)

	output := w.String()
	if !strings.Contains(output, "Open Questions") {
		t.Errorf("expected Open Questions section in output")
	}
	if !strings.Contains(output, "3") {
		t.Errorf("expected open questions count in output")
	}
}

// TestRenderTaskCardWithQuestionsThread tests that task show inlines the
// Q&A thread (via renderQuestions) under a "## Questions" section, on top
// of the existing open-questions count line.
func TestRenderTaskCardWithQuestionsThread(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:            1,
		Title:         "My Task",
		Status:        "in_progress",
		ProjectID:     "proj-1",
		OpenQuestions: 1,
	}
	questions := []questionRow{
		{
			ID:        42,
			TaskID:    1,
			Ordinal:   1,
			AskedBy:   "orch-1",
			Body:      "Which approach should I take?",
			Status:    "open",
			WhoseTurn: "user",
			Messages: []questionMessageRow{
				{ID: 1, Author: "orch-1", Kind: "question", Body: "Which approach should I take?"},
			},
		},
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, questions, w, now)

	output := w.String()
	if !strings.Contains(output, "## Questions") {
		t.Errorf("expected ## Questions section in output, got:\n%s", output)
	}
	if !strings.Contains(output, "1/Q1 [open]") {
		t.Errorf("expected inlined question header in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Which approach should I take?") {
		t.Errorf("expected question body in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Open Questions") || !strings.Contains(output, "1") {
		t.Errorf("expected open questions count line preserved, got:\n%s", output)
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
	renderTaskCard(task, []taskDocRow{}, logs, nil, w, now)

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
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, nil, w, now)

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

// TestRenderTaskCardSubtaskWithPRAndCI tests that subtask table includes PR and CI columns.
func TestRenderTaskCardSubtaskWithPRAndCI(t *testing.T) {
	now := time.Now()
	task := taskDetailRow{
		ID:        1,
		Title:     "My Task",
		Status:    "in_progress",
		ProjectID: "proj-1",
		Subtasks: []taskRow{
			{ID: 10, Title: "Subtask with PR", Status: "in_progress", SessionID: "sess-1", PRNumber: 42, PRState: "open", CIState: "passing"},
			{ID: 11, Title: "Subtask without PR", Status: "backlog", SessionID: "sess-2", PRNumber: 0, PRState: "", CIState: ""},
		},
	}

	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, nil, w, now)

	output := w.String()

	// Check for PR column header
	if !strings.Contains(output, "PR") || !strings.Contains(output, "CI") {
		t.Errorf("expected PR and CI column headers in output")
	}

	// Check for PR #42 (open) in first subtask
	if !strings.Contains(output, "#42 (open)") {
		t.Errorf("expected PR #42 (open) in output, got:\n%s", output)
	}

	// Check for CI passing
	if !strings.Contains(output, "passing") {
		t.Errorf("expected CI state 'passing' in output")
	}

	// Check for dashes in subtask without PR
	lines := strings.Split(output, "\n")
	subtaskSection := false
	foundNoPR := false
	for _, line := range lines {
		if strings.Contains(line, "Subtasks") {
			subtaskSection = true
			continue
		}
		if subtaskSection && strings.Contains(line, "#11") && strings.Contains(line, "Subtask without PR") {
			foundNoPR = true
			// Should have at least one dash for empty PR/CI
			if !strings.Contains(line, "-") {
				t.Errorf("expected dashes for empty PR/CI in line: %q", line)
			}
			break
		}
	}
	if !foundNoPR {
		t.Errorf("did not find subtask without PR in output")
	}
}

// TestParseTo covers the --to normalisation: comma splitting, repetition,
// trimming, empty segments and deduplication.
func TestParseTo(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, nil},
		{"empty string", []string{""}, nil},
		{"single", []string{"cto"}, []string{"cto"}},
		{"comma separated", []string{"cto,human"}, []string{"cto", "human"}},
		{"repeated flag", []string{"cto", "human"}, []string{"cto", "human"}},
		{"spaces trimmed", []string{" cto , human "}, []string{"cto", "human"}},
		{"empty segments dropped", []string{"cto,,"}, []string{"cto"}},
		{"deduped", []string{"cto", "cto,human"}, []string{"cto", "human"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTo(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("parseTo(%v) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("parseTo(%v) = %v, want %v", tt.in, got, tt.want)
				}
			}
		})
	}
}

// TestSetToOmitsEmpty tests that no --to leaves the request body byte-identical
// to what the CLI sent before the participant model existed: no "to" key at
// all, not an empty array.
func TestSetToOmitsEmpty(t *testing.T) {
	body := map[string]any{"body": "q"}
	setTo(body, nil)
	if _, ok := body["to"]; ok {
		t.Errorf("expected no \"to\" key for empty to, got %v", body)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"body":"q"}` {
		t.Errorf("expected unchanged body JSON, got %s", raw)
	}
}

// TestSetToAddsRecipients tests that --to reaches the request body as an array.
func TestSetToAddsRecipients(t *testing.T) {
	body := map[string]any{"body": "q"}
	setTo(body, []string{"cto", "human"})
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"body":"q","to":["cto","human"]}` {
		t.Errorf("unexpected body JSON: %s", raw)
	}
}

// TestTaskThreadCommandsHaveToFlag tests that every task thread-writing
// command registers --to.
func TestTaskThreadCommandsHaveToFlag(t *testing.T) {
	cmds := map[string]*cobra.Command{
		"ask":      newTaskAskCmd(),
		"ask-orch": newTaskAskOrchCmd(),
		"reply":    newTaskReplyCmd(),
		"answer":   newTaskAnswerCmd(),
	}
	for name, cmd := range cmds {
		if cmd.Flags().Lookup("to") == nil {
			t.Errorf("task %s: expected --to flag", name)
		}
	}
}

// TestTaskWritingCommandsFileFlag pins that every text-writing task command
// accepts --file as an alternative to the positional text, and that
// supplying both sources (or neither) is a usage error — exit code 3, not a
// silent preference that could drop the user's markdown on a typo.
//
// Each case resolves its body before connect(), which is disabled under go
// test, so only the usage-error paths run to completion here; the happy
// path is covered at the textBody level in io_test.go.
func TestTaskWritingCommandsFileFlag(t *testing.T) {
	body := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(body, []byte("markdown `body`"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []struct {
		name string
		new  func() *cobra.Command
		args []string
	}{
		{"log both", newTaskLogCmd, []string{"1", "text", "--kind", "note", "--file", body}},
		{"log neither", newTaskLogCmd, []string{"1", "--kind", "note"}},
		{"ask both", newTaskAskCmd, []string{"1", "text", "--file", body}},
		{"ask neither", newTaskAskCmd, []string{"1"}},
		{"ask-orch both", newTaskAskOrchCmd, []string{"1", "text", "--file", body}},
		{"ask-orch neither", newTaskAskOrchCmd, []string{"1"}},
		{"reply both", newTaskReplyCmd, []string{"7", "text", "--file", body}},
		{"reply neither", newTaskReplyCmd, []string{"7"}},

		{"close both", newTaskCloseCmd0, []string{"7", "text", "--file", body}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.new()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected a usage error, got nil")
			}
			if code := exitCode(err); code != 3 {
				t.Fatalf("exitCode = %d, want 3 (err=%v)", code, err)
			}
		})
	}
}

// TestTaskWritingCommandsRegisterFileFlag proves the flag exists on each of
// them, independent of the daemon.
func TestTaskWritingCommandsRegisterFileFlag(t *testing.T) {
	for name, newCmd := range map[string]func() *cobra.Command{
		"log":      newTaskLogCmd,
		"ask":      newTaskAskCmd,
		"ask-orch": newTaskAskOrchCmd,
		"reply":    newTaskReplyCmd,
		"answer":   newTaskAnswerCmd,
	} {
		if f := newCmd().Flags().Lookup("file"); f == nil {
			t.Errorf("rocket task %s: --file flag is not registered", name)
		}
	}
}

// TestTaskShowJSONCarriesEverythingTheCardShows pins that --json is not
// poorer than the human card: whatever renderTaskCard draws (subtasks,
// docs, journal, question threads) must have a home in the JSON shape.
// Before this, --json short-circuited before fetching three of those four
// and emitted the bare task row.
//
// The task's own fields must stay at the TOP level — web and mobile already
// read id/title/subtasks from there, so --json may only gain keys.
func TestTaskShowJSONCarriesEverythingTheCardShows(t *testing.T) {
	v := taskShowJSON{
		taskDetailRow: taskDetailRow{ID: 5, Title: "t", Subtasks: []taskRow{{ID: 6}}},
		Docs:          []taskDocRow{{ID: 1, Kind: "spec"}},
		Log:           []taskLogRow{{ID: 2, Kind: "decision"}},
		Questions:     []questionRow{{ID: 3, Ordinal: 1}},
	}

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"id", "title", "subtasks", "docs", "log", "questions"} {
		if _, ok := got[key]; !ok {
			t.Errorf("task show --json is missing key %q; got keys %v", key, keysOf(got))
		}
	}
}

// TestTaskShowJSONEmptyCollectionsAreArrays keeps the shape stable for
// machine consumers: an empty journal is [], never null, so `jq '.log[]'`
// works on a fresh task exactly as it does on a busy one.
func TestTaskShowJSONEmptyCollectionsAreArrays(t *testing.T) {
	raw, err := json.Marshal(taskShowJSON{
		taskDetailRow: taskDetailRow{ID: 5},
		Docs:          []taskDocRow{},
		Log:           []taskLogRow{},
		Questions:     []questionRow{},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"docs":[]`, `"log":[]`, `"questions":[]`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("json = %s, want it to contain %s", raw, want)
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
