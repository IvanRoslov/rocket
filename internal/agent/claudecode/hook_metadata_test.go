package claudecode

import (
	"context"
	"encoding/json"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHookExtractsOnlyBoundedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        bool
	}{
		{"valid", `{"session_id":"target","hook_event_name":"Stop","prompt":"SECRET_PROMPT","tool_input":{"secret":"SECRET_TOOL"},"message":"SECRET_MESSAGE"}`, true},
		{"malformed", "{", false}, {"missing", "", false}, {"oversize", strings.Repeat("x", (16<<10)+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			capture := filepath.Join(dir, "capture.json")
			curl := "#!/bin/sh\nprev=''\nfor arg in \"$@\"; do\n if [ \"$prev\" = '-d' ]; then printf '%s' \"$arg\" > \"$HOOK_CAPTURE\"; fi\n prev=\"$arg\"\ndone\n"
			mustWriteHookFixture(t, filepath.Join(dir, "curl"), curl)
			mustWriteHookFixture(t, filepath.Join(dir, "ps"), "#!/bin/sh\nprintf '%s\\n' 'Sun Nov 1 00:00:00 2026'\n")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			// Metadata extraction is deterministic here: subprocess startup can
			// legitimately exceed the production best-effort0.5s ps budget
			// under contention. Separate tests exercise real process failure.
			probe := `import os
from types import SimpleNamespace
def run(args, **kwargs):
    assert args == ["ps", "-p", str(os.getppid()), "-o", "lstart="]
    assert kwargs["timeout"] == 0.5 and kwargs["check"] and kwargs["capture_output"] and kwargs["text"]
    assert kwargs["env"]["TZ"] == "UTC" and kwargs["env"]["LC_ALL"] == "C"
    with open(os.environ["HOOK_PS_CALL"], "w") as out:
        out.write(args[2])
    return SimpleNamespace(stdout="Sun Nov 1 00:00:00 2026\n")
`
			mustWriteHookFixture(t, filepath.Join(dir, "subprocess.py"), probe)
			psCall := filepath.Join(dir, "ps-call")
			cmd := exec.CommandContext(ctx, "sh", "-c", activityHookScript, "hook", "ready")
			cmd.Env = []string{"PATH=" + dir + ":" + os.Getenv("PATH"), "ROCKET_SOCKET=/tmp/fixture.sock", "ROCKET_SESSION_ID=s", "ROCKET_CLAUDE_LAUNCH=g", "HOOK_CAPTURE=" + capture, "PYTHONPATH=" + dir, "HOOK_PS_CALL=" + psCall}
			cmd.Stdin = strings.NewReader(tc.input)
			out, runErr := cmd.CombinedOutput()
			if e := runErr; e != nil {
				t.Fatalf("hook must be best effort: %v %s", e, out)
			}
			raw, e := os.ReadFile(capture)
			if !tc.want {
				if e == nil {
					t.Fatal("invalid stdin emitted activity")
				}
				return
			}
			if e != nil {
				t.Fatalf("missing metadata capture: %v; output=%s", e, out)
			}
			if _, e := os.ReadFile(psCall); e != nil {
				t.Fatalf("deterministic metadata process fixture not invoked: %v", e)
			}
			if strings.Contains(string(raw), "SECRET_") {
				t.Fatal("input content leaked into metadata")
			}
			var req struct {
				Claude claudeinstance.HookEvent `json:"claude"`
			}
			if e = json.Unmarshal(raw, &req); e != nil || req.Claude.HookEventName != "Stop" || req.Claude.ClaudeSessionID != "target" || req.Claude.LaunchGeneration != "g" || req.Claude.ProducerPID <= 0 || !req.Claude.ProducerStart.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) || req.Claude.At <= 0 {
				t.Fatalf("missing qualified metadata %s %v", raw, e)
			}
		})
	}
}

func mustWriteHookFixture(t *testing.T, path, body string) {
	t.Helper()
	if e := os.WriteFile(path, []byte(body), 0700); e != nil {
		t.Fatal(e)
	}
}

func TestHookDropsFailedProcessMetadataWithinBounds(t *testing.T) {
	for _, which := range []string{"nonzero", "malformed start", "timeout"} {
		t.Run(which, func(t *testing.T) {
			dir := t.TempDir()
			capture := filepath.Join(dir, "capture")
			body := "#!/bin/sh\nexit 1\n"
			if which == "malformed start" {
				body = "#!/bin/sh\nprintf '%s\\n' 'invalid process start'\n"
			}
			if which == "timeout" {
				body = "#!/bin/sh\nexec sleep 2\n"
			}
			mustWriteHookFixture(t, filepath.Join(dir, "ps"), body)
			mustWriteHookFixture(t, filepath.Join(dir, "curl"), "#!/bin/sh\nprintf unexpected > \"$HOOK_CAPTURE\"\n")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sh", "-c", activityHookScript, "hook", "ready")
			cmd.Env = []string{"PATH=" + dir + ":" + os.Getenv("PATH"), "ROCKET_SOCKET=/tmp/fixture.sock", "ROCKET_SESSION_ID=s", "ROCKET_CLAUDE_LAUNCH=g", "HOOK_CAPTURE=" + capture}
			cmd.Stdin = strings.NewReader(`{"session_id":"target","hook_event_name":"Stop","prompt":"SECRET_PROMPT"}`)
			start := time.Now()
			out, e := cmd.CombinedOutput()
			elapsed := time.Since(start)
			if e != nil || len(out) != 0 || elapsed >= 2*time.Second {
				t.Fatalf("best-effort bound: error=%v elapsed=%s output=%s", e, elapsed, out)
			}
			if _, e := os.Stat(capture); !os.IsNotExist(e) {
				t.Fatalf("failed metadata emitted activity: %v", e)
			}
		})
	}
}
