package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/store"
)

// mpDeps is a real store seeded with the starter profiles (claude-opus,
// claude-sonnet, codex; both defaults = claude-opus) plus a feature task
// #featureID owned by orchestrator "orch" and its subtask #subID owned by
// worker "wrk".
type mpFixture struct {
	d         Deps
	url       string
	featureID int64
	subID     int64
}

func newMPFixture(t *testing.T) mpFixture {
	t.Helper()
	d := settingsDeps(t)
	d.Cfg = &config.Config{DefaultAgent: "claude-code"}
	st := d.Store
	if err := st.SeedModelProfiles("claude-code"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []store.Session{
		{ID: "orch", Kind: "orchestrator", ProjectID: "p", FeatureSlug: "f", Agent: "claude-code", State: "running"},
		{ID: "wrk", Kind: "worker", ProjectID: "p", FeatureSlug: "f", ParentID: "orch", Agent: "claude-code", State: "running"},
		{ID: "cto", Kind: "agent", Agent: "claude-code", State: "running"},
	} {
		if err := st.AddSession(s); err != nil {
			t.Fatal(err)
		}
	}
	featureID, err := st.AddTask(store.Task{Title: "feature", ProjectID: "p", SessionID: "orch", Status: "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	subID, err := st.AddTask(store.Task{Title: "sub", ProjectID: "p", ParentID: featureID, SessionID: "wrk", Status: "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, d)
	return mpFixture{d: d, url: srv.URL, featureID: featureID, subID: subID}
}

// mpDo sends method path with an optional JSON body and X-Rocket-Session,
// returning the status and the decoded JSON object (nil for an empty body).
func mpDo(t *testing.T, base, method, path, session string, body any) (int, map[string]any) {
	t.Helper()
	var rdr *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = strings.NewReader(string(b))
	} else {
		rdr = strings.NewReader("")
	}
	req, err := http.NewRequest(method, base+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.Header.Set(sessionHeader, session)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func errMessage(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	m, _ := e["message"].(string)
	return m
}

func profileNamesOf(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["profiles"].([]any)
	if !ok {
		t.Fatalf("profiles missing or not an array: %#v", body)
	}
	out := []string{}
	for _, r := range raw {
		out = append(out, r.(map[string]any)["name"].(string))
	}
	return out
}

func TestListModelProfiles(t *testing.T) {
	f := newMPFixture(t)
	status, body := mpDo(t, f.url, "GET", "/v1/model-profiles", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if got := profileNamesOf(t, body); !reflect.DeepEqual(got, []string{"claude-opus", "claude-sonnet", "codex"}) {
		t.Errorf("names = %v", got)
	}
	first := body["profiles"].([]any)[0].(map[string]any)
	want := map[string]any{"name": "claude-opus", "agent": "claude-code", "model": "opus", "effort": "",
		"description": first["description"], "enabled": true, "position": float64(0)}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("profile JSON = %#v\nwant %#v", first, want)
	}
}

func TestListModelProfilesEmptyIsArray(t *testing.T) {
	d := settingsDeps(t)
	srv := newTestServer(t, d)
	_, body := mpDo(t, srv.URL, "GET", "/v1/model-profiles", "", nil)
	if got := profileNamesOf(t, body); len(got) != 0 {
		t.Errorf("names = %v", got)
	}
}

func TestCreateModelProfile(t *testing.T) {
	f := newMPFixture(t)
	status, body := mpDo(t, f.url, "POST", "/v1/model-profiles", "", map[string]any{
		"name": "deep", "agent": "codex", "model": "gpt-5", "effort": "high", "description": "deep thought"})
	if status != http.StatusCreated {
		t.Fatalf("status = %d body %v", status, body)
	}
	if body["name"] != "deep" || body["effort"] != "high" || body["enabled"] != true || body["position"] != float64(3) {
		t.Errorf("created = %v", body)
	}
	p, err := f.d.Store.GetModelProfile("deep")
	if err != nil || p.Model != "gpt-5" || p.Description != "deep thought" {
		t.Errorf("stored = %+v err %v", p, err)
	}
}

func TestCreateModelProfileValidation(t *testing.T) {
	f := newMPFixture(t)
	cases := []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"bad name", map[string]any{"name": "Bad_Name", "agent": "codex"}, 400, "bad_request"},
		{"empty name", map[string]any{"name": "", "agent": "codex"}, 400, "bad_request"},
		{"name too long", map[string]any{"name": strings.Repeat("a", 41), "agent": "codex"}, 400, "bad_request"},
		{"unknown agent", map[string]any{"name": "x", "agent": "gemini"}, 400, "agent_unavailable"},
		{"bad effort", map[string]any{"name": "x", "agent": "codex", "effort": "max"}, 400, "bad_effort"},
		{"duplicate", map[string]any{"name": "codex", "agent": "codex"}, 409, "profile_exists"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := mpDo(t, f.url, "POST", "/v1/model-profiles", "", tc.body)
			if status != tc.status || errCode(body) != tc.code {
				t.Errorf("got %d %q, want %d %q (%v)", status, errCode(body), tc.status, tc.code, body)
			}
		})
	}
	if _, body := mpDo(t, f.url, "POST", "/v1/model-profiles", "", map[string]any{"name": "x", "agent": "codex", "effort": "max"}); !strings.Contains(errMessage(body), "minimal") {
		t.Errorf("bad_effort message should list allowed efforts: %q", errMessage(body))
	}
}

func TestPatchModelProfile(t *testing.T) {
	f := newMPFixture(t)
	status, body := mpDo(t, f.url, "PATCH", "/v1/model-profiles/claude-sonnet", "", map[string]any{
		"effort": "max", "enabled": false, "position": 9})
	if status != http.StatusOK {
		t.Fatalf("status = %d %v", status, body)
	}
	if body["effort"] != "max" || body["enabled"] != false || body["position"] != float64(9) || body["model"] != "sonnet" {
		t.Errorf("patched = %v", body)
	}
	// Switching agent re-validates the kept effort against the new agent.
	status, body = mpDo(t, f.url, "PATCH", "/v1/model-profiles/claude-sonnet", "", map[string]any{"agent": "codex"})
	if status != 400 || errCode(body) != "bad_effort" {
		t.Errorf("agent switch with stale effort = %d %v", status, body)
	}
	status, body = mpDo(t, f.url, "PATCH", "/v1/model-profiles/claude-sonnet", "", map[string]any{"agent": "codex", "effort": ""})
	if status != 200 || body["agent"] != "codex" || body["effort"] != "" {
		t.Errorf("agent switch clearing effort = %d %v", status, body)
	}
	status, body = mpDo(t, f.url, "PATCH", "/v1/model-profiles/nope", "", map[string]any{"model": "x"})
	if status != 404 || errCode(body) != "profile_not_found" {
		t.Errorf("missing = %d %v", status, body)
	}
}

func TestDeleteModelProfile(t *testing.T) {
	f := newMPFixture(t)
	if status, _ := mpDo(t, f.url, "DELETE", "/v1/model-profiles/codex", "", nil); status != http.StatusNoContent {
		t.Errorf("delete status = %d", status)
	}
	if _, err := f.d.Store.GetModelProfile("codex"); err == nil {
		t.Error("codex still there")
	}
	status, body := mpDo(t, f.url, "DELETE", "/v1/model-profiles/codex", "", nil)
	if status != 404 || errCode(body) != "profile_not_found" {
		t.Errorf("delete missing = %d %v", status, body)
	}
}

func TestDeleteDefaultProfileIsInUse(t *testing.T) {
	f := newMPFixture(t)
	if err := f.d.Store.SetSetting(store.SettingDefaultWorkerProfile, "claude-sonnet"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"claude-opus", "claude-sonnet"} {
		status, body := mpDo(t, f.url, "DELETE", "/v1/model-profiles/"+n, "", nil)
		if status != http.StatusConflict || errCode(body) != "profile_in_use" {
			t.Errorf("delete %s = %d %v", n, status, body)
		}
	}
}

// Review Focus 2: no agent session may widen its own choice.
func TestModelProfileMutationsAreHumanOnly(t *testing.T) {
	f := newMPFixture(t)
	reqs := []struct {
		method, path string
		body         any
	}{
		{"POST", "/v1/model-profiles", map[string]any{"name": "x", "agent": "codex"}},
		{"PATCH", "/v1/model-profiles/codex", map[string]any{"enabled": false}},
		{"DELETE", "/v1/model-profiles/codex", nil},
		{"PUT", "/v1/settings", map[string]any{"default_worker_profile": "codex"}},
		{"PATCH", "/v1/tasks/" + itoa(f.featureID), map[string]any{"allowed_profiles": []string{"codex"}}},
	}
	for _, caller := range []string{"orch", "wrk", "cto"} {
		for _, r := range reqs {
			status, body := mpDo(t, f.url, r.method, r.path, caller, r.body)
			// A worker may not write its feature task at all: it is refused
			// by the general task permission before the allowlist check.
			wantCode := "human_only"
			if caller == "wrk" && r.method == "PATCH" && strings.HasPrefix(r.path, "/v1/tasks/") {
				wantCode = "forbidden"
			}
			if status != http.StatusForbidden || errCode(body) != wantCode {
				t.Errorf("%s %s as %s = %d %v, want 403 %s", r.method, r.path, caller, status, body, wantCode)
			}
		}
	}
	if p, _ := f.d.Store.GetModelProfile("codex"); !p.Enabled {
		t.Error("codex got disabled by an agent")
	}
	if task, _ := f.d.Store.GetTask(f.featureID); task.AllowedProfiles != nil {
		t.Errorf("allowlist changed by an agent: %v", task.AllowedProfiles)
	}
}

func TestAvailableForHuman(t *testing.T) {
	f := newMPFixture(t)
	if status, body := mpDo(t, f.url, "PATCH", "/v1/model-profiles/codex", "", map[string]any{"enabled": false}); status != 200 {
		t.Fatalf("disable codex = %d %v", status, body)
	}
	status, body := mpDo(t, f.url, "GET", "/v1/model-profiles/available", "", nil)
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if got := profileNamesOf(t, body); !reflect.DeepEqual(got, []string{"claude-opus", "claude-sonnet"}) {
		t.Errorf("available = %v", got)
	}
	if body["default"] != "claude-opus" {
		t.Errorf("default = %v", body["default"])
	}
	first := body["profiles"].([]any)[0].(map[string]any)
	if _, has := first["enabled"]; has {
		t.Errorf("available profile carries registry-only fields: %v", first)
	}
}

// Review Focus 1: a worker sees its PARENT feature task's allowlist.
func TestAvailableForWorkerUsesParentAllowlist(t *testing.T) {
	f := newMPFixture(t)
	if err := f.d.Store.SetTaskAllowedProfiles(f.featureID, []string{"codex", "claude-sonnet"}); err != nil {
		t.Fatal(err)
	}
	for _, caller := range []string{"wrk", "orch"} {
		status, body := mpDo(t, f.url, "GET", "/v1/model-profiles/available", caller, nil)
		if status != 200 {
			t.Fatalf("status = %d %v", status, body)
		}
		if got := profileNamesOf(t, body); !reflect.DeepEqual(got, []string{"claude-sonnet", "codex"}) {
			t.Errorf("%s available = %v", caller, got)
		}
		// default_worker_profile (claude-opus) is outside the allowlist.
		if body["default"] != "claude-sonnet" {
			t.Errorf("%s default = %v", caller, body["default"])
		}
	}
}

func TestAvailableEmptyIntersection(t *testing.T) {
	f := newMPFixture(t)
	if err := f.d.Store.SetTaskAllowedProfiles(f.featureID, []string{"deleted-one"}); err != nil {
		t.Fatal(err)
	}
	_, body := mpDo(t, f.url, "GET", "/v1/model-profiles/available", "wrk", nil)
	if got := profileNamesOf(t, body); len(got) != 0 || body["default"] != "" {
		t.Errorf("available = %v default %v, want none", got, body["default"])
	}
}

func TestAvailableUnknownSession(t *testing.T) {
	f := newMPFixture(t)
	if status, _ := mpDo(t, f.url, "GET", "/v1/model-profiles/available", "ghost", nil); status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
}

func TestPatchTaskAllowedProfiles(t *testing.T) {
	f := newMPFixture(t)
	path := "/v1/tasks/" + itoa(f.featureID)
	status, body := mpDo(t, f.url, "PATCH", path, "", map[string]any{"allowed_profiles": []string{"codex"}})
	if status != 200 {
		t.Fatalf("status = %d %v", status, body)
	}
	if !reflect.DeepEqual(body["allowed_profiles"], []any{"codex"}) {
		t.Errorf("allowed_profiles = %v", body["allowed_profiles"])
	}
	_, body = mpDo(t, f.url, "GET", path, "", nil)
	if !reflect.DeepEqual(body["allowed_profiles"], []any{"codex"}) || body["orchestrator_profile"] != "" {
		t.Errorf("GET task = %v / %v", body["allowed_profiles"], body["orchestrator_profile"])
	}
	status, body = mpDo(t, f.url, "PATCH", path, "", map[string]any{"allowed_profiles": []string{}})
	if status != 200 || !reflect.DeepEqual(body["allowed_profiles"], []any{}) {
		t.Errorf("clear = %d %v", status, body["allowed_profiles"])
	}
}

// Review Focus 5 (API side): an unknown name rejects the whole PATCH.
func TestPatchTaskUnknownProfileAppliesNothing(t *testing.T) {
	f := newMPFixture(t)
	path := "/v1/tasks/" + itoa(f.featureID)
	status, body := mpDo(t, f.url, "PATCH", path, "", map[string]any{
		"title": "renamed", "allowed_profiles": []string{"codex", "nope"}})
	if status != 400 || errCode(body) != "profile_not_found" || !strings.Contains(errMessage(body), "nope") {
		t.Errorf("got %d %v", status, body)
	}
	task, _ := f.d.Store.GetTask(f.featureID)
	if task.Title != "feature" || task.AllowedProfiles != nil {
		t.Errorf("partial apply: title %q allowed %v", task.Title, task.AllowedProfiles)
	}
}

// Agents keep patching other task fields as before.
func TestPatchTaskWithoutAllowlistStillWorksForAgents(t *testing.T) {
	f := newMPFixture(t)
	status, body := mpDo(t, f.url, "PATCH", "/v1/tasks/"+itoa(f.subID), "wrk", map[string]any{"title": "sub2"})
	if status != 200 || body["title"] != "sub2" {
		t.Errorf("got %d %v", status, body)
	}
}

func TestSettingsDefaultProfiles(t *testing.T) {
	f := newMPFixture(t)
	body := getSettingsBody(t, f.url)
	if body["default_orchestrator_profile"] != "claude-opus" || body["default_worker_profile"] != "claude-opus" {
		t.Errorf("GET = %v", body)
	}
	status, body := mpDo(t, f.url, "PUT", "/v1/settings", "", map[string]any{
		"default_worker_profile": "codex", "default_orchestrator_profile": "claude-sonnet"})
	if status != 200 || body["default_worker_profile"] != "codex" || body["default_orchestrator_profile"] != "claude-sonnet" {
		t.Errorf("PUT = %d %v", status, body)
	}
	status, body = mpDo(t, f.url, "PUT", "/v1/settings", "", map[string]any{"default_worker_profile": ""})
	if status != 200 || body["default_worker_profile"] != "" {
		t.Errorf("PUT clear = %d %v", status, body)
	}
	if _, err := f.d.Store.GetSetting(store.SettingDefaultWorkerProfile); err == nil {
		t.Error("cleared default still stored")
	}
}

func TestSettingsDefaultProfileMustExist(t *testing.T) {
	f := newMPFixture(t)
	status, body := mpDo(t, f.url, "PUT", "/v1/settings", "", map[string]any{
		"orchestrator_brainstorm_custom": true, "default_worker_profile": "nope"})
	if status != 400 || errCode(body) != "profile_not_found" {
		t.Errorf("got %d %v", status, body)
	}
	if on, _ := f.d.Store.OrchestratorBrainstormCustom(); on {
		t.Error("toggle applied despite rejected profile")
	}
}

func TestSessionResponseCarriesProfileSnapshot(t *testing.T) {
	f := newMPFixture(t)
	sess, _ := f.d.Store.GetSession("wrk")
	sess.Profile, sess.Model, sess.Effort = "claude-opus", "opus", "high"
	if err := f.d.Store.UpdateSession(sess); err != nil {
		t.Fatal(err)
	}
	status, body := mpDo(t, f.url, "GET", "/v1/sessions/wrk", "", nil)
	if status != 200 || body["profile"] != "claude-opus" || body["model"] != "opus" || body["effort"] != "high" {
		t.Errorf("got %d %v", status, body)
	}
}
