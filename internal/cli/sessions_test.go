package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestHumanAgeBoundaries(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"just now", 0, "0s"},
		{"59 seconds", 59 * time.Second, "59s"},
		{"60 seconds rolls to minutes", 60 * time.Second, "1m"},
		{"59 minutes", 59*time.Minute + 59*time.Second, "59m"},
		{"60 minutes rolls to hours", 60 * time.Minute, "1h"},
		{"two hours", 2 * time.Hour, "2h"},
		{"23 hours", 23*time.Hour + 59*time.Minute, "23h"},
		{"24 hours rolls to days", 24 * time.Hour, "1d"},
		{"three days", 3 * 24 * time.Hour, "3d"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			created := now.Add(-tc.ago).Unix()
			got := humanAge(created, now)
			if got != tc.want {
				t.Errorf("humanAge(%v ago) = %q, want %q", tc.ago, got, tc.want)
			}
		})
	}
}

func TestRenderSessionsColumnsAndDash(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	sessions := []sessionRow{
		{
			ID:        "demo-probe",
			Kind:      "worker",
			ProjectID: "demo",
			RepoID:    "scratch",
			State:     "running",
			Activity:  "editing foo.go",
			CreatedAt: now.Add(-5 * time.Minute).Unix(),
		},
		{
			ID:        "demo-orch",
			Kind:      "orchestrator",
			ProjectID: "demo",
			RepoID:    "scratch",
			State:     "spawning",
			Activity:  "",
			CreatedAt: now.Add(-2 * time.Hour).Unix(),
		},
	}

	var buf bytes.Buffer
	renderSessions(sessions, &buf, now)
	out := buf.String()

	for _, col := range []string{"SESSION", "KIND", "PROJECT", "REPO", "STATE", "ACTIVITY", "AGE"} {
		if !strings.Contains(out, col) {
			t.Errorf("output missing header column %q; got:\n%s", col, out)
		}
	}

	for _, want := range []string{
		"demo-probe", "worker", "demo", "scratch", "running", "editing foo.go", "5m",
		"demo-orch", "orchestrator", "spawning", "2h",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}

	// Empty activity renders as a lone "-" field on the orchestrator's row.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	found := false
	for _, l := range lines {
		if strings.Contains(l, "demo-orch") {
			found = true
			fields := strings.Fields(l)
			hasDash := false
			for _, f := range fields {
				if f == "-" {
					hasDash = true
				}
			}
			if !hasDash {
				t.Errorf("expected empty activity to render as '-' in line: %q", l)
			}
		}
	}
	if !found {
		t.Fatal("did not find demo-orch row")
	}
}
