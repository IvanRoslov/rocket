package api

import (
	"encoding/json"
	"net/http"
	"testing"

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
		{"on", "true", "orchestrator-brainstorming"},
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

			resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/start", nil)
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
