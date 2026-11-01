package monitor

import (
	"context"
	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
	"path/filepath"
	"time"
)

func (m *Monitor) isClaude(sess store.Session) bool {
	if m.resolveAgent == nil {
		return false
	}
	a, e := m.resolveAgent(sess.Agent)
	return e == nil && agent.SupportsClaudeSocket(a)
}

type claudeObservation struct {
	target   claudeinstance.Identity
	revision uint64
	eventAt  int64
}

func (a claudeObservation) same(b claudeObservation) bool {
	return a.revision == b.revision && a.eventAt == b.eventAt && claudeinstance.SameIdentity(a.target, b.target)
}

type claudeResolution struct {
	claudeinstance.Resolution
	observation claudeObservation
}

func (m *Monitor) currentClaude(ctx context.Context, id string) (claudeObservation, error) {
	l, target, event, e := m.st.GetClaudeBinding(ctx, id)
	if e != nil {
		return claudeObservation{}, e
	}
	if target == nil {
		return claudeObservation{}, agent.ErrNoSignal
	}
	return claudeObservation{*target, l.BindingRevision, event}, nil
}
func (m *Monitor) resolveClaude(ctx context.Context, sess store.Session) claudeResolution {
	unknown := func(reason string) claudeResolution {
		return claudeResolution{Resolution: claudeinstance.Resolution{State: claudeinstance.Unknown, Reason: reason}}
	}
	l, pinned, event, e := m.st.GetClaudeBinding(ctx, sess.ID)
	if e != nil {
		return unknown("unproven_launch")
	}
	all, e := claudeinstance.ReadRegistry(ctx, m.claudeSessionsDir)
	if e != nil {
		return unknown("registry_lookup")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	projects := filepath.Join(filepath.Dir(m.claudeSessionsDir), "projects")
	r := claudeinstance.Resolve(probeCtx, l, pinned, all, m.claudeProbe, projects)
	if r.State == claudeinstance.Verified {
		var ok bool
		if pinned != nil && !claudeinstance.SameIdentity(*pinned, r.Identity) {
			fresh, err := claudeinstance.ReadRegistry(probeCtx, m.claudeSessionsDir)
			if err != nil {
				return unknown("registry_lookup")
			}
			again := claudeinstance.Resolve(probeCtx, l, pinned, fresh, m.claudeProbe, projects)
			if again.State != claudeinstance.Verified || !claudeinstance.SameIdentity(again.Identity, r.Identity) {
				return unknown("binding_changed")
			}
			at := time.Now().UnixNano()
			ok, e = m.st.AdvanceClaudeBinding(probeCtx, *pinned, r.Identity, l.BindingRevision, event, at)
			if ok && e == nil {
				l.BindingRevision++
				event = at
			}
		} else {
			ok, e = m.st.BindClaudeIdentity(r.Identity)
		}
		if e != nil || !ok {
			return unknown("binding_changed")
		}
	}
	observation, e := m.currentClaude(ctx, sess.ID)
	if e != nil {
		return unknown("unbound_launch")
	}
	expected := pinned
	if r.State == claudeinstance.Verified {
		expected = &r.Identity
	}
	if expected == nil || !observation.same(claudeObservation{*expected, l.BindingRevision, event}) {
		return unknown("binding_changed")
	}
	return claudeResolution{r, observation}
}
func (m *Monitor) PushClaudeUpdate(ctx context.Context, sessionID string, state activity.State, event claudeinstance.HookEvent) {
	sess, e := m.st.GetSession(sessionID)
	if e != nil || !m.isClaude(sess) {
		return
	}
	r := m.resolveClaude(ctx, sess)
	if r.State != claudeinstance.Verified {
		return
	}
	if claudeinstance.ValidateHook(ctx, r.Identity, event, string(state), 0, time.Now(), m.claudeProbe) != nil {
		return
	}
	sess, e = m.st.GetSession(sessionID)
	if e != nil {
		return
	}
	next := string(state)
	if state == activity.Exited {
		next = ""
	} else {
		// A root Stop also follows an API-error turn. Qualified hook metadata
		// cannot bypass the exact journal's genuine block. Uncertain/older
		// evidence likewise cannot clear an already established block.
		journal, at, journalErr := claudeJournalActivity(ctx, r.Identity)
		if (journalErr == nil && journal == activity.Blocked) ||
			(sess.Activity == "blocked" && (journalErr != nil || !at.After(time.Unix(sess.ActivityTS, 0)))) {
			next = "blocked"
		}
	} // SessionEnd != OS death.
	ok, e := m.st.AcceptClaudeHookForBinding(ctx, r.observation.target, r.observation.revision, event.At, next, event.At/int64(time.Second))
	if e != nil || !ok {
		return
	}
	m.mu.Lock()
	delete(m.cache, sessionID)
	delete(m.push, sessionID)
	m.mu.Unlock()
	if next != "" && next != sess.Activity {
		m.bus.Publish("session.activity_changed", sessionID, map[string]any{"from": sess.Activity, "to": next, "source": "claude-qualified-hook"})
	}
}
func (m *Monitor) applyClaude(ctx context.Context, sess store.Session, observation claudeObservation, state activity.State, ts time.Time, source string) {
	if state == activity.Ready && time.Since(ts) > m.cfg.ReadyToIdle {
		state = activity.Idle
	}
	ok, e := m.st.ApplyClaudeActivityForBinding(ctx, observation.target, observation.revision, observation.eventAt, string(state), ts.Unix())
	if e != nil || !ok {
		return
	}
	m.mu.Lock()
	delete(m.cache, sess.ID)
	delete(m.push, sess.ID)
	m.mu.Unlock()
	if string(state) != sess.Activity {
		m.bus.Publish("session.activity_changed", sess.ID, map[string]any{"from": sess.Activity, "to": state, "source": source})
	}
}
func (m *Monitor) pollClaudeSession(ctx context.Context, sess store.Session, a agent.Agent, liveSet map[string]bool, haveLiveSet bool) {
	r := m.resolveClaude(ctx, sess)
	// Even runtime disappearance is applied only to the captured exact binding.
	if r.observation.target.RocketSessionID == "" {
		return
	}
	current, err := m.st.GetSession(sess.ID)
	if err != nil {
		return
	}
	sess = current
	if (sess.State != "spawning" && haveLiveSet && !liveSet[sess.TmuxName]) || r.State == claudeinstance.Dead {
		m.applyClaude(ctx, sess, r.observation, activity.Exited, time.Now(), "claude-exact-root-dead")
		return
	}
	state, ts, e := claudeJournalActivity(ctx, r.Identity)
	if e != nil {
		if sess.Activity != "blocked" && sess.Activity != "waiting_input" {
			m.applyClaude(ctx, sess, r.observation, "", time.Now(), "claude-journal-uncertain")
		}
		return
	}

	if state == activity.Exited {
		// Neither a transcript/hook hint nor an old persisted/cache exit proves
		// root death. Clear only the old hint; preserve a prior valid activity.
		if sess.Activity == "exited" {
			m.applyClaude(ctx, sess, r.observation, "", time.Now(), "claude-exit-unverified")
		}
		return
	}
	if state == "" {
		return
	}
	if r.State != claudeinstance.Verified && state != activity.Blocked {
		return
	}
	if sess.Activity == string(activity.WaitingInput) && state != activity.Blocked {
		// A qualified Notification is a suspicion until the pane has been
		// prompt-free for two sweeps. Quiz/capture uncertainty retains it.
		if !m.clearClaudeWait(ctx, sess, r.observation) {
			return
		}
		if now := time.Now(); now.After(ts) {
			ts = now
		}
	} else {
		m.mu.Lock()
		m.resetClaudeWaitLocked(sess.ID, r.observation)
		m.mu.Unlock()
	}
	m.applyClaude(ctx, sess, r.observation, state, ts, "claude-journal-poll")
}

// Full observation tokens isolate captures and misses from transitions/notifications.
type claudeWaitCounter struct {
	observation claudeObservation
	misses      int
}

func (m *Monitor) resetClaudeWaitLocked(id string, observation claudeObservation) {
	if count, ok := m.claudeWaitMiss[id]; ok && count.observation.same(observation) {
		delete(m.claudeWaitMiss, id)
	}
}
func (m *Monitor) clearClaudeWait(ctx context.Context, sess store.Session, observation claudeObservation) bool {
	reset := func() { m.mu.Lock(); defer m.mu.Unlock(); m.resetClaudeWaitLocked(sess.ID, observation) }
	if sess.PendingQuiz != "" {
		reset()
		return false
	}
	out, e := m.rt.Capture(ctx, runtime.Handle{Name: sess.TmuxName}, inputWaitCaptureLines)
	if e != nil {
		return false
	}
	if runtime.LooksLikeInputWait(out) {
		reset()
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, e := m.currentClaude(ctx, sess.ID)
	if e != nil || !current.same(observation) {
		return false
	}
	if m.claudeWaitMiss == nil {
		m.claudeWaitMiss = make(map[string]claudeWaitCounter)
	}
	count := m.claudeWaitMiss[sess.ID]
	if !count.observation.same(observation) {
		count = claudeWaitCounter{observation: observation}
	}
	count.misses++
	m.claudeWaitMiss[sess.ID] = count
	return count.misses >= inputWaitMissThreshold
}
