package codex

import (
	"bufio"
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

	// bufio.Reader, not Scanner: tool output can make one line megabytes
	// long, past any fixed Scanner limit.
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return false, time.Time{}, err
		}
		line, readErr := r.ReadBytes('\n')
		if at, ok := matchUserRecord(line, want, since); ok {
			return true, at, nil
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return false, time.Time{}, nil
			}
			return false, time.Time{}, readErr
		}
	}
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
