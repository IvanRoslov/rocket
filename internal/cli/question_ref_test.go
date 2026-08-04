package cli

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestParseQuestionRefGlobal tests that a bare global id passes through
// untouched — the pre-existing form every script and prompt still uses.
func TestParseQuestionRefGlobal(t *testing.T) {
	ref, err := parseQuestionRef("372", "")
	if err != nil {
		t.Fatalf("parseQuestionRef(372) error: %v", err)
	}
	if ref.Global != "372" || ref.Scope != "" || ref.Ordinal != 0 {
		t.Errorf("parseQuestionRef(372) = %+v, want global 372", ref)
	}
}

// TestParseQuestionRefLocal tests the local forms: a scope from the flag, a
// scope inline before the slash, and case-insensitive Q.
func TestParseQuestionRefLocal(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		scope   string
		want    string
		ordinal int
	}{
		{"flag scope", "Q1", "799", "799", 1},
		{"flag scope lowercase", "q1", "799", "799", 1},
		{"flag scope bare number", "2", "799", "799", 2},
		{"inline scope", "799/Q1", "", "799", 1},
		{"inline scope lowercase", "799/q3", "", "799", 3},
		{"inline scope bare number", "799/4", "", "799", 4},
		{"inline role scope", "sre/Q1", "", "sre", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := parseQuestionRef(tt.arg, tt.scope)
			if err != nil {
				t.Fatalf("parseQuestionRef(%q, %q) error: %v", tt.arg, tt.scope, err)
			}
			if ref.Global != "" {
				t.Errorf("Global = %q, want empty for a local ref", ref.Global)
			}
			if ref.Scope != tt.want || ref.Ordinal != tt.ordinal {
				t.Errorf("parseQuestionRef(%q, %q) = %+v, want scope %q ordinal %d",
					tt.arg, tt.scope, ref, tt.want, tt.ordinal)
			}
		})
	}
}

// TestParseQuestionRefUsageErrors tests the rejected forms. Every one of them
// is a usageError so the command prints its usage line.
func TestParseQuestionRefUsageErrors(t *testing.T) {
	tests := []struct {
		name  string
		arg   string
		scope string
	}{
		{"empty", "", ""},
		{"ordinal without a scope", "Q1", ""},
		{"not a number", "not-a-number", ""},
		{"scope twice", "800/Q1", "799"},
		{"zero ordinal", "Q0", "799"},
		{"negative ordinal", "Q-1", "799"},
		{"inline zero ordinal", "799/Q0", ""},
		{"empty ordinal", "799/", ""},
		{"empty scope", "/Q1", ""},
		{"non-numeric ordinal", "799/Qx", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseQuestionRef(tt.arg, tt.scope)
			if err == nil {
				t.Fatalf("parseQuestionRef(%q, %q): want an error", tt.arg, tt.scope)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Fatalf("error is %T (%v), want *usageError", err, err)
			}
		})
	}
}

// TestParseQuestionRefNoScopeHint tests that the "which task?" error shows
// both accepted local forms — the message is the only place a user learns them.
func TestParseQuestionRefNoScopeHint(t *testing.T) {
	_, err := parseQuestionRef("Q1", "")
	if err == nil {
		t.Fatal("want an error for Q1 without a scope")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--task") || !strings.Contains(msg, "/Q1") {
		t.Errorf("error %q should show both --task and <id>/Q1 forms", msg)
	}
}

// TestPickOrdinal tests the ordinal → global id match over a fetched thread
// list, including the "no such ordinal" message.
func TestPickOrdinal(t *testing.T) {
	ids := map[int]int64{1: 372, 2: 410}

	got, err := pickOrdinal(ids, questionRef{Scope: "799", Ordinal: 2}, "задачи #799")
	if err != nil {
		t.Fatalf("pickOrdinal error: %v", err)
	}
	if got != "410" {
		t.Errorf("pickOrdinal = %q, want 410", got)
	}

	_, err = pickOrdinal(ids, questionRef{Scope: "799", Ordinal: 7}, "задачи #799")
	if err == nil {
		t.Fatal("want an error for a missing ordinal")
	}
	if err.Error() != "у задачи #799 нет вопроса Q7" {
		t.Errorf("error = %q, want %q", err.Error(), "у задачи #799 нет вопроса Q7")
	}
}

// TestResolveQuestionRefGlobalSkipsLookup tests that a global id resolves
// without touching the daemon — connect() is disabled under go test, so a
// lookup here would fail the test outright.
func TestResolveQuestionRefGlobalSkipsLookup(t *testing.T) {
	got, err := resolveQuestionRef("372", 0)
	if err != nil {
		t.Fatalf("resolveQuestionRef(372) error: %v", err)
	}
	if got != "372" {
		t.Errorf("resolveQuestionRef(372) = %q, want 372", got)
	}
}

// TestTaskThreadCommandsAcceptLocalRefs tests that reply and answer take a
// local reference: parsing must succeed and only then fail on connect(), which
// is disabled under go test. A usageError here would mean the form was
// rejected outright.
func TestTaskThreadCommandsAcceptLocalRefs(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"reply global", []string{"372", "text"}},
		{"reply inline scope", []string{"799/Q1", "text"}},
		{"reply flag scope", []string{"--task", "799", "Q1", "text"}},
		{"reply flag scope lowercase", []string{"--task", "799", "q1", "text"}},
		{"answer global", []string{"372", "text"}},
		{"answer inline scope", []string{"799/Q1", "text"}},
		{"answer flag scope", []string{"--task", "799", "Q1", "text"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskReplyCmd()
			if strings.HasPrefix(tt.name, "answer") {
				cmd = newTaskAnswerCmd()
			}
			cmd.SetArgs(tt.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("want the connect() error under go test")
			}
			var usageErr *usageError
			if errors.As(err, &usageErr) {
				t.Fatalf("reference %v rejected: %v", tt.args, err)
			}
			if !strings.Contains(err.Error(), "connect()") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestTaskThreadCommandsRejectScopelessOrdinal tests that a bare Q1 with no
// task named is a usage error naming both local forms.
func TestTaskThreadCommandsRejectScopelessOrdinal(t *testing.T) {
	for _, newCmd := range []func() *cobra.Command{newTaskReplyCmd, newTaskAnswerCmd} {
		cmd := newCmd()
		cmd.SetArgs([]string{"Q1", "text"})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		var usageErr *usageError
		if !errors.As(err, &usageErr) {
			t.Fatalf("error is %T (%v), want *usageError", err, err)
		}
		if !strings.Contains(err.Error(), "--task") {
			t.Errorf("error %q should show the --task form", err)
		}
	}
}

// TestAgentReplyAcceptsLocalRefs tests the role-scoped forms of agent reply
// and agent answer.
func TestAgentReplyAcceptsLocalRefs(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"reply global", []string{"372", "text"}},
		{"reply inline role", []string{"sre/Q1", "text"}},
		{"reply inline role lowercase", []string{"sre/q1", "text"}},
		{"reply agent flag", []string{"--agent", "sre", "Q1", "text"}},
		{"reply agent flag bare number", []string{"--agent", "sre", "2", "text"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newAgentReplyCmd()
			cmd.SetArgs(tt.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			var usageErr *usageError
			if errors.As(err, &usageErr) {
				t.Fatalf("reference %v rejected: %v", tt.args, err)
			}
			if err == nil || !strings.Contains(err.Error(), "connect()") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestResolveAgentQuestionRefGlobalSkipsLookup mirrors the task-side check for
// role threads.
func TestResolveAgentQuestionRefGlobalSkipsLookup(t *testing.T) {
	got, err := resolveAgentQuestionRef("372", "")
	if err != nil {
		t.Fatalf("resolveAgentQuestionRef(372) error: %v", err)
	}
	if got != "372" {
		t.Errorf("resolveAgentQuestionRef(372) = %q, want 372", got)
	}
}

// TestResolveAgentQuestionRefNoAgent tests that a bare ordinal outside an
// agent session is a usageError naming both explicit forms — never a guess.
func TestResolveAgentQuestionRefNoAgent(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "")
	t.Setenv("TMUX", "")
	_, err := resolveAgentQuestionRef("Q1", "")
	if err == nil {
		t.Fatal("want an error for Q1 with no agent to infer")
	}
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("error is %T (%v), want *usageError", err, err)
	}
	if !strings.Contains(err.Error(), "--agent") || !strings.Contains(err.Error(), "/Q1") {
		t.Errorf("error %q should show both explicit forms", err)
	}
}
