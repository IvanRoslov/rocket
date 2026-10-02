package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// setProfiles replaces the registry with ps (the test deps seed one "fake"
// profile) and sets the two default-profile settings.
func setProfiles(t *testing.T, d Deps, defWorker, defOrch string, ps ...store.ModelProfile) {
	t.Helper()
	existing, err := d.Store.ListModelProfiles()
	if err != nil {
		t.Fatalf("ListModelProfiles: %v", err)
	}
	for _, p := range existing {
		if err := d.Store.DeleteModelProfile(p.Name); err != nil {
			t.Fatalf("DeleteModelProfile: %v", err)
		}
	}
	for _, p := range ps {
		if err := d.Store.CreateModelProfile(p); err != nil {
			t.Fatalf("CreateModelProfile %s: %v", p.Name, err)
		}
	}
	for key, v := range map[string]string{
		store.SettingDefaultWorkerProfile:       defWorker,
		store.SettingDefaultOrchestratorProfile: defOrch,
	} {
		if err := d.Store.SetSetting(key, v); err != nil {
			t.Fatalf("SetSetting %s: %v", key, err)
		}
	}
}

func fakeProfile(name, model, effort string, position int) store.ModelProfile {
	return store.ModelProfile{Name: name, Agent: "fake", Model: model, Effort: effort, Enabled: true, Position: position}
}

// spawnAs posts a worker spawn on behalf of orchID with extra body fields.
func spawnAs(t *testing.T, srvURL, orchID string, extra map[string]any) *http.Response {
	t.Helper()
	body := map[string]any{"repo": "proj1-repo", "task": "mytask", "prompt": "go"}
	for k, v := range extra {
		body[k] = v
	}
	return postJSONWithHeader(t, srvURL+"/v1/sessions", orchID, body)
}

func assertSessionSnapshot(t *testing.T, d Deps, id, profile, model, effort string) {
	t.Helper()
	s, err := d.Store.GetSession(id)
	if err != nil {
		t.Fatalf("GetSession %s: %v", id, err)
	}
	if s.Profile != profile || s.Model != model || s.Effort != effort {
		t.Errorf("snapshot of %s = %q/%q/%q, want %q/%q/%q", id, s.Profile, s.Model, s.Effort, profile, model, effort)
	}
}

func TestPostSessionProfileResolution(t *testing.T) {
	tests := []struct {
		name        string
		allow       []string
		extra       map[string]any
		wantStatus  int
		wantCode    string
		wantMsg     string
		wantProfile string
	}{
		{"explicit allowed profile", nil, map[string]any{"profile": "deep"}, 201, "", "", "deep"},
		{"profile outside allowlist", []string{"cheap"}, map[string]any{"profile": "deep"}, 400, "profile_not_allowed", "allowed: cheap", ""},
		{"disabled profile", nil, map[string]any{"profile": "off"}, 400, "profile_not_allowed", "allowed: cheap, deep", ""},
		{"unknown profile", nil, map[string]any{"profile": "nope"}, 400, "profile_not_found", "", ""},
		{"agent only takes first allowed of agent", nil, map[string]any{"agent": "fake"}, 201, "", "", "cheap"},
		{"nothing takes default worker", nil, nil, 201, "", "", "deep"},
		{"default outside allowlist takes first allowed", []string{"cheap"}, nil, 201, "", "", "cheap"},
		{"profile and agent disagree", nil, map[string]any{"profile": "deep", "agent": "codex"}, 400, "bad_request", "runs agent fake", ""},
		{"allowlist of vanished names", []string{"gone"}, nil, 400, "no_profiles_allowed", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := sessionsTestDeps(t)
			srv := newTestServer(t, d)
			orchID, rootID := seedOrchestratorWithTask(t, d, "proj1", "myfeat")
			off := fakeProfile("off", "x", "", 3)
			off.Enabled = false
			setProfiles(t, d, "deep", "deep",
				fakeProfile("cheap", "sonnet", "", 1), fakeProfile("deep", "opus", "high", 2), off)
			if tt.allow != nil {
				if err := d.Store.SetTaskAllowedProfiles(rootID, tt.allow); err != nil {
					t.Fatalf("SetTaskAllowedProfiles: %v", err)
				}
			}

			resp := spawnAs(t, srv.URL, orchID, tt.extra)
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%+v)", resp.StatusCode, tt.wantStatus, decodeErr(t, resp))
			}
			if tt.wantStatus != http.StatusCreated {
				eb := decodeErr(t, resp)
				if eb.Error.Code != tt.wantCode || !strings.Contains(eb.Error.Message, tt.wantMsg) {
					t.Errorf("error = %+v, want code %s with %q", eb.Error, tt.wantCode, tt.wantMsg)
				}
				subs, err := d.Store.ListTasks(store.TaskFilter{ParentSet: true, Parent: rootID})
				if err != nil {
					t.Fatalf("ListTasks: %v", err)
				}
				if len(subs) != 0 {
					t.Errorf("refused spawn left %d subtasks, want 0", len(subs))
				}
				return
			}
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["profile"] != tt.wantProfile {
				t.Errorf("response profile = %v, want %s", body["profile"], tt.wantProfile)
			}
			model := map[string]string{"cheap": "sonnet", "deep": "opus"}[tt.wantProfile]
			effort := map[string]string{"deep": "high"}[tt.wantProfile]
			assertSessionSnapshot(t, d, "myfeat-mytask", tt.wantProfile, model, effort)
		})
	}
}

func TestPostSessionEmptyRegistry(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")
	setProfiles(t, d, "", "")

	resp := spawnAs(t, srv.URL, orchID, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "no_profiles_allowed" {
		t.Errorf("code = %q, want no_profiles_allowed", eb.Error.Code)
	}
}

// startTask posts /v1/tasks/{id}/start as caller ("" = the human).
func startTask(t *testing.T, srvURL string, id int64, caller string, body map[string]any) *http.Response {
	t.Helper()
	if body == nil {
		body = map[string]any{}
	}
	return postJSONWithHeader(t, srvURL+"/v1/tasks/"+itoa(id)+"/start", caller, body)
}

func newBacklogTask(t *testing.T, d Deps) int64 {
	t.Helper()
	addTestProject(t, d, "proj1")
	id, err := d.Store.AddTask(store.Task{Title: "Choose model", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	return id
}

func TestPostTaskStartProfile(t *testing.T) {
	tests := []struct {
		name        string
		body        map[string]any
		wantProfile string
		wantModel   string
	}{
		{"explicit profile", map[string]any{"profile": "cheap"}, "cheap", "sonnet"},
		{"global default", nil, "deep", "opus"},
		{"agent only takes first enabled of agent", map[string]any{"agent": "fake"}, "cheap", "sonnet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tasksTestDeps(t)
			srv := newTestServer(t, d)
			id := newBacklogTask(t, d)
			setProfiles(t, d, "cheap", "deep", fakeProfile("cheap", "sonnet", "", 1), fakeProfile("deep", "opus", "", 2))

			resp := startTask(t, srv.URL, id, "", tt.body)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (%+v)", resp.StatusCode, decodeErr(t, resp))
			}
			task, err := d.Store.GetTask(id)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			if task.OrchestratorProfile != tt.wantProfile {
				t.Errorf("orchestrator_profile = %q, want %q", task.OrchestratorProfile, tt.wantProfile)
			}
			assertSessionSnapshot(t, d, task.SessionID, tt.wantProfile, tt.wantModel, "")
		})
	}
}

// TestPostTaskStartEmptyRegistryIsLegacy: with every profile deleted the
// task still starts, the old way — no model, no recorded profile.
func TestPostTaskStartEmptyRegistryIsLegacy(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	id := newBacklogTask(t, d)
	setProfiles(t, d, "", "")

	resp := startTask(t, srv.URL, id, "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%+v)", resp.StatusCode, decodeErr(t, resp))
	}
	task, err := d.Store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.OrchestratorProfile != "" {
		t.Errorf("orchestrator_profile = %q, want empty", task.OrchestratorProfile)
	}
	assertSessionSnapshot(t, d, task.SessionID, "", "", "")
}

func TestPostTaskStartRefusals(t *testing.T) {
	tests := []struct {
		name       string
		asAgent    bool
		body       map[string]any
		wantStatus int
		wantCode   string
	}{
		{"unknown profile", false, map[string]any{"profile": "nope"}, 400, "profile_not_found"},
		{"disabled profile", false, map[string]any{"profile": "off"}, 400, "profile_not_allowed"},
		{"unknown allowlist name", false, map[string]any{"allowed_profiles": []string{"nope"}}, 400, "profile_not_found"},
		{"allowlist from an agent session", true, map[string]any{"allowed_profiles": []string{"cheap"}}, 403, "human_only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tasksTestDeps(t)
			srv := newTestServer(t, d)
			id := newBacklogTask(t, d)
			off := fakeProfile("off", "", "", 2)
			off.Enabled = false
			setProfiles(t, d, "cheap", "cheap", fakeProfile("cheap", "sonnet", "", 1), off)
			caller := ""
			if tt.asAgent {
				caller = addTestSession(t, d, "cto", "agent", "proj1").ID
			}

			resp := startTask(t, srv.URL, id, caller, tt.body)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if eb := decodeErr(t, resp); eb.Error.Code != tt.wantCode {
				t.Errorf("code = %q, want %s", eb.Error.Code, tt.wantCode)
			}
			task, err := d.Store.GetTask(id)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			if task.Status != "backlog" || len(task.AllowedProfiles) != 0 {
				t.Errorf("refused start changed the task: status %q, allowlist %v", task.Status, task.AllowedProfiles)
			}
		})
	}
}

// TestPostTaskStartStoresAllowlistBeforeSpawn: the allowlist given at start
// is on the task by the time the orchestrator exists, so its very first
// `rocket models ls` already sees the narrowed list.
func TestPostTaskStartStoresAllowlistBeforeSpawn(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	id := newBacklogTask(t, d)
	setProfiles(t, d, "cheap", "cheap", fakeProfile("cheap", "sonnet", "", 1), fakeProfile("deep", "opus", "", 2))

	resp := startTask(t, srv.URL, id, "", map[string]any{"allowed_profiles": []string{"deep"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	task, err := d.Store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(task.AllowedProfiles) != 1 || task.AllowedProfiles[0] != "deep" {
		t.Errorf("allowlist = %v, want [deep]", task.AllowedProfiles)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/model-profiles/available", nil)
	req.Header.Set(sessionHeader, task.SessionID)
	avResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET available: %v", err)
	}
	defer avResp.Body.Close()
	var av struct {
		Profiles []struct{ Name string } `json:"profiles"`
		Default  string                  `json:"default"`
	}
	if err := json.NewDecoder(avResp.Body).Decode(&av); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(av.Profiles) != 1 || av.Profiles[0].Name != "deep" || av.Default != "deep" {
		t.Errorf("available = %+v, want only deep", av)
	}
}
