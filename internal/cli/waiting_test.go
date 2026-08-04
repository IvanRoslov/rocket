package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestRenderTaskBoardWaitingTerminal: a task whose session is stalled on
// interactive input is marked on its board line; one that isn't stays clean.
func TestRenderTaskBoardWaitingTerminal(t *testing.T) {
	board := map[string][]taskRow{
		"in_progress": {
			{ID: 1, Title: "Stalled", Status: "in_progress", SessionID: "orch-1", WaitingTerminal: true},
			{ID: 2, Title: "Moving", Status: "in_progress", SessionID: "orch-2"},
		},
	}

	var buf bytes.Buffer
	renderTaskBoard(board, &buf, false)
	out := buf.String()

	lines := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "#1 ") {
			lines["stalled"] = line
		}
		if strings.HasPrefix(line, "#2 ") {
			lines["moving"] = line
		}
	}
	if !strings.Contains(lines["stalled"], waitingTerminalMark) {
		t.Errorf("stalled task line = %q, want the %q marker", lines["stalled"], waitingTerminalMark)
	}
	if strings.Contains(lines["moving"], waitingTerminalMark) {
		t.Errorf("moving task line = %q, want no marker", lines["moving"])
	}
}

// TestRenderStatusWaitingTerminal: `rocket status` marks both the
// orchestrator line and a worker row when the session waits on a keystroke.
func TestRenderStatusWaitingTerminal(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	sessions := []sessionRow{
		{ID: "demo-orch", Kind: "orchestrator", State: "running", Activity: "waiting_input",
			WaitingTerminal: true, CreatedAt: now.Add(-2 * time.Hour).Unix()},
		{ID: "wk-stalled", Kind: "worker", State: "running", Activity: "waiting_input",
			WaitingTerminal: true, CreatedAt: now.Add(-30 * time.Minute).Unix()},
		{ID: "wk-busy", Kind: "worker", State: "running", Activity: "editing foo.go",
			CreatedAt: now.Add(-5 * time.Minute).Unix()},
	}

	var buf bytes.Buffer
	renderStatus("demo-feature", sessions, &buf, now)
	out := buf.String()

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "orchestrator:"), strings.HasPrefix(line, "wk-stalled"):
			if !strings.Contains(line, waitingTerminalMark) {
				t.Errorf("line %q, want the %q marker", line, waitingTerminalMark)
			}
		case strings.HasPrefix(line, "wk-busy"):
			if strings.Contains(line, waitingTerminalMark) {
				t.Errorf("line %q, want no marker", line)
			}
		}
	}
}
