package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/store"
)

// Starting a task fixes which brainstorm skill its orchestrator runs with,
// from the setting at that moment, and exposes it in the task JSON.
func TestPostTaskStartRecordsBrainstormSkill(t *testing.T) {
	cases := []struct {
		name    string
		setting string // "" = unset
		want    string
	}{
		{"unset", "", "superpowers:brainstorming"},
		{"off", "false", "superpowers:brainstorming"},
		{"on", "true", "orchestrator-brainstorming@1.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := tasksTestDeps(t)
			srv := newTestServer(t, d)
			addTestProject(t, d, "proj1")
			if tc.setting != "" {
				if err := d.Store.SetSetting(store.SettingOrchestratorBrainstormCustom, tc.setting); err != nil {
					t.Fatal(err)
				}
			}
			id, err := d.Store.AddTask(store.Task{Title: "Add login page", ProjectID: "proj1"})
			if err != nil {
				t.Fatal(err)
			}

			resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/start", map[string]string{"agent": "fake-skills"})
			resp.Body.Close()
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("start status = %d", resp.StatusCode)
			}

			task, err := d.Store.GetTask(id)
			if err != nil {
				t.Fatal(err)
			}
			if task.BrainstormSkill != tc.want {
				t.Errorf("stored brainstorm_skill = %q, want %q", task.BrainstormSkill, tc.want)
			}

			get := getJSON(t, srv.URL+"/v1/tasks/"+itoa(id))
			defer get.Body.Close()
			var body map[string]any
			if err := json.NewDecoder(get.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["brainstorm_skill"] != tc.want {
				t.Errorf("GET task brainstorm_skill = %#v, want %q", body["brainstorm_skill"], tc.want)
			}
		})
	}
}

// Tasks started before the column existed report an empty skill rather than
// omitting the field, so clients can bucket them as "unknown".
func TestTaskJSONBrainstormSkillEmptyForOldTasks(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	id, err := d.Store.AddTask(store.Task{Title: "old", ProjectID: "proj1"})
	if err != nil {
		t.Fatal(err)
	}
	get := getJSON(t, srv.URL+"/v1/tasks/"+itoa(id))
	defer get.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(get.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	v, ok := body["brainstorm_skill"]
	if !ok || v != "" {
		t.Errorf("brainstorm_skill = %#v (present=%v), want \"\"", v, ok)
	}
}

// skillsFakeAgent is the "fake" test agent that also ships the custom
// brainstorm skill, like claude-code does; unavailable makes spawning fail
// after the handler has done its own bookkeeping.
type skillsFakeAgent struct {
	sessFakeAgent
	unavailable bool
}

func (skillsFakeAgent) ShipsBrainstormSkill() bool { return true }

func (a skillsFakeAgent) Available() error {
	if a.unavailable {
		return errors.New("not installed")
	}
	return nil
}

func init() {
	agent.Register("fake-skills", func() agent.Agent { return skillsFakeAgent{} })
	agent.Register("fake-skills-unavailable", func() agent.Agent { return skillsFakeAgent{unavailable: true} })
}

func startWithAgent(t *testing.T, agentName, setting string) (store.Task, int) {
	t.Helper()
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	if err := d.Store.SetSetting(store.SettingOrchestratorBrainstormCustom, setting); err != nil {
		t.Fatal(err)
	}
	id, err := d.Store.AddTask(store.Task{Title: "Add login page", ProjectID: "proj1"})
	if err != nil {
		t.Fatal(err)
	}
	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/start", map[string]string{"agent": agentName})
	resp.Body.Close()
	task, err := d.Store.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	return task, resp.StatusCode
}

// The custom skill only exists where the agent lays it out (claude-code). For
// any other orchestrator agent (codex) the toggle must not name it: the
// kickoff would point at a skill that is not there and the metric would
// file the storm under the wrong skill.
func TestPostTaskStartCustomSkillOnlyForShippingAgent(t *testing.T) {
	task, code := startWithAgent(t, "fake-skills", "true")
	if code != http.StatusCreated {
		t.Fatalf("start status = %d", code)
	}
	if task.BrainstormSkill != "orchestrator-brainstorming@1.1" {
		t.Errorf("shipping agent: brainstorm_skill = %q, want orchestrator-brainstorming@1.1", task.BrainstormSkill)
	}

	task, code = startWithAgent(t, "fake", "true")
	if code != http.StatusCreated {
		t.Fatalf("start status = %d", code)
	}
	if task.BrainstormSkill != "superpowers:brainstorming" {
		t.Errorf("non-shipping agent: brainstorm_skill = %q, want superpowers:brainstorming", task.BrainstormSkill)
	}
}

// The skill is stored before the orchestrator is spawned, so no running
// orchestrator can exist with an empty stored skill.
func TestPostTaskStartStoresSkillBeforeSpawn(t *testing.T) {
	task, code := startWithAgent(t, "fake-skills-unavailable", "true")
	if code == http.StatusCreated {
		t.Fatalf("start with an unavailable agent succeeded")
	}
	if task.BrainstormSkill != "orchestrator-brainstorming@1.1" {
		t.Errorf("brainstorm_skill after failed spawn = %q, want orchestrator-brainstorming@1.1 stored before spawning", task.BrainstormSkill)
	}
}
