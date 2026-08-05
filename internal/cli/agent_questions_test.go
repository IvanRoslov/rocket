package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestAgentQuestionCommandUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		cmd  func() *cobra.Command
		args []string
	}{
		{"ask without args", ctor(newAgentAskCmd), []string{}},
		{"ask without text", ctor(newAgentAskCmd), []string{"sre"}},
		{"ask with too many args", ctor(newAgentAskCmd), []string{"sre", "q", "extra"}},
		{"questions with too many args", ctor(newAgentQuestionsCmd), []string{"sre", "extra"}},
		{"reply without text", ctor(newAgentReplyCmd), []string{"7"}},
		{"reply with invalid id", ctor(newAgentReplyCmd), []string{"not-a-number", "text"}},
		{"answer without args", ctor(newAgentAnswerCmd), []string{}},
		{"answer with invalid id", ctor(newAgentAnswerCmd), []string{"nope", "text"}},
		{"answer without body or dismiss", ctor(newAgentAnswerCmd), []string{"7"}},
		{"answer with body and dismiss", ctor(newAgentAnswerCmd), []string{"7", "text", "--dismiss"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.cmd()
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected an error for args %v", tc.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Fatalf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestAgentQuestionsAgentDefaultsToTheSession covers the no-argument form:
// inside an agent's session the agent is its session name, outside one (and
// outside tmux) the command is a usage error.
func TestAgentQuestionsAgentDefaultsToTheSession(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "sre")
	if agent, err := resolveAgentID(""); err != nil || agent != "sre" {
		t.Fatalf("resolveAgentID = %q, %v; want sre", agent, err)
	}

	t.Setenv("ROCKET_SESSION_ID", "")
	t.Setenv("TMUX", "")
	cmd := newAgentQuestionsCmd()
	cmd.SetArgs(nil)
	err := cmd.Execute()
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("expected usageError outside an instance, got %T: %v", err, err)
	}
}

func TestRenderAgentQuestionsEmpty(t *testing.T) {
	if out := renderAgentQuestions("sre", nil); out != "" {
		t.Fatalf("renderAgentQuestions(empty) = %q, want empty", out)
	}
}

func TestRenderAgentQuestionsThread(t *testing.T) {
	qs := []agentQuestionRow{{
		ID:           7,
		RoleID:       "sre",
		Ordinal:      1,
		Status:       "open",
		WhoseTurn:    "user",
		Body:         "нужно решение",
		Context:      "детали",
		Participants: []string{"human", "sre"},
		WaitingOn:    []string{"human"},
		Messages: []questionMessageRow{
			{Author: "sre-run-1", Body: "смотрю"},
			{Author: "", Body: "жду"},
			{Author: "human", Body: "и я жду"},
		},
	}}

	out := renderAgentQuestions("sre", qs)
	for _, want := range []string{
		"agent sre",
		"sre/Q1 [open] → ждут: human",
		"нужно решение",
		"context: детали",
		"  участники: human, sre",
		"[sre-run-1] смотрю",
		"[user] жду",
		"[user] и я жду",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderAgentQuestions missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ждёт ответа") {
		t.Errorf("expected the whose_turn arrow to be gone:\n%s", out)
	}
}

// TestRenderAgentQuestionsYourTurn tests that a role thread waiting on the
// caller is marked, so "rocket agent questions" shows what needs an answer.
func TestRenderAgentQuestionsYourTurn(t *testing.T) {
	qs := []agentQuestionRow{{
		ID: 8, Ordinal: 2, Status: "open", Body: "как быть?",
		Participants: []string{"human", "sre"},
		WaitingOn:    []string{"sre"},
		YourTurn:     true,
	}}
	out := renderAgentQuestions("sre", qs)
	if !strings.Contains(out, "→ ждут: sre (ваш ход)") {
		t.Errorf("renderAgentQuestions = %q, want the caller's turn marked", out)
	}
}

// TestRenderAgentQuestionsAddressedTo tests that a targeted message names its
// addressees in the frame.
func TestRenderAgentQuestionsAddressedTo(t *testing.T) {
	qs := []agentQuestionRow{{
		ID: 10, Ordinal: 1, Status: "open", Body: "вопрос",
		Messages: []questionMessageRow{
			{Author: "sre", Body: "уточню", AddressedTo: []string{"human"}},
		},
	}}
	out := renderAgentQuestions("sre", qs)
	if !strings.Contains(out, "[sre → human] уточню") {
		t.Errorf("expected addressed-to frame, got: %q", out)
	}
}

func TestRenderAgentQuestionsResolvedHasNoArrow(t *testing.T) {
	qs := []agentQuestionRow{{ID: 9, Ordinal: 1, Status: "resolved", Body: "старое"}}
	out := renderAgentQuestions("sre", qs)
	if strings.Contains(out, "ждут") || strings.Contains(out, "участники") {
		t.Errorf("resolved question must have no turn marker: %q", out)
	}
}

func TestAgentQuestionCommandsRegistered(t *testing.T) {
	want := map[string]bool{"ask": false, "questions": false, "reply": false, "answer": false}
	for _, c := range newAgentCmd().Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("rocket agent %s is not registered", name)
		}
	}
}

func TestQuestionsCell(t *testing.T) {
	cases := []struct {
		open, awaiting any
		want           string
	}{
		{float64(0), float64(0), "-"},
		{float64(3), float64(0), "3"},
		{float64(3), float64(2), "3 (2 ждут)"},
	}
	for _, tc := range cases {
		if got := questionsCell(tc.open, tc.awaiting); got != tc.want {
			t.Errorf("questionsCell(%v, %v) = %q, want %q", tc.open, tc.awaiting, got, tc.want)
		}
	}
}

// TestAgentThreadCommandsHaveToFlag tests that every role thread-writing
// command registers --to.
func TestAgentThreadCommandsHaveToFlag(t *testing.T) {
	cmds := map[string]*cobra.Command{
		"ask":    newAgentAskCmd(),
		"reply":  newAgentReplyCmd(),
		"answer": newAgentAnswerCmd(),
	}
	for name, cmd := range cmds {
		if cmd.Flags().Lookup("to") == nil {
			t.Errorf("agent %s: expected --to flag", name)
		}
	}
}

// TestAgentWritingCommandsFileFlag mirrors the task-side test: --file is an
// alternative to the positional text on every role-thread writing command,
// and supplying both (or neither) is a usage error, exit code 3.
func TestAgentWritingCommandsFileFlag(t *testing.T) {
	body := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(body, []byte("markdown `body`"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []struct {
		name string
		new  func() *cobra.Command
		args []string
	}{
		{"ask both", newAgentAskCmd, []string{"sre", "text", "--file", body}},
		{"ask neither", newAgentAskCmd, []string{"sre"}},
		{"reply both", newAgentReplyCmd, []string{"sre/Q1", "text", "--file", body}},
		{"reply neither", newAgentReplyCmd, []string{"sre/Q1"}},
		{"answer both", newAgentAnswerCmd, []string{"sre/Q1", "text", "--file", body}},
		{"answer file plus dismiss", newAgentAnswerCmd, []string{"sre/Q1", "--dismiss", "--file", body}},
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

// TestAgentWritingCommandsRegisterFileFlag proves the flag exists on each.
func TestAgentWritingCommandsRegisterFileFlag(t *testing.T) {
	for name, newCmd := range map[string]func() *cobra.Command{
		"ask":    newAgentAskCmd,
		"reply":  newAgentReplyCmd,
		"answer": newAgentAnswerCmd,
	} {
		if f := newCmd().Flags().Lookup("file"); f == nil {
			t.Errorf("rocket agent %s: --file flag is not registered", name)
		}
	}
}
