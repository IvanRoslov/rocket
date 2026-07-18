// Package heartbeat implements rocket's orchestrator heartbeat: a periodic
// sweep that detects stalled workers and overdue question threads for each
// live, in-progress orchestrator, and — if the orchestrator itself isn't
// currently active — nudges it with a summary message so it can act
// autonomously (restart/replace stalled workers, answer pending questions)
// without waiting for a human to notice.
package heartbeat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/store"
)

// Heartbeat runs the periodic stall-detection and question-reminder sweep.
type Heartbeat struct {
	st          *store.Store
	bus         *bus.Bus
	cfg         *config.Config
	getActivity func(sessionID string) (activity.State, bool)
	wake        func(to string)

	// nowFunc returns the current time; overridable by tests.
	nowFunc func() time.Time

	mu       sync.Mutex
	lastSent map[string]time.Time
}

// New builds a Heartbeat. getActivity is typically Monitor.Activity; wake is
// typically Queue.Wake. Messages are delivered via st.AddMessage directly
// (mirroring the enqueue pattern in internal/api/messages.go and
// internal/api/questions.go's deliverToOrchestrator), decoupling this
// package from the concrete *queue.Queue type.
func New(st *store.Store, b *bus.Bus, cfg *config.Config, getActivity func(sessionID string) (activity.State, bool), wake func(to string)) *Heartbeat {
	return &Heartbeat{
		st:          st,
		bus:         b,
		cfg:         cfg,
		getActivity: getActivity,
		wake:        wake,
		nowFunc:     time.Now,
		lastSent:    make(map[string]time.Time),
	}
}

// Run runs Tick at cfg.HeartbeatInterval, blocking until ctx is cancelled.
// It performs one immediate Tick before the first tick.
func (h *Heartbeat) Run(ctx context.Context) {
	if err := h.Tick(ctx); err != nil {
		slog.Error("heartbeat: tick", "error", err)
	}

	ticker := time.NewTicker(h.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.Tick(ctx); err != nil {
				slog.Error("heartbeat: tick", "error", err)
			}
		}
	}
}

// Tick runs a single pass of the heartbeat sweep over every live
// (spawning|running) orchestrator session.
func (h *Heartbeat) Tick(ctx context.Context) error {
	orchestrators, err := h.st.ListSessions(store.SessionFilter{Kind: "orchestrator"})
	if err != nil {
		return fmt.Errorf("list orchestrators: %w", err)
	}

	for _, orch := range orchestrators {
		if err := h.tickOne(orch); err != nil {
			slog.Error("heartbeat: tick orchestrator", "orchestrator", orch.ID, "error", err)
		}
	}
	return nil
}

// tickOne evaluates and, if warranted, sends a heartbeat for a single
// orchestrator session.
func (h *Heartbeat) tickOne(orch store.Session) error {
	task, ok, err := h.rootTask(orch.ID)
	if err != nil {
		return fmt.Errorf("find root task: %w", err)
	}
	if !ok || task.Status != "in_progress" {
		return nil
	}

	workers, err := h.st.ListSessions(store.SessionFilter{Kind: "worker", All: true})
	if err != nil {
		return fmt.Errorf("list workers: %w", err)
	}

	now := h.nowFunc()
	var stalledLines []string
	for _, w := range workers {
		if w.ParentID != orch.ID {
			continue
		}
		if line, stalled := stalledWorkerLine(w, now, h.cfg.WorkerStallThreshold); stalled {
			stalledLines = append(stalledLines, line)
		}
	}

	questions, err := h.st.ListQuestions(task.ID, true)
	if err != nil {
		return fmt.Errorf("list questions: %w", err)
	}

	var reminderLines []string
	for _, q := range questions {
		line, overdue, err := h.reminderLine(q, now)
		if err != nil {
			return fmt.Errorf("reminder for question %d: %w", q.ID, err)
		}
		if overdue {
			reminderLines = append(reminderLines, line)
		}
	}

	if len(stalledLines) == 0 && len(reminderLines) == 0 {
		return nil
	}

	if state, known := h.getActivity(orch.ID); known && state == activity.Active {
		// Orchestrator is actively working: don't interrupt it.
		return nil
	}

	if !h.antiSpamOK(orch.ID, now) {
		return nil
	}

	body := buildSummary(orch.FeatureSlug, stalledLines, reminderLines)
	if _, err := h.st.AddMessage(store.Message{ToSession: orch.ID, Body: body}); err != nil {
		return fmt.Errorf("enqueue heartbeat message: %w", err)
	}
	if h.bus != nil {
		h.bus.Publish("message.queued", orch.ID, map[string]any{"from": "", "to": orch.ID})
	}
	if h.wake != nil {
		h.wake(orch.ID)
	}

	h.mu.Lock()
	h.lastSent[orch.ID] = now
	h.mu.Unlock()

	if h.bus != nil {
		h.bus.Publish("orchestrator.heartbeat_sent", orch.ID, map[string]any{
			"task_id":   task.ID,
			"stalled":   len(stalledLines),
			"reminders": len(reminderLines),
		})
	}

	return nil
}

// rootTask returns the root task (parent_id IS NULL) whose session_id
// matches orchID, if any.
func (h *Heartbeat) rootTask(orchID string) (store.Task, bool, error) {
	tasks, err := h.st.ListTasks(store.TaskFilter{ParentSet: true, Parent: 0})
	if err != nil {
		return store.Task{}, false, err
	}
	for _, t := range tasks {
		if t.SessionID == orchID {
			return t, true, nil
		}
	}
	return store.Task{}, false, nil
}

// stalledWorkerLine reports whether w counts as stalled and, if so, its
// summary line. A worker is stalled if its state is "exited", or if its
// activity is idle/blocked/waiting_input and it has been that way longer
// than threshold.
func stalledWorkerLine(w store.Session, now time.Time, threshold time.Duration) (string, bool) {
	if w.State == "exited" {
		return fmt.Sprintf("- %s: exited", w.ID), true
	}

	switch activity.State(w.Activity) {
	case activity.Idle, activity.Blocked, activity.WaitingInput:
		if w.ActivityTS == 0 {
			return "", false
		}
		since := now.Sub(time.Unix(w.ActivityTS, 0))
		if since > threshold {
			mins := int(since.Minutes())
			return fmt.Sprintf("- %s: %s %dm, no PR", w.ID, w.Activity, mins), true
		}
	}
	return "", false
}

// reminderLine reports whether q is overdue for the orchestrator's
// attention (its thread's last entry is from the human, and it has been
// waiting longer than QuestionReminderThreshold) and, if so, its summary
// line.
//
// whoseTurn mirrors the logic in internal/api/questions.go: the question
// itself (asked by the orchestrator) counts as the first entry when no
// messages exist yet; otherwise the last entry's author determines whose
// turn it is. A human-authored last entry (Author == "") means it's the
// orchestrator's turn to act.
func (h *Heartbeat) reminderLine(q store.Question, now time.Time) (string, bool, error) {
	msgs, err := h.st.ListQuestionMessages(q.ID)
	if err != nil {
		return "", false, err
	}
	if len(msgs) == 0 {
		// Awaiting the human's first reply; not the orchestrator's turn.
		return "", false, nil
	}
	last := msgs[len(msgs)-1]
	if last.Author != "" {
		// Last entry from the orchestrator; not overdue for it.
		return "", false, nil
	}

	since := now.Sub(time.Unix(last.CreatedAt, 0))
	if since <= h.cfg.QuestionReminderThreshold {
		return "", false, nil
	}

	ordinal, err := h.st.QuestionOrdinal(q)
	if err != nil {
		return "", false, err
	}

	mins := int(since.Minutes())
	snippet := q.Body
	if len(snippet) > 60 {
		snippet = snippet[:60]
	}
	return fmt.Sprintf("- Q%d %q waiting for your reply %dm", ordinal, snippet, mins), true, nil
}

// antiSpamOK reports whether enough time has passed since the last
// heartbeat sent to orchID (or none has ever been sent).
func (h *Heartbeat) antiSpamOK(orchID string, now time.Time) bool {
	h.mu.Lock()
	last, ok := h.lastSent[orchID]
	h.mu.Unlock()
	if !ok {
		return true
	}
	return now.Sub(last) > h.cfg.HeartbeatInterval
}

// buildSummary assembles the heartbeat message body.
func buildSummary(featureSlug string, stalledLines, reminderLines []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[rocket heartbeat] Feature %s status:\n", featureSlug)
	for _, l := range stalledLines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	for _, l := range reminderLines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	b.WriteString("Act autonomously: unblock, restart or replace stalled workers. Answer pending question threads (rocket task reply).")
	return b.String()
}
