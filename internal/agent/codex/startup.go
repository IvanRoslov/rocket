package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// turnStartedMarker is a cheap prefilter: only lines containing it are
// decoded.
var turnStartedMarker = []byte(`"task_started"`)

// HasStartedTurn implements agent.StartupProber: it reports whether the
// session's journal (the newest one recorded for ref.WorktreePath) holds an
// event_msg with payload.type task_started — i.e. Codex got past its start
// screens and ran a turn. No journal yet is agent.ErrNoSignal.
//
// With ref.Since set, only a task_started stamped at or after it counts:
// after a restore the newest journal in the worktree is the previous run's
// until the new process writes its own, and its turns say nothing about
// the new process. A record without a readable timestamp never counts then.
//
// The file is read from the start and the scan stops at the first match,
// which sits right after the first user message; the monitor stops asking
// once this is true.
func (c *Codex) HasStartedTurn(ctx context.Context, ref agent.ActivityRef) (bool, error) {
	path, _, err := findMatchingSession(ref.WorktreePath)
	if err != nil {
		return false, agent.ErrNoSignal
	}
	f, err := os.Open(path)
	if err != nil {
		return false, agent.ErrNoSignal
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, turnStartedMarker) {
			continue
		}
		var rec struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				Type string `json:"type"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Type != "event_msg" || rec.Payload.Type != "task_started" {
			continue
		}
		if ref.Since.IsZero() {
			return true, nil
		}
		if ts, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil && !ts.Before(ref.Since) {
			return true, nil
		}
	}
	return false, scanner.Err()
}
