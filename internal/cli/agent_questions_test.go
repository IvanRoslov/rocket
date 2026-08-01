package cli

import (
	"errors"
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

// TestAgentQuestionsRoleDefaultsToInstance covers the no-argument form: inside
// a role instance the role is taken from ROCKET_SESSION_ID, outside one the
// command is a usage error.
func TestAgentQuestionsRoleDefaultsToInstance(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "sre-run-2")
	if role, err := resolveRole(""); err != nil || role != "sre" {
		t.Fatalf("resolveRole = %q, %v; want sre", role, err)
	}

	t.Setenv("ROCKET_SESSION_ID", "")
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
		ID:        7,
		RoleID:    "sre",
		Ordinal:   1,
		Status:    "open",
		WhoseTurn: "user",
		Body:      "нужно решение",
		Context:   "детали",
		Messages: []questionMessageRow{
			{Author: "sre-run-1", Body: "смотрю"},
			{Author: "", Body: "жду"},
		},
	}}

	out := renderAgentQuestions("sre", qs)
	for _, want := range []string{
		"agent sre",
		"Q1 (#7) [open]",
		"ждёт ответа пользователя",
		"нужно решение",
		"context: детали",
		"[sre-run-1] смотрю",
		"[user] жду",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderAgentQuestions missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderAgentQuestionsAwaitingRole(t *testing.T) {
	qs := []agentQuestionRow{{ID: 8, Ordinal: 2, Status: "open", WhoseTurn: "role", Body: "как быть?"}}
	out := renderAgentQuestions("sre", qs)
	if !strings.Contains(out, "ждёт роль") {
		t.Errorf("renderAgentQuestions = %q, want the role's turn marked", out)
	}
}

func TestRenderAgentQuestionsResolvedHasNoArrow(t *testing.T) {
	qs := []agentQuestionRow{{ID: 9, Ordinal: 1, Status: "resolved", Body: "старое"}}
	out := renderAgentQuestions("sre", qs)
	if strings.Contains(out, "ждёт") {
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
