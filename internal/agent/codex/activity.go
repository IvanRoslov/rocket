package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// sessionScanDays bounds how many trailing day-shards sessionsDirs scans.
// Codex shards a session's JSONL file by the date the session *started*
// (YYYY/MM/DD under $CODEX_HOME/sessions/) and keeps appending to that same
// file for the session's whole lifetime — including its mtime, which is
// what findMatchingSession/classify use as the activity signal. A worker
// session that started more than a day or two ago (long-running task,
// resumed session, etc.) therefore lives in an old date-shard, not today's
// or yesterday's. Scanning only the last two days made such sessions
// invisible to Activity, which decays them to idle right when phase 5's
// merge-grace idle check needs a real signal most. 14 days comfortably
// covers rocket's longest expected worker lifetimes while keeping the
// per-poll directory scan bounded and cheap.
const sessionScanDays = 14

// sessionsRoot returns $CODEX_HOME/sessions/, the single root all codex
// session JSONL files live under. Shared by sessionsDirs (day-sharded scan)
// and resolveTailCursor (chat.go, containment check for client-supplied
// cursor paths) so there is exactly one place that computes it.
func sessionsRoot() string {
	base := codexHome()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "sessions")
}

// sessionsDirs returns the sessionScanDays trailing date-sharded directories
// under $CODEX_HOME/sessions/ (today back through sessionScanDays-1 days
// ago) that could contain a still-active session, since codex shards a
// session's file by its start date and keeps appending to it for the
// session's lifetime (per recon). Nonexistent directories are skipped by
// the caller (os.ReadDir on a missing dir just errs and is ignored).
func sessionsDirs(now time.Time) []string {
	root := sessionsRoot()
	if root == "" {
		return nil
	}
	dirs := make([]string, 0, sessionScanDays)
	for i := 0; i < sessionScanDays; i++ {
		day := now.Add(-time.Duration(i) * 24 * time.Hour)
		dirs = append(dirs, filepath.Join(root, day.Format("2006"), day.Format("01"), day.Format("02")))
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

// sessionCwdCache memoizes path -> session_meta payload.cwd across calls,
// since a session file's first line is written once and never changes
// (immutable) once it has been successfully read: findMatchingSession scans
// up to 14 day-dirs and opens every .jsonl to read its first line, and it
// runs on every Activity/TranscriptStat/TranscriptTail poll tick (5s), so
// without this cache the same already-inspected files get reopened and
// re-parsed every tick for the lifetime of the daemon. No eviction: bounded
// by the number of distinct session files ever seen, and entries for
// deleted files are harmless (just unused memory, never look wrong).
var (
	sessionCwdMu    sync.Mutex
	sessionCwdCache = make(map[string]string)
)

// sessionCwd returns the session_meta payload.cwd recorded in the first
// line of the session file at path, if the first line is in fact a
// session_meta record. Memoized via sessionCwdCache; only successful reads
// are cached (a file whose first line isn't a valid session_meta yet — e.g.
// still being written — must be retried on the next call, not stuck as a
// permanent miss).
func sessionCwd(path string) (string, bool) {
	sessionCwdMu.Lock()
	if cwd, ok := sessionCwdCache[path]; ok {
		sessionCwdMu.Unlock()
		return cwd, true
	}
	sessionCwdMu.Unlock()

	cwd, ok := readSessionCwd(path)
	if !ok {
		return "", false
	}

	sessionCwdMu.Lock()
	sessionCwdCache[path] = cwd
	sessionCwdMu.Unlock()
	return cwd, true
}

// readSessionCwd does the actual file read behind sessionCwd (unmemoized).
func readSessionCwd(path string) (string, bool) {
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

// findMatchingSession scans the last sessionScanDays session directories
// for *.jsonl files whose session_meta.payload.cwd matches worktreePath,
// and returns the path and mtime of the newest match. Returns
// agent.ErrNoSignal if no matching file is found (missing dirs, no cwd
// match, or no session files at all).
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

// isErrorRecord reports whether a successfully-parsed JSONL record looks
// like an error/failed-turn signal, classified strictly by its actual
// "type" field structure. Recon's live sample only ever showed
// event_msg{token_count, task_complete} on a clean turn, and explicitly
// flagged the error-event taxonomy as unverified — so this is a
// conservative best-effort check (a nested event payload "type" containing
// "error", or a top-level "type":"error"), not an exhaustive list. Anything
// that doesn't match falls through to Ready, per the binding instruction
// ("unknown types → Ready"). This must only be called with top from a
// record whose JSON parsed successfully — see classify's raw-substring
// fallback for the parse-failure case, which this function intentionally
// does not duplicate (a record whose *content* happens to mention the
// literal `"type":"error"` — e.g. a response_item echoing user-visible
// text — must not be misclassified as Blocked just because that substring
// appears somewhere in the line).
func isErrorRecord(top map[string]json.RawMessage) bool {
	typeRaw, ok := top["type"]
	if !ok {
		return false
	}
	var t string
	if err := json.Unmarshal(typeRaw, &t); err != nil {
		return false
	}
	if t == "error" {
		return true
	}
	if t != "event_msg" {
		return false
	}
	payloadRaw, ok := top["payload"]
	if !ok {
		return false
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return false
	}
	ptRaw, ok := payload["type"]
	if !ok {
		return false
	}
	var pt string
	if err := json.Unmarshal(ptRaw, &pt); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(pt), "error")
}

// classify determines the activity state from the last session-file line
// and the file's mtime. mtime within freshWindow always wins (Active).
// Otherwise, if the record parses as JSON, it classifies strictly by its
// actual type field (isErrorRecord) — a record whose text content happens
// to mention `"type":"error"` is not enough on its own to flip it to
// Blocked. Only when the record fails to parse as JSON at all does the raw
// substring check apply, as a best-effort fallback for malformed/unknown
// line shapes. Everything else (including task_complete/turn-end records
// and any type this adapter doesn't recognize) is Ready.
func classify(raw string, mtime time.Time) activity.State {
	if time.Since(mtime) < freshWindow {
		return activity.Active
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		if strings.Contains(raw, `"type":"error"`) {
			return activity.Blocked
		}
		return activity.Ready
	}

	if isErrorRecord(top) {
		return activity.Blocked
	}

	return activity.Ready
}

// Activity implements agent.Agent. It locates the newest session file whose
// recorded cwd matches ref.WorktreePath (searching the last sessionScanDays
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
