package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// TestInboxCommandUsageErrors covers the argument-shape errors. `rocket inbox
// <word>` is not among them: cobra reports it as an unknown subcommand of
// `inbox`, which is the clearer message anyway.
func TestInboxCommandUsageErrors(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "sre")

	cases := []struct {
		name string
		cmd  func() *cobra.Command
		args []string
	}{
		{"next with args", ctor(newInboxNextCmd), []string{"extra"}},
		{"peek without id", ctor(newInboxPeekCmd), []string{}},
		{"peek with a non-numeric id", ctor(newInboxPeekCmd), []string{"latest"}},
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

// TestResolveAgentID covers the three ways an inbox command learns whose inbox
// it is looking at.
func TestResolveAgentID(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "")
	t.Setenv("TMUX", "")

	if _, err := resolveAgentID(""); err == nil {
		t.Error("resolveAgentID outside a session and outside tmux: want an error")
	}
	if id, err := resolveAgentID("sre"); err != nil || id != "sre" {
		t.Errorf("resolveAgentID(\"sre\") = %q, %v", id, err)
	}

	t.Setenv("ROCKET_SESSION_ID", "triage")
	if id, err := resolveAgentID(""); err != nil || id != "triage" {
		t.Errorf("resolveAgentID from the session = %q, %v, want triage", id, err)
	}
	if id, err := resolveAgentID("sre"); err != nil || id != "sre" {
		t.Errorf("the explicit flag must win, got %q, %v", id, err)
	}
}

// TestInboxOutsideAnAgentSessionIsAUsageError guards the message an operator
// sees when they run `rocket inbox` from their own shell.
func TestInboxOutsideAnAgentSessionIsAUsageError(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "")
	t.Setenv("TMUX", "")

	cmd := newInboxCmd()
	cmd.SetArgs(nil)
	err := cmd.Execute()

	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("error = %v, want a usage error", err)
	}
	if !strings.Contains(usageErr.message, "--agent") {
		t.Errorf("message = %q, want it to mention --agent", usageErr.message)
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("one\ntwo"); got != "one" {
		t.Errorf("firstLine(multiline) = %q, want one", got)
	}
	if got := firstLine("short"); got != "short" {
		t.Errorf("firstLine(short) = %q", got)
	}
	long := strings.Repeat("x", 100)
	got := firstLine(long)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 73 {
		t.Errorf("firstLine(long) = %q, want it truncated to 72 runes plus an ellipsis", got)
	}
}

func TestAge(t *testing.T) {
	now := time.Now()
	cases := map[string]any{
		"just now": float64(now.Unix()),
		"5m":       float64(now.Add(-5 * time.Minute).Unix()),
		"3h":       float64(now.Add(-3 * time.Hour).Unix()),
		"2d":       float64(now.Add(-48 * time.Hour).Unix()),
		"-":        float64(0),
	}
	for want, in := range cases {
		if got := age(in); got != want {
			t.Errorf("age(%v) = %q, want %q", in, got, want)
		}
	}
}
