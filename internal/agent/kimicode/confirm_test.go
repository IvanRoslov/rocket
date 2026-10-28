package kimicode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func promptLine(t *testing.T, kind, text string, at int64) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type":    "turn.prompt",
		"agentId": "main",
		"input":   []map[string]string{{"type": "text", "text": text}},
		"origin":  map[string]string{"kind": kind},
		"turnId":  0,
		"time":    at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func steerLine(t *testing.T, text string, at int64) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type":    "turn.steer",
		"agentId": "main",
		"input":   []map[string]string{{"type": "text", "text": text}},
		"origin":  map[string]string{"kind": "skill_activation"},
		"time":    at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func confirm(t *testing.T, wire, text string, since int64) (bool, time.Time, error) {
	t.Helper()
	wt, _ := fakeKimi(t, []byte(wire), 1, oldMtime)
	return New().(agent.DeliveryConfirmer).ConfirmDelivered(context.Background(), agent.ActivityRef{WorktreePath: wt}, text, time.UnixMilli(since))
}

func TestAcceptsInputWhileBusy(t *testing.T) {
	if !agent.AcceptsInputWhileBusy(New()) {
		t.Fatal("kimi-code must accept input while busy")
	}
}

func TestConfirmExactAfterSince(t *testing.T) {
	wire := `{"type":"metadata"}` + "\n" + promptLine(t, "user", "hello there", 2000)
	found, at, err := confirm(t, wire, "hello there", 1000)
	if err != nil || !found || !at.Equal(time.UnixMilli(2000)) {
		t.Fatalf("got %v %v %v", found, at, err)
	}
}

func TestConfirmWhitespaceNormalized(t *testing.T) {
	wire := promptLine(t, "user", "line one\n\n  line   two\n", 2000)
	if found, _, err := confirm(t, wire, "line one line two", 1000); err != nil || !found {
		t.Fatalf("got %v %v", found, err)
	}
}

func TestConfirmIgnoresRecordAtOrBeforeSince(t *testing.T) {
	wire := promptLine(t, "user", "hello", 1000) + promptLine(t, "user", "other", 3000)
	if found, _, err := confirm(t, wire, "hello", 1000); err != nil || found {
		t.Fatalf("got %v %v", found, err)
	}
}

func TestConfirmRunOfWholeLines(t *testing.T) {
	wire := promptLine(t, "user", "first message\nsecond message", 2000)
	if found, _, _ := confirm(t, wire, "second message", 1000); !found {
		t.Fatal("run of whole lines not matched")
	}
	if found, _, _ := confirm(t, wire, "second", 1000); found {
		t.Fatal("partial line matched")
	}
}

func TestConfirmEarliestMatch(t *testing.T) {
	wire := promptLine(t, "user", "x", 2000) + promptLine(t, "user", "x", 3000)
	if _, at, _ := confirm(t, wire, "x", 1000); !at.Equal(time.UnixMilli(2000)) {
		t.Fatalf("at = %v, want earliest", at)
	}
}

// A message typed while Kimi was busy is recorded only when its own turn
// starts; the skill-activation steer inside the first turn is not delivery.
func TestConfirmQueuedMessageByLaterTurnPrompt(t *testing.T) {
	wire := steerLine(t, "reply QUEUED_OK", 1793194500000) + string(fixture(t, "queued-second-message.wire.jsonl"))
	found, at, err := confirm(t, wire, "reply QUEUED_OK", 1793194497000)
	if err != nil || !found || !at.Equal(time.UnixMilli(1793194545361)) {
		t.Fatalf("got %v %v %v", found, at, err)
	}
}

func TestConfirmSteerAndNonUserOriginDoNotCount(t *testing.T) {
	wire := steerLine(t, "do it", 2000) + promptLine(t, "system_trigger", "do it", 3000)
	if found, _, err := confirm(t, wire, "do it", 1000); err != nil || found {
		t.Fatalf("got %v %v", found, err)
	}
}

func TestConfirmLongLines(t *testing.T) {
	big := strings.Repeat("y", 200*1024)
	wire := promptLine(t, "user", "wanted", 2000) +
		`{"type":"context.append_loop_event","time":2500,"event":{"output":"` + big + `"}}` + "\n" +
		promptLine(t, "user", big, 3000)
	if found, _, _ := confirm(t, wire, "wanted", 1000); !found {
		t.Fatal("record before a long line not found")
	}
	if found, _, _ := confirm(t, wire, big, 1000); !found {
		t.Fatal("long prompt not matched")
	}
}

// Large bodies are delivered as a short reference to an inbox file; that
// reference is what Kimi records.
func TestConfirmInboxReference(t *testing.T) {
	ref := "[from orch] Large message saved to /x/.rocket/inbox/msg-12.md — read that file now."
	wire := promptLine(t, "user", ref, 2000)
	if found, _, _ := confirm(t, wire, ref, 1000); !found {
		t.Fatal("inbox reference not matched")
	}
}

func TestConfirmPaste5007(t *testing.T) {
	var rec struct {
		Input []struct{ Text string } `json:"input"`
	}
	for _, line := range strings.Split(string(fixture(t, "paste-5007.wire.jsonl")), "\n") {
		if strings.Contains(line, `"turn.prompt"`) {
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatal(err)
			}
		}
	}
	if found, _, _ := confirm(t, string(fixture(t, "paste-5007.wire.jsonl")), rec.Input[0].Text, 1); !found {
		t.Fatal("5007-byte paste not confirmed")
	}
}

func TestConfirmNoJournal(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	_, _, err := New().(agent.DeliveryConfirmer).ConfirmDelivered(context.Background(), agent.ActivityRef{WorktreePath: t.TempDir()}, "x", time.Time{})
	if !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("err = %v", err)
	}
}
