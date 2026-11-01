package monitor

import (
	"context"
	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
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
func (m *Monitor) resolveClaude(ctx context.Context, sess store.Session) claudeinstance.Resolution {
	l, pinned, e := m.st.GetClaudeLaunch(sess.ID)
	if e != nil {
		return claudeinstance.Resolution{State: claudeinstance.Unknown, Reason: "unproven_launch"}
	}
	all, e := socketmsg.ListSessions(m.claudeSessionsDir)
	if e != nil {
		return claudeinstance.Resolution{State: claudeinstance.Unknown, Reason: "registry_lookup"}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	r := claudeinstance.Resolve(probeCtx, l, pinned, all, m.claudeProbe, filepath.Join(filepath.Dir(m.claudeSessionsDir), "projects"))
	if r.State == claudeinstance.Verified {
		ok, e := m.st.BindClaudeIdentity(r.Identity)
		if e != nil || !ok {
			return claudeinstance.Resolution{State: claudeinstance.Unknown, Reason: "binding_changed"}
		}
	}
	return r
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
	next := string(state)
	if state == activity.Exited {
		next = ""
	} // SessionEnd != OS death.
	ok, e := m.st.AcceptClaudeHook(sessionID, event.LaunchGeneration, event.At, next, event.At/int64(time.Second))
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
func (m *Monitor) applyClaude(sess store.Session, generation string, state activity.State, ts time.Time, source string) {
	if state == activity.Ready && time.Since(ts) > m.cfg.ReadyToIdle {
		state = activity.Idle
	}
	ok, e := m.st.ApplyClaudeActivity(sess.ID, generation, string(state), ts.Unix())
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
	l, _, _ := m.st.GetClaudeLaunch(sess.ID)
	if sess.State != "spawning" && haveLiveSet && !liveSet[sess.TmuxName] {
		m.applyClaude(sess, l.Generation, activity.Exited, time.Now(), "claude-runtime-gone")
		return
	}
	r := m.resolveClaude(ctx, sess)
	if r.State == claudeinstance.Dead {
		m.applyClaude(sess, l.Generation, activity.Exited, time.Now(), "claude-exact-root-dead")
		return
	}
	state, ts, e := claudeJournalActivity(ctx, r.Identity)
	if e != nil {
		if sess.Activity != "blocked" {
			m.applyClaude(sess, l.Generation, "", time.Now(), "claude-journal-uncertain")
		}
		return
	}

	if state == activity.Exited {
		// Neither a transcript/hook hint nor an old persisted/cache exit proves
		// root death. Clear only the old hint; preserve a prior valid activity.
		if sess.Activity == "exited" {
			m.applyClaude(sess, l.Generation, "", time.Now(), "claude-exit-unverified")
		}
		return
	}
	if state == "" {
		return
	}
	if r.State != claudeinstance.Verified && state != activity.Blocked {
		return
	}
	m.applyClaude(sess, l.Generation, state, ts, "claude-journal-poll")
}
