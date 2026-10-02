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

// skillVersionMigration is the file that relabels tasks started on the
// custom skill before it carried a version (task #5027).
const skillVersionMigration = "migrations/0022_brainstorm_skill_version.sql"

// The migration labels the pre-version custom skill as 1.0 and leaves the
// stock skill, empty values and already versioned values alone; running it
// again changes nothing.
func TestMigrateBrainstormSkillVersion(t *testing.T) {
	s := openTestStore(t)
	values := []string{
		"orchestrator-brainstorming",
		"superpowers:brainstorming",
		"",
		"orchestrator-brainstorming@1.1",
	}
	want := []string{
		"orchestrator-brainstorming@1.0",
		"superpowers:brainstorming",
		"",
		"orchestrator-brainstorming@1.1",
	}
	ids := make([]int64, len(values))
	for i, v := range values {
		id, err := s.AddTask(Task{Title: "t", ProjectID: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetTaskBrainstormSkill(id, v); err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	body, err := migrationsFS.ReadFile(skillVersionMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for run := 1; run <= 2; run++ {
		if _, err := s.db.Exec(string(body)); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		for i, id := range ids {
			got, err := s.GetTask(id)
			if err != nil {
				t.Fatal(err)
			}
			if got.BrainstormSkill != want[i] {
				t.Errorf("run %d: %q → %q, want %q", run, values[i], got.BrainstormSkill, want[i])
			}
		}
	}
}
