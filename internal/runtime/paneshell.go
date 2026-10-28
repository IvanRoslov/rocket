package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// shellNames are basenames (leading '-' stripped, as tmux/login shells often
// prefix argv[0] with a hyphen) that count as "no agent process running" for
// the pane-probe exited check.
var shellNames = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true,
}

// PaneOnlyShell reports whether every process attached to tmuxName's pane(s)
// is a bare shell, i.e. the agent has exited and nothing replaced it. It is
// shared by the activity monitor and by Manager.Restore. Returns an error if
// the check could not be performed (tmux/ps failed); callers should treat an
// error as "inconclusive", not as "exited".
func PaneOnlyShell(ctx context.Context, tmuxName string) (bool, error) {
	out, err := exec.CommandContext(ctx, "tmux", "list-panes", "-t", "="+tmuxName, "-F", "#{pane_tty}").Output()
	if err != nil {
		return false, fmt.Errorf("list-panes: %w", err)
	}

	ttys := nonEmptyLines(string(out))
	if len(ttys) == 0 {
		return false, fmt.Errorf("no panes found for %q", tmuxName)
	}

	for _, tty := range ttys {
		psOut, err := exec.CommandContext(ctx, "ps", "-t", tty, "-o", "comm=").Output()
		if err != nil {
			return false, fmt.Errorf("ps -t %s: %w", tty, err)
		}
		for _, comm := range nonEmptyLines(string(psOut)) {
			base := filepath.Base(comm)
			base = strings.TrimPrefix(base, "-")
			if !shellNames[base] {
				return false, nil
			}
		}
	}
	return true, nil
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
