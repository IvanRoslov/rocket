package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/store"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func creativeRequest(t *testing.T, d Deps, method, path, body, caller string, status int) []byte {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if caller != "" {
		r.Header.Set(sessionHeader, caller)
	}
	w := httptest.NewRecorder()
	NewHandler(d).ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status %d want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	return w.Body.Bytes()
}
func creativeAPISetup(t *testing.T) (Deps, store.CreativeRun) {
	t.Helper()
	d := tasksTestDeps(t)
	addTestProject(t, d, "p")
	addTestSession(t, d, "orch", "orchestrator", "p")
	addTestSession(t, d, "outsider", "orchestrator", "other")
	b := creativeRequest(t, d, "POST", "/v1/creative/pipelines", `{"project_id":"p","name":"Video","stages":[{"name":"Plan","approval":"stage"},{"name":"Scenes","approval":"item","repeated":true}]}`, "", 201)
	var p store.CreativePipeline
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	b = creativeRequest(t, d, "POST", "/v1/creative/runs", fmt.Sprintf(`{"project_id":"p","pipeline_id":%d,"name":"Run","brief":"Brief"}`, p.ID), "", 201)
	var run store.CreativeRun
	if err := json.Unmarshal(b, &run); err != nil {
		t.Fatal(err)
	}
	return d, run
}
func TestCreativeAPIApprovalsAndDevelopmentSeparation(t *testing.T) {
	d, r := creativeAPISetup(t)
	path := fmt.Sprintf("/v1/creative/runs/%d/stages/%d", r.ID, r.Stages[0].ID)
	creativeRequest(t, d, "POST", path+"/actions", `{"action":"start"}`, "unknown", 401)
	creativeRequest(t, d, "POST", path+"/actions", `{"action":"start"}`, "outsider", 403)
	creativeRequest(t, d, "POST", path+"/actions", `{"action":"start"}`, "orch", 200)
	creativeRequest(t, d, "PATCH", path, `{"progress":100,"progress_note":"Ready"}`, "orch", 200)
	creativeRequest(t, d, "PATCH", path, `{"status":"completed"}`, "orch", 400)
	creativeRequest(t, d, "POST", path+"/actions", `{"action":"submit"}`, "orch", 200)
	creativeRequest(t, d, "POST", path+"/actions", `{"action":"approve","review_revision":1}`, "orch", 403)
	creativeRequest(t, d, "POST", path+"/actions", `{"action":"approve","review_revision":2}`, "", 409)
	creativeRequest(t, d, "POST", path+"/actions", `{"action":"approve","review_revision":1}`, "", 200)
	creativeRequest(t, d, "POST", path+"/actions", `{"action":"reopen"}`, "orch", 403)
	creativeRequest(t, d, "POST", path+"/actions", `{"action":"reopen"}`, "", 200)
	tasks, err := d.Store.ListTasks(store.TaskFilter{})
	if err != nil || len(tasks) != 0 {
		t.Fatalf("task lifecycle changed: %v %v", tasks, err)
	}
}
func TestCreativeAPIContextItemsAndStrictBodies(t *testing.T) {
	d, r := creativeAPISetup(t)
	base := fmt.Sprintf("/v1/creative/runs/%d", r.ID)
	stage := base + fmt.Sprintf("/stages/%d", r.Stages[1].ID)
	creativeRequest(t, d, "GET", base+"/context?item_id=1", "", "", 400)
	creativeRequest(t, d, "GET", base+"/context?stage_id=99999", "", "", 404)
	creativeRequest(t, d, "GET", base+"/context?stage_id=abc", "", "", 400)
	creativeRequest(t, d, "POST", stage+"/items", `{"name":"Scene 1"}`, "orch", 201)
	got, err := d.Store.GetCreativeRun(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	item := got.Stages[1].Items[0]
	itemPath := stage + fmt.Sprintf("/items/%d", item.ID)
	creativeRequest(t, d, "PATCH", itemPath, `{"name":"Updated scene"}`, "orch", 200)
	creativeRequest(t, d, "GET", base+fmt.Sprintf("/context?stage_id=%d&item_id=%d", r.Stages[1].ID, item.ID), "", "", 200)
	creativeRequest(t, d, "DELETE", itemPath, "", "orch", 200)
	creativeRequest(t, d, "POST", stage+"/items", `{"name":"X"} {"name":"Y"}`, "", 400)
	creativeRequest(t, d, "PUT", base+"/orchestrator", `{"session_id":"orch"}`, "", 200)
	creativeRequest(t, d, "GET", "/v1/creative/runs?project_id=p", "", "", 200)
	creativeRequest(t, d, "GET", "/v1/creative/pipelines?project_id=p", "", "", 200)
}

func TestCreativeGlobalPipelineAPI(t *testing.T) {
	d := tasksTestDeps(t)
	// The catalog works even before there are any projects.
	body := `{"name":"Shared","stages":[{"name":"Original","approval":"auto"}]}`
	b := creativeRequest(t, d, "POST", "/v1/creative/pipelines", body, "", 201)
	var p store.CreativePipeline
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"project_id"`)) {
		t.Fatalf("template exposes ownership: %s", b)
	}
	addTestProject(t, d, "p")
	addTestProject(t, d, "other")
	addTestSession(t, d, "global-editor", "orchestrator", "other")
	// Legacy project hints do not require a project or restrict global editing.
	legacy := `{"project_id":"nonexistent","name":"Same","stages":[{"name":"Future","approval":"auto"}]}`
	creativeRequest(t, d, "POST", "/v1/creative/pipelines", legacy, "global-editor", 201)
	path := fmt.Sprintf("/v1/creative/pipelines/%d", p.ID)
	creativeRequest(t, d, "PUT", path, legacy, "global-editor", 200)
	for _, query := range []string{"", "?project_id=p", "?project_id=other", "?project_id=nonexistent"} {
		b := creativeRequest(t, d, "GET", "/v1/creative/pipelines"+query, "", "", 200)
		var catalog []store.CreativePipeline
		if err := json.Unmarshal(b, &catalog); err != nil {
			t.Fatal(err)
		}
		if len(catalog) != 2 || catalog[0].ID != p.ID || catalog[0].Name != "Same" || catalog[1].Name != "Same" {
			t.Fatalf("filtered/merged catalog: %s", b)
		}
		if bytes.Contains(b, []byte(`"project_id"`)) {
			t.Fatalf("template exposes ownership: %s", b)
		}
	}
	for _, project := range []string{"p", "other"} {
		create := fmt.Sprintf(`{"project_id":%q,"pipeline_id":%d,"name":"Run","brief":"Brief"}`, project, p.ID)
		b := creativeRequest(t, d, "POST", "/v1/creative/runs", create, "", 201)
		var run store.CreativeRun
		if err := json.Unmarshal(b, &run); err != nil {
			t.Fatal(err)
		}
		if run.ProjectID != project || run.PipelineID != p.ID {
			t.Fatalf("run: %+v", run)
		}
		if project == "p" {
			stage := fmt.Sprintf("/v1/creative/runs/%d/stages/%d/actions", run.ID, run.Stages[0].ID)
			creativeRequest(t, d, "POST", stage, `{"action":"start"}`, "global-editor", 403)
			creativeRequest(t, d, "POST", "/v1/creative/runs", create, "global-editor", 403)
		}
	}
	creativeRequest(t, d, "POST", "/v1/creative/pipelines", body, "unknown", 401)
	creativeRequest(t, d, "PUT", path, body, "unknown", 401)
}

// Removing the successful-action publish or taking versions/actor from request
// data instead of the committed result/resolved caller must break these checks.
func TestCreativeActionEventsCommittedContextAndAttribution(t *testing.T) {
	d, run := creativeAPISetup(t)
	startTime := time.Now().Unix()
	base := fmt.Sprintf("/v1/creative/runs/%d/stages/%d", run.ID, run.Stages[0].ID)
	creativeRequest(t, d, "POST", base+"/actions", `{"action":"start"}`, "orch", 200)
	// Publish an immutable result and exercise the entire stage review cycle.
	publication := decodePublication(t, artifactRequest(NewHandler(d), "POST", creativePublishPath(run.ID, run.Stages[0].ID, "Plan result", "result", "plan.txt"), "text/plain", "orch", strings.NewReader("plan")))
	submit := fmt.Sprintf(`{"action":"submit","artifact_version_ids":[%d]}`, publication.Version.ID)
	creativeRequest(t, d, "POST", base+"/actions", submit, "orch", 200)
	creativeRequest(t, d, "POST", base+"/actions", `{"action":"reject","review_revision":1,"reason":"Fix ending"}`, "", 200)
	creativeRequest(t, d, "POST", base+"/actions", submit, "", 200) // Deliberate same-version resubmission.
	creativeRequest(t, d, "POST", base+"/actions", `{"action":"approve","review_revision":2}`, "", 200)
	creativeRequest(t, d, "POST", base+"/actions", `{"action":"reopen"}`, "", 200)
	events, err := d.Store.ListEvents(0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	var actions []store.Event
	for _, event := range events {
		if event.Type == "creative.stage_action" {
			actions = append(actions, event)
		}
	}
	if len(actions) != 6 {
		t.Fatalf("stage action events = %d, want 6: %+v", len(actions), events)
	}
	wants := []struct {
		action, status, session string
		revision                int
		versions                []int64
	}{
		{"start", "in_progress", "orch", 0, []int64{}},
		{"submit", "awaiting_approval", "orch", 1, []int64{publication.Version.ID}},
		{"reject", "in_progress", "", 1, []int64{publication.Version.ID}},
		{"submit", "awaiting_approval", "", 2, []int64{publication.Version.ID}},
		{"approve", "completed", "", 2, []int64{publication.Version.ID}},
		{"reopen", "in_progress", "", 2, []int64{publication.Version.ID}},
	}
	for i, want := range wants {
		event := actions[i]
		if event.TS < startTime || event.TS > time.Now().Unix() {
			t.Fatalf("event time = %d", event.TS)
		}
		if event.SessionID != want.session || event.Data["actor_session_id"] != want.session {
			t.Fatalf("actor: %+v", event)
		}
		kind, source := "session", "session_header"
		if want.session == "" {
			kind, source = "unattributed", "no_session_header"
		}
		expected := map[string]any{"project_id": "p", "run_id": float64(run.ID), "stage_id": float64(run.Stages[0].ID), "item_id": float64(0), "action": want.action, "status": want.status, "review_revision": float64(want.revision), "actor_kind": kind, "actor_source": source, "actor_session_id": want.session}
		for key, value := range expected {
			if event.Data[key] != value {
				t.Errorf("event %d %s = %v, want %v", i, key, event.Data[key], value)
			}
		}
		raw, _ := json.Marshal(want.versions)
		got, _ := json.Marshal(event.Data["artifact_version_ids"])
		if string(got) != string(raw) {
			t.Errorf("event %d versions = %s, want %s", i, got, raw)
		}
	}
}

func TestCreativeItemActionEventsAndFailures(t *testing.T) {
	d, run := creativeAPISetup(t)
	first := fmt.Sprintf("/v1/creative/runs/%d/stages/%d/actions", run.ID, run.Stages[0].ID)
	creativeRequest(t, d, "POST", first, `{"action":"start"}`, "orch", 200)
	creativeRequest(t, d, "POST", first, `{"action":"submit"}`, "orch", 200)
	creativeRequest(t, d, "POST", first, `{"action":"approve","review_revision":1}`, "", 200)
	base := fmt.Sprintf("/v1/creative/runs/%d/stages/%d", run.ID, run.Stages[1].ID)
	creativeRequest(t, d, "POST", base+"/actions", `{"action":"start"}`, "orch", 200)
	body := creativeRequest(t, d, "POST", base+"/items", `{"name":"Scene"}`, "orch", 201)
	var item store.CreativeItem
	if err := json.Unmarshal(body, &item); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("%s/items/%d/actions", base, item.ID)
	for _, step := range []struct {
		body, caller string
		status       int
	}{
		{`{"action":"start"}`, "unknown", 401},
		{`{"action":"start"}`, "outsider", 403},
		{`{"action":"start","actor_session_id":"invented"}`, "", 400},
		{`{"action":"start"}`, "orch", 200},
		{`{"action":"start"}`, "orch", 409},
		{`{"action":"submit"}`, "orch", 200},
		{`{"action":"reject","review_revision":1}`, "orch", 403},
		{`{"action":"approve","review_revision":9}`, "", 409},
		{`{"action":"reject","review_revision":1,"reason":"Fix ending"}`, "", 200},
		{`{"action":"submit"}`, "orch", 200},
		{`{"action":"approve","review_revision":2}`, "", 200},
		{`{"action":"reopen"}`, "", 200},
	} {
		creativeRequest(t, d, "POST", path, step.body, step.caller, step.status)
	}
	events, err := d.Store.ListEvents(0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, event := range events {
		if event.Type != "creative.item_action" {
			continue
		}
		if event.Data["item_id"] != float64(item.ID) || event.Data["stage_id"] != float64(run.Stages[1].ID) || event.Data["run_id"] != float64(run.ID) {
			t.Fatalf("wrong scope: %+v", event)
		}
		actions = append(actions, event.Data["action"].(string))
	}
	if !reflect.DeepEqual(actions, []string{"start", "submit", "reject", "submit", "approve", "reopen"}) {
		t.Fatalf("successful item actions = %v", actions)
	}
}
