package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// installConfirmFixture copies testdata/confirm/session.jsonl into today's
// session shard of a fresh CODEX_HOME, bound to a fresh worktree, after
// applying edit (nil for none) to its lines.
func installConfirmFixture(t *testing.T, edit func([]string) []string) agent.ActivityRef {
	t.Helper()
	home := withCodexHome(t)
	wt := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("testdata", "confirm", "session.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(raw), "__WORKTREE__", wt), "\n"), "\n")
	if edit != nil {
		lines = edit(lines)
	}
	now := time.Now()
	dir := filepath.Join(home, "sessions", now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rollout-confirm.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return agent.ActivityRef{SessionID: "s", WorktreePath: wt}
}

func ts(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func confirm(t *testing.T, ref agent.ActivityRef, text, since string) (bool, time.Time, error) {
	t.Helper()
	return (&Codex{}).ConfirmDelivered(context.Background(), ref, text, ts(t, since))
}

func TestConfirmDeliveredExactTextAfterSince(t *testing.T) {
	ref := installConfirmFixture(t, nil)
	found, at, err := confirm(t, ref, "[from orch] first message ALPHA", "2026-10-28T12:18:50Z")
	if err != nil || !found {
		t.Fatalf("want found, got found=%v err=%v", found, err)
	}
	if want := ts(t, "2026-10-28T12:19:00Z"); !at.Equal(want) {
		t.Fatalf("at = %v, want %v", at, want)
	}
}

func TestConfirmDeliveredBeforeSinceIsNotFound(t *testing.T) {
	ref := installConfirmFixture(t, nil)
	if found, _, err := confirm(t, ref, "[from orch] first message ALPHA", "2026-10-28T12:30:00Z"); err != nil || found {
		t.Fatalf("record before since: want not found, got found=%v err=%v", found, err)
	}
	// Strictly after: a record stamped exactly at since does not count.
	if found, _, err := confirm(t, ref, "[from orch] multi line message with spaces", "2026-10-28T12:20:00Z"); err != nil || found {
		t.Fatalf("record at since: want not found, got found=%v err=%v", found, err)
	}
}

func TestConfirmDeliveredReturnsFirstRecordTimestamp(t *testing.T) {
	// The same text is recorded at 12:19:00 and 12:23:00; since excludes
	// the first, so at is the second's timestamp.
	ref := installConfirmFixture(t, nil)
	found, at, err := confirm(t, ref, "[from orch] first message ALPHA", "2026-10-28T12:19:00Z")
	if err != nil || !found {
		t.Fatalf("want found, got found=%v err=%v", found, err)
	}
	if want := ts(t, "2026-10-28T12:23:00Z"); !at.Equal(want) {
		t.Fatalf("at = %v, want %v", at, want)
	}
}

func TestConfirmDeliveredWhitespaceInsensitive(t *testing.T) {
	ref := installConfirmFixture(t, nil)
	for _, text := range []string{
		"[from orch] multi line message with spaces",
		"  [from orch] multi\n\nline message with   spaces\n",
	} {
		found, at, err := confirm(t, ref, text, "2026-10-28T12:18:00Z")
		if err != nil || !found {
			t.Fatalf("%q: want found, got found=%v err=%v", text, found, err)
		}
		if want := ts(t, "2026-10-28T12:20:00Z"); !at.Equal(want) {
			t.Fatalf("%q: at = %v, want %v", text, at, want)
		}
	}
}

func TestConfirmDeliveredInsideMergedQueueRecord(t *testing.T) {
	// Codex joins messages queued back to back into one user record with
	// "\n" (live probe, codex-cli 0.160.0, task #5713).
	ref := installConfirmFixture(t, nil)
	found, at, err := confirm(t, ref, "[from orch] second BETA\nline two\n\nlast line BETA-END", "2026-10-28T12:18:00Z")
	if err != nil || !found {
		t.Fatalf("want found, got found=%v err=%v", found, err)
	}
	if want := ts(t, "2026-10-28T12:21:00Z"); !at.Equal(want) {
		t.Fatalf("at = %v, want %v", at, want)
	}
	// Only whole lines count: a fragment of a line is not that message.
	if found, _, _ := confirm(t, ref, "second BETA", "2026-10-28T12:18:00Z"); found {
		t.Fatal("a fragment of a recorded line was treated as the message")
	}
}

func TestConfirmDeliveredInboxPointerText(t *testing.T) {
	// For a long body the queue injects (and Codex records) the pointer
	// written by queue.prepareText, not the body itself.
	ref := installConfirmFixture(t, nil)
	text := "[from orch] [large message] Full text written to .rocket/inbox/msg-42.md — read that file now."
	if found, _, err := confirm(t, ref, text, "2026-10-28T12:18:00Z"); err != nil || !found {
		t.Fatalf("want found, got found=%v err=%v", found, err)
	}
}

func TestConfirmDeliveredIgnoresNonUserRecords(t *testing.T) {
	// "ghost" is only in an assistant message and an event_msg.
	ref := installConfirmFixture(t, nil)
	if found, _, err := confirm(t, ref, "[from orch] ghost GHOST", "2026-10-28T12:18:00Z"); err != nil || found {
		t.Fatalf("want not found, got found=%v err=%v", found, err)
	}
	if found, _, _ := confirm(t, ref, "[from orch] never sent", "2026-10-28T12:18:00Z"); found {
		t.Fatal("text absent from the journal was found")
	}
}

func TestConfirmDeliveredNoTranscript(t *testing.T) {
	withCodexHome(t)
	ref := agent.ActivityRef{SessionID: "s", WorktreePath: t.TempDir()}
	_, _, err := confirm(t, ref, "anything", "2026-10-28T12:18:00Z")
	if !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("want ErrNoSignal, got %v", err)
	}
}

func TestConfirmDeliveredHugeLine(t *testing.T) {
	// Tool output can make one JSONL line megabytes long; the scan must
	// read past it.
	huge := `{"timestamp":"2026-10-28T12:24:00.000Z","type":"event_msg","payload":{"type":"exec_command_end","aggregated_output":"` + strings.Repeat("x", 3<<20) + `"}}`
	late := `{"timestamp":"2026-10-28T12:25:00.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"[from orch] after huge output"}]}}`
	ref := installConfirmFixture(t, func(l []string) []string { return append(l, huge, late) })
	if found, _, err := confirm(t, ref, "[from orch] after huge output", "2026-10-28T12:18:00Z"); err != nil || !found {
		t.Fatalf("want found, got found=%v err=%v", found, err)
	}
}

func TestCodexAcceptsInputWhileBusy(t *testing.T) {
	var a agent.Agent = &Codex{}
	if !agent.AcceptsInputWhileBusy(a) {
		t.Fatal("codex must accept input while busy")
	}
	if _, ok := a.(agent.DeliveryConfirmer); !ok {
		t.Fatal("codex must implement agent.DeliveryConfirmer")
	}
}
