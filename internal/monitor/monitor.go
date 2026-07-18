// Package monitor implements rocket's activity cascade: a periodic sweep
// over live sessions that determines each session's activity state by
// checking, in order, whether the tmux session is gone, whether its pane
// holds only a bare shell, and finally the agent adapter's own signal —
// merged against any push notification the API has received in the
// meantime.
package monitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// shellNames are basenames (leading '-' stripped, as tmux/login shells often
// prefix argv[0] with a hyphen) that count as "no agent process running" for
// the pane-probe exited check.
var shellNames = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true,
}

// pushEntry records the last state pushed for a session, via PushUpdate.
type pushEntry struct {
	state activity.State
	ts    time.Time
}

// paneProber checks whether a tmux session's pane(s) hold only a bare shell
// process (i.e. the agent has exited and nothing replaced it), via tmux/ps.
// It is a small unexported interface so tests can fake process inspection
// without shelling out.
type paneProber interface {
	// onlyShellRunning reports whether every process attached to tmuxName's
	// pane(s) is a bare shell. Returns an error if the check could not be
	// performed (e.g. tmux/ps failed); callers should treat an error as
	// "inconclusive" and skip this check, not as "exited".
	onlyShellRunning(ctx context.Context, tmuxName string) (bool, error)
}

// execProber is the real paneProber, shelling out to tmux and ps.
type execProber struct{}

func (execProber) onlyShellRunning(ctx context.Context, tmuxName string) (bool, error) {
	out, err := exec.CommandContext(ctx, "tmux", "list-panes", "-t", "="+tmuxName, "-F", "#{pane_tty}").Output()
	if err != nil {
		return false, fmt.Errorf("list-panes: %w", err)
	}

	ttys := nonEmptyLines(string(out))
	if len(ttys) == 0 {
		return false, fmt.Errorf("no panes found for %q", tmuxName)
	}

	for _, tty := range ttys {
		psOut, err := exec.CommandContext(ctx, "ps", "-t", tty, "-o", "comm=").Output()
		if err != nil {
			return false, fmt.Errorf("ps -t %s: %w", tty, err)
		}
		for _, comm := range nonEmptyLines(string(psOut)) {
			base := filepath.Base(comm)
			base = strings.TrimPrefix(base, "-")
			if !shellNames[base] {
				return false, nil
			}
		}
	}
	return true, nil
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// Monitor runs the activity polling cascade and exposes a push path for
// out-of-band updates (e.g. from an API endpoint fed by the agent itself).
type Monitor struct {
	st           *store.Store
	bus          *bus.Bus
	rt           runtime.Runtime
	cfg          *config.Config
	resolveAgent func(name string) (agent.Agent, error)
	prober       paneProber

	mu    sync.Mutex
	push  map[string]pushEntry
	cache map[string]activity.State
}

// New builds a Monitor. resolveAgent is typically agent.Get.
func New(st *store.Store, b *bus.Bus, rt runtime.Runtime, cfg *config.Config, resolveAgent func(name string) (agent.Agent, error)) *Monitor {
	return &Monitor{
		st:           st,
		bus:          b,
		rt:           rt,
		cfg:          cfg,
		resolveAgent: resolveAgent,
		prober:       execProber{},
		push:         make(map[string]pushEntry),
		cache:        make(map[string]activity.State),
	}
}

// Run polls the activity cascade at cfg.ActivityPollInterval, blocking until
// ctx is cancelled. It performs one immediate sweep before the first tick.
func (m *Monitor) Run(ctx context.Context) {
	m.sweep(ctx)

	ticker := time.NewTicker(m.cfg.ActivityPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sweep(ctx)
		}
	}
}

// PushUpdate records an out-of-band activity signal for sessionID and
// immediately applies the merge+store+event logic for it (the fast reaction
// path). Invalid states are ignored.
func (m *Monitor) PushUpdate(sessionID string, state activity.State, ts time.Time) {
	if !state.Valid() {
		slog.Warn("monitor: ignoring invalid pushed activity state", "session", sessionID, "state", state)
		return
	}

	m.mu.Lock()
	m.push[sessionID] = pushEntry{state: state, ts: ts}
	m.mu.Unlock()

	sess, err := m.st.GetSession(sessionID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("monitor: get session for push", "session", sessionID, "error", err)
		}
		return
	}

	m.applyMerge(sess, state, ts, false, "push")
}

// Activity returns the last known activity state for sessionID: from the
// in-memory cache if present, else falling back to the store.
func (m *Monitor) Activity(sessionID string) (activity.State, bool) {
	m.mu.Lock()
	st, ok := m.cache[sessionID]
	m.mu.Unlock()
	if ok {
		return st, true
	}

	sess, err := m.st.GetSession(sessionID)
	if err != nil || sess.Activity == "" {
		return "", false
	}
	return activity.State(sess.Activity), true
}

// sweep runs one pass of the polling cascade over all live (spawning or
// running) sessions.
func (m *Monitor) sweep(ctx context.Context) {
	sessions, err := m.st.ListSessions(store.SessionFilter{All: false})
	if err != nil {
		slog.Error("monitor: list sessions", "error", err)
		return
	}

	liveSet := make(map[string]bool)
	liveNames, err := m.rt.List(ctx)
	if err != nil {
		slog.Error("monitor: list runtime sessions", "error", err)
	} else {
		for _, n := range liveNames {
			liveSet[n] = true
		}
	}

	sessionIDs := make(map[string]bool)
	for _, sess := range sessions {
		sessionIDs[sess.ID] = true
		m.pollSession(ctx, sess, liveSet, err == nil)
	}

	// Prune stale entries from cache and push maps whose sessions no longer exist.
	m.mu.Lock()
	for id := range m.cache {
		if !sessionIDs[id] {
			delete(m.cache, id)
		}
	}
	for id := range m.push {
		if !sessionIDs[id] {
			delete(m.push, id)
		}
	}
	m.mu.Unlock()
}

// pollSession runs the cascade for a single session: tmux-dead check,
// pane-only-shell check, then the agent's own signal, and applies the
// result. For spawning sessions, tmux checks are skipped since the session
// may not exist yet; only the agent Activity signal applies.
func (m *Monitor) pollSession(ctx context.Context, sess store.Session, liveSet map[string]bool, haveLiveSet bool) {
	var (
		state  activity.State
		ts     time.Time
		exited bool
	)

	// For spawning sessions, skip tmux-dead and pane-prober checks; only agent
	// Activity applies since the tmux session may not exist yet.
	if sess.State != "spawning" {
		switch {
		case haveLiveSet && !liveSet[sess.TmuxName]:
			state, ts, exited = activity.Exited, time.Now(), true
		default:
			if only, err := m.prober.onlyShellRunning(ctx, sess.TmuxName); err == nil && only {
				state, ts, exited = activity.Exited, time.Now(), true
			}
		}
	}

	if !exited {
		ag, err := m.resolveAgent(sess.Agent)
		if err != nil {
			slog.Error("monitor: resolve agent", "session", sess.ID, "agent", sess.Agent, "error", err)
			return
		}

		rawState, rawTS, err := ag.Activity(ctx, agent.ActivityRef{SessionID: sess.ID, WorktreePath: sess.WorktreePath})
		switch {
		case errors.Is(err, agent.ErrNoSignal):
			state = activity.State(sess.Activity)
			if state == "" {
				state = activity.Ready
			}
			ts = time.Unix(sess.ActivityTS, 0)
			// For spawning sessions with no signal and empty activity, don't write anything.
			if sess.State == "spawning" && sess.Activity == "" {
				return
			}
		case err != nil:
			slog.Error("monitor: agent activity", "session", sess.ID, "error", err)
			return
		default:
			state, ts = rawState, rawTS
		}
	}

	m.applyMerge(sess, state, ts, exited, "poll")
}

// applyMerge merges candidate (state, ts) against any pushed entry for
// sess.ID — the newer timestamp wins, except an exited candidate from the
// poll cascade (forceWin) always wins — applies the Ready→Idle threshold,
// and if the result differs from the stored activity, persists it and
// publishes a session.activity_changed event.
func (m *Monitor) applyMerge(sess store.Session, candState activity.State, candTS time.Time, forceWin bool, source string) {
	m.mu.Lock()
	pe, hasPush := m.push[sess.ID]
	m.mu.Unlock()

	finalState, finalTS := candState, candTS
	if hasPush && !forceWin && pe.ts.After(candTS) {
		finalState, finalTS = pe.state, pe.ts
	}

	if finalState == activity.Ready && !finalTS.IsZero() && time.Since(finalTS) > m.cfg.ReadyToIdle {
		finalState = activity.Idle
	}

	m.mu.Lock()
	m.cache[sess.ID] = finalState
	m.mu.Unlock()

	if string(finalState) == sess.Activity {
		return
	}

	if err := m.st.UpdateSessionActivity(sess.ID, string(finalState), finalTS.Unix()); err != nil {
		slog.Error("monitor: update session activity", "session", sess.ID, "error", err)
		return
	}

	m.bus.Publish("session.activity_changed", sess.ID, map[string]any{
		"from":   sess.Activity,
		"to":     string(finalState),
		"source": source,
	})
}
