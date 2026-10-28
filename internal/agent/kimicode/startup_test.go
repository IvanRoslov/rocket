package kimicode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func started(t *testing.T, wire string, since time.Time) (bool, error) {
	t.Helper()
	wt, _ := fakeKimi(t, []byte(wire), since.UnixMilli(), oldMtime)
	return New().(agent.StartupProber).HasStartedTurn(context.Background(), agent.ActivityRef{WorktreePath: wt, Since: since})
}

func TestHasStartedTurn(t *testing.T) {
	if ok, err := started(t, string(fixture(t, "completed-tool-turn.wire.jsonl")), time.Time{}); err != nil || !ok {
		t.Fatalf("completed turn: %v %v", ok, err)
	}
	if ok, err := started(t, `{"type":"metadata"}`+"\n", time.Time{}); err != nil || ok {
		t.Fatalf("metadata only: %v %v", ok, err)
	}
	if ok, _ := started(t, steerLine(t, "x", 5000)+promptLine(t, "system_trigger", "x", 5000), time.Time{}); ok {
		t.Fatal("non-user prompt counted as a started turn")
	}
}

func TestHasStartedTurnRespectsSince(t *testing.T) {
	wire := promptLine(t, "user", "old", 1000) + promptLine(t, "user", "new", 3000)
	if ok, _ := started(t, wire, time.UnixMilli(3000)); !ok {
		t.Fatal("prompt at Since not counted")
	}
	wire = promptLine(t, "user", "old", 1000)
	if ok, _ := started(t, wire, time.UnixMilli(2000)); ok {
		t.Fatal("prompt before Since counted")
	}
}

func TestHasStartedTurnNoJournal(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	_, err := New().(agent.StartupProber).HasStartedTurn(context.Background(), agent.ActivityRef{WorktreePath: t.TempDir()})
	if !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("err = %v", err)
	}
}
