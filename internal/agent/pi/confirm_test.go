package pi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/runtime"
)

func confirmPi(t *testing.T, wire, text string, since int64) (bool, time.Time, error) {
	t.Helper()
	wt, _ := fakePiSession(t, wire, "s1", piOldMtime)
	return New().(agent.DeliveryConfirmer).ConfirmDelivered(context.Background(), agent.ActivityRef{SessionID: "s1", WorktreePath: wt}, text, time.UnixMilli(since))
}

func TestAcceptsInputWhileBusy(t *testing.T) {
	if !agent.AcceptsInputWhileBusy(New()) {
		t.Fatal("Pi must accept steering while busy")
	}
}

func TestPaneDialectAndBracketedPaste(t *testing.T) {
	if got := agent.PaneDialectOf(New()); got != runtime.PaneDialectPi {
		t.Fatalf("PaneDialectOf = %q, want %q", got, runtime.PaneDialectPi)
	}
	if !agent.WantsBracketedPaste(New()) {
		t.Fatal("Pi must want bracketed paste")
	}
}

func TestConfirmExactAfterSince(t *testing.T) {
	wire := piMessageLine(t, "u1", "", "user", "hello there", 2000)
	found, at, err := confirmPi(t, wire, "hello there", 1000)
	if err != nil || !found || !at.Equal(time.UnixMilli(2000)) {
		t.Fatalf("got %v %v %v", found, at, err)
	}
}

func TestConfirmWhitespaceNormalized(t *testing.T) {
	wire := piMessageLine(t, "u1", "", "user", "line one\n\n  line   two\n", 2000)
	if found, _, err := confirmPi(t, wire, "line one line two", 1000); err != nil || !found {
		t.Fatalf("got %v %v", found, err)
	}
}

func TestConfirmRunOfWholeLines(t *testing.T) {
	wire := piMessageLine(t, "u1", "", "user", "first message\nsecond message", 2000)
	if found, _, _ := confirmPi(t, wire, "second message", 1000); !found {
		t.Fatal("run of whole lines not matched")
	}
	if found, _, _ := confirmPi(t, wire, "second", 1000); found {
		t.Fatal("partial line matched")
	}
}

func TestConfirmInboxReference(t *testing.T) {
	ref := "[from orch] [large message] Full text written to .rocket/inbox/msg-12.md — read that file now."
	wire := piMessageLine(t, "u1", "", "user", ref, 2000)
	if found, _, err := confirmPi(t, wire, ref, 1000); err != nil || !found {
		t.Fatalf("inbox reference: got %v %v", found, err)
	}
}

func TestConfirmEarliestMatch(t *testing.T) {
	wire := piMessageLine(t, "u1", "", "user", "x", 2000) + piMessageLine(t, "u2", "u1", "user", "x", 3000)
	_, at, err := confirmPi(t, wire, "x", 1000)
	if err != nil || !at.Equal(time.UnixMilli(2000)) {
		t.Fatalf("at = %v, err = %v; want earliest match", at, err)
	}
}

func TestConfirmIgnoresRecordAtOrBeforeSince(t *testing.T) {
	wire := piMessageLine(t, "u1", "", "user", "hello", 1000) + piMessageLine(t, "u2", "u1", "user", "other", 3000)
	if found, _, err := confirmPi(t, wire, "hello", 1000); err != nil || found {
		t.Fatalf("got %v %v", found, err)
	}
}

func TestConfirmOnlyExactSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	wt := t.TempDir()
	target := piMessageLine(t, "u1", "", "user", "unrelated", 2000)
	other := piMessageLine(t, "u1", "", "user", "wanted", 2000)
	if err := os.WriteFile(writeSessionFile(t, home, wt, "2026_s1.jsonl"), []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(writeSessionFile(t, home, wt, "2026_other.jsonl"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	found, _, err := New().(agent.DeliveryConfirmer).ConfirmDelivered(context.Background(), agent.ActivityRef{SessionID: "s1", WorktreePath: wt}, "wanted", time.UnixMilli(1000))
	if err != nil || found {
		t.Fatalf("got %v %v; another session confirmed delivery", found, err)
	}
}

func TestConfirmQueuedPaneIsNotDelivery(t *testing.T) {
	wire := `{"type":"custom","id":"c1","parentId":null,"timestamp":"2026-10-29T17:15:42.093Z","text":"Steering: reply QUEUED_OK"}` + "\n"
	if found, _, err := confirmPi(t, wire, "reply QUEUED_OK", 1000); err != nil || found {
		t.Fatalf("got %v %v; a queued pane must not confirm without a user record", found, err)
	}
}

func TestConfirmSteerAndNonUserRecordsDoNotCount(t *testing.T) {
	wire := piMessageLine(t, "s1", "", "system", "do it", 2000) +
		`{"type":"custom","id":"c1","parentId":"s1","timestamp":"2026-10-29T17:15:42.093Z","message":{"role":"user","content":[{"type":"text","text":"do it"}]}}` + "\n"
	if found, _, err := confirmPi(t, wire, "do it", 1000); err != nil || found {
		t.Fatalf("got %v %v", found, err)
	}
}

func TestConfirmAbandonedBranchDoesNotCount(t *testing.T) {
	wire := piMessageLine(t, "root", "", "user", "start", 1000) +
		piMessageLine(t, "abandoned", "root", "user", "wanted", 2000) +
		piMessageLineWithStop(t, "current", "root", "assistant", "done", 3000, "stop", "")
	if found, _, err := confirmPi(t, wire, "wanted", 1000); err != nil || found {
		t.Fatalf("got %v %v; abandoned branch confirmed delivery", found, err)
	}
}

func TestConfirmNoJournal(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	_, _, err := New().(agent.DeliveryConfirmer).ConfirmDelivered(context.Background(), agent.ActivityRef{SessionID: "missing", WorktreePath: t.TempDir()}, "x", time.Time{})
	if !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("error = %v, want ErrNoSignal", err)
	}
}

func TestConfirmReadErrorPropagates(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(home, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", home)
	_, _, err := New().(agent.DeliveryConfirmer).ConfirmDelivered(context.Background(), agent.ActivityRef{SessionID: "s1", WorktreePath: t.TempDir()}, "x", time.Time{})
	if err == nil || errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("error = %v, want real session-store read failure", err)
	}
}
