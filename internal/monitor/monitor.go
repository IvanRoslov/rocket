// Package monitor implements rocket's activity cascade: a periodic sweep
// over live sessions that determines each session's activity state by
// checking, in order, whether the tmux session is gone, whether its pane
// holds only a bare shell, and finally the agent adapter's own signal —
// merged against any push notification the API has received in the
// meantime.
package monitor

import (
	"context"
	"encoding/json"
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

// chatStat records the last-observed transcript (mtime, size) for a session,
// as reported by agent.Agent.TranscriptStat.
type chatStat struct {
	mtime int64
	size  int64
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

	mu       sync.Mutex
	push     map[string]pushEntry
	cache    map[string]activity.State
	chat     map[string]chatStat
	quizMiss map[string]int
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
		chat:         make(map[string]chatStat),
		quizMiss:     make(map[string]int),
	}
}

// SweepOnce runs a single synchronous pass of the activity polling cascade
// over all live sessions. It exists so callers (namely the daemon at
// startup) can force activity state in the store to be current before
// depending on it, without waiting for Run's background ticker.
//
// In particular, the daemon must call SweepOnce AFTER session reconciliation
// but BEFORE queue.Recover: otherwise Recover's startup Wakes can hit
// recipients whose store activity is stale from before a restart (e.g. still
// "active" from the crash moment), causing delivery to needlessly wait a
// full poll cycle before re-checking.
func (m *Monitor) SweepOnce(ctx context.Context) {
	m.sweep(ctx)
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
		m.pollChat(ctx, sess)
		m.pollQuiz(ctx, sess)
	}

	// Prune stale entries from cache, push and chat maps whose sessions no
	// longer exist.
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
	for id := range m.chat {
		if !sessionIDs[id] {
			delete(m.chat, id)
		}
	}
	for id := range m.quizMiss {
		if !sessionIDs[id] {
			delete(m.quizMiss, id)
		}
	}
	m.mu.Unlock()
}

// quizRenderGrace is how old a pending quiz must be before pollQuiz starts
// pane-checking it: right after the PreToolUse hook fires the widget takes
// a moment to render, and checking too early would read its absence as
// "already closed".
const quizRenderGrace = 10 * time.Second

// quizMissThreshold is how many consecutive sweeps must observe the pane
// WITHOUT the quiz widget before pollQuiz declares the quiz closed. Two
// misses (~2 poll intervals) filter out transient capture glitches.
const quizMissThreshold = 2

// pollQuiz is the cancelled-quiz backstop. The precise close signal is the
// PostToolUse quiz hook (POST /v1/internal/quiz resolved), but Claude Code
// does NOT fire PostToolUse for a REJECTED tool call (Esc'd quiz), and the
// Stop hook was observed not firing at all on CLI v2.1.215 — so a quiz
// declined in the terminal would otherwise leave pending_quiz stale
// forever, gating message delivery. While a session has a pending quiz
// older than quizRenderGrace, this checks the pane tail each sweep: the
// AskUserQuestion widget always renders at the bottom of the pane while
// open (runtime.LooksLikeQuizWidget), so quizMissThreshold consecutive
// sweeps without it mean the quiz is closed — clear pending and publish
// session.quiz_resolved, exactly as the resolved hook would.
func (m *Monitor) pollQuiz(ctx context.Context, sess store.Session) {
	if sess.PendingQuiz == "" {
		m.mu.Lock()
		delete(m.quizMiss, sess.ID)
		m.mu.Unlock()
		return
	}

	var quiz struct {
		AskedAt int64 `json:"asked_at"`
	}
	if err := json.Unmarshal([]byte(sess.PendingQuiz), &quiz); err == nil &&
		quiz.AskedAt > 0 && time.Since(time.Unix(quiz.AskedAt, 0)) < quizRenderGrace {
		return
	}

	out, err := m.rt.Capture(ctx, runtime.Handle{Name: sess.TmuxName}, 15)
	if err != nil {
		// Pane gone or capture failed — best-effort skip; a dead session's
		// pending_quiz is cleared by its terminal state transition.
		return
	}
	if runtime.LooksLikeQuizWidget(out) {
		m.mu.Lock()
		delete(m.quizMiss, sess.ID)
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	m.quizMiss[sess.ID]++
	misses := m.quizMiss[sess.ID]
	if misses >= quizMissThreshold {
		delete(m.quizMiss, sess.ID)
	}
	m.mu.Unlock()
	if misses < quizMissThreshold {
		return
	}

	if err := m.st.ClearPendingQuiz(sess.ID); err != nil {
		slog.Warn("monitor: clear stale pending quiz", "session", sess.ID, "error", err)
		return
	}
	slog.Info("monitor: pending quiz closed without resolved hook (widget gone)", "session", sess.ID)
	m.bus.Publish("session.quiz_resolved", sess.ID, map[string]any{})
}

// pollChat checks whether sess's transcript has changed since the last
// sweep (via the agent's cheap TranscriptStat) and, if so, publishes
// session.chat_updated. The first observation of a session is seeded
// silently (no event), so daemon startup doesn't produce a spurious ping for
// every already-running session. ErrNoSignal (no transcript yet) and any
// agent-resolution failure are skipped silently — this is a best-effort
// ping, not a correctness-critical path.
func (m *Monitor) pollChat(ctx context.Context, sess store.Session) {
	ag, err := m.resolveAgent(sess.Agent)
	if err != nil {
		return
	}

	mtime, size, err := ag.TranscriptStat(ctx, agent.ActivityRef{SessionID: sess.ID, WorktreePath: sess.WorktreePath})
	if err != nil {
		return
	}

	m.mu.Lock()
	prev, seen := m.chat[sess.ID]
	m.chat[sess.ID] = chatStat{mtime: mtime, size: size}
	m.mu.Unlock()

	if !seen {
		return
	}
	if prev.mtime == mtime && prev.size == size {
		return
	}

	m.bus.Publish("session.chat_updated", sess.ID, map[string]any{})
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
			if sess.ActivityTS == 0 {
				// No stored timestamp yet (fresh running/spawning session
				// that hasn't reported any signal): use now as a fresh
				// grace period rather than the epoch, which would make the
				// Ready->Idle threshold in applyMerge fire instantly and
				// mark a brand-new session idle before it has had any
				// chance to become active.
				ts = time.Now()
			} else {
				ts = time.Unix(sess.ActivityTS, 0)
			}
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
