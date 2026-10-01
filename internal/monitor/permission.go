package monitor

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// claudeCodeAgent is the only agent whose permission dialogs are read off
// the pane; Codex runs with approvals disabled.
const claudeCodeAgent = "claude-code"

// permissionMissThreshold is how many consecutive sweeps must see the pane
// without the dialog before a pending permission quiz is declared closed —
// the same two-miss filter against capture glitches as quizMissThreshold.
const permissionMissThreshold = 2

// permissionChangeThreshold is how many consecutive sweeps must read a
// different dialog than the pending one before the card switches: a single
// capture taken mid-redraw, or after a resize cut the dialog's header, can
// read the same dialog with another Context.
const permissionChangeThreshold = 2

// permissionAnswerSettle is how long after an API answer's keypress the
// same dialog may still be on the pane. A digit closes a permission dialog
// at once (recon §3), so the identical dialog seen later is a new instance
// — Claude asked the same command again — not the answered one. sent_at
// has one-second resolution, so this is compared in whole seconds and is
// at least one full second in real time.
const permissionAnswerSettle = 2 * time.Second

// permissionCandidate is a dialog read off the pane that differs from the
// pending one, and on how many consecutive sweeps it was read.
type permissionCandidate struct {
	prompt runtime.PermissionPrompt
	seen   int
}

// pollPermission publishes a Claude Code permission dialog sitting on the
// pane as the session's pending quiz (source "permission"), switches it
// when the agent moves on to the next dialog, and clears it once the dialog
// is gone — answered from chat, in the terminal, or cancelled. The
// transcript says nothing while a dialog is open and no hook reports it, so
// the pane is the only source; permission_prompts journals every dialog for
// the chat feed.
//
// It looks only at claude-code sessions that either already hold a
// permission quiz or claim to wait on the human (waiting_input, which the
// Notification hook sets on a permission dialog, or blocked). A hook-driven
// AskUserQuestion quiz is never touched: its lifecycle belongs to the hooks
// and pollQuiz. Writes to pending_quiz are compare-and-swap against what
// this sweep read, so a hook quiz landing in between is never overwritten.
//
// Identity is Title + Context (runtime.PermissionPrompt.SameDialog), read
// at runtime.PermissionCaptureLines — the depth the answer path re-reads at.
func (m *Monitor) pollPermission(ctx context.Context, sess store.Session) {
	if sess.Agent != claudeCodeAgent {
		m.forgetPermissionMisses(sess.ID)
		m.forgetPermissionCandidate(sess.ID)
		return
	}
	pending, hasPending := session.ParseQuiz(sess.PendingQuiz)
	if sess.PendingQuiz != "" && (!hasPending || !pending.IsPermission()) {
		m.forgetPermissionMisses(sess.ID)
		m.forgetPermissionCandidate(sess.ID)
		return
	}
	if !hasPending {
		if state, _ := m.Activity(sess.ID); state != activity.WaitingInput && state != activity.Blocked {
			m.forgetPermissionMisses(sess.ID)
			m.forgetPermissionCandidate(sess.ID)
			return
		}
	}

	out, err := m.rt.Capture(ctx, runtime.Handle{Name: sess.TmuxName}, runtime.PermissionCaptureLines)
	if err != nil {
		return
	}

	if p, ok := runtime.ParsePermissionPrompt(out); ok {
		m.forgetPermissionMisses(sess.ID)
		switch {
		case !hasPending:
			m.publishPermission(sess, p, false)
		case pending.PermissionPrompt().SameDialog(p):
			m.forgetPermissionCandidate(sess.ID)
			if m.answeredAndStillShown(pending) {
				// The answered dialog closed and Claude asked the very
				// same thing again within one sweep.
				if err := m.st.ResolvePermissionPrompt(pending.Permission.PromptID, time.Now().Unix()); err != nil {
					slog.Warn("monitor: resolve answered permission prompt", "session", sess.ID, "error", err)
					return
				}
				m.publishPermission(sess, p, true)
			}
		case m.permissionChangeConfirmed(sess.ID, p):
			m.publishPermission(sess, p, true)
		}
		return
	}
	m.forgetPermissionCandidate(sess.ID)
	if !hasPending {
		return
	}

	m.mu.Lock()
	m.permMiss[sess.ID]++
	misses := m.permMiss[sess.ID]
	if misses >= permissionMissThreshold {
		delete(m.permMiss, sess.ID)
	}
	m.mu.Unlock()
	if misses < permissionMissThreshold {
		return
	}

	swapped, err := m.st.CompareAndSwapPendingQuiz(sess.ID, sess.PendingQuiz, "")
	if err != nil {
		slog.Warn("monitor: clear permission quiz", "session", sess.ID, "error", err)
		return
	}
	if !swapped {
		return
	}
	if err := m.st.ResolvePermissionPrompt(pending.Permission.PromptID, time.Now().Unix()); err != nil {
		slog.Warn("monitor: resolve permission prompt", "session", sess.ID, "error", err)
	}
	slog.Info("monitor: permission dialog closed", "session", sess.ID, "title", pending.Permission.Title)
	m.bus.Publish("session.quiz_resolved", sess.ID, map[string]any{})
}

// publishPermission journals dialog p (reusing the open row of the same
// dialog, e.g. after a daemon restart, and closing any other) and makes it
// the session's pending quiz. Replacing a previous permission quiz first
// publishes session.quiz_resolved for it: that is what ends a chat answer's
// in-flight wait for the old dialog instead of a false
// quiz_answer_unconfirmed.
func (m *Monitor) publishPermission(sess store.Session, p runtime.PermissionPrompt, replacing bool) {
	options, err := json.Marshal(p.Options)
	if err != nil {
		return
	}
	now := time.Now().Unix()
	id, reused, err := m.st.OpenPermissionPrompt(sess.ID, p.Title, p.Context, string(options), now)
	if err != nil {
		slog.Warn("monitor: journal permission prompt", "session", sess.ID, "error", err)
		return
	}
	quiz, err := json.Marshal(session.NewPermissionQuiz(p, id, now))
	if err != nil {
		return
	}
	swapped, err := m.st.CompareAndSwapPendingQuiz(sess.ID, sess.PendingQuiz, string(quiz))
	if err != nil {
		slog.Warn("monitor: set permission quiz", "session", sess.ID, "error", err)
		return
	}
	if !swapped {
		// A hook quiz (or another writer) got there first; nothing points
		// at a row opened just now, so it must not stay behind.
		if !reused {
			if err := m.st.DeletePermissionPrompt(id); err != nil {
				slog.Warn("monitor: drop unattached permission prompt", "session", sess.ID, "error", err)
			}
		}
		return
	}
	slog.Info("monitor: permission dialog open", "session", sess.ID, "title", p.Title)
	if replacing {
		m.bus.Publish("session.quiz_resolved", sess.ID, map[string]any{})
	}
	m.bus.Publish("session.quiz_asked", sess.ID, map[string]any{})
}

func (m *Monitor) forgetPermissionMisses(sessionID string) {
	m.mu.Lock()
	delete(m.permMiss, sessionID)
	m.mu.Unlock()
}

// permissionChangeConfirmed records that p, a different dialog than the
// pending one, was read this sweep, and reports whether it has now been
// read on permissionChangeThreshold consecutive sweeps.
func (m *Monitor) permissionChangeConfirmed(sessionID string, p runtime.PermissionPrompt) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.permNext[sessionID]
	if c.seen > 0 && c.prompt.SameDialog(p) {
		c.seen++
	} else {
		c = permissionCandidate{prompt: p, seen: 1}
	}
	if c.seen >= permissionChangeThreshold {
		delete(m.permNext, sessionID)
		return true
	}
	m.permNext[sessionID] = c
	return false
}

func (m *Monitor) forgetPermissionCandidate(sessionID string) {
	m.mu.Lock()
	delete(m.permNext, sessionID)
	m.mu.Unlock()
}

// answeredAndStillShown reports whether an API answer's keypress for the
// pending dialog was sent at least permissionAnswerSettle ago — so the
// identical dialog on the pane now is a new one. Without an API answer
// (terminal answers) this never applies: those close through the miss path.
func (m *Monitor) answeredAndStillShown(pending session.Quiz) bool {
	row, err := m.st.GetPermissionPrompt(pending.Permission.PromptID)
	if err != nil || row.SentAt == 0 || row.ResolvedAt != 0 {
		return false
	}
	return time.Now().Unix()-row.SentAt >= int64(permissionAnswerSettle/time.Second)
}
