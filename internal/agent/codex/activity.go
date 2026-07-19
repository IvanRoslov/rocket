package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
)

// freshWindow is how recent a session file's mtime must be for the session
// to be considered actively working, regardless of the last record's type.
// Same threshold as claudecode.
const freshWindow = 30 * time.Second

// tailReadSize is how many trailing bytes of the session file to read when
// looking for the last JSONL record. Same as claudecode.
const tailReadSize = 64 * 1024

// sessionsDirs returns the two date-sharded directories under
// $CODEX_HOME/sessions/ that could contain a session started "today" in
// local time: today's dir and yesterday's, to cover a session that started
// just before local midnight and is still being polled just after (per
// recon: codex shards sessions by YYYY/MM/DD).
func sessionsDirs(now time.Time) []string {
	base := codexHome()
	if base == "" {
		return nil
	}
	dirs := make([]string, 0, 2)
	for _, day := range []time.Time{now, now.Add(-24 * time.Hour)} {
		dirs = append(dirs, filepath.Join(base, "sessions", day.Format("2006"), day.Format("01"), day.Format("02")))
	}
	return dirs
}

// sessionMetaRecord is the shape of a session_meta JSONL line (always the
// first line of a codex session file, per recon).
type sessionMetaRecord struct {
	Type    string `json:"type"`
	Payload struct {
		Cwd string `json:"cwd"`
	} `json:"payload"`
}

// sessionCwd reads the first line of the session file at path and returns
// its session_meta payload.cwd, if the first line is in fact a session_meta
// record.
func sessionCwd(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return "", false
	}

	var rec sessionMetaRecord
	if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
		return "", false
	}
	if rec.Type != "session_meta" {
		return "", false
	}
	return rec.Payload.Cwd, true
}

// findMatchingSession scans today's and yesterday's session directories for
// *.jsonl files whose session_meta.payload.cwd matches worktreePath, and
// returns the path and mtime of the newest match. Returns agent.ErrNoSignal
// if no matching file is found (missing dirs, no cwd match, or no session
// files at all).
func findMatchingSession(worktreePath string) (string, time.Time, error) {
	want := worktreePath
	if r, err := filepath.EvalSymlinks(worktreePath); err == nil {
		want = r
	}
	want = filepath.Clean(want)

	var bestPath string
	var bestMTime time.Time
	for _, dir := range sessionsDirs(time.Now()) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			cwd, ok := sessionCwd(path)
			if !ok {
				continue
			}
			cwdResolved := cwd
			if r, err := filepath.EvalSymlinks(cwd); err == nil {
				cwdResolved = r
			}
			cwdResolved = filepath.Clean(cwdResolved)
			if cwdResolved != want {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if bestPath == "" || info.ModTime().After(bestMTime) {
				bestPath = path
				bestMTime = info.ModTime()
			}
		}
	}
	if bestPath == "" {
		return "", time.Time{}, agent.ErrNoSignal
	}
	return bestPath, bestMTime, nil
}

// lastNonEmptyLine reads (at most) the trailing tailReadSize bytes of the
// file at path and returns the last non-empty line. Same approach as
// claudecode.lastNonEmptyLine.
func lastNonEmptyLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}

	start := int64(0)
	if info.Size() > tailReadSize {
		start = info.Size() - tailReadSize
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var last string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			last = line
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return last, nil
}

// isErrorRecord reports whether the last JSONL record looks like an
// error/failed-turn signal. Recon's live sample only ever showed
// event_msg{token_count, task_complete} on a clean turn, and explicitly
// flagged the error-event taxonomy as unverified — so this is a
// conservative best-effort check (a nested event payload "type" containing
// "error", or a top-level "type":"error"), not an exhaustive list. Anything
// that doesn't match falls through to Ready, per the binding instruction
// ("unknown types → Ready").
func isErrorRecord(raw string, top map[string]json.RawMessage) bool {
	if typeRaw, ok := top["type"]; ok {
		var t string
		if err := json.Unmarshal(typeRaw, &t); err == nil {
			if t == "error" {
				return true
			}
			if t == "event_msg" {
				if payloadRaw, ok := top["payload"]; ok {
					var payload map[string]json.RawMessage
					if err := json.Unmarshal(payloadRaw, &payload); err == nil {
						if ptRaw, ok := payload["type"]; ok {
							var pt string
							if err := json.Unmarshal(ptRaw, &pt); err == nil && strings.Contains(strings.ToLower(pt), "error") {
								return true
							}
						}
					}
				}
			}
		}
	}
	return strings.Contains(raw, `"type":"error"`)
}

// classify determines the activity state from the last session-file line
// and the file's mtime. mtime within freshWindow always wins (Active);
// otherwise an error-shaped last record is Blocked, and everything else
// (including task_complete/turn-end records and any type this adapter
// doesn't recognize) is Ready.
func classify(raw string, mtime time.Time) activity.State {
	if time.Since(mtime) < freshWindow {
		return activity.Active
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return activity.Ready
	}

	if isErrorRecord(raw, top) {
		return activity.Blocked
	}

	return activity.Ready
}

// Activity implements agent.Agent. It locates the newest session file whose
// recorded cwd matches ref.WorktreePath (searching today's and yesterday's
// date-sharded session directories) and classifies activity state from its
// mtime and last record, same shape as claudecode.Activity.
func (c *Codex) Activity(ctx context.Context, ref agent.ActivityRef) (activity.State, time.Time, error) {
	path, mtime, err := findMatchingSession(ref.WorktreePath)
	if err != nil {
		return "", time.Time{}, agent.ErrNoSignal
	}

	line, err := lastNonEmptyLine(path)
	if err != nil || line == "" {
		return "", time.Time{}, agent.ErrNoSignal
	}

	return classify(line, mtime), mtime, nil
}
