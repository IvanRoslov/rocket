package ghpoller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/github"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// maxGraceReschedules bounds how many times Reactions will push a merge
// grace timer back out while the worker session is still active, so a
// worker that never goes idle doesn't get rescheduled forever.
const maxGraceReschedules = 5

// Reactions is the real PRNotifier implementation wired into the daemon's
// ghpoller.Poller: it drives subtask status transitions off PR lifecycle
// events, nudges a stuck worker via the message queue, and auto-destroys a
// worker's session/workspace some grace period after its PR merges (subject
// to the repo's AutoCleanup setting and the worker still being idle).
type Reactions struct {
	st          *store.Store
	bus         *bus.Bus
	wake        func(sessionID string)
	mgr         *session.Manager
	getActivity func(sessionID string) (activity.State, bool)
	cfg         *config.Config

	mu              sync.Mutex
	lastNotifiedSHA map[string]string // sessionID -> last CI-failing HeadSHA notified
	timers          []*time.Timer
	stopped         bool
}

// NewReactions builds a Reactions notifier.
func NewReactions(
	st *store.Store,
	b *bus.Bus,
	wake func(sessionID string),
	mgr *session.Manager,
	getActivity func(sessionID string) (activity.State, bool),
	cfg *config.Config,
) *Reactions {
	return &Reactions{
		st:              st,
		bus:             b,
		wake:            wake,
		mgr:             mgr,
		getActivity:     getActivity,
		cfg:             cfg,
		lastNotifiedSHA: make(map[string]string),
	}
}

// Stop cancels every pending merge-grace timer and prevents new ones from
// being scheduled. Callers (the daemon) should call it on shutdown.
func (r *Reactions) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	for _, t := range r.timers {
		t.Stop()
	}
	r.timers = nil
}

// PROpened moves sess's subtask (if any, and if currently in_progress) to
// review and logs the transition.
func (r *Reactions) PROpened(sess store.Session, pr *github.PR) {
	task, err := r.st.GetTaskBySessionID(sess.ID)
	if err != nil {
		return
	}
	if task.Status != "in_progress" {
		return
	}
	if err := r.st.UpdateTaskStatus(task.ID, "review"); err != nil {
		slog.Error("ghpoller: reactions: update task status to review", "task_id", task.ID, "error", err)
		return
	}
	if _, err := r.st.AddTaskLog(store.TaskLogEntry{
		TaskID: task.ID,
		Kind:   "status",
		Body:   fmt.Sprintf("PR #%d opened → review", pr.Number),
	}); err != nil {
		slog.Error("ghpoller: reactions: add task log", "task_id", task.ID, "error", err)
	}
}

// CIFailing enqueues a nudge to sess's worker asking it to investigate,
// once per (session, PR head SHA) — repeated poll ticks observing the same
// failing SHA don't re-notify.
func (r *Reactions) CIFailing(sess store.Session, pr *github.PR, failingSummary string) {
	r.mu.Lock()
	if r.lastNotifiedSHA[sess.ID] == pr.HeadSHA {
		r.mu.Unlock()
		return
	}
	r.lastNotifiedSHA[sess.ID] = pr.HeadSHA
	r.mu.Unlock()

	body := fmt.Sprintf("[rocket] CI failing on PR #%d: %s. Investigate and fix.", pr.Number, failingSummary)
	r.enqueue(sess.ID, body)
}

// ChangesRequested enqueues a nudge to sess's worker asking it to address
// review comments. The poller already dedupes per review-decision change,
// so no additional dedup is applied here.
func (r *Reactions) ChangesRequested(sess store.Session, pr *github.PR) {
	body := fmt.Sprintf("[rocket] Changes requested on PR #%d. Address the review comments.", pr.Number)
	r.enqueue(sess.ID, body)
}

// Merged moves sess's subtask (if any, and if currently in_progress or
// review) to done, logs the transition, and schedules the merge-grace
// auto-cleanup timer.
func (r *Reactions) Merged(sess store.Session, pr *github.PR) {
	task, err := r.st.GetTaskBySessionID(sess.ID)
	if err == nil && (task.Status == "in_progress" || task.Status == "review") {
		if err := r.st.UpdateTaskStatus(task.ID, "done"); err != nil {
			slog.Error("ghpoller: reactions: update task status to done", "task_id", task.ID, "error", err)
		} else if _, err := r.st.AddTaskLog(store.TaskLogEntry{
			TaskID: task.ID,
			Kind:   "status",
			Body:   fmt.Sprintf("PR #%d merged → done", pr.Number),
		}); err != nil {
			slog.Error("ghpoller: reactions: add task log", "task_id", task.ID, "error", err)
		}
	}

	r.scheduleGrace(sess.ID, 0)
}

// enqueue queues body for delivery to sessionID, mirroring the enqueue
// pattern used elsewhere (internal/api's deliverToOrchestrator): insert
// with status "queued", publish message.queued, wake the recipient's
// delivery worker.
func (r *Reactions) enqueue(sessionID, body string) {
	id, err := r.st.AddMessage(store.Message{ToSession: sessionID, Body: body})
	if err != nil {
		slog.Error("ghpoller: reactions: enqueue message", "session", sessionID, "error", err)
		return
	}
	r.bus.Publish("message.queued", sessionID, map[string]any{
		"id": id, "from": "", "to": sessionID,
	})
	if r.wake != nil {
		r.wake(sessionID)
	}
}

// scheduleGrace arms a one-shot timer that fires after cfg.MergeGrace and
// evaluates whether sessionID's workspace can be auto-destroyed. attempt
// counts how many times this grace window has already been rescheduled
// (0 for the original schedule from Merged).
func (r *Reactions) scheduleGrace(sessionID string, attempt int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}

	timer := time.AfterFunc(r.cfg.MergeGrace, func() {
		r.onGraceExpired(sessionID, attempt)
	})
	r.timers = append(r.timers, timer)
}

// onGraceExpired is the merge-grace timer callback. It never lets a panic
// escape (this runs on its own goroutine, outside any test/request call
// stack that could otherwise report it). Fired timers are left in r.timers
// (Stop() on an already-fired timer is a harmless no-op); the list is
// bounded in practice by maxGraceReschedules per merge event.
func (r *Reactions) onGraceExpired(sessionID string, attempt int) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("ghpoller: reactions: grace callback panic", "session", sessionID, "recover", rec)
		}
	}()

	sess, err := r.st.GetSession(sessionID)
	if err != nil {
		return // session gone entirely; nothing to do
	}
	if sess.State != "spawning" && sess.State != "running" {
		// Already killed/done/errored by some other path: leave it alone.
		return
	}

	repo, err := r.st.GetRepo(sess.RepoID)
	if err != nil {
		slog.Error("ghpoller: reactions: get repo for grace cleanup", "session", sessionID, "error", err)
		return
	}
	if !repo.AutoCleanup {
		slog.Info("ghpoller: reactions: skipping auto-cleanup, AutoCleanup disabled", "session", sessionID, "repo", repo.ID)
		return
	}

	if state, known := r.getActivity(sessionID); known && state == activity.Active {
		if attempt+1 >= maxGraceReschedules {
			slog.Warn("ghpoller: reactions: giving up on merge-grace reschedule, worker still active", "session", sessionID, "attempts", attempt+1)
			return
		}
		r.scheduleGrace(sessionID, attempt+1)
		return
	}

	if err := r.mgr.Complete(context.Background(), sessionID); err != nil {
		slog.Error("ghpoller: reactions: complete session after merge grace", "session", sessionID, "error", err)
	}
}
