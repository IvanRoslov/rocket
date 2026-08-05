package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// --- helpers ----------------------------------------------------------

// patchJSONWithHeader is patchJSON plus an optional X-Rocket-Session header.
func patchJSONWithHeader(t *testing.T, url, sessionID string, payload any) *http.Response {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, url, bytesReader(b))
	if err != nil {
		t.Fatalf("build PATCH request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set(sessionHeader, sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH %s: %v", url, err)
	}
	return resp
}

// registerTestAgent registers a persistent agent and gives it a live session,
// so it can call the API as an agent caller.
func registerTestAgent(t *testing.T, d Deps, id string) {
	t.Helper()
	if err := d.Store.AddAgent(store.Agent{ID: id, Enabled: true}); err != nil {
		t.Fatalf("AddAgent %s: %v", id, err)
	}
	if err := d.Store.AddSession(store.Session{
		ID: id, Kind: "agent", TmuxName: id, State: "running",
	}); err != nil {
		t.Fatalf("AddSession %s: %v", id, err)
	}
}

// addMilestone inserts a milestone task directly into the store.
func addMilestone(t *testing.T, d Deps, title string) int64 {
	t.Helper()
	id, err := d.Store.AddTask(store.Task{Title: title, Milestone: true, CreatedBy: "user"})
	if err != nil {
		t.Fatalf("AddTask milestone: %v", err)
	}
	return id
}

// --- creation ---------------------------------------------------------

func TestPostTaskMilestone(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/tasks", map[string]any{"title": "Agents UX", "milestone": true})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeMap(t, resp)
	if body["milestone"] != true {
		t.Errorf("milestone = %v, want true", body["milestone"])
	}
	if body["project_id"] != "" {
		t.Errorf("project_id = %v, want empty", body["project_id"])
	}
	if _, ok := body["assigned_role"]; ok {
		t.Errorf("assigned_role present on an untaken milestone: %v", body["assigned_role"])
	}
}

func TestPostTaskMilestoneWithProjectRejected(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	resp := postJSON(t, srv.URL+"/v1/tasks",
		map[string]any{"title": "x", "milestone": true, "project": "proj1"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "milestone_with_project" {
		t.Errorf("code = %q, want milestone_with_project", eb.Error.Code)
	}
}

func TestPostTaskMilestoneCallers(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	registerTestAgent(t, d, "cto")
	addTestSession(t, d, "orch1", "orchestrator", "proj1")

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks", "cto",
		map[string]any{"title": "by agent", "milestone": true})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("agent create: status = %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()

	resp = postJSONWithHeader(t, srv.URL+"/v1/tasks", "orch1",
		map[string]any{"title": "by orch", "milestone": true})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("orchestrator create: status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestListTasksMilestonesFilter(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	if _, err := d.Store.AddTask(store.Task{Title: "regular", ProjectID: "proj1"}); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	msID := addMilestone(t, d, "milestone")

	resp := getJSON(t, srv.URL+"/v1/tasks?milestones=true")
	defer resp.Body.Close()
	var body struct {
		Tasks []struct {
			ID        int64 `json:"id"`
			Milestone bool  `json:"milestone"`
		} `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Tasks) != 1 || body.Tasks[0].ID != msID || !body.Tasks[0].Milestone {
		t.Fatalf("tasks = %+v, want only milestone #%d", body.Tasks, msID)
	}
}

// --- take -------------------------------------------------------------

func TestTakeMilestone(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	registerTestAgent(t, d, "cto")
	id := addMilestone(t, d, "Agents UX")
	path := srv.URL + "/v1/tasks/" + itoa(id) + "/take"

	resp := postJSONWithHeader(t, path, "cto", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := decodeMap(t, resp)["assigned_role"]; got != "cto" {
		t.Errorf("assigned_role = %v, want cto", got)
	}

	// Idempotent for the holder.
	resp = postJSONWithHeader(t, path, "cto", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-take by holder: status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// The take is journalled.
	entries, err := d.Store.ListTaskLog(id, "status")
	if err != nil {
		t.Fatalf("ListTaskLog: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("take wrote no status log entry")
	}
}

func TestTakeMilestoneConflict(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	registerTestAgent(t, d, "cto")
	registerTestAgent(t, d, "cfo")
	id := addMilestone(t, d, "Agents UX")
	path := srv.URL + "/v1/tasks/" + itoa(id) + "/take"

	resp := postJSONWithHeader(t, path, "cto", nil)
	resp.Body.Close()

	resp = postJSONWithHeader(t, path, "cfo", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "already_taken" {
		t.Errorf("code = %q, want already_taken", eb.Error.Code)
	}
	if !strings.Contains(eb.Error.Message, "cto") {
		t.Errorf("message = %q, want it to name the holder cto", eb.Error.Message)
	}
}

func TestTakeRejectsNonAgentCallers(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch1", "orchestrator", "proj1")
	id := addMilestone(t, d, "Agents UX")
	path := srv.URL + "/v1/tasks/" + itoa(id) + "/take"

	resp := postJSON(t, path, nil) // human
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("human take: status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	resp = postJSONWithHeader(t, path, "orch1", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("orchestrator take: status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTakeRejectsRegularTask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	registerTestAgent(t, d, "cto")
	id, err := d.Store.AddTask(store.Task{Title: "regular", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(id)+"/take", "cto", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "not_a_milestone" {
		t.Errorf("code = %q, want not_a_milestone", eb.Error.Code)
	}
}

// --- assign -----------------------------------------------------------

func TestAssignMilestoneDeliversAndClears(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	registerTestAgent(t, d, "cto")
	id := addMilestone(t, d, "Agents UX")
	path := srv.URL + "/v1/tasks/" + itoa(id) + "/assign"

	resp := postJSON(t, path, map[string]any{"agent_id": "cto"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := decodeMap(t, resp)["assigned_role"]; got != "cto" {
		t.Errorf("assigned_role = %v, want cto", got)
	}

	msgs, err := d.Store.ListMessages("cto", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0].Body, "You have been assigned milestone") {
		t.Fatalf("assignment notice not delivered: %+v", msgs)
	}

	resp = postJSON(t, path, map[string]any{"none": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unassign: status = %d, want 200", resp.StatusCode)
	}
	if got := decodeMap(t, resp)["assigned_role"]; got != nil && got != "" {
		t.Errorf("assigned_role after --none = %v, want empty", got)
	}
}

func TestAssignRejectsAgentCallerAndUnknownAgent(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	registerTestAgent(t, d, "cto")
	id := addMilestone(t, d, "Agents UX")
	path := srv.URL + "/v1/tasks/" + itoa(id) + "/assign"

	resp := postJSONWithHeader(t, path, "cto", map[string]any{"agent_id": "cto"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("agent assign: status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	resp = postJSON(t, path, map[string]any{"agent_id": "nope"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown agent: status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "agent_not_found" {
		t.Errorf("code = %q, want agent_not_found", eb.Error.Code)
	}
}

func TestAssignRejectsRegularTask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	registerTestAgent(t, d, "cto")
	id, err := d.Store.AddTask(store.Task{Title: "regular", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/assign", map[string]any{"agent_id": "cto"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "not_a_milestone" {
		t.Errorf("code = %q, want not_a_milestone", eb.Error.Code)
	}
}

// --- lifecycle gates --------------------------------------------------

func TestStartRejectedOnMilestone(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	id := addMilestone(t, d, "Agents UX")

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/start", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "milestone_not_startable" {
		t.Errorf("code = %q, want milestone_not_startable", eb.Error.Code)
	}
}

func TestMilestoneReviewGate(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	registerTestAgent(t, d, "cto")
	id := addMilestone(t, d, "Agents UX")
	if err := d.Store.SetTaskAssignedRole(id, "cto"); err != nil {
		t.Fatalf("SetTaskAssignedRole: %v", err)
	}
	path := srv.URL + "/v1/tasks/" + itoa(id)

	// Nothing to show yet — even the auto-status entries a take writes don't
	// count as work.
	if _, err := d.Store.AddTaskLog(store.TaskLogEntry{
		TaskID: id, Kind: "status", Body: "taken by cto", Author: "cto",
	}); err != nil {
		t.Fatalf("AddTaskLog: %v", err)
	}
	resp := patchJSONWithHeader(t, path, "cto", map[string]any{"status": "review"})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("empty milestone: status = %d, want 422", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "milestone_empty" {
		t.Errorf("code = %q, want milestone_empty", eb.Error.Code)
	}

	// A journal entry from the agent opens the gate.
	if _, err := d.Store.AddTaskLog(store.TaskLogEntry{
		TaskID: id, Kind: "decision", Body: "chose X because Y", Author: "cto",
	}); err != nil {
		t.Fatalf("AddTaskLog: %v", err)
	}
	resp = patchJSONWithHeader(t, path, "cto", map[string]any{"status": "review"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with journal entry: status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestMilestoneReviewGateOpenedByDoc(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	registerTestAgent(t, d, "cto")
	id := addMilestone(t, d, "Agents UX")
	if err := d.Store.SetTaskAssignedRole(id, "cto"); err != nil {
		t.Fatalf("SetTaskAssignedRole: %v", err)
	}
	if _, err := d.Store.PutTaskDoc(store.TaskDoc{
		TaskID: id, Kind: "report", Title: "result", Body: "done", Author: "cto",
	}); err != nil {
		t.Fatalf("PutTaskDoc: %v", err)
	}

	resp := patchJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(id), "cto",
		map[string]any{"status": "review"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestMilestoneDoneAndCancelAreHumanOnly(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	registerTestAgent(t, d, "cto")
	id := addMilestone(t, d, "Agents UX")
	if err := d.Store.SetTaskAssignedRole(id, "cto"); err != nil {
		t.Fatalf("SetTaskAssignedRole: %v", err)
	}
	path := srv.URL + "/v1/tasks/" + itoa(id)

	resp := patchJSONWithHeader(t, path, "cto", map[string]any{"status": "done"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("agent done: status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "human_only" {
		t.Errorf("code = %q, want human_only", eb.Error.Code)
	}

	resp = postJSONWithHeader(t, path+"/cancel", "cto", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("agent cancel: status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	resp = patchJSON(t, path, map[string]any{"status": "done"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("human done: status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestMilestoneWritesRestrictedToHolder(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	registerTestAgent(t, d, "cto")
	registerTestAgent(t, d, "cfo")
	id := addMilestone(t, d, "Agents UX")
	if err := d.Store.SetTaskAssignedRole(id, "cto"); err != nil {
		t.Fatalf("SetTaskAssignedRole: %v", err)
	}
	path := srv.URL + "/v1/tasks/" + itoa(id) + "/log"

	resp := postJSONWithHeader(t, path, "cfo", map[string]any{"kind": "note", "body": "hi"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("other agent write: status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	resp = postJSONWithHeader(t, path, "cto", map[string]any{"kind": "note", "body": "hi"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("holder write: status = %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()

	resp = postJSON(t, path, map[string]any{"kind": "note", "body": "hi"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("human write: status = %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAgentResponseListsMilestones(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	registerTestAgent(t, d, "cto")
	id := addMilestone(t, d, "Agents UX")
	if err := d.Store.SetTaskAssignedRole(id, "cto"); err != nil {
		t.Fatalf("SetTaskAssignedRole: %v", err)
	}
	addMilestone(t, d, "not taken")

	resp := getJSON(t, srv.URL+"/v1/agents/cto")
	defer resp.Body.Close()
	var body struct {
		Milestones []struct {
			ID     int64  `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"milestones"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Milestones) != 1 || body.Milestones[0].ID != id || body.Milestones[0].Title != "Agents UX" {
		t.Fatalf("milestones = %+v, want only #%d", body.Milestones, id)
	}
}

// --- quiet flag -------------------------------------------------------

// addQuietMilestone inserts a milestone taken by agentID whose last movement
// is age hours ago, with no trace of the agent since.
func addQuietMilestone(t *testing.T, d Deps, title, agentID string, ageHours int64) int64 {
	t.Helper()
	ts := time.Now().Add(-time.Duration(ageHours) * time.Hour).Unix()
	id, err := d.Store.AddTask(store.Task{
		Title: title, Status: "in_progress", Milestone: true, CreatedBy: "user",
		AssignedRole: agentID, CreatedAt: ts, UpdatedAt: ts,
	})
	if err != nil {
		t.Fatalf("AddTask milestone: %v", err)
	}
	return id
}

// TestMilestoneQuietFlag: criterion 5 — the human's channel for a silent
// milestone is the derived `quiet` flag, present on both the detail and the
// list response and absent from everything that isn't silent.
func TestMilestoneQuietFlag(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	registerTestAgent(t, d, "cto")

	quiet := addQuietMilestone(t, d, "silent", "cto", 30)
	fresh := addQuietMilestone(t, d, "busy", "cto", 30)
	if _, err := d.Store.AddTaskLog(store.TaskLogEntry{
		TaskID: fresh, Kind: "decision", Body: "chose A", Author: "cto",
		CreatedAt: time.Now().Add(-time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("AddTaskLog: %v", err)
	}
	addTestProject(t, d, "billing")
	plain, err := d.Store.AddTask(store.Task{
		Title: "regular", ProjectID: "billing", Status: "in_progress",
		CreatedAt: 1000, UpdatedAt: 1000,
	})
	if err != nil {
		t.Fatalf("AddTask regular: %v", err)
	}

	detail := decodeMap(t, getJSON(t, srv.URL+"/v1/tasks/"+strconv.FormatInt(quiet, 10)))
	if detail["quiet"] != true {
		t.Errorf("detail quiet = %v, want true", detail["quiet"])
	}

	resp := getJSON(t, srv.URL+"/v1/tasks?parent=all")
	defer resp.Body.Close()
	var listed struct {
		Tasks []struct {
			ID    int64 `json:"id"`
			Quiet bool  `json:"quiet"`
		} `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	got := map[int64]bool{}
	for _, tk := range listed.Tasks {
		got[tk.ID] = tk.Quiet
	}
	if !got[quiet] {
		t.Errorf("list: milestone #%d must be quiet", quiet)
	}
	if got[fresh] {
		t.Errorf("list: milestone #%d had activity an hour ago, must not be quiet", fresh)
	}
	if got[plain] {
		t.Errorf("list: regular task #%d must never be quiet", plain)
	}
}
