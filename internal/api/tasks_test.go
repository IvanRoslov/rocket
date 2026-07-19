package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// tasksTestDeps builds Deps with a real store/bus/manager (fake
// runtime/workspace), for tests that exercise the /v1/tasks endpoints.
// Reuses sessionsTestDeps since tasks need session-kill cascades too.
func tasksTestDeps(t *testing.T) Deps {
	t.Helper()
	return sessionsTestDeps(t)
}

func addTestProject(t *testing.T, d Deps, id string) {
	t.Helper()
	repo := addTestRepo(t, d, id+"-repo")
	_ = repo
	if err := d.Store.AddProject(store.Project{ID: id, Name: id, MainRepo: id + "-repo"}); err != nil {
		t.Fatalf("AddProject %s: %v", id, err)
	}
}

// addTestSession inserts a session directly into the store for use as an
// agent caller or as a task's attached session.
func addTestSession(t *testing.T, d Deps, id, kind, projectID string) store.Session {
	t.Helper()
	sess := store.Session{
		ID:        id,
		Kind:      kind,
		ProjectID: projectID,
		RepoID:    projectID + "-repo",
		Agent:     "fake",
		Branch:    "feature/x/" + id,
		TmuxName:  id,
		State:     "running",
	}
	if err := d.Store.AddSession(sess); err != nil {
		t.Fatalf("AddSession %s: %v", id, err)
	}
	return sess
}

func getJSON(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

// --- POST /v1/tasks ---------------------------------------------------

func TestPostTaskEmptyTitle(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	resp := postJSON(t, srv.URL+"/v1/tasks", map[string]any{"title": "", "project": "proj1"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "empty_title" {
		t.Errorf("code = %q, want empty_title", eb.Error.Code)
	}
}

func TestPostTaskProjectNotFound(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/tasks", map[string]any{"title": "T", "project": "nope"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "project_not_found" {
		t.Errorf("code = %q, want project_not_found", eb.Error.Code)
	}
}

func TestPostTaskHappyPath(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	resp := postJSON(t, srv.URL+"/v1/tasks", map[string]any{
		"title": "Do the thing", "description": "desc", "project": "proj1",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeRepo(t, resp)
	if body["title"] != "Do the thing" {
		t.Errorf("title = %v", body["title"])
	}
	if body["status"] != "backlog" {
		t.Errorf("status = %v, want backlog", body["status"])
	}
	if body["created_by"] != "user" {
		t.Errorf("created_by = %v, want user", body["created_by"])
	}

	// task.created event was published.
	events, err := d.Store.ListEventsTail(10, "")
	if err != nil {
		t.Fatalf("ListEventsTail: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Type == "task.created" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected task.created event, got %+v", events)
	}
}

func TestPostTaskParentNotFound(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	resp := postJSON(t, srv.URL+"/v1/tasks", map[string]any{
		"title": "T", "project": "proj1", "parent_id": 999,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "task_not_found" {
		t.Errorf("code = %q, want task_not_found", eb.Error.Code)
	}
}

func TestPostTaskParentInheritsProjectWhenProjectEmpty(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestProject(t, d, "proj2")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj2"})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks", map[string]any{
		"title": "Sub", "parent_id": rootID,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeRepo(t, resp)
	if body["project_id"] != "proj2" {
		t.Errorf("project_id = %v, want proj2 (inherited from parent)", body["project_id"])
	}
}

func TestPostTaskNestedSubtaskRejected(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	subID, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID})
	if err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks", map[string]any{
		"title": "GrandChild", "project": "proj1", "parent_id": subID,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "nested_subtask" {
		t.Errorf("code = %q, want nested_subtask", eb.Error.Code)
	}
}

func TestPostTaskUnknownSession(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/tasks", bytesReader([]byte(`{"title":"T","project":"proj1"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "nope")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "unknown_session" {
		t.Errorf("code = %q, want unknown_session", eb.Error.Code)
	}
}

func TestPostTaskWorkerCannotCreateTask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "worker-1", "worker", "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: "worker-1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/tasks",
		bytesReader([]byte(`{"title":"Sub","project":"proj1","parent_id":`+itoa(rootID)+`}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "worker-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", eb.Error.Code)
	}
}

func TestPostTaskAgentRootTaskForbidden(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/tasks", bytesReader([]byte(`{"title":"T","project":"proj1"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "orch-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", eb.Error.Code)
	}
}

func TestPostTaskOrchestratorCanCreateSubtaskOfOwnTask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: "orch-1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/tasks",
		bytesReader([]byte(`{"title":"Sub","project":"proj1","parent_id":`+itoa(rootID)+`}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "orch-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeRepo(t, resp)
	if body["created_by"] != "orchestrator" {
		t.Errorf("created_by = %v, want orchestrator", body["created_by"])
	}
}

func TestPostTaskOrchestratorCannotCreateSubtaskOfOtherTask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	addTestSession(t, d, "orch-2", "orchestrator", "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: "orch-2"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/tasks",
		bytesReader([]byte(`{"title":"Sub","project":"proj1","parent_id":`+itoa(rootID)+`}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "orch-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// --- GET /v1/tasks (list, parent filter, board) ------------------------

func TestListTasksParentFilterDefaultsToRoot(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	if _, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID}); err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := getJSON(t, srv.URL+"/v1/tasks")
	defer resp.Body.Close()
	body := decodeRepo(t, resp)
	tasks := body["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1 (root only)", len(tasks))
	}
}

func TestListTasksParentByID(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	if _, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID}); err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := getJSON(t, srv.URL+"/v1/tasks?parent="+itoa(rootID))
	defer resp.Body.Close()
	body := decodeRepo(t, resp)
	tasks := body["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1 (subtask)", len(tasks))
	}
}

func TestListTasksParentAll(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	if _, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID}); err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := getJSON(t, srv.URL+"/v1/tasks?parent=all")
	defer resp.Body.Close()
	body := decodeRepo(t, resp)
	tasks := body["tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2 (all)", len(tasks))
	}
}

func TestListTasksBoardGrouping(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id1, err := d.Store.AddTask(store.Task{Title: "A", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if _, err := d.Store.AddTask(store.Task{Title: "B", ProjectID: "proj1"}); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := d.Store.UpdateTaskStatus(id1, "in_progress"); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	resp := getJSON(t, srv.URL+"/v1/tasks?board=true")
	defer resp.Body.Close()
	body := decodeRepo(t, resp)
	board := body["board"].(map[string]any)
	backlog := board["backlog"].([]any)
	inProgress := board["in_progress"].([]any)
	if len(backlog) != 1 {
		t.Errorf("len(backlog) = %d, want 1", len(backlog))
	}
	if len(inProgress) != 1 {
		t.Errorf("len(in_progress) = %d, want 1", len(inProgress))
	}
}

// --- GET /v1/tasks/{id} -------------------------------------------------

func TestGetTaskNotFound(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)

	resp := getJSON(t, srv.URL+"/v1/tasks/999")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "task_not_found" {
		t.Errorf("code = %q, want task_not_found", eb.Error.Code)
	}
}

func TestGetTaskDetailWithSubtasksAndSession(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	sess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: sess.ID})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	if _, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID}); err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := getJSON(t, srv.URL+"/v1/tasks/"+itoa(rootID))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeRepo(t, resp)
	subtasks := body["subtasks"].([]any)
	if len(subtasks) != 1 {
		t.Fatalf("len(subtasks) = %d, want 1", len(subtasks))
	}
	sessMap, ok := body["session"].(map[string]any)
	if !ok {
		t.Fatalf("session field missing or wrong shape: %+v", body["session"])
	}
	if sessMap["id"] != "orch-1" {
		t.Errorf("session.id = %v, want orch-1", sessMap["id"])
	}
	if sessMap["tmux_name"] != "orch-1" {
		t.Errorf("session.tmux_name = %v", sessMap["tmux_name"])
	}
	if body["open_questions"] != float64(0) {
		t.Errorf("open_questions = %v, want 0", body["open_questions"])
	}
}

func TestGetTaskDetailSubtasksIncludePRAndCI(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}

	// Create subtask without session
	subNoSessionID, err := d.Store.AddTask(store.Task{Title: "Sub No Session", ProjectID: "proj1", ParentID: rootID})
	if err != nil {
		t.Fatalf("AddTask sub no session: %v", err)
	}

	// Create subtask with session and PR/CI info
	sess := store.Session{
		ID:        "worker-1",
		Kind:      "worker",
		ProjectID: "proj1",
		RepoID:    "proj1-repo",
		Agent:     "fake",
		Branch:    "feature/x",
		TmuxName:  "worker-1",
		State:     "running",
		PRNumber:  42,
		PRState:   "open",
		CIState:   "passing",
	}
	if err := d.Store.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	subWithSessionID, err := d.Store.AddTask(store.Task{
		Title:     "Sub With Session",
		ProjectID: "proj1",
		ParentID:  rootID,
		SessionID: sess.ID,
	})
	if err != nil {
		t.Fatalf("AddTask sub with session: %v", err)
	}

	resp := getJSON(t, srv.URL+"/v1/tasks/"+itoa(rootID))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body := decodeRepo(t, resp)
	subtasks := body["subtasks"].([]any)
	if len(subtasks) != 2 {
		t.Fatalf("len(subtasks) = %d, want 2", len(subtasks))
	}

	// Find the subtasks by ID
	var subNoSession, subWithSession map[string]any
	for _, st := range subtasks {
		stMap := st.(map[string]any)
		id := int64(stMap["id"].(float64))
		if id == subNoSessionID {
			subNoSession = stMap
		} else if id == subWithSessionID {
			subWithSession = stMap
		}
	}

	if subNoSession == nil {
		t.Fatalf("subtask without session not found")
	}
	if subWithSession == nil {
		t.Fatalf("subtask with session not found")
	}

	// Subtask without session should have zero PR/CI fields
	if pr, ok := subNoSession["pr_number"]; ok && pr != float64(0) {
		t.Errorf("subtask without session pr_number = %v, want 0 or omitted", pr)
	}
	if ci, ok := subNoSession["ci_state"]; ok && ci != "" {
		t.Errorf("subtask without session ci_state = %v, want empty or omitted", ci)
	}

	// Subtask with session should have PR/CI fields from the session
	if subWithSession["pr_number"] != float64(42) {
		t.Errorf("subtask pr_number = %v, want 42", subWithSession["pr_number"])
	}
	if subWithSession["pr_state"] != "open" {
		t.Errorf("subtask pr_state = %v, want open", subWithSession["pr_state"])
	}
	if subWithSession["ci_state"] != "passing" {
		t.Errorf("subtask ci_state = %v, want passing", subWithSession["ci_state"])
	}
}

// --- PATCH /v1/tasks/{id} -----------------------------------------------

func TestPatchTaskStatusChangeEmitsEventAndLog(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := patchJSON(t, srv.URL+"/v1/tasks/"+itoa(id), map[string]any{"status": "in_progress"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeRepo(t, resp)
	if body["status"] != "in_progress" {
		t.Errorf("status = %v, want in_progress", body["status"])
	}

	updated, err := d.Store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.Status != "in_progress" {
		t.Errorf("stored status = %q, want in_progress", updated.Status)
	}

	events, err := d.Store.ListEventsTail(10, "")
	if err != nil {
		t.Fatalf("ListEventsTail: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Type == "task.status_changed" && e.Data["to"] == "in_progress" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected task.status_changed event, got %+v", events)
	}

	logs, err := d.Store.ListTaskLog(id, "status")
	if err != nil {
		t.Fatalf("ListTaskLog: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}
	if logs[0].Body != "status: backlog → in_progress (by user)" {
		t.Errorf("log body = %q", logs[0].Body)
	}
}

func TestPatchTaskTitleDescription(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "Old", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := patchJSON(t, srv.URL+"/v1/tasks/"+itoa(id), map[string]any{"title": "New", "description": "desc"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	updated, err := d.Store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.Title != "New" || updated.Description != "desc" {
		t.Errorf("task = %+v", updated)
	}
}

func TestPatchTaskInvalidStatus(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := patchJSON(t, srv.URL+"/v1/tasks/"+itoa(id), map[string]any{"status": "bogus"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "invalid_status" {
		t.Errorf("code = %q, want invalid_status", eb.Error.Code)
	}
}

func TestPatchTaskCancelledRejected(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := patchJSON(t, srv.URL+"/v1/tasks/"+itoa(id), map[string]any{"status": "cancelled"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "use_cancel" {
		t.Errorf("code = %q, want use_cancel", eb.Error.Code)
	}

	task, err := d.Store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status == "cancelled" {
		t.Errorf("task status should not have changed to cancelled")
	}
}

func TestPatchTaskNotFound(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)

	resp := patchJSON(t, srv.URL+"/v1/tasks/999", map[string]any{"status": "in_progress"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestPatchTaskWorkerCannotWriteOtherTask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "worker-1", "worker", "proj1")
	addTestSession(t, d, "worker-2", "worker", "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1", SessionID: "worker-2"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/v1/tasks/"+itoa(id), bytesReader([]byte(`{"status":"in_progress"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "worker-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", eb.Error.Code)
	}
}

func TestPatchTaskWorkerCanWriteOwnTask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "worker-1", "worker", "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1", SessionID: "worker-1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/v1/tasks/"+itoa(id), bytesReader([]byte(`{"status":"in_progress"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "worker-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestPatchTaskOrchestratorCanWriteSubtaskOfOwnTask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: "orch-1"})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	subID, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID})
	if err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/v1/tasks/"+itoa(subID), bytesReader([]byte(`{"status":"in_progress"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "orch-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestPatchTaskOrchestratorCannotWriteOtherOrchestratorsTask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	addTestSession(t, d, "orch-2", "orchestrator", "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1", SessionID: "orch-2"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/v1/tasks/"+itoa(id), bytesReader([]byte(`{"status":"in_progress"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "orch-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// --- POST /v1/tasks/{id}/cancel -----------------------------------------

func TestCancelRootTaskCascadesKill(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	orchSess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	workerSess := addTestSession(t, d, "worker-1", "worker", "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: orchSess.ID})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	subID, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID, SessionID: workerSess.ID})
	if err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(rootID)+"/cancel", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	root, err := d.Store.GetTask(rootID)
	if err != nil {
		t.Fatalf("GetTask root: %v", err)
	}
	if root.Status != "cancelled" {
		t.Errorf("root status = %q, want cancelled", root.Status)
	}
	sub, err := d.Store.GetTask(subID)
	if err != nil {
		t.Fatalf("GetTask sub: %v", err)
	}
	if sub.Status != "backlog" {
		t.Errorf("sub status = %q, want unchanged (backlog)", sub.Status)
	}

	orchAfter, err := d.Store.GetSession("orch-1")
	if err != nil {
		t.Fatalf("GetSession orch-1: %v", err)
	}
	if orchAfter.State != "killed" {
		t.Errorf("orch-1 state = %q, want killed", orchAfter.State)
	}
	workerAfter, err := d.Store.GetSession("worker-1")
	if err != nil {
		t.Fatalf("GetSession worker-1: %v", err)
	}
	if workerAfter.State != "killed" {
		t.Errorf("worker-1 state = %q, want killed", workerAfter.State)
	}
}

func TestCancelSubtaskOnlyKillsItsOwnSession(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	orchSess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	workerSess := addTestSession(t, d, "worker-1", "worker", "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: orchSess.ID})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	subID, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID, SessionID: workerSess.ID})
	if err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(subID)+"/cancel", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	sub, err := d.Store.GetTask(subID)
	if err != nil {
		t.Fatalf("GetTask sub: %v", err)
	}
	if sub.Status != "cancelled" {
		t.Errorf("sub status = %q, want cancelled", sub.Status)
	}

	workerAfter, err := d.Store.GetSession("worker-1")
	if err != nil {
		t.Fatalf("GetSession worker-1: %v", err)
	}
	if workerAfter.State != "killed" {
		t.Errorf("worker-1 state = %q, want killed", workerAfter.State)
	}
	orchAfter, err := d.Store.GetSession("orch-1")
	if err != nil {
		t.Fatalf("GetSession orch-1: %v", err)
	}
	if orchAfter.State != "running" {
		t.Errorf("orch-1 state = %q, want still running", orchAfter.State)
	}
}

func TestCancelTaskNotFound(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/tasks/999/cancel", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// --- PATCH /v1/tasks/{id} review gate & done cascade --------------------

// addTestWorkerSession inserts a worker session directly into the store,
// with ParentID set to the given orchestrator session id, as
// liveWorkerSessionIDs (the review gate) and Manager.KillCascade (the done
// cascade) both key off ParentID.
func addTestWorkerSession(t *testing.T, d Deps, id, projectID, parentSessionID string) store.Session {
	t.Helper()
	sess := store.Session{
		ID:        id,
		Kind:      "worker",
		ProjectID: projectID,
		RepoID:    projectID + "-repo",
		ParentID:  parentSessionID,
		Agent:     "fake",
		Branch:    "feature/x/" + id,
		TmuxName:  id,
		State:     "running",
	}
	if err := d.Store.AddSession(sess); err != nil {
		t.Fatalf("AddSession %s: %v", id, err)
	}
	return sess
}

func decodeJSONBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func TestPatchTaskReviewBlockedByOpenSubtask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	orchSess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: orchSess.ID})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	subID, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID})
	if err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := patchJSON(t, srv.URL+"/v1/tasks/"+itoa(rootID), map[string]any{"status": "review"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "review_blocked" {
		t.Errorf("code = %v, want review_blocked", errObj["code"])
	}
	open, _ := body["open_subtasks"].([]any)
	if len(open) != 1 || int64(open[0].(float64)) != subID {
		t.Errorf("open_subtasks = %v, want [%d]", open, subID)
	}
	if lw, ok := body["live_workers"]; ok && lw != nil {
		t.Errorf("live_workers = %v, want absent/empty", lw)
	}

	root, err := d.Store.GetTask(rootID)
	if err != nil {
		t.Fatalf("GetTask root: %v", err)
	}
	if root.Status == "review" {
		t.Errorf("root status moved to review despite open subtask")
	}
}

func TestPatchTaskReviewBlockedByLiveWorker(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	orchSess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	workerSess := addTestWorkerSession(t, d, "worker-1", "proj1", orchSess.ID)

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: orchSess.ID})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	subID, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID, SessionID: workerSess.ID})
	if err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}
	if err := d.Store.UpdateTaskStatus(subID, "done"); err != nil {
		t.Fatalf("UpdateTaskStatus sub done: %v", err)
	}

	resp := patchJSON(t, srv.URL+"/v1/tasks/"+itoa(rootID), map[string]any{"status": "review"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "review_blocked" {
		t.Errorf("code = %v, want review_blocked", errObj["code"])
	}
	live, _ := body["live_workers"].([]any)
	if len(live) != 1 || live[0] != "worker-1" {
		t.Errorf("live_workers = %v, want [worker-1]", live)
	}
}

func TestPatchTaskReviewForceBypasses(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	orchSess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	addTestWorkerSession(t, d, "worker-1", "proj1", orchSess.ID)

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: orchSess.ID})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	if _, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID}); err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := patchJSON(t, srv.URL+"/v1/tasks/"+itoa(rootID)+"?force=true", map[string]any{"status": "review"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	root, err := d.Store.GetTask(rootID)
	if err != nil {
		t.Fatalf("GetTask root: %v", err)
	}
	if root.Status != "review" {
		t.Errorf("root status = %q, want review", root.Status)
	}
}

func TestPatchTaskReviewForceWithOne(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	orchSess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	addTestWorkerSession(t, d, "worker-1", "proj1", orchSess.ID)

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: orchSess.ID})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	if _, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID}); err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	// Test that ParseBool accepts "1" as a valid true value
	resp := patchJSON(t, srv.URL+"/v1/tasks/"+itoa(rootID)+"?force=1", map[string]any{"status": "review"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	root, err := d.Store.GetTask(rootID)
	if err != nil {
		t.Fatalf("GetTask root: %v", err)
	}
	if root.Status != "review" {
		t.Errorf("root status = %q, want review", root.Status)
	}
}

func TestPatchTaskReviewCleanStatePasses(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	orchSess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: orchSess.ID})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	subID, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID})
	if err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}
	if err := d.Store.UpdateTaskStatus(subID, "done"); err != nil {
		t.Fatalf("UpdateTaskStatus sub done: %v", err)
	}

	resp := patchJSON(t, srv.URL+"/v1/tasks/"+itoa(rootID), map[string]any{"status": "review"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	root, err := d.Store.GetTask(rootID)
	if err != nil {
		t.Fatalf("GetTask root: %v", err)
	}
	if root.Status != "review" {
		t.Errorf("root status = %q, want review", root.Status)
	}
}

func TestPatchTaskDoneCascadesOrchestratorCleanup(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	orchSess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	workerSess := addTestWorkerSession(t, d, "worker-1", "proj1", orchSess.ID)

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: orchSess.ID})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}

	resp := patchJSON(t, srv.URL+"/v1/tasks/"+itoa(rootID), map[string]any{"status": "done"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	if body["cleaned_up"] != true {
		t.Errorf("cleaned_up = %v, want true", body["cleaned_up"])
	}

	orchAfter, err := d.Store.GetSession(orchSess.ID)
	if err != nil {
		t.Fatalf("GetSession orch-1: %v", err)
	}
	if orchAfter.State != "killed" {
		t.Errorf("orch-1 state = %q, want killed", orchAfter.State)
	}
	workerAfter, err := d.Store.GetSession(workerSess.ID)
	if err != nil {
		t.Fatalf("GetSession worker-1: %v", err)
	}
	if workerAfter.State != "killed" {
		t.Errorf("worker-1 state = %q, want killed", workerAfter.State)
	}

	entries, err := d.Store.ListTaskLog(rootID, "")
	if err != nil {
		t.Fatalf("ListTaskLog: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Body == "feature closed, orchestrator and workers cleaned up" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cleanup log entry, got %+v", entries)
	}
}

func TestPatchTaskDoneWithDeadOrchestratorNoCascade(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	orchSess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	if err := d.Store.UpdateSessionState(orchSess.ID, "killed"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: orchSess.ID})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}

	resp := patchJSON(t, srv.URL+"/v1/tasks/"+itoa(rootID), map[string]any{"status": "done"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (kill errors must not fail the request): %s", resp.StatusCode, decodeJSONBody(t, resp))
	}
	body := decodeJSONBody(t, resp)
	if body["cleaned_up"] != false {
		t.Errorf("cleaned_up = %v, want false", body["cleaned_up"])
	}

	root, err := d.Store.GetTask(rootID)
	if err != nil {
		t.Fatalf("GetTask root: %v", err)
	}
	if root.Status != "done" {
		t.Errorf("root status = %q, want done", root.Status)
	}
}

func TestPatchSubtaskDoneDoesNotCascade(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	orchSess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	workerSess := addTestWorkerSession(t, d, "worker-1", "proj1", orchSess.ID)

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: orchSess.ID})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	subID, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID, SessionID: workerSess.ID})
	if err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/v1/tasks/"+itoa(subID), bytesReader([]byte(`{"status":"done"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "worker-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	if v, ok := body["cleaned_up"]; ok {
		t.Errorf("cleaned_up = %v, want field absent for subtask done", v)
	}

	orchAfter, err := d.Store.GetSession(orchSess.ID)
	if err != nil {
		t.Fatalf("GetSession orch-1: %v", err)
	}
	if orchAfter.State != "running" {
		t.Errorf("orch-1 state = %q, want still running", orchAfter.State)
	}
}

// --- GET/PUT /v1/tasks/{id}/docs -----------------------------------------

func TestPutTaskDocInvalidKind(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/v1/tasks/"+itoa(id)+"/docs",
		bytesReader([]byte(`{"kind":"bogus","title":"x","body":"y"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "invalid_kind" {
		t.Errorf("code = %q, want invalid_kind", eb.Error.Code)
	}
}

func TestPutThenGetTaskDocVersioning(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	putDoc := func(body string) *http.Response {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/v1/tasks/"+itoa(id)+"/docs", bytesReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PUT: %v", err)
		}
		return resp
	}

	resp1 := putDoc(`{"kind":"spec","title":"Design","body":"v1"}`)
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp1.StatusCode)
	}
	body1 := decodeRepo(t, resp1)
	if body1["version"] != float64(1) {
		t.Errorf("version = %v, want 1", body1["version"])
	}

	resp2 := putDoc(`{"kind":"spec","title":"Design","body":"v2"}`)
	defer resp2.Body.Close()
	body2 := decodeRepo(t, resp2)
	if body2["version"] != float64(2) {
		t.Errorf("version = %v, want 2", body2["version"])
	}

	// Without history: only the latest version.
	resp3 := getJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/docs")
	defer resp3.Body.Close()
	docs3 := decodeRepo(t, resp3)["docs"].([]any)
	if len(docs3) != 1 {
		t.Fatalf("len(docs) = %d, want 1 (latest only)", len(docs3))
	}

	// With history: both versions.
	resp4 := getJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/docs?history=true")
	defer resp4.Body.Close()
	docs4 := decodeRepo(t, resp4)["docs"].([]any)
	if len(docs4) != 2 {
		t.Fatalf("len(docs) = %d, want 2 (history)", len(docs4))
	}
}

func TestPutTaskDocForbiddenForWrongWorker(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "worker-1", "worker", "proj1")
	addTestSession(t, d, "worker-2", "worker", "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1", SessionID: "worker-2"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/v1/tasks/"+itoa(id)+"/docs",
		bytesReader([]byte(`{"kind":"doc","title":"x","body":"y"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "worker-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// --- GET/POST /v1/tasks/{id}/log -----------------------------------------

func TestPostTaskLogInvalidKind(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/log", map[string]any{"kind": "bogus", "body": "x"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "invalid_kind" {
		t.Errorf("code = %q, want invalid_kind", eb.Error.Code)
	}
}

func TestPostThenGetTaskLog(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/log", map[string]any{"kind": "decision", "body": "chose X"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	if _, err := d.Store.AddTaskLog(store.TaskLogEntry{TaskID: id, Kind: "note", Body: "n"}); err != nil {
		t.Fatalf("AddTaskLog: %v", err)
	}

	respAll := getJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/log")
	defer respAll.Body.Close()
	entriesAll := decodeRepo(t, respAll)["log"].([]any)
	if len(entriesAll) != 2 {
		t.Fatalf("len(log) = %d, want 2", len(entriesAll))
	}

	respFiltered := getJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/log?kind=decision")
	defer respFiltered.Body.Close()
	entriesFiltered := decodeRepo(t, respFiltered)["log"].([]any)
	if len(entriesFiltered) != 1 {
		t.Fatalf("len(log) = %d, want 1 (filtered)", len(entriesFiltered))
	}
}

func TestPostTaskLogNotFound(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/tasks/999/log", map[string]any{"kind": "note", "body": "x"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// --- POST /v1/tasks/{id}/start ------------------------------------------

func TestPostTaskStartHappyPath(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "Add login page", Description: "desc", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/start", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	var body struct {
		TaskID      int64  `json:"task_id"`
		FeatureSlug string `json:"feature_slug"`
		SessionID   string `json:"session_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.TaskID != id {
		t.Errorf("task_id = %d, want %d", body.TaskID, id)
	}
	if body.FeatureSlug != "add-login-page" {
		t.Errorf("feature_slug = %q, want add-login-page", body.FeatureSlug)
	}
	if body.SessionID != "add-login-page-orch" {
		t.Errorf("session_id = %q, want add-login-page-orch", body.SessionID)
	}

	task, err := d.Store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != "in_progress" {
		t.Errorf("task status = %q, want in_progress", task.Status)
	}
	if task.FeatureSlug != "add-login-page" {
		t.Errorf("task feature_slug = %q, want add-login-page", task.FeatureSlug)
	}
	if task.SessionID != "add-login-page-orch" {
		t.Errorf("task session_id = %q, want add-login-page-orch", task.SessionID)
	}
}

func TestPostTaskStartAgentCallerForbidden(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/tasks/"+itoa(id)+"/start", nil)
	req.Header.Set(sessionHeader, "orch-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", eb.Error.Code)
	}
}

func TestPostTaskStartNonRootTaskRejected(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	subID, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID})
	if err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(subID)+"/start", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "not_root_task" {
		t.Errorf("code = %q, want not_root_task", eb.Error.Code)
	}
}

func TestPostTaskStartNonBacklogRejected(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := d.Store.UpdateTaskStatus(id, "in_progress"); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/start", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "task_not_startable" {
		t.Errorf("code = %q, want task_not_startable", eb.Error.Code)
	}
}

func TestPostTaskStartAlreadyStartedRejected(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	liveSess := addTestSession(t, d, "orch-live", "orchestrator", "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1", SessionID: liveSess.ID})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/start", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "already_started" {
		t.Errorf("code = %q, want already_started", eb.Error.Code)
	}
}

func TestPostTaskStartAllowedAfterPriorOrchestratorKilled(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	deadSess := addTestSession(t, d, "old-orch", "orchestrator", "proj1")
	deadSess.State = "killed"
	if err := d.Store.UpdateSession(deadSess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1", SessionID: deadSess.ID})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/start", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
}

func TestPostTaskStartNotFound(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/tasks/999/start", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestPostTaskStartConcurrentStartsSerializedCorrectly(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "Concurrent start test", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	// Spawn two concurrent POST requests to start the same task.
	// Due to the startMu lock, exactly one should succeed (201) and the other
	// should get a 409 conflict. We also verify that exactly one orchestrator
	// session exists in the store.
	type result struct {
		statusCode int
		err        error
	}

	resultCh := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(id)+"/start", nil)
			defer resp.Body.Close()
			resultCh <- result{statusCode: resp.StatusCode}
		}()
	}

	// Collect results from both goroutines
	res1 := <-resultCh
	res2 := <-resultCh

	// Sort results so we can check them consistently
	results := []int{res1.statusCode, res2.statusCode}
	created := 0
	conflict := 0
	for _, r := range results {
		if r == http.StatusCreated {
			created++
		} else if r == http.StatusConflict {
			conflict++
		} else {
			t.Errorf("unexpected status code: %d", r)
		}
	}

	if created != 1 {
		t.Errorf("expected exactly 1 success (201), got %d", created)
	}
	if conflict != 1 {
		t.Errorf("expected exactly 1 conflict (409), got %d", conflict)
	}

	// Verify the task now has a session_id
	task, err := d.Store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.SessionID == "" {
		t.Errorf("task.SessionID is empty, want a session id")
	}

	// Verify exactly one session exists for this task
	sess, err := d.Store.GetSession(task.SessionID)
	if err != nil {
		t.Fatalf("GetSession %s: %v", task.SessionID, err)
	}
	if sess.ID == "" {
		t.Errorf("session.ID is empty")
	}
}

// itoa is a tiny helper to avoid importing strconv in every test that needs
// to interpolate a task id into a URL.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
