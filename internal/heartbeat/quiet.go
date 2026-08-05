package heartbeat

import (
	"fmt"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/store"
)

// QuietMilestone reports how long a milestone has gone without a visible trace
// of the agent holding it, and whether that exceeds after (task #1023, spec
// v2, §«Видимость работы» п.2).
//
// lastActivity is the agent's last trace in the milestone as
// store.MilestoneActivity computes it — a journal entry it wrote, a doc it
// put, a thread entry of its own — or 0 when it has left none. With no trace
// the silence is measured from the task's updated_at, which take/assign stamp:
// the clock starts when the milestone became somebody's.
//
// Three kinds of task are never quiet: a regular project task (the milestone
// convention doesn't apply to it), an untaken milestone (nobody is silent when
// nobody holds it), and one whose status isn't active work (store.
// IsActiveTaskStatus) — backlog work hasn't started, and review/done/cancelled
// is the human's move, not the agent's. Brainstorming counts as work: an agent
// that goes silent while still thinking a milestone through is the very case
// the reminder exists for.
// Neither is a milestone without a usable timestamp: as in StaleThread, a
// missing reference point means "not quiet", never "quiet since the epoch".
//
// It is exported because the same rule is read twice: by the heartbeat sweep
// that sends the reminders, and by the API, which derives the milestone's
// `quiet` flag from it on every read.
func QuietMilestone(task store.Task, lastActivity int64, now time.Time, after time.Duration) (since time.Duration, ok bool) {
	if !task.Milestone || task.AssignedRole == "" || !store.IsActiveTaskStatus(task.Status) {
		return 0, false
	}

	ref := task.UpdatedAt
	if lastActivity > 0 {
		ref = lastActivity
	}
	if ref <= 0 {
		return 0, false
	}

	since = now.Sub(time.Unix(ref, 0))
	return since, since > after
}

// milestoneLister is the slice of the store both callers of ActiveMilestones
// depend on: the heartbeat's own store handle and the API's Deps.Store.
type milestoneLister interface {
	ListTasks(store.TaskFilter) ([]store.Task, error)
}

// ActiveMilestones lists every milestone in a status that counts as active
// work. store.TaskFilter takes one status at a time and widening it would
// serve nothing but this call, so the sweep is one query per active status,
// concatenated. Like QuietMilestone it is exported because the same set is
// read twice: by the heartbeat sweep and by the API deriving the `quiet` flag.
func ActiveMilestones(st milestoneLister) ([]store.Task, error) {
	var out []store.Task
	for _, status := range store.ActiveTaskStatuses {
		tasks, err := st.ListTasks(store.TaskFilter{Milestones: true, Status: status})
		if err != nil {
			return nil, err
		}
		out = append(out, tasks...)
	}
	return out, nil
}

// quietReminderInterval is the anti-spam floor between two reminders about the
// same milestone. Like staleReminderInterval it is a constant and not
// cfg.MilestoneQuietAfter: shortening the threshold should make milestones go
// quiet sooner, not make the reminder repeat every few hours.
const quietReminderInterval = 24 * time.Hour

// quietKeyPrefix namespaces per-milestone reminder entries in lastSent, so a
// quiet-milestone reminder never suppresses a thread reminder or an
// orchestrator summary (same trick as staleKeyPrefix).
const quietKeyPrefix = "quiet-milestone:"

// sweepQuietMilestones reminds the agent holding each in-progress milestone
// that has shown no trace of its work for longer than cfg.MilestoneQuietAfter.
// It runs once per tick over every milestone: a milestone belongs to no
// project and to no orchestrator, so it sits outside the per-orchestrator
// sweep, exactly like sweepStaleThreads.
//
// The reminder reaches the agent through the same channel a stale-thread
// reminder does (see remindParticipant): its session when it is live, its
// inbox when it is not. The human is not messaged — the milestone.quiet event
// and the derived `quiet` flag are their channel.
func (h *Heartbeat) sweepQuietMilestones() error {
	tasks, err := ActiveMilestones(h.st)
	if err != nil {
		return fmt.Errorf("list milestones: %w", err)
	}
	if len(tasks) == 0 {
		return nil
	}

	activity, err := h.st.MilestoneActivity()
	if err != nil {
		return fmt.Errorf("milestone activity: %w", err)
	}

	now := h.nowFunc()
	after := h.cfg.MilestoneQuietAfter
	if after <= 0 {
		after = config.DefaultMilestoneQuietAfter
	}

	for _, task := range tasks {
		since, quiet := QuietMilestone(task, activity[task.ID], now, after)
		if !quiet {
			continue
		}

		reminded := false
		key := quietKeyPrefix + fmt.Sprint(task.ID)
		if h.antiSpamOK(key, now, quietReminderInterval) &&
			h.remindParticipant(task.AssignedRole, quietBody(task, since)) {
			h.mu.Lock()
			h.lastSent[key] = now
			h.mu.Unlock()
			reminded = true
		}

		if h.bus != nil {
			h.bus.Publish("milestone.quiet", "", map[string]any{
				"task_id":       task.ID,
				"agent_id":      task.AssignedRole,
				"title":         task.Title,
				"since_seconds": int64(since.Seconds()),
				"reminded":      reminded,
			})
		}
	}
	return nil
}

// quietBody assembles the reminder: which milestone, how long it has shown
// nothing, and the three commands that end the silence — the same three the
// agent's CLAUDE.md snippet asks for (journal, doc, thread).
func quietBody(task store.Task, since time.Duration) string {
	id := task.ID
	return fmt.Sprintf(
		"[rocket quiet milestone] #%d %q has had no visible activity from you for %s.\n"+
			"Log progress: rocket task log %d --kind note \"<what you did>\"\n"+
			"Put results: rocket task doc put %d --kind doc --title \"<title>\" --file <path>\n"+
			"Ask if blocked: rocket task ask %d \"<question>\"",
		id, task.Title, humanSince(since), id, id, id)
}
