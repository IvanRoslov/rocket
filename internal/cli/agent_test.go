package cli

import (
	"errors"
	"reflect"
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
		{"add without args", ctor(newAgentAddCmd), []string{"--project", "p", "--prompt-file", "f"}},
		{"add without project", ctor(newAgentAddCmd), []string{"sre", "--prompt-file", "f"}},
		{"add without prompt file", ctor(newAgentAddCmd), []string{"sre", "--project", "p"}},
		{"ls with args", ctor(newAgentLsCmd), []string{"extra"}},
		{"show without id", ctor(newAgentShowCmd), []string{}},
		{"rm with two ids", ctor(newAgentRmCmd), []string{"a", "b"}},
		{"enable without id", ctor(newAgentEnableCmd), []string{}},
		{"disable without id", ctor(newAgentDisableCmd), []string{}},
		{"wake without id", ctor(newAgentWakeCmd), []string{}},
		{"wake with too many args", ctor(newAgentWakeCmd), []string{"sre", "hi", "extra"}},
		{"state set without state", ctor(newAgentStateSetCmd), []string{"issue:acme/platform#1"}},
		{"state set with bad ref", ctor(newAgentStateSetCmd), []string{"issue", "taken", "--agent", "sre"}},
		{"state ls with args", ctor(newAgentStateLsCmd), []string{"extra"}},
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

func TestParseWatch(t *testing.T) {
	cases := []struct {
		spec    string
		want    map[string]any
		wantErr bool
	}{
		{spec: "acme/platform", want: map[string]any{"repo": "acme/platform"}},
		{
			spec: "acme/platform,label=bug,mention-only",
			want: map[string]any{"repo": "acme/platform", "labels": []string{"bug"}, "mention_only": true},
		},
		{
			spec: "acme/platform,label=bug,label=ops",
			want: map[string]any{"repo": "acme/platform", "labels": []string{"bug", "ops"}},
		},
		{spec: "platform", wantErr: true},
		{spec: "", wantErr: true},
		{spec: "acme/platform,label=", wantErr: true},
		{spec: "acme/platform,whatever", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := parseWatch(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseWatch(%q) = %+v, want error", tc.spec, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseWatch(%q): %v", tc.spec, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseWatch(%q) = %+v, want %+v", tc.spec, got, tc.want)
			}
		})
	}
}

func TestRoleFromSessionID(t *testing.T) {
	cases := map[string]string{
		"sre-run-3":          "sre",
		"issue-triage-run-1": "issue-triage",
		"task-639-orch":      "",
		"-run-1":             "",
		"":                   "",
	}
	for in, want := range cases {
		if got := roleFromSessionID(in); got != want {
			t.Errorf("roleFromSessionID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseUntil(t *testing.T) {
	if got, err := parseUntil(""); err != nil || got != 0 {
		t.Errorf("parseUntil(\"\") = %d, %v, want 0, nil", got, err)
	}

	day, err := parseUntil("2026-08-15")
	if err != nil {
		t.Fatalf("parseUntil(date): %v", err)
	}
	rfc, err := parseUntil("2026-08-15T00:00:00Z")
	if err != nil {
		t.Fatalf("parseUntil(rfc3339): %v", err)
	}
	if day != rfc {
		t.Errorf("date form = %d, RFC3339 form = %d, want equal", day, rfc)
	}

	if _, err := parseUntil("15.08.2026"); err == nil {
		t.Errorf("parseUntil(bad format): want error")
	}
}

func TestSplitItemRef(t *testing.T) {
	kind, ref, err := splitItemRef("issue:acme/platform#12")
	if err != nil {
		t.Fatalf("splitItemRef: %v", err)
	}
	if kind != "issue" || ref != "acme/platform#12" {
		t.Errorf("kind, ref = %q, %q", kind, ref)
	}

	for _, bad := range []string{"issue", ":ref", "task:", ""} {
		if _, _, err := splitItemRef(bad); err == nil {
			t.Errorf("splitItemRef(%q): want error", bad)
		}
	}
}

func TestResolveRole(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "")

	if _, err := resolveRole(""); err == nil {
		t.Errorf("resolveRole with no flag and no session: want error")
	}
	if role, err := resolveRole("sre"); err != nil || role != "sre" {
		t.Errorf("resolveRole(\"sre\") = %q, %v", role, err)
	}

	t.Setenv("ROCKET_SESSION_ID", "sre-run-2")
	if role, err := resolveRole(""); err != nil || role != "sre" {
		t.Errorf("resolveRole from session = %q, %v, want sre", role, err)
	}
	if role, err := resolveRole("triage"); err != nil || role != "triage" {
		t.Errorf("explicit flag must win, got %q, %v", role, err)
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := dashIfEmpty(""); got != "-" {
		t.Errorf("dashIfEmpty(\"\") = %q", got)
	}
	if got := dashIfZero(float64(0)); got != "-" {
		t.Errorf("dashIfZero(0) = %q", got)
	}
	if got := dashIfZero(float64(45)); got != "45" {
		t.Errorf("dashIfZero(45) = %q", got)
	}
	if got := formatUnix(float64(0)); got != "-" {
		t.Errorf("formatUnix(0) = %q", got)
	}
	if got := formatUnix(float64(1800000000)); got == "-" || got == "" {
		t.Errorf("formatUnix(1800000000) = %q, want a date", got)
	}
	if got := labelsSuffix([]any{"bug", "ops"}); got != " labels=bug|ops" {
		t.Errorf("labelsSuffix = %q", got)
	}
	if got := labelsSuffix(nil); got != "" {
		t.Errorf("labelsSuffix(nil) = %q", got)
	}
	if got := mentionSuffix(true); got != " mention-only" {
		t.Errorf("mentionSuffix(true) = %q", got)
	}
	if got := mentionSuffix(false); got != "" {
		t.Errorf("mentionSuffix(false) = %q", got)
	}
}

func TestAgentDoneOutsideAnInstanceIsAUsageError(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "myfeat-mytask")

	cmd := newAgentDoneCmd()
	cmd.SetArgs(nil)
	err := cmd.Execute()
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("error = %v, want a usage error (done is instance-only)", err)
	}
}

func TestAgentDoneRejectsArguments(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "sre-run-3")

	cmd := newAgentDoneCmd()
	cmd.SetArgs([]string{"sre"})
	err := cmd.Execute()
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("error = %v, want a usage error", err)
	}
}
