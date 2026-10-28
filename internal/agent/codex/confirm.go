package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

var (
	_ agent.BusyInputAccepter = (*Codex)(nil)
	_ agent.DeliveryConfirmer = (*Codex)(nil)
)

// AcceptsInputWhileBusy implements agent.BusyInputAccepter: text submitted
// while Codex runs a turn lands in its «Messages to be submitted after next
// tool call» queue and reaches the model on its own.
func (c *Codex) AcceptsInputWhileBusy() bool { return true }

// userRecord is the part of a JSONL record ConfirmDelivered looks at. A
// message that reached the model is recorded as
//
//	{"timestamp":"…","type":"response_item","payload":{"type":"message","role":"user",
//	 "content":[{"type":"input_text","text":"<exact text>"}]}}
type userRecord struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"payload"`
}

// userRoleMark is a cheap pre-filter: only lines holding it are decoded.
var userRoleMark = []byte(`"role":"user"`)

// ConfirmDelivered implements agent.DeliveryConfirmer over the newest
// session file of ref.WorktreePath (the same one Activity reads).
//
// A record matches when its text, whitespace-normalized, equals text — or
// when a run of whole lines inside it does: Codex joins messages queued back
// to back into one user record with "\n" (observed with codex-cli 0.160.0),
// so one record can carry several of rocket's messages.
//
// The journal is read from its end backwards and the scan stops at the
// first record stamped at or before since: the queue calls this every few
// seconds for every accepted message with since = the moment of the paste,
// and a Codex journal can be tens of megabytes, so only the tail written
// after the paste is ever read. Records are chronological, so nothing
// matching can precede that stop point.
func (c *Codex) ConfirmDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	want := normalizeSpace(text)
	if want == "" {
		return false, time.Time{}, nil
	}
	path, _, err := findMatchingSession(ref.WorktreePath)
	if err != nil {
		return false, time.Time{}, agent.ErrNoSignal
	}
	f, err := os.Open(path)
	if err != nil {
		return false, time.Time{}, agent.ErrNoSignal
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, time.Time{}, agent.ErrNoSignal
	}

	// The scan yields newest first; the answer is the earliest match, so
	// keep overwriting until the scan ends.
	var at time.Time
	found := false
	err = scanTailSince(ctx, f, st.Size(), since, func(line []byte) {
		if t, ok := matchUserRecord(line, want, since); ok {
			at, found = t, true
		}
	})
	if err != nil {
		return false, time.Time{}, err
	}
	return found, at, nil
}

// tailChunk is how many bytes scanTailSince reads per step from the end of
// the journal. Lines longer than this (tool output) are reassembled across
// steps.
const tailChunk = 64 * 1024

// timestampKey opens the timestamp field every rollout record starts with.
var timestampKey = []byte(`"timestamp":"`)

// recordTimestamp extracts a record's timestamp without decoding the whole
// line (which can be megabytes). ok is false for a line without a parsable
// timestamp.
func recordTimestamp(line []byte) (time.Time, bool) {
	i := bytes.Index(line, timestampKey)
	if i < 0 {
		return time.Time{}, false
	}
	rest := line[i+len(timestampKey):]
	j := bytes.IndexByte(rest, '"')
	if j < 0 {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, string(rest[:j]))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// scanTailSince walks the lines of a size-byte journal from the last one
// backwards and calls visit for each, until it meets a record whose
// timestamp is not after since (that record is not visited). Lines without
// a parsable timestamp are visited and do not stop the scan. An empty last
// line (trailing newline) is skipped.
func scanTailSince(ctx context.Context, r io.ReaderAt, size int64, since time.Time, visit func(line []byte)) error {
	// carry holds the bytes of the (possibly partial) first line of the
	// region read so far; it grows until its line start is found.
	var carry []byte
	off := size
	handle := func(line []byte) bool {
		if len(line) == 0 {
			return false
		}
		if t, ok := recordTimestamp(line); ok && !t.After(since) {
			return true
		}
		visit(line)
		return false
	}
	for off > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := int64(tailChunk)
		if off < n {
			n = off
		}
		off -= n
		buf := make([]byte, n, n+int64(len(carry)))
		if _, err := r.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		buf = append(buf, carry...)
		// Everything after the first newline is complete lines, newest last.
		cut := bytes.IndexByte(buf, '\n')
		if cut < 0 {
			carry = buf
			continue
		}
		lines := bytes.Split(buf[cut+1:], []byte{'\n'})
		for i := len(lines) - 1; i >= 0; i-- {
			if handle(lines[i]) {
				return nil
			}
		}
		carry = append([]byte(nil), buf[:cut]...)
	}
	handle(carry)
	return nil
}

// matchUserRecord reports whether line is a user message recorded strictly
// after since that carries want (already normalized), and its timestamp.
func matchUserRecord(line []byte, want string, since time.Time) (time.Time, bool) {
	if !bytes.Contains(line, userRoleMark) {
		return time.Time{}, false
	}
	var rec userRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return time.Time{}, false
	}
	if rec.Type != "response_item" || rec.Payload.Type != "message" || rec.Payload.Role != "user" {
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
	if err != nil || !at.After(since) {
		return time.Time{}, false
	}
	var parts []string
	for _, c := range rec.Payload.Content {
		if c.Type == "input_text" {
			parts = append(parts, c.Text)
		}
	}
	for _, p := range append(parts, strings.Join(parts, "\n")) {
		if holdsLines(p, want) {
			return at, true
		}
	}
	return time.Time{}, false
}

// holdsLines reports whether recorded, or a run of its whole lines, equals
// want once whitespace is normalized.
func holdsLines(recorded, want string) bool {
	if !strings.Contains(normalizeSpace(recorded), want) {
		return false
	}
	lines := strings.Split(recorded, "\n")
	for i := range lines {
		for j := i + 1; j <= len(lines); j++ {
			got := normalizeSpace(strings.Join(lines[i:j], "\n"))
			if got == want {
				return true
			}
			if len(got) > len(want) {
				break
			}
		}
	}
	return false
}

// normalizeSpace collapses every run of whitespace (spaces, tabs, newlines)
// to one space and trims the ends.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
