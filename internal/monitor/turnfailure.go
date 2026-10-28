package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/store"
)

// turnFailedEvent journals each failed turn the monitor has reported, so a
// daemon restart does not report it again.
const turnFailedEvent = "session.turn_failed"

// agentTurnLabel names an agent in a turn-failure alert.
func agentTurnLabel(name string) string {
	switch name {
	case "kimi-code":
		return "Kimi"
	case "codex":
		return "Codex"
	}
	return name
}

// reportTurnFailure tells sess's parent (else its task's problem log), once
// per failed turn, that the agent's latest turn failed and why — e.g. Kimi's
// subscription quota ran out (HTTP 429), which Activity only shows as
// blocked. It applies to adapters implementing agent.TurnFailureReporter.
// The last reported turn is kept in the event log (turnFailedEvent), and in
// memory too, so a failed event write cannot repeat the alert every sweep.
func (m *Monitor) reportTurnFailure(ctx context.Context, sess store.Session, ag agent.Agent, ref agent.ActivityRef) {
	r, ok := ag.(agent.TurnFailureReporter)
	if !ok || sess.Kind == "agent" {
		return
	}
	turnID, msg, failed, err := r.LastTurnFailure(ctx, ref)
	if err != nil || !failed {
		return
	}
	m.mu.Lock()
	reported := m.turnFailures[sess.ID] == turnID
	m.mu.Unlock()
	if reported {
		return
	}
	last, seen, err := m.st.LastSessionEventData(sess.ID, turnFailedEvent)
	if err != nil {
		slog.Warn("monitor: read last reported turn failure", "session", sess.ID, "error", err)
		return
	}
	if seen && last["turn"] == turnID {
		return
	}
	body := fmt.Sprintf("[rocket] session %s: %s turn failed: %s", sess.ID, agentTurnLabel(sess.Agent), msg)
	if !m.alertParentOrTask(sess, body, "turn failure", msg) {
		return
	}
	m.mu.Lock()
	if m.turnFailures == nil {
		m.turnFailures = make(map[string]string)
	}
	m.turnFailures[sess.ID] = turnID
	m.mu.Unlock()
	if _, err := m.st.AppendEvent(store.Event{
		TS: time.Now().Unix(), Type: turnFailedEvent, SessionID: sess.ID,
		Data: map[string]any{"turn": turnID, "error": msg},
	}); err != nil {
		slog.Warn("monitor: journal reported turn failure", "session", sess.ID, "error", err)
	}
}
