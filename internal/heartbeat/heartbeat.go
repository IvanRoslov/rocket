// Package heartbeat implements rocket's orchestrator heartbeat: a periodic
// sweep that detects stalled workers and overdue question threads for each
// live, in-progress orchestrator, and — if the orchestrator itself isn't
// currently active — nudges it with a summary message so it can act
// autonomously (restart/replace stalled workers, answer pending questions)
// without waiting for a human to notice.
package heartbeat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// escalationAgent is the persistent agent an input-stalled orchestrator is
// escalated to. Its inbox — not the message queue — is the target: the agent
// may well not be running, and an escalation must not be lost when it isn't.
const escalationAgent = "cto"

// taskLogAuthor is what the heartbeat signs its task_log entries with. Not
// the stalled orchestrator's id — the entry is written BY the heartbeat ABOUT
// the orchestrator, and attributing it to the orchestrator reads as a
// confession it never made. Not the empty author either: `rocket task show`
// renders that as "user" (see internal/cli/task.go), which would credit the
// human. task_log.author is a free-text column with no foreign key, so a
// named non-session author is both allowed and the only spelling that
// displays honestly.
const taskLogAuthor = "heartbeat"

// escalationKeyPrefix namespaces input-stall entries in lastSent so an
// ordinary heartbeat summary and an escalation never suppress each other.
const escalationKeyPrefix = "input-stall:"

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
	// lastStallRef remembers, per orchestrator, the reference timestamp of the
	// input stall already acted on. The nudge and the problem-log entry fire
	// once per stall EPISODE (not once per interval like the cto escalation):
	// while the same prompt is still open the reference does not move, and
	// nothing is repeated.
	lastStallRef map[string]int64
}

// New builds a Heartbeat. getActivity is typically Monitor.Activity; wake is
// typically Queue.Wake. Messages are delivered via st.AddMessage directly
// (mirroring the enqueue pattern in internal/api/messages.go and
// internal/api/questions.go's deliverToOrchestrator), decoupling this
// package from the concrete *queue.Queue type.
func New(st *store.Store, b *bus.Bus, cfg *config.Config, getActivity func(sessionID string) (activity.State, bool), wake func(to string)) *Heartbeat {
	return &Heartbeat{
		st:           st,
		bus:          b,
		cfg:          cfg,
		getActivity:  getActivity,
		wake:         wake,
		nowFunc:      time.Now,
		lastSent:     make(map[string]time.Time),
		lastStallRef: make(map[string]int64),
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
	// Thread staleness is a property of the thread, not of any orchestrator:
	// it is swept once per tick, and its failure must not cost the
	// orchestrator sweep below.
	if err := h.sweepStaleThreads(); err != nil {
		slog.Error("heartbeat: sweep stale threads", "error", err)
	}

	// A milestone belongs to no project and to no orchestrator either: like
	// thread staleness, its silence is swept once per tick and its failure
	// must not cost the orchestrator sweep below.
	if err := h.sweepQuietMilestones(); err != nil {
		slog.Error("heartbeat: sweep quiet milestones", "error", err)
	}

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
	if !ok || !store.IsActiveTaskStatus(task.Status) {
		return nil
	}

	// The orchestrator's own input stall is escalated outward, independently
	// of the worker/question summary below: that summary is addressed to the
	// orchestrator itself, which is exactly the party that cannot act while
	// it waits for a keystroke.
	h.escalateInputStall(orch, task)

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

	if !h.antiSpamOK(orch.ID, now, h.cfg.HeartbeatInterval) {
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

// InputStalled reports how long sess has been stalled on interactive input
// and whether that exceeds threshold. A session is stalled on interactive
// input while it holds a pending AskUserQuestion quiz, or while its activity
// is waiting_input — in both cases nothing moves until somebody types.
//
// The reference point is the quiz's asked_at when a quiz is present (the
// activity timestamp keeps moving while a quiz is open, so it would
// understate the wait), otherwise the activity timestamp. Without a usable
// reference point the session is reported as not stalled rather than stalled
// since the epoch.
func InputStalled(sess store.Session, now time.Time, threshold time.Duration) (since time.Duration, ok bool) {
	ref := inputStallRef(sess)
	if ref <= 0 {
		return 0, false
	}
	since = now.Sub(time.Unix(ref, 0))
	return since, since > threshold
}

// inputStallRef returns the timestamp an input stall is measured from, or 0
// when the session is not waiting on input at all. It doubles as the identity
// of the stall episode: it stays put for as long as the same prompt or quiz is
// open, and moves when a new one starts.
func inputStallRef(sess store.Session) int64 {
	switch {
	case sess.PendingQuiz != "":
		var quiz session.Quiz
		if err := json.Unmarshal([]byte(sess.PendingQuiz), &quiz); err == nil && quiz.AskedAt > 0 {
			return quiz.AskedAt
		}
		return sess.ActivityTS
	case activity.State(sess.Activity) == activity.WaitingInput:
		return sess.ActivityTS
	default:
		return 0
	}
}

// escalateInputStall writes an escalation to the cto agent's inbox and
// publishes orchestrator.input_stalled when orch has been waiting on
// interactive input longer than the configured threshold. Failures are
// logged, never returned: one unreachable escalation must not abort the
// sweep over the remaining orchestrators.
func (h *Heartbeat) escalateInputStall(orch store.Session, task store.Task) {
	now := h.nowFunc()
	since, stalled := InputStalled(orch, now, h.cfg.InputStallThreshold)
	if !stalled {
		return
	}
	if !h.antiSpamOK(escalationKeyPrefix+orch.ID, now, h.cfg.HeartbeatInterval) {
		return
	}

	kind := "prompt"
	question := ""
	if orch.PendingQuiz != "" {
		kind = "quiz"
		var quiz session.Quiz
		if err := json.Unmarshal([]byte(orch.PendingQuiz), &quiz); err == nil && len(quiz.Questions) > 0 {
			question = quiz.Questions[0].Question
		}
	}

	if _, err := h.st.AddInboxMessage(store.InboxMessage{
		AgentID: escalationAgent,
		From:    orch.ID,
		Body:    escalationBody(orch, task, since, kind, question),
	}); err != nil {
		// Most likely the cto agent is not registered (agent_inbox has a
		// foreign key to agents): the bus event below still carries the
		// signal to the dashboard and `rocket events`.
		slog.Warn("heartbeat: escalate input stall to agent inbox",
			"agent", escalationAgent, "orchestrator", orch.ID, "error", err)
	}

	h.mu.Lock()
	h.lastSent[escalationKeyPrefix+orch.ID] = now
	h.mu.Unlock()

	h.nudgeInputStall(orch, task, since, kind)

	if h.bus != nil {
		h.bus.Publish("orchestrator.input_stalled", orch.ID, map[string]any{
			"task_id":       task.ID,
			"session_id":    orch.ID,
			"since_seconds": int64(since.Seconds()),
			"kind":          kind,
		})
	}
}

// nudgeInputStall is the half of the response addressed to the stalled
// orchestrator itself: a message telling it to stop asking through a terminal
// nobody watches, plus a `problem` entry in its task log so the episode is
// visible afterwards in `rocket task show`.
//
// Both fire once per stall episode (not once per heartbeat interval): the log
// is a record of "this happened", and a fresh copy every five minutes would
// bury the task's other entries.
//
// Nothing is sent while a quiz is pending. An open AskUserQuestion quiz pauses
// delivery to the session entirely (docs/06-messaging.md), so a nudge would
// only queue up behind the very thing it is meant to break — for that case the
// escalation to cto above is the whole answer.
func (h *Heartbeat) nudgeInputStall(orch store.Session, task store.Task, since time.Duration, kind string) {
	if kind != "prompt" {
		return
	}

	ref := inputStallRef(orch)
	h.mu.Lock()
	already := h.lastStallRef[orch.ID] == ref && ref != 0
	if !already {
		h.lastStallRef[orch.ID] = ref
	}
	h.mu.Unlock()
	if already {
		return
	}

	body := nudgeBody(task)
	if id, err := h.st.AddMessage(store.Message{ToSession: orch.ID, Body: body}); err != nil {
		slog.Warn("heartbeat: queue input-stall nudge", "orchestrator", orch.ID, "error", err)
	} else {
		if h.bus != nil {
			h.bus.Publish("message.queued", orch.ID, map[string]any{"id": id, "from": "", "to": orch.ID})
		}
		if h.wake != nil {
			h.wake(orch.ID)
		}
	}

	if _, err := h.st.AddTaskLog(store.TaskLogEntry{
		TaskID: task.ID,
		Kind:   "problem",
		Body: fmt.Sprintf(
			"Оркестратор %s висит на интерактивном промпте %dm — ждёт нажатия клавиши, которого никто не видит. "+
				"Ожидание в терминале невыразимо в системе: спрашивать надо тредом (rocket task ask), а не промптом.",
			orch.ID, int(since.Minutes())),
		Author: taskLogAuthor,
	}); err != nil {
		slog.Warn("heartbeat: log input stall as a problem",
			"orchestrator", orch.ID, "task", task.ID, "error", err)
	}
}

// nudgeBody is the message injected into a prompt-stalled orchestrator. It
// names the exact way out rather than describing the situation: the recipient
// is an agent that will act on the first actionable line it reads.
func nudgeBody(task store.Task) string {
	return fmt.Sprintf(
		"[rocket] You are stalled on an interactive prompt nobody watches. "+
			"Never ask through the terminal — ask through the task instead: "+
			"rocket task ask %d \"<question>\" (add --to <who> to name whose turn it is, "+
			"--option \"A\" --option \"B\" to offer choices). "+
			"Answer the prompt you are sitting on now, then re-ask that way.",
		task.ID)
}

// escalationBody assembles the inbox message: what is stuck, for how long,
// what it is being asked, and how to unstick it.
func escalationBody(orch store.Session, task store.Task, since time.Duration, kind, question string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[rocket input stall] Orchestrator %s (task #%d %q, feature %s) has been waiting on interactive input for %dm.\n",
		orch.ID, task.ID, task.Title, orch.FeatureSlug, int(since.Minutes()))
	if kind == "quiz" {
		b.WriteString("It is showing an AskUserQuestion quiz")
		if question != "" {
			fmt.Fprintf(&b, ": %q", question)
		}
		b.WriteString("\n")
	} else {
		b.WriteString("It is sitting at a text prompt (activity waiting_input).\n")
	}
	fmt.Fprintf(&b, "Answer it: `rocket attach %s`, or use the quiz bubble in the dashboard chat.", orch.ID)
	return b.String()
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

// liveSession reports whether s is still a live session — one that can
// still make progress or stall. Mirrors sessionWaitingTerminal in
// internal/api/waiting.go: the two must share the same notion of "live"
// so the stall vocabulary does not drift.
func liveSession(s store.Session) bool {
	return s.State == "spawning" || s.State == "running"
}

// stalledWorkerLine reports whether w counts as stalled and, if so, its
// summary line. Only a live session (State spawning/running) can stall: a
// killed or finished worker keeps its last Activity forever and would
// otherwise be re-reported on every tick until the root task leaves
// in_progress. Within a live session, a worker is stalled if its activity
// is "exited" (its tmux pane died; store.Session.State stays "running"
// until the reconciler later promotes it to "errored", so State alone is
// not the field to check here), or if its activity is
// idle/blocked/waiting_input and it has been that way longer than
// threshold.
func stalledWorkerLine(w store.Session, now time.Time, threshold time.Duration) (string, bool) {
	if !liveSession(w) {
		return "", false
	}

	if activity.State(w.Activity) == activity.Exited {
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
			prInfo := "no PR"
			if w.PRNumber > 0 {
				ci := w.CIState
				if ci == "" {
					ci = "unknown"
				}
				prInfo = fmt.Sprintf("PR #%d CI %s", w.PRNumber, ci)
			}
			return fmt.Sprintf("- %s: %s %dm, %s", w.ID, w.Activity, mins, prInfo), true
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
	if !store.IsHuman(last.Author) {
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
	if runes := []rune(snippet); len(runes) > 60 {
		snippet = string(runes[:60])
	}
	return fmt.Sprintf("- Q%d %q waiting for your reply %dm", ordinal, snippet, mins), true, nil
}

// antiSpamOK reports whether more than window has passed since the last
// message sent under key (or none has ever been sent). key is an orchestrator
// id for the ordinary summary, and a prefixed compound for the other senders
// (see escalationKeyPrefix, staleKeyPrefix) so they never suppress each other.
func (h *Heartbeat) antiSpamOK(key string, now time.Time, window time.Duration) bool {
	h.mu.Lock()
	last, ok := h.lastSent[key]
	h.mu.Unlock()
	if !ok {
		return true
	}
	return now.Sub(last) > window
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
