package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// AcceptCreativeRun is called only after the API verifies human identity. Hold
// the same lock as Spawn/Restore until the durable archive is committed.
func (m *Manager) AcceptCreativeRun(ctx context.Context, runID int64) (store.CreativeRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, err := m.st.BeginCreativeAcceptance(runID, true)
	if err != nil || run.ClosureState == "accepted" {
		return run, err
	}
	fail := func(cause error) (store.CreativeRun, error) {
		if saveErr := m.st.RecordCreativeClosureError(runID, cause.Error()); saveErr != nil {
			cause = errors.Join(cause, fmt.Errorf("save closure error: %w", saveErr))
		}
		m.bus.Publish("creative.run_changed", "", map[string]any{"run_id": runID, "closure_state": "closing", "closure_error": cause.Error()})
		return run, cause
	}
	ids, err := m.st.ListCreativeSessionIDs(runID)
	if err != nil {
		return fail(err)
	}
	sessions := make([]store.Session, 0, len(ids))
	for _, id := range ids {
		s, err := m.st.GetSession(id)
		if err != nil {
			return fail(err)
		}
		if s.ProjectID != run.ProjectID || s.TaskID != 0 || s.SubtaskID != 0 || (s.Kind != "orchestrator" && s.Kind != "worker") {
			return fail(fmt.Errorf("session %s is not owned exclusively by this Creative run", id))
		}
		sessions = append(sessions, s)
	}
	// Destroy even terminal handles: an earlier best-effort Kill may have marked
	// the row terminal while leaving a runtime alive. Never delete workspaces.
	for _, s := range sessions {
		if s.TmuxName != "" {
			if err = m.rt.Destroy(ctx, runtime.Handle{Name: s.TmuxName}); err != nil {
				return fail(fmt.Errorf("stop %s: %w", s.ID, err))
			}
		}
	}
	// Alive is boolean and can swallow command failures; List reports errors.
	live, err := m.rt.List(ctx)
	if err != nil {
		return fail(fmt.Errorf("verify stopped runtimes: %w", err))
	}
	for _, s := range sessions {
		for _, name := range live {
			if name == s.TmuxName && name != "" {
				return fail(fmt.Errorf("session %s runtime is still active", s.ID))
			}
		}
	}
	for _, s := range sessions {
		if s.State != "killed" && s.State != "done" {
			if err = m.st.UpdateSessionState(s.ID, "killed"); err != nil {
				return fail(fmt.Errorf("save stopped session %s: %w", s.ID, err))
			}
			m.clearPendingForTerminal(s.ID)
			m.bus.Publish("session.killed", s.ID, nil)
		}
	}
	if err = m.st.FinishCreativeAcceptance(runID); err != nil {
		return fail(err)
	}
	run, err = m.st.GetCreativeRun(runID)
	if err != nil {
		return fail(err)
	}
	m.bus.Publish("creative.run_changed", "", map[string]any{"run_id": runID, "closure_state": "accepted"})
	return run, nil
}
