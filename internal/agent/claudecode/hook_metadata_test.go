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
			os.WriteFile(filepath.Join(dir, "curl"), []byte(curl), 0700)
			os.WriteFile(filepath.Join(dir, "ps"), []byte("#!/bin/sh\nprintf '%s\\n' 'Sun Nov 1 00:00:00 2026'\n"), 0700)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sh", "-c", activityHookScript, "hook", "ready")
			cmd.Env = []string{"PATH=" + dir + ":" + os.Getenv("PATH"), "ROCKET_SOCKET=/tmp/fixture.sock", "ROCKET_SESSION_ID=s", "ROCKET_CLAUDE_LAUNCH=g", "HOOK_CAPTURE=" + capture}
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
				t.Fatal(e)
			}
			if strings.Contains(string(raw), "SECRET_") {
				t.Fatal("input content leaked into metadata")
			}
			var req struct {
				Claude claudeinstance.HookEvent `json:"claude"`
			}
			if e = json.Unmarshal(raw, &req); e != nil || req.Claude.HookEventName != "Stop" || req.Claude.ClaudeSessionID != "target" || req.Claude.LaunchGeneration != "g" || req.Claude.ProducerPID <= 0 || req.Claude.At <= 0 {
				t.Fatalf("missing qualified metadata %s %v", raw, e)
			}
		})
	}
}
