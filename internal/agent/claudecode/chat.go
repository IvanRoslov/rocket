package claudecode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// chatDigestMaxRunes bounds the length (in runes) of a tool_use digest
// string, per the design doc's "однострочный дайджест входа ≤120 симв."
const chatDigestMaxRunes = 120

// transcriptRecord is the subset of a claude-code JSONL transcript line's
// top-level fields TranscriptTail cares about. Confirmed live against a real
// transcript (~/.claude/projects/-Users-ivanroslov-projects-rocket*/*.jsonl):
// every record carries a top-level "type" field; "user" and "assistant"
// records additionally carry "message" (with nested "role"/"content") and a
// top-level "timestamp" in RFC3339 form (e.g. "2026-07-18T21:00:26.830Z").
// Many other record types exist (attachment, system, mode, permission-mode,
// last-prompt, relocated, worktree-state, bridge-session, queue-operation,
// file-history-delta/snapshot, ai-title, ...) — all are skipped by parseLine
// via the default case.
type transcriptRecord struct {
	Type      string       `json:"type"`
	Timestamp string       `json:"timestamp"`
	Message   *chatMessage `json:"message"`
}

// chatMessage is the "message" object of a user/assistant transcript
// record. Content is left as raw JSON since it is either a plain string
// (simple user turns) or an array of content blocks (assistant turns, and
// user turns that embed e.g. tool_result blocks).
type chatMessage struct {
	Content json.RawMessage `json:"content"`
}

// contentBlock is one element of a message's content block array.
type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`  // tool_use only
	Input json.RawMessage `json:"input"` // tool_use only
}

// parseChatTimestamp converts a transcript record's RFC3339 "timestamp"
// field to unix seconds, returning 0 if absent or unparsable.
func parseChatTimestamp(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	return t.Unix()
}

// digestInput compacts a tool_use block's JSON input to a single line and
// truncates it to at most chatDigestMaxRunes runes, appending an ellipsis
// when truncated so the result is still ≤ chatDigestMaxRunes runes long.
func digestInput(input json.RawMessage) string {
	var buf bytes.Buffer
	s := string(input)
	if err := json.Compact(&buf, input); err == nil {
		s = buf.String()
	}
	r := []rune(s)
	if len(r) <= chatDigestMaxRunes {
		return s
	}
	return string(r[:chatDigestMaxRunes-1]) + "…"
}

// extractUserText extracts the chat text for a "user" record's message
// content: the content string as-is when content is a plain string
// (including our own "[from ...]" injected prefixes — nothing is stripped),
// or the concatenation of any content[].text blocks when content is an
// array. Returns ok=false when content is an array with no text blocks at
// all (e.g. a tool_result-only user record), since there is nothing
// meaningful to show as chat text.
func extractUserText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}

	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", false
	}
	var sb strings.Builder
	found := false
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
			found = true
		}
	}
	if !found {
		return "", false
	}
	return sb.String(), true
}

// extractAssistantEntries builds ChatEntry values for an "assistant"
// record's message content blocks: all text blocks are merged into a single
// {Role: "assistant"} entry (in block order), and each tool_use block
// becomes its own {Role: "tool"} entry with a truncated input digest.
// thinking blocks and any other block type are ignored. Returns ok=false
// (no entries) for a thinking-only (or otherwise entry-less) message.
func extractAssistantEntries(raw json.RawMessage, ts int64) ([]agent.ChatEntry, bool) {
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, false
	}

	var text strings.Builder
	hasText := false
	for _, b := range blocks {
		if b.Type == "text" {
			text.WriteString(b.Text)
			hasText = true
		}
	}

	var entries []agent.ChatEntry
	if hasText {
		entries = append(entries, agent.ChatEntry{Role: "assistant", Text: text.String(), TS: ts})
	}
	for _, b := range blocks {
		if b.Type == "tool_use" {
			entries = append(entries, agent.ChatEntry{
				Role:     "tool",
				ToolName: b.Name,
				Text:     digestInput(b.Input),
				TS:       ts,
			})
		}
	}
	if len(entries) == 0 {
		return nil, false
	}
	return entries, true
}

// parseChatLine parses one complete (already newline-stripped) JSONL
// transcript line into zero or more ChatEntry values. Returns ok=false for
// blank lines, lines that fail to parse as JSON, record types other than
// "user"/"assistant", and messages that yield no chat-worthy content
// (thinking-only assistant turns, tool_result-only user turns).
func parseChatLine(line string) ([]agent.ChatEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false
	}

	var rec transcriptRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return nil, false
	}
	if rec.Message == nil {
		return nil, false
	}
	ts := parseChatTimestamp(rec.Timestamp)

	switch rec.Type {
	case "user":
		text, ok := extractUserText(rec.Message.Content)
		if !ok {
			return nil, false
		}
		return []agent.ChatEntry{{Role: "user", Text: text, TS: ts}}, true
	case "assistant":
		return extractAssistantEntries(rec.Message.Content, ts)
	default:
		return nil, false
	}
}

// readChatEntriesFrom reads complete JSONL lines from path starting at byte
// offset through EOF, parsing each into ChatEntry values. It returns the
// entries found and the byte offset immediately after the last complete
// line consumed; a trailing partial line (no terminating '\n' yet, because
// the writer is still mid-write) is left unread and does not advance the
// returned offset, so the next call picks it up once it's complete.
func readChatEntriesFrom(path string, offset int64) ([]agent.ChatEntry, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}

	reader := bufio.NewReaderSize(f, 64*1024)
	var entries []agent.ChatEntry
	pos := offset
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Partial trailing line (if any): do not emit, do not
				// advance the offset past it.
				break
			}
			return entries, pos, err
		}
		pos += int64(len(line))
		if es, ok := parseChatLine(line); ok {
			entries = append(entries, es...)
		}
	}
	return entries, pos, nil
}

// pathWithinDir reports whether path is contained within dir, after
// cleaning both. Guards against a naive prefix check on unclean paths (e.g.
// dir="/a/b" wrongly matching path="/a/bc") and against directory traversal
// via "..". A path equal to dir itself is not "within" it.
func pathWithinDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// resolveTailCursor decodes cursor (format "<path>:<offset>") into a
// starting (path, offset) pair to read from. An empty cursor, a malformed
// cursor, a cursor whose path falls outside dir (untrusted client input —
// see pathWithinDir), a cursor whose file no longer exists, or an offset
// beyond the current file size (the file was truncated/replaced) all fall
// back to starting from byte 0 of the directory's current newest transcript
// file.
func resolveTailCursor(dir, cursor string) (string, int64, error) {
	if cursor != "" {
		if idx := strings.LastIndex(cursor, ":"); idx >= 0 {
			p := cursor[:idx]
			if off, err := strconv.ParseInt(cursor[idx+1:], 10, 64); err == nil && pathWithinDir(p, dir) {
				if info, statErr := os.Stat(p); statErr == nil && off <= info.Size() {
					return p, off, nil
				}
			}
		}
	}
	path, _, err := newestTranscript(dir)
	if err != nil {
		return "", 0, agent.ErrNoSignal
	}
	return path, 0, nil
}

// TranscriptTail implements agent.Agent. See resolveTailCursor for cursor
// resolution/reset semantics and readChatEntriesFrom for line-reading
// semantics. If the file read to reaches EOF exactly at a point where a
// newer matching transcript file now exists (rotation — a new .jsonl became
// the newest in dir since the cursor's file was last read), it switches to
// the newer file at offset 0 within the same call, appending its entries.
func (c *ClaudeCode) TranscriptTail(ctx context.Context, ref agent.ActivityRef, cursor string) ([]agent.ChatEntry, string, error) {
	dir := transcriptDir(ref.WorktreePath)
	if dir == "" {
		return nil, "", agent.ErrNoSignal
	}

	path, offset, err := resolveTailCursor(dir, cursor)
	if err != nil {
		return nil, "", err
	}

	entries, newOffset, err := readChatEntriesFrom(path, offset)
	if err != nil {
		return nil, "", agent.ErrNoSignal
	}

	if info, statErr := os.Stat(path); statErr == nil && newOffset == info.Size() {
		if newestPath, _, nErr := newestTranscript(dir); nErr == nil && newestPath != path {
			moreEntries, moreOffset, err2 := readChatEntriesFrom(newestPath, 0)
			if err2 == nil {
				entries = append(entries, moreEntries...)
				path = newestPath
				newOffset = moreOffset
			}
		}
	}

	return entries, fmt.Sprintf("%s:%d", path, newOffset), nil
}

// TranscriptStat implements agent.Agent, reporting the mtime/size of the
// newest transcript file for ref's worktree (same file selection as
// Activity/TranscriptTail).
func (c *ClaudeCode) TranscriptStat(ctx context.Context, ref agent.ActivityRef) (int64, int64, error) {
	dir := transcriptDir(ref.WorktreePath)
	if dir == "" {
		return 0, 0, agent.ErrNoSignal
	}

	path, _, err := newestTranscript(dir)
	if err != nil {
		return 0, 0, agent.ErrNoSignal
	}

	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, agent.ErrNoSignal
	}
	return info.ModTime().Unix(), info.Size(), nil
}
