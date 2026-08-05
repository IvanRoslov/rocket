package heartbeat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/store"
)

// TestQuietMilestone pins the rule itself, away from delivery: which
// milestones can be quiet at all, and what the silence is measured from.
func TestQuietMilestone(t *testing.T) {
	now := time.Unix(1_000_000_000, 0)
	old := now.Add(-30 * time.Hour).Unix()
	fresh := now.Add(-time.Hour).Unix()
	after := 24 * time.Hour

	base := func() store.Task {
		return store.Task{
			ID: 7, Title: "Improve external agents", Status: "in_progress",
			Milestone: true, AssignedRole: "cto", UpdatedAt: old,
		}
	}

	t.Run("no trace at all: the assignment is the reference point", func(t *testing.T) {
		since, ok := QuietMilestone(base(), 0, now, after)
		if !ok || since != 30*time.Hour {
			t.Fatalf("QuietMilestone = (%s, %v), want (30h, true)", since, ok)
		}
	})

	t.Run("the last trace is the reference point", func(t *testing.T) {
		if since, ok := QuietMilestone(base(), fresh, now, after); ok {
			t.Fatalf("QuietMilestone = (%s, true), want quiet=false after fresh activity", since)
		}
	})

	t.Run("a regular task is never quiet", func(t *testing.T) {
		task := base()
		task.Milestone = false
		if _, ok := QuietMilestone(task, 0, now, after); ok {
			t.Fatal("a regular task must not be reported quiet")
		}
	})

	t.Run("an untaken milestone is never quiet", func(t *testing.T) {
		task := base()
		task.AssignedRole = ""
		if _, ok := QuietMilestone(task, 0, now, after); ok {
			t.Fatal("nobody holds it, so nobody is silent")
		}
	})

	t.Run("a milestone still being brainstormed can be quiet", func(t *testing.T) {
		task := base()
		task.Status = "brainstorm"
		since, ok := QuietMilestone(task, 0, now, after)
		if !ok || since != 30*time.Hour {
			t.Fatalf("QuietMilestone = (%s, %v), want (30h, true): brainstorming is work too", since, ok)
		}
	})

	t.Run("only active statuses can be quiet", func(t *testing.T) {
		for _, status := range []string{"backlog", "review", "done", "cancelled"} {
			task := base()
			task.Status = status
			if _, ok := QuietMilestone(task, 0, now, after); ok {
				t.Errorf("status %q: must not be reported quiet", status)
			}
		}
	})

	t.Run("without a usable timestamp nothing is claimed", func(t *testing.T) {
		task := base()
		task.UpdatedAt = 0
		if _, ok := QuietMilestone(task, 0, now, after); ok {
			t.Fatal("a milestone without a reference point must not be quiet since the epoch")
		}
	})

	t.Run("exactly at the threshold is not yet quiet", func(t *testing.T) {
		task := base()
		task.UpdatedAt = now.Add(-after).Unix()
		if _, ok := QuietMilestone(task, 0, now, after); ok {
			t.Fatal("exactly at the threshold must not be quiet yet")
		}
	})
}

// seedQuietMilestone creates a milestone in progress, held by the cto agent,
// whose last movement is age ago.
func seedQuietMilestone(t *testing.T, st *store.Store, title string, age time.Duration) int64 {
	t.Helper()
	ts := time.Now().Add(-age).Unix()
	// AssignedRole goes in at insert time: SetTaskAssignedRole would stamp
	// updated_at with "now", and the fixture is a milestone taken long ago.
	id, err := st.AddTask(store.Task{
		Title: title, Status: "in_progress", Milestone: true,
		AssignedRole: escalationAgent, CreatedAt: ts, UpdatedAt: ts,
	})
	if err != nil {
		t.Fatalf("AddTask milestone: %v", err)
	}
	return id
}

// TestSweepQuietMilestones_RemindsAgentOnce: criterion 5 — a milestone silent
// longer than the threshold reminds its agent exactly once inside the
// anti-spam window, and publishes milestone.quiet for the dashboard.
func TestSweepQuietMilestones_RemindsAgentOnce(t *testing.T) {
	st := openTestStore(t)
	addCTOAgent(t, st)
	seedQuietMilestone(t, st, "Improve external agents", 30*time.Hour)

	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	bodies := inboxBodies(t, st)
	if len(bodies) != 1 {
		t.Fatalf("inbox = %q, want exactly one reminder", bodies)
	}
	for _, want := range []string{
		"quiet milestone", "Improve external agents", "30h",
		"rocket task log", "rocket task doc put", "rocket task ask",
	} {
		if !strings.Contains(bodies[0], want) {
			t.Errorf("reminder %q must contain %q", bodies[0], want)
		}
	}
	if !hasEvent(eventTypes(t, st), "milestone.quiet") {
		t.Errorf("expected a milestone.quiet event, got %v", eventTypes(t, st))
	}

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if bodies := inboxBodies(t, st); len(bodies) != 1 {
		t.Fatalf("inbox = %q, want the reminder not repeated inside the window", bodies)
	}
}

// TestSweepQuietMilestones_ActivityResetsTheClock: a journal entry the agent
// wrote an hour ago is exactly the trace the reminder asks for.
func TestSweepQuietMilestones_ActivityResetsTheClock(t *testing.T) {
	st := openTestStore(t)
	addCTOAgent(t, st)
	id := seedQuietMilestone(t, st, "Improve external agents", 30*time.Hour)
	if _, err := st.AddTaskLog(store.TaskLogEntry{
		TaskID: id, Kind: "decision", Body: "chose A over B",
		Author: escalationAgent, CreatedAt: time.Now().Add(-time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("AddTaskLog: %v", err)
	}

	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if bodies := inboxBodies(t, st); len(bodies) != 0 {
		t.Errorf("inbox = %q, want nothing after fresh activity", bodies)
	}
	if hasEvent(eventTypes(t, st), "milestone.quiet") {
		t.Error("a milestone with fresh activity must not publish milestone.quiet")
	}
}

// TestSweepQuietMilestones_UntakenAndRegularAreLeftAlone: the sweep touches
// only milestones somebody actually holds.
func TestSweepQuietMilestones_UntakenAndRegularAreLeftAlone(t *testing.T) {
	st := openTestStore(t)
	addCTOAgent(t, st)
	if _, err := st.AddTask(store.Task{
		Title: "untaken", Status: "in_progress", Milestone: true,
		CreatedAt: 1000, UpdatedAt: 1000,
	}); err != nil {
		t.Fatalf("AddTask untaken: %v", err)
	}
	seedOrchAndTask(t, st, "orch1", "in_progress")

	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if bodies := inboxBodies(t, st); len(bodies) != 0 {
		t.Errorf("inbox = %q, want nothing", bodies)
	}
	if hasEvent(eventTypes(t, st), "milestone.quiet") {
		t.Error("no held milestone: milestone.quiet must not be published")
	}
}

// TestSweepQuietMilestones_CoversBrainstorm: the sweep must look at every
// active status, not just in_progress — a milestone whose agent went silent
// while still brainstorming is exactly the case the reminder exists for.
func TestSweepQuietMilestones_CoversBrainstorm(t *testing.T) {
	st := openTestStore(t)
	addCTOAgent(t, st)
	// The status goes in at insert time for the same reason AssignedRole does
	// in seedQuietMilestone: every status-changing store call stamps
	// updated_at with "now", and this fixture is a milestone silent for 30h.
	ts := time.Now().Add(-30 * time.Hour).Unix()
	if _, err := st.AddTask(store.Task{
		Title: "Improve external agents", Status: "brainstorm", Milestone: true,
		AssignedRole: escalationAgent, CreatedAt: ts, UpdatedAt: ts,
	}); err != nil {
		t.Fatalf("AddTask milestone: %v", err)
	}

	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	bodies := inboxBodies(t, st)
	if len(bodies) != 1 {
		t.Fatalf("inbox = %q, want exactly one reminder for the brainstorming milestone", bodies)
	}
	if !strings.Contains(bodies[0], "Improve external agents") {
		t.Errorf("reminder %q must name the milestone", bodies[0])
	}
}
