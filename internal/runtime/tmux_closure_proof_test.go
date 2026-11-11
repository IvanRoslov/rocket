package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A failed probe must never prove that Creative agents stopped. Run the real
// command adapter against an isolated executable, not the user's tmux server.
func TestTmuxClosureProofFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, stderr string
		absent       bool
	}{
		{"silent command failure", "", false},
		{"permission denied", "error connecting to /tmp/tmux-test/default (Permission denied)", false},
		{"missing socket", "error connecting to /tmp/tmux-test/default (No such file or directory)", true},
		{"no server", "no server running on /tmp/tmux-test/default", true},
		{"connection refused", "error connecting to /tmp/tmux-test/default (Connection refused)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte("#!/bin/sh\nprintf '%s' \"$ROCKET_TEST_TMUX_ERROR\" >&2\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			t.Setenv("ROCKET_TEST_TMUX_ERROR", tc.stderr)
			rt := NewTmux()
			names, err := rt.List(context.Background())
			if tc.absent {
				if err != nil || len(names) != 0 {
					t.Fatalf("absent server: %v %v", names, err)
				}
			} else if err == nil {
				t.Fatal("failed inventory reported empty success")
			}
			err = rt.Destroy(context.Background(), Handle{Name: "rocket-test-closure-proof"})
			if tc.absent && err != nil {
				t.Fatalf("absent destroy: %v", err)
			}
			if !tc.absent && err == nil {
				t.Fatal("unconfirmed destroy reported success")
			}
		})
	}
}
