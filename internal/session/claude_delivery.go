package session

import (
	"context"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"github.com/IvanRoslov/rocket/internal/store"
	"log/slog"
	"time"
)

func (m *Manager) prepareClaudeLaunch(a agent.Agent, spec *agent.LaunchSpec, sess *store.Session) error {
	if !agent.SupportsClaudeSocket(a) {
		return nil
	}
	generation, e := socketmsg.NewMsgID()
	if e != nil {
		return e
	}
	l := claudeinstance.Launch{RocketSessionID: sess.ID, Generation: generation, StartedAt: time.Now().UTC()}
	if e = m.st.PutClaudeLaunch(l); e != nil {
		return e
	}
	spec.ClaudeLaunchGeneration = generation
	sess.Activity = ""
	sess.ActivityTS = 0
	return nil
}
func (m *Manager) captureClaudeLaunch(ctx context.Context, spec agent.LaunchSpec) {
	if spec.ClaudeLaunchGeneration == "" {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	l, _, e := m.st.GetClaudeLaunch(spec.SessionID)
	if e == nil && l.Generation == spec.ClaudeLaunchGeneration && m.claudeProbe != nil {
		var pane claudeinstance.Pane
		var exists bool
		pane, exists, e = m.claudeProbe.Pane(probeCtx, spec.SessionID)
		if e == nil && exists && pane.PID > 0 && pane.Identity != "" && !pane.Start.IsZero() {
			l.PaneIdentity, l.PanePID, l.PaneStart = pane.Identity, pane.PID, pane.Start
			e = m.st.PutClaudeLaunch(l)
			if e == nil {
				return
			}
		}
	}
	slog.Warn("session: Claude launch anchor unavailable; socket delivery disabled for this launch", "session", spec.SessionID)
}
