package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func inboxNow() time.Time { return time.Unix(1_800_000_000, 0) }

func inboxSample() []threadRow {
	return []threadRow{
		{
			LocalRef: "1023/Q2", Kind: "task", TaskID: 1023,
			Subject: `task #1023 "Улучшение агентов"`,
			Body:    "Какой подход выбрать?\nвторая строка не нужна",
			Status:  "open", Type: "decision",
			Options:      []string{"откатить", "чинить вперёд"},
			Participants: []string{"cto", "human", "orch-1"},
			Attention:    []string{"human"},
			YourTurn:     true,
			AskedAt:      inboxNow().Add(-3 * time.Hour).Unix(),
			UpdatedAt:    inboxNow().Add(-2 * time.Hour).Unix(),
		},
		{
			LocalRef: "cto/Q1", Kind: "role", RoleID: "cto",
			Subject: "role cto", Body: "нужен доступ к проду",
			Status:  "open", Type: "decision",
			Participants: []string{"cto", "human"},
			Attention:    []string{"cto"},
			UpdatedAt:    inboxNow().Add(-30 * time.Minute).Unix(),
		},
	}
}

// TestRenderThreadInbox: one screen answering "what is open and on whom",
// across tasks AND roles — the loop over tasks it replaces is what made agents
// miss threads (task #1023, spec v1 §«Единый инбокс»).
func TestRenderThreadInbox(t *testing.T) {
	out := renderThreadInbox(inboxSample(), inboxNow())

	for _, want := range []string{
		"1023/Q2",
		`task #1023 "Улучшение агентов"`,
		"Какой подход выбрать?",
		"ждут: human (ваш ход)",
		"участники: cto, human, orch-1",
		"варианты: 1) откатить  2) чинить вперёд",
		"cto/Q1",
		"role cto",
		"ждут: cto",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the inbox, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "вторая строка не нужна") {
		t.Errorf("expected only the first line of the question, got:\n%s", out)
	}
	// Age is measured from the last movement, not from when the thread opened.
	if !strings.Contains(out, "(2h)") {
		t.Errorf("expected the age since the last entry, got:\n%s", out)
	}
}

// TestRenderThreadInboxEmpty: nothing open is a normal state and says so,
// rather than printing a bare blank.
func TestRenderThreadInboxEmpty(t *testing.T) {
	if out := renderThreadInbox(nil, inboxNow()); !strings.Contains(out, "нет открытых тредов") {
		t.Errorf("expected an explicit empty state, got: %q", out)
	}
}

// TestRenderThreadInboxResolved: with --all a closed thread shows its
// resolution instead of pretending somebody still owes an answer.
func TestRenderThreadInboxResolved(t *testing.T) {
	out := renderThreadInbox([]threadRow{{
		LocalRef: "1023/Q3", Kind: "task", Subject: `task #1023 "T"`,
		Body: "выкатили", Status: "resolved", Type: "fyi", Resolution: "fyi",
		UpdatedAt: inboxNow().Unix(),
	}}, inboxNow())

	if !strings.Contains(out, "1023/Q3 [fyi]") {
		t.Errorf("expected the fyi marker, got:\n%s", out)
	}
	if strings.Contains(out, "ждут") {
		t.Errorf("a resolved thread waits on nobody, got:\n%s", out)
	}
}

// TestThreadInboxQuery: the two filters are the server's, not the CLI's — the
// inbox must not fetch everything and sieve it locally.
func TestThreadInboxQuery(t *testing.T) {
	tests := []struct {
		name      string
		waitingOn string
		all       bool
		want      string
	}{
		{"default", "", false, "/v1/threads"},
		{"waiting on", "human", false, "/v1/threads?waiting_on=human"},
		{"all", "", true, "/v1/threads?all=true"},
		{"both", "cto", true, "/v1/threads?all=true&waiting_on=cto"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := threadInboxPath(tt.waitingOn, tt.all); got != tt.want {
				t.Errorf("threadInboxPath(%q, %v) = %q, want %q",
					tt.waitingOn, tt.all, got, tt.want)
			}
		})
	}
}

// TestQuestionsCmdUsage: the command takes no positional arguments — a stray
// one is a typo for a flag, not a filter to guess at.
func TestQuestionsCmdUsage(t *testing.T) {
	cmd := newQuestionsCmd()
	cmd.SetArgs([]string{"1023"})
	cmd.SetOut(new(strings.Builder))
	cmd.SetErr(new(strings.Builder))
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for a positional argument")
	}
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Errorf("expected usageError, got %T: %v", err, err)
	}
}

// TestQuestionsCmdFlags: both filters are registered and parse.
func TestQuestionsCmdFlags(t *testing.T) {
	cmd := newQuestionsCmd()
	for _, name := range []string{"waiting-on", "all"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("expected --%s to be registered", name)
		}
	}
	if err := cmd.ParseFlags([]string{"--waiting-on", "human", "--all"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
}

// TestRootRegistersQuestions: the inbox is a top-level command, not a
// subcommand of task — it spans tasks and roles alike.
func TestRootRegistersQuestions(t *testing.T) {
	root := NewRootCmd()
	var found *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "questions" {
			found = c
		}
	}
	if found == nil {
		t.Fatal("expected a top-level `rocket questions` command")
	}
}
