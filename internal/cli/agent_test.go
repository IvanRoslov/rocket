package cli

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

// ctor keeps the table below readable: every command constructor has the
// same shape.
func ctor(f func() *cobra.Command) func() *cobra.Command { return f }

func TestAgentCommandUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		cmd  func() *cobra.Command
		args []string
	}{
		{"add without args", ctor(newAgentAddCmd), []string{"--dir", "/tmp"}},
		{"add with two ids", ctor(newAgentAddCmd), []string{"sre", "triage"}},
		{"edit without flags", ctor(newAgentEditCmd), []string{"sre"}},
		{"edit without id", ctor(newAgentEditCmd), []string{"--dir", "/tmp"}},
		{"ls with args", ctor(newAgentLsCmd), []string{"extra"}},
		{"show without id", ctor(newAgentShowCmd), []string{}},
		{"rm with two ids", ctor(newAgentRmCmd), []string{"a", "b"}},
		{"enable without id", ctor(newAgentEnableCmd), []string{}},
		{"disable without id", ctor(newAgentDisableCmd), []string{}},
		{"start without id", ctor(newAgentStartCmd), []string{}},
		{"stop with two ids", ctor(newAgentStopCmd), []string{"a", "b"}},
		{"attach without id", ctor(newAgentAttachCmd), []string{}},
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

func TestFormatHelpers(t *testing.T) {
	if got := dashIfEmpty(""); got != "-" {
		t.Errorf("dashIfEmpty(\"\") = %q", got)
	}
	if got := dashIfEmpty("sre"); got != "sre" {
		t.Errorf("dashIfEmpty(sre) = %q", got)
	}
	if got := dashIfZero(float64(0)); got != "-" {
		t.Errorf("dashIfZero(0) = %q", got)
	}
	if got := dashIfZero(float64(45)); got != "45" {
		t.Errorf("dashIfZero(45) = %q", got)
	}
	if got := sessionCell(true); got != "live" {
		t.Errorf("sessionCell(true) = %q", got)
	}
	if got := sessionCell(false); got != "-" {
		t.Errorf("sessionCell(false) = %q", got)
	}
	if got := questionsCell(float64(2), float64(1)); got != "2 (1 ждут)" {
		t.Errorf("questionsCell(2, 1) = %q", got)
	}
	if got := questionsCell(float64(0), float64(0)); got != "-" {
		t.Errorf("questionsCell(0, 0) = %q", got)
	}
}
