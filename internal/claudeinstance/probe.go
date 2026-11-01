package claudeinstance

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// SystemProbe reads process metadata only: never argv, environment or input.
type SystemProbe struct{}

func (SystemProbe) Process(ctx context.Context, pid int) (Process, bool, error) {
	if pid <= 0 {
		return Process{}, false, fmt.Errorf("invalid PID")
	}
	cmd := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "pid=,ppid=,lstart=,comm=")
	cmd.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok && e.ExitCode() == 1 && len(out) == 0 {
			return Process{}, false, nil
		}
		return Process{}, false, fmt.Errorf("process metadata lookup failed: %w", err)
	}
	p, err := parseProcess(string(out))
	return p, err == nil, err
}
func (p SystemProbe) Pane(ctx context.Context, session string) (Pane, bool, error) {
	out, err := exec.CommandContext(ctx, "tmux", "list-panes", "-t", "="+session, "-F", "#{session_name}:#{window_id}.#{pane_id} #{pane_pid}").Output()
	if err != nil {
		return Pane{}, false, fmt.Errorf("pane metadata lookup failed: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 1 {
		return Pane{}, false, fmt.Errorf("ambiguous pane")
	}
	f := strings.Fields(lines[0])
	if len(f) != 2 {
		return Pane{}, false, fmt.Errorf("invalid pane metadata")
	}
	pid, err := strconv.Atoi(f[1])
	if err != nil {
		return Pane{}, false, err
	}
	process, exists, err := p.Process(ctx, pid)
	if err != nil || !exists {
		return Pane{}, false, err
	}
	return Pane{Identity: f[0], PID: pid, Start: process.Start}, true, nil
}
func parseProcess(raw string) (Process, error) {
	if strings.Contains(strings.TrimSpace(raw), "\n") {
		return Process{}, fmt.Errorf("multiple processes")
	}
	f := strings.Fields(raw)
	if len(f) < 8 {
		return Process{}, fmt.Errorf("invalid process metadata")
	}
	pid, e1 := strconv.Atoi(f[0])
	ppid, e2 := strconv.Atoi(f[1])
	start, e3 := ParseProcessStart(strings.Join(f[2:7], " "))
	if e1 != nil || e2 != nil || e3 != nil || pid <= 0 {
		return Process{}, fmt.Errorf("invalid process metadata")
	}
	return Process{PID: pid, PPID: ppid, Start: start, Name: filepath.Base(strings.Join(f[7:], " "))}, nil
}
