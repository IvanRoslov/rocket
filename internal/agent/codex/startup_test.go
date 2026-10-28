package codex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

var _ agent.StartupProber = (*Codex)(nil)

func TestHasStartedTurnNoJournal(t *testing.T) {
	withCodexHome(t)
	started, err := New().(*Codex).HasStartedTurn(context.TODO(), agent.ActivityRef{WorktreePath: t.TempDir()})
	if !errors.Is(err, agent.ErrNoSignal) || started {
		t.Errorf("HasStartedTurn = %v, %v; want false, ErrNoSignal", started, err)
	}
}

func TestHasStartedTurnMetaOnly(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()
	writeSessionFile(t, home, now, "rollout-1", wt,
		[]string{`{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"task_started"}]}}`},
		now)

	started, err := New().(*Codex).HasStartedTurn(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != nil || started {
		t.Errorf("HasStartedTurn = %v, %v; want false, nil", started, err)
	}
}

func TestHasStartedTurnWithTaskStarted(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()
	writeSessionFile(t, home, now, "rollout-1", wt,
		[]string{
			`{"type":"event_msg","timestamp":"x","payload":{"type":"task_started","turn_id":"t1"}}`,
			`{"type":"event_msg","timestamp":"x","payload":{"type":"task_complete","turn_id":"t1"}}`,
		},
		now.Add(-time.Hour))

	started, err := New().(*Codex).HasStartedTurn(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != nil || !started {
		t.Errorf("HasStartedTurn = %v, %v; want true, nil", started, err)
	}
}

// After a restore, the worktree's newest journal may still be the previous
// run's: its turns predate the relaunch and must not count.
func TestHasStartedTurnIgnoresTurnsBeforeSince(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()
	writeSessionFile(t, home, now, "rollout-old", wt,
		[]string{`{"type":"event_msg","timestamp":"2026-10-28T09:00:00.000Z","payload":{"type":"task_started","turn_id":"t1"}}`},
		now.Add(-time.Hour))
	c := New().(*Codex)

	since := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	started, err := c.HasStartedTurn(context.TODO(), agent.ActivityRef{WorktreePath: wt, Since: since})
	if err != nil || started {
		t.Errorf("before relaunch: HasStartedTurn = %v, %v; want false, nil", started, err)
	}

	started, err = c.HasStartedTurn(context.TODO(), agent.ActivityRef{WorktreePath: wt, Since: since.Add(-2 * time.Hour)})
	if err != nil || !started {
		t.Errorf("after launch: HasStartedTurn = %v, %v; want true, nil", started, err)
	}
}
