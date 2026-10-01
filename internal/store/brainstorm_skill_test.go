package store

import (
	"errors"
	"testing"
)

func TestTaskBrainstormSkillDefaultsEmpty(t *testing.T) {
	s := openTestStore(t)
	id, err := s.AddTask(Task{Title: "t", ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.BrainstormSkill != "" {
		t.Errorf("new task BrainstormSkill = %q, want empty", got.BrainstormSkill)
	}
}

func TestSetTaskBrainstormSkill(t *testing.T) {
	s := openTestStore(t)
	id, err := s.AddTask(Task{Title: "t", ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskBrainstormSkill(id, "orchestrator-brainstorming"); err != nil {
		t.Fatalf("SetTaskBrainstormSkill: %v", err)
	}
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.BrainstormSkill != "orchestrator-brainstorming" {
		t.Errorf("BrainstormSkill = %q", got.BrainstormSkill)
	}

	// UpdateTask (title/slug/session edits) must not wipe it.
	got.Title = "renamed"
	if err := s.UpdateTask(got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetTask(id)
	if again.BrainstormSkill != "orchestrator-brainstorming" {
		t.Errorf("UpdateTask cleared BrainstormSkill: %q", again.BrainstormSkill)
	}

	if err := s.SetTaskBrainstormSkill(99999, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown task: err = %v, want ErrNotFound", err)
	}
}

func TestOrchestratorBrainstormCustom(t *testing.T) {
	s := openTestStore(t)
	on, err := s.OrchestratorBrainstormCustom()
	if err != nil || on {
		t.Fatalf("unset: got %v, %v; want false, nil", on, err)
	}
	if err := s.SetSetting(SettingOrchestratorBrainstormCustom, "true"); err != nil {
		t.Fatal(err)
	}
	if on, err := s.OrchestratorBrainstormCustom(); err != nil || !on {
		t.Errorf("\"true\": got %v, %v", on, err)
	}
	if err := s.SetSetting(SettingOrchestratorBrainstormCustom, "false"); err != nil {
		t.Fatal(err)
	}
	if on, err := s.OrchestratorBrainstormCustom(); err != nil || on {
		t.Errorf("\"false\": got %v, %v", on, err)
	}
}
