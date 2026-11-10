package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

func creativeModelsFixture(t *testing.T) (Deps, store.CreativeRun, string) {
	t.Helper()
	d := sessionsTestDeps(t)
	addTestProject(t, d, "film")
	p, err := d.Store.CreateCreativePipeline(store.CreativePipelineInput{Name: "Film", Stages: []store.CreativeStageDefinition{{Name: "Scene", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.Store.CreateCreativeRun(store.CreativeRunInput{ProjectID: "film", PipelineID: p.ID, Name: "Film", Brief: "Make film"})
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"cheap", "deep"} {
		if err := d.Store.CreateModelProfile(store.ModelProfile{Name: name, Agent: "fake", Model: name + "-model", Enabled: true, Position: i - 10}); err != nil {
			t.Fatal(err)
		}
	}
	return d, run, fmt.Sprintf("/v1/creative/runs/%d", run.ID)
}
func creativeModelsRequest(t *testing.T, h http.Handler, method, path, caller, body string, status int) map[string]any {
	t.Helper()
	w := artifactRequest(h, method, path, "application/json", caller, strings.NewReader(body))
	if w.Code != status {
		t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func expectCreativeProfiles(t *testing.T, out map[string]any, names ...string) {
	t.Helper()
	want := make([]any, 0, len(names))
	for _, name := range names {
		want = append(want, name)
	}
	if !reflect.DeepEqual(out["allowed_profiles"], want) {
		t.Fatalf("allowed_profiles=%v want %v", out["allowed_profiles"], want)
	}
}
func TestCreativeModelsReplace(t *testing.T) {
	d, run, path := creativeModelsFixture(t)
	h := NewHandler(d)
	expectCreativeProfiles(t, creativeModelsRequest(t, h, "GET", path, "", "", 200))
	expectCreativeProfiles(t, creativeModelsRequest(t, h, "PUT", path+"/models", "", `{"allowed_profiles":["cheap"]}`, 200), "cheap")
	expectCreativeProfiles(t, creativeModelsRequest(t, h, "GET", path, "", "", 200), "cheap")
	for _, body := range []string{`{}`, `{"allowed_profiles":null}`, `{"allowed_profiles":"cheap"}`, `{"allowed_profiles":[]} {}`} {
		out := creativeModelsRequest(t, h, "PUT", path+"/models", "", body, 400)
		if out["error"].(map[string]any)["code"] != "bad_request" {
			t.Fatalf("%v", out)
		}
	}
	out := creativeModelsRequest(t, h, "PUT", path+"/models", "", `{"allowed_profiles":["deep","unknown"]}`, 400)
	if out["error"].(map[string]any)["code"] != "profile_not_found" {
		t.Fatalf("%v", out)
	}
	expectCreativeProfiles(t, creativeModelsRequest(t, h, "GET", path, "", "", 200), "cheap")
	for _, id := range []string{"run-orch", "run-worker", "dev-agent"} {
		if err := d.Store.AddSession(store.Session{ID: id, Kind: "worker", ProjectID: run.ProjectID, Agent: "fake", State: "running"}); err != nil {
			t.Fatal(err)
		}
		if id != "dev-agent" {
			if err := d.Store.BindCreativeSession(id, store.CreativeBinding{RunID: run.ID}); err != nil {
				t.Fatal(err)
			}
		}
		out = creativeModelsRequest(t, h, "PUT", path+"/models", id, `{"allowed_profiles":[]}`, 403)
		if out["error"].(map[string]any)["code"] != "human_only" {
			t.Fatalf("%v", out)
		}
	}
	expectCreativeProfiles(t, creativeModelsRequest(t, h, "GET", path, "", "", 200), "cheap")
	expectCreativeProfiles(t, creativeModelsRequest(t, h, "PUT", path+"/models", "", `{"allowed_profiles":[]}`, 200))
	creativeModelsRequest(t, h, "PUT", "/v1/creative/runs/99999/models", "", `{"allowed_profiles":[]}`, 404)
}

type creativeModelsRuntime struct {
	sessFakeRuntime
	before func()
}

func (rt creativeModelsRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	rt.before()
	return rt.sessFakeRuntime.Create(ctx, spec)
}

func TestCreativeModelsLaunchAndAvailability(t *testing.T) {
	d, run, path := creativeModelsFixture(t)
	h := NewHandler(d)
	observed := false
	rt := creativeModelsRuntime{before: func() {
		observed = true
		expectCreativeProfiles(t, creativeModelsRequest(t, h, "GET", path, "", "", 200), "cheap")
	}}
	d.Manager = session.NewManager(d.Store, d.Bus, rt, sessFakeWorkspace{}, d.Cfg)
	h = NewHandler(d)
	orch := creativeModelsRequest(t, h, "POST", path+"/sessions", "", `{"profile":"deep","allowed_profiles":["cheap"]}`, 201)
	if !observed {
		t.Fatal("runtime did not start")
	}
	orchID := orch["id"].(string)
	worker := creativeModelsRequest(t, h, "POST", path+"/workers", orchID, fmt.Sprintf(`{"stage_id":%d,"name":"scene","prompt":"Draw scene","profile":"cheap"}`, run.Stages[0].ID), 201)
	for _, id := range []string{orchID, worker["id"].(string)} {
		out := creativeModelsRequest(t, h, "GET", "/v1/model-profiles/available", id, "", 200)
		profiles := out["profiles"].([]any)
		if len(profiles) != 1 || profiles[0].(map[string]any)["name"] != "cheap" || out["default"] != "cheap" {
			t.Fatalf("models for %s: %v", id, out)
		}
	}
	out := creativeModelsRequest(t, h, "POST", path+"/workers", orchID, fmt.Sprintf(`{"stage_id":%d,"name":"escape","prompt":"x","allowed_profiles":[]}`, run.Stages[0].ID), 403)
	if out["error"].(map[string]any)["code"] != "human_only" {
		t.Fatalf("%v", out)
	}
}
func TestCreativeModelsWorkerProfiles(t *testing.T) {
	for _, tc := range []struct{ name, list, profile, agent, want, code string }{
		{"allowed", `["cheap"]`, "cheap", "", "cheap", ""},
		{"outside", `["cheap"]`, "deep", "", "", "profile_not_allowed"},
		{"unrestricted", `[]`, "deep", "", "deep", ""},
		{"agent only", `["cheap"]`, "", "fake", "cheap", ""},
		{"default excluded", `["cheap"]`, "", "", "cheap", ""},
		{"default allowed", `["cheap","deep"]`, "", "", "deep", ""},
		{"disabled", `["cheap"]`, "", "", "", "no_profiles_allowed"},
		{"deleted", `["cheap"]`, "", "", "", "no_profiles_allowed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, run, path := creativeModelsFixture(t)
			h := NewHandler(d)
			if err := d.Store.SetSetting(store.SettingDefaultWorkerProfile, "deep"); err != nil {
				t.Fatal(err)
			}
			creativeModelsRequest(t, h, "PUT", path+"/models", "", `{"allowed_profiles":`+tc.list+`}`, 200)
			if tc.name == "disabled" {
				p, err := d.Store.GetModelProfile("cheap")
				if err != nil {
					t.Fatal(err)
				}
				p.Enabled = false
				if err = d.Store.UpdateModelProfile(p); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "deleted" {
				if err := d.Store.DeleteModelProfile("cheap"); err != nil {
					t.Fatal(err)
				}
			}
			orch := creativeModelsRequest(t, h, "POST", path+"/sessions", "", `{"profile":"deep"}`, 201)
			// Omitted list must preserve the run's current restriction.
			expectCreativeProfilesFromList(t, h, path, tc.list)
			before, err := d.Store.ListCreativeSessionIDs(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"stage_id": run.Stages[0].ID, "name": "scene", "prompt": "Scene", "profile": tc.profile, "agent": tc.agent})
			status := 201
			if tc.code != "" {
				status = 400
			}
			out := creativeModelsRequest(t, h, "POST", path+"/workers", orch["id"].(string), string(body), status)
			if tc.code != "" {
				if out["error"].(map[string]any)["code"] != tc.code {
					t.Fatalf("%v", out)
				}
				if tc.code == "profile_not_allowed" && !strings.Contains(fmt.Sprint(out), "allowed: cheap") {
					t.Fatalf("missing allowed list: %v", out)
				}
				after, err := d.Store.ListCreativeSessionIDs(run.ID)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("refused spawn created session: %v %v", after, err)
				}
			} else {
				s, err := d.Store.GetSession(out["id"].(string))
				if err != nil {
					t.Fatal(err)
				}
				if s.Profile != tc.want {
					t.Fatalf("profile=%q want %q", s.Profile, tc.want)
				}
			}
		})
	}
}
func expectCreativeProfilesFromList(t *testing.T, h http.Handler, path, list string) {
	t.Helper()
	var names []string
	if err := json.Unmarshal([]byte(list), &names); err != nil {
		t.Fatal(err)
	}
	expectCreativeProfiles(t, creativeModelsRequest(t, h, "GET", path, "", "", 200), names...)
}

func TestCreativeModelsLaunchRejectsInvalidListWithoutChanges(t *testing.T) {
	d, run, path := creativeModelsFixture(t)
	h := NewHandler(d)
	creativeModelsRequest(t, h, "PUT", path+"/models", "", `{"allowed_profiles":["cheap"]}`, 200)
	out := creativeModelsRequest(t, h, "POST", path+"/sessions", "", `{"profile":"deep","allowed_profiles":["deep","unknown"]}`, 400)
	if out["error"].(map[string]any)["code"] != "profile_not_found" {
		t.Fatalf("%v", out)
	}
	expectCreativeProfiles(t, creativeModelsRequest(t, h, "GET", path, "", "", 200), "cheap")
	ids, err := d.Store.ListCreativeSessionIDs(run.ID)
	if err != nil || len(ids) != 0 {
		t.Fatalf("invalid launch created sessions: %v %v", ids, err)
	}
	if err := d.Store.AddSession(store.Session{ID: "agent", Kind: "orchestrator", ProjectID: "film", Agent: "fake", State: "running"}); err != nil {
		t.Fatal(err)
	}
	out = creativeModelsRequest(t, h, "POST", path+"/sessions", "agent", `{"allowed_profiles":[]}`, 403)
	if out["error"].(map[string]any)["code"] != "human_only" {
		t.Fatalf("%v", out)
	}
	expectCreativeProfiles(t, creativeModelsRequest(t, h, "GET", path, "", "", 200), "cheap")
	creativeModelsRequest(t, h, "POST", path+"/sessions", "", `{"profile":"deep","allowed_profiles":[]}`, 201)
	expectCreativeProfiles(t, creativeModelsRequest(t, h, "GET", path, "", "", 200))
}
