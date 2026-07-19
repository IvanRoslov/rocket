package codex

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

// chatDigestMaxRunes bounds the length (in runes) of a tool digest string,
// per the design doc's "однострочный дайджест входа ≤120 симв.". Same
// value as claudecode.
const chatDigestMaxRunes = 120

// chatRecord is the subset of a codex session JSONL line's top-level
// fields TranscriptTail cares about. Confirmed live against real session
// files (~/.codex/sessions/YYYY/MM/DD/*.jsonl, read-only): every record
// carries a top-level "type" (session_meta | turn_context | event_msg |
// response_item) and a top-level "timestamp" in RFC3339 form with
// millisecond fraction (e.g. "2026-07-19T08:28:06.208Z"). session_meta and
// turn_context carry no chat content and are skipped entirely; event_msg
// and response_item both nest their real payload under "payload", whose
// own "type" field disambiguates the specific record kind.
type chatRecord struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// eventMsgPayload is the payload shape of the two event_msg kinds that
// carry chat text: user_message (the human/orchestrator's turn) and
// agent_message (the model's turn). Both were confirmed live to carry a
// plain string "message" field. Other event_msg kinds seen live
// (task_started, task_complete, token_count, patch_apply_end, ...) are
// skipped by parseChatLine's default case.
type eventMsgPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// responseItemPayload is the payload shape of the response_item kinds that
// carry tool-call content: function_call (payload.name is the function,
// e.g. "exec_command"/"write_stdin"; payload.arguments is a JSON-encoded
// *string*, confirmed live e.g. `"arguments":"{\"cmd\":\"git add ...\"}"`),
// custom_tool_call (payload.name e.g. "apply_patch"; payload.input is
// plain, non-JSON text — an apply_patch diff body, confirmed live), and
// web_search_call (no "name" field; payload.action is a JSON object
// describing the search/open_page/find_in_page action, confirmed live).
// response_item/message (role user/assistant/developer) and
// response_item/reasoning are deliberately NOT mapped here — see
// parseChatLine's doc comment for why.
type responseItemPayload struct {
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Input     string          `json:"input"`
	Action    json.RawMessage `json:"action"`
}

// parseChatTimestamp converts a record's RFC3339 "timestamp" field to unix
// seconds, returning 0 if absent or unparsable. Same as claudecode's
// parseChatTimestamp.
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

// compactJSON compacts a JSON value to a single line, falling back to the
// original text unmodified if it doesn't parse as JSON (defensive; codex's
// own JSON should always be valid here).
func compactJSON(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

// singleLine collapses a plain-text (non-JSON) blob such as an
// apply_patch diff body onto one line by folding all whitespace runs
// (including newlines) into single spaces.
func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncateDigest truncates s to at most chatDigestMaxRunes runes,
// appending an ellipsis when truncated so the result is still
// ≤ chatDigestMaxRunes runes long. Same behavior as claudecode's
// digestInput truncation.
func truncateDigest(s string) string {
	r := []rune(s)
	if len(r) <= chatDigestMaxRunes {
		return s
	}
	return string(r[:chatDigestMaxRunes-1]) + "…"
}

// parseChatLine parses one complete (already newline-stripped) session
// JSONL line into zero or one ChatEntry. Returns ok=false for blank lines,
// lines that fail to parse as JSON, and record/payload kinds that carry no
// chat-worthy content.
//
// Mapping decision (verified against real session files, not just the
// binding text): event_msg/user_message and event_msg/agent_message are
// used as the {user}/{assistant} text source rather than
// response_item/message. A real session showed response_item/message
// (role "user") duplicates event_msg/user_message byte-for-byte at the
// same timestamp for actual human turns, but ALSO appears once more at
// session start carrying injected AGENTS.md instructions text with no
// event_msg counterpart at all — so mapping response_item/message directly
// would both double the real turns and leak the instructions blob as a
// spurious first "user message". event_msg/agent_message vs.
// response_item/message role="assistant" showed the same exact duplication
// for every agent turn observed. response_item/message and
// response_item/reasoning are therefore skipped entirely; response_item is
// used only for tool-call kinds (function_call, custom_tool_call,
// web_search_call), which have no event_msg counterpart at all.
func parseChatLine(line string) (agent.ChatEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return agent.ChatEntry{}, false
	}

	var rec chatRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return agent.ChatEntry{}, false
	}
	ts := parseChatTimestamp(rec.Timestamp)

	switch rec.Type {
	case "event_msg":
		var p eventMsgPayload
		if err := json.Unmarshal(rec.Payload, &p); err != nil {
			return agent.ChatEntry{}, false
		}
		switch p.Type {
		case "user_message":
			return agent.ChatEntry{Role: "user", Text: p.Message, TS: ts}, true
		case "agent_message":
			return agent.ChatEntry{Role: "assistant", Text: p.Message, TS: ts}, true
		default:
			return agent.ChatEntry{}, false
		}
	case "response_item":
		var p responseItemPayload
		if err := json.Unmarshal(rec.Payload, &p); err != nil {
			return agent.ChatEntry{}, false
		}
		switch p.Type {
		case "function_call":
			digest := truncateDigest(compactJSON([]byte(p.Arguments)))
			return agent.ChatEntry{Role: "tool", ToolName: p.Name, Text: digest, TS: ts}, true
		case "custom_tool_call":
			digest := truncateDigest(singleLine(p.Input))
			return agent.ChatEntry{Role: "tool", ToolName: p.Name, Text: digest, TS: ts}, true
		case "web_search_call":
			digest := truncateDigest(compactJSON(p.Action))
			return agent.ChatEntry{Role: "tool", ToolName: "web_search", Text: digest, TS: ts}, true
		default:
			return agent.ChatEntry{}, false
		}
	default:
		return agent.ChatEntry{}, false
	}
}

// readChatEntriesFrom reads complete JSONL lines from path starting at byte
// offset through EOF, parsing each into a ChatEntry. It returns the
// entries found and the byte offset immediately after the last complete
// line consumed; a trailing partial line (no terminating '\n' yet, because
// the writer is still mid-write) is left unread and does not advance the
// returned offset, so the next call picks it up once it's complete — and
// any further complete lines appended after it in the same read are still
// returned in that same call, since the loop only stops at the first
// genuinely incomplete tail. Mirrors claudecode's readChatEntriesFrom (not
// factored into a shared helper: the per-line parse function and entry
// cardinality per line differ enough — claudecode's parseChatLine can
// emit multiple entries per line, this one emits at most one — that a
// shared generic wrapper would add more indirection than it would save).
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
		if e, ok := parseChatLine(line); ok {
			entries = append(entries, e)
		}
	}
	return entries, pos, nil
}

// pathWithinDir reports whether path is contained within dir, after
// cleaning both. Guards against a naive prefix check on unclean paths (e.g.
// dir="/a/b" wrongly matching path="/a/bc") and against directory traversal
// via "..". A path equal to dir itself is not "within" it. Same helper as
// claudecode's (not shared: the two packages don't currently share any
// non-agent-interface code, and this is a two-line pure function).
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
// cursor, a cursor whose path falls outside the codex sessions root (
// $CODEX_HOME/sessions/ — untrusted client input, see pathWithinDir and
// sessionsRoot in activity.go), a cursor whose file no longer exists, or an
// offset beyond the current file size (the file was truncated/replaced) all
// fall back to starting from byte 0 of worktreePath's current newest
// matching session file (same selection as Activity: findMatchingSession).
func resolveTailCursor(worktreePath, cursor string) (string, int64, error) {
	if cursor != "" {
		if idx := strings.LastIndex(cursor, ":"); idx >= 0 {
			p := cursor[:idx]
			if off, err := strconv.ParseInt(cursor[idx+1:], 10, 64); err == nil && pathWithinDir(p, sessionsRoot()) {
				if info, statErr := os.Stat(p); statErr == nil && off <= info.Size() {
					return p, off, nil
				}
			}
		}
	}
	path, _, err := findMatchingSession(worktreePath)
	if err != nil {
		return "", 0, agent.ErrNoSignal
	}
	return path, 0, nil
}

// TranscriptTail implements agent.Agent. See resolveTailCursor for cursor
// resolution/reset semantics and readChatEntriesFrom for line-reading
// semantics. If the file read to reaches EOF exactly at a point where a
// newer matching session file now exists (a new session started for this
// worktree since the cursor's file was last read), it switches to the
// newer file at offset 0 within the same call, appending its entries. Same
// shape as claudecode.TranscriptTail.
func (c *Codex) TranscriptTail(ctx context.Context, ref agent.ActivityRef, cursor string) ([]agent.ChatEntry, string, error) {
	path, offset, err := resolveTailCursor(ref.WorktreePath, cursor)
	if err != nil {
		return nil, "", err
	}

	entries, newOffset, err := readChatEntriesFrom(path, offset)
	if err != nil {
		return nil, "", agent.ErrNoSignal
	}

	if info, statErr := os.Stat(path); statErr == nil && newOffset == info.Size() {
		if newestPath, _, nErr := findMatchingSession(ref.WorktreePath); nErr == nil && newestPath != path {
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
// newest matching session file for ref's worktree (same file selection as
// Activity/TranscriptTail).
func (c *Codex) TranscriptStat(ctx context.Context, ref agent.ActivityRef) (int64, int64, error) {
	path, _, err := findMatchingSession(ref.WorktreePath)
	if err != nil {
		return 0, 0, agent.ErrNoSignal
	}

	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, agent.ErrNoSignal
	}
	return info.ModTime().Unix(), info.Size(), nil
}
