package store

import (
	"errors"
	"testing"
)

func TestAddTask_MilestoneRoundTrip(t *testing.T) {
	s := openTestStore(t)

	id, err := s.AddTask(Task{Title: "Improve external agents", Milestone: true})
	if err != nil {
		t.Fatalf("AddTask milestone: %v", err)
	}
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !got.Milestone {
		t.Errorf("Milestone = false, want true")
	}
	if got.ProjectID != "" {
		t.Errorf("ProjectID = %q, want empty", got.ProjectID)
	}
	if got.AssignedRole != "" {
		t.Errorf("AssignedRole = %q, want empty", got.AssignedRole)
	}

	plainID, err := s.AddTask(Task{Title: "regular", ProjectID: "billing"})
	if err != nil {
		t.Fatalf("AddTask regular: %v", err)
	}
	plain, err := s.GetTask(plainID)
	if err != nil {
		t.Fatalf("GetTask regular: %v", err)
	}
	if plain.Milestone {
		t.Errorf("regular task: Milestone = true, want false")
	}
}

func TestSetTaskAssignedRole(t *testing.T) {
	s := openTestStore(t)

	id, err := s.AddTask(Task{Title: "milestone", Milestone: true})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if err := s.SetTaskAssignedRole(id, "cto"); err != nil {
		t.Fatalf("SetTaskAssignedRole: %v", err)
	}
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.AssignedRole != "cto" {
		t.Errorf("AssignedRole = %q, want cto", got.AssignedRole)
	}

	if err := s.SetTaskAssignedRole(id, ""); err != nil {
		t.Fatalf("SetTaskAssignedRole clear: %v", err)
	}
	got, err = s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask after clear: %v", err)
	}
	if got.AssignedRole != "" {
		t.Errorf("AssignedRole after clear = %q, want empty", got.AssignedRole)
	}

	if err := s.SetTaskAssignedRole(9999, "cto"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetTaskAssignedRole on missing task = %v, want ErrNotFound", err)
	}
}

func TestListTasks_MilestoneFilters(t *testing.T) {
	s := openTestStore(t)

	if _, err := s.AddTask(Task{Title: "regular", ProjectID: "billing"}); err != nil {
		t.Fatalf("AddTask regular: %v", err)
	}
	mine, err := s.AddTask(Task{Title: "mine", Milestone: true})
	if err != nil {
		t.Fatalf("AddTask mine: %v", err)
	}
	theirs, err := s.AddTask(Task{Title: "theirs", Milestone: true})
	if err != nil {
		t.Fatalf("AddTask theirs: %v", err)
	}
	if err := s.SetTaskAssignedRole(mine, "cto"); err != nil {
		t.Fatalf("SetTaskAssignedRole: %v", err)
	}
	if err := s.SetTaskAssignedRole(theirs, "cfo"); err != nil {
		t.Fatalf("SetTaskAssignedRole: %v", err)
	}

	ms, err := s.ListTasks(TaskFilter{Milestones: true})
	if err != nil {
		t.Fatalf("ListTasks milestones: %v", err)
	}
	if len(ms) != 2 {
		t.Fatalf("milestones = %d, want 2", len(ms))
	}

	byRole, err := s.ListTasks(TaskFilter{Milestones: true, AssignedRole: "cto"})
	if err != nil {
		t.Fatalf("ListTasks by role: %v", err)
	}
	if len(byRole) != 1 || byRole[0].ID != mine {
		t.Fatalf("by role = %+v, want only #%d", byRole, mine)
	}
}
