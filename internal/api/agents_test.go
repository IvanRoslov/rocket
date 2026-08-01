package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/IvanRoslov/rocket/internal/roles"
	"github.com/IvanRoslov/rocket/internal/store"
)

// agentsTestDeps builds Deps with a real store and a temp home, so role home
// directories are created under Cfg.Home.
func agentsTestDeps(t *testing.T) Deps {
	t.Helper()
	d := sessionsTestDeps(t)
	addTestProject(t, d, "platform")
	return d
}

// putJSONWithHeader is putJSON plus an optional X-Rocket-Session header.
func putJSONWithHeader(t *testing.T, url, sessionID string, payload any) *http.Response {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, url, bytesReader(b))
	if err != nil {
		t.Fatalf("build PUT request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set(sessionHeader, sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	return resp
}

func decodeMap(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func decodeList(t *testing.T, resp *http.Response) []map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var body []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return body
}

func createTestAgent(t *testing.T, srv *httptest.Server, id string) map[string]any {
	t.Helper()
	resp := postJSON(t, srv.URL+"/v1/agents", map[string]any{
		"id":      id,
		"project": "platform",
		"prompt":  "you are the " + id + " role",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /v1/agents status = %d, want 201", resp.StatusCode)
	}
	return decodeMap(t, resp)
}

func TestPostAgentCreatesRoleAndHomeDir(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)

	body := createTestAgent(t, srv, "sre")

	if body["id"] != "sre" || body["project"] != "platform" {
		t.Errorf("body = %+v", body)
	}
	if body["enabled"] != true {
		t.Errorf("enabled = %v, want true", body["enabled"])
	}
	if body["agent"] != "fake" { // Cfg.DefaultAgent in tests
		t.Errorf("agent = %v, want the daemon default", body["agent"])
	}

	promptPath := roles.PromptPath(d.Cfg.Home, "sre")
	if body["prompt_path"] != promptPath {
		t.Errorf("prompt_path = %v, want %s", body["prompt_path"], promptPath)
	}
	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read role.md: %v", err)
	}
	if string(prompt) != "you are the sre role" {
		t.Errorf("role.md = %q", prompt)
	}
	if _, err := os.Stat(filepath.Join(roles.MemoryDir(d.Cfg.Home, "sre"), "MEMORY.md")); err != nil {
		t.Errorf("MEMORY.md: %v", err)
	}
}

func TestPostAgentValidation(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)

	cases := []struct {
		name    string
		payload map[string]any
		status  int
		code    string
	}{
		{"bad id", map[string]any{"id": "SRE Bot", "project": "platform"}, http.StatusBadRequest, "invalid_id"},
		{"missing id", map[string]any{"project": "platform"}, http.StatusBadRequest, "invalid_id"},
		{"unknown project", map[string]any{"id": "sre2", "project": "ghost"}, http.StatusBadRequest, "project_not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, srv.URL+"/v1/agents", tc.payload)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			body := decodeErr(t, resp)
			if body.Error.Code != tc.code {
				t.Errorf("code = %q, want %q", body.Error.Code, tc.code)
			}
		})
	}
}

func TestPostAgentDuplicate(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	resp := postJSON(t, srv.URL+"/v1/agents", map[string]any{"id": "sre", "project": "platform"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if code := decodeErr(t, resp).Error.Code; code != "agent_exists" {
		t.Errorf("code = %q, want agent_exists", code)
	}
}

func TestListAgentsFilterAndCounters(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")
	addTestProject(t, d, "web")
	resp := postJSON(t, srv.URL+"/v1/agents", map[string]any{"id": "triage", "project": "web"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create triage: %d", resp.StatusCode)
	}
	resp.Body.Close()

	if resp := postJSON(t, srv.URL+"/v1/agents/sre/wake", map[string]any{"text": "hi"}); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("wake status = %d, want 202", resp.StatusCode)
	}

	all := decodeList(t, getJSON(t, srv.URL+"/v1/agents"))
	if len(all) != 2 {
		t.Fatalf("agents = %d, want 2", len(all))
	}
	if all[0]["id"] != "sre" || all[0]["inbox_queued"] != float64(1) {
		t.Errorf("first agent = %+v, want sre with inbox_queued 1", all[0])
	}

	web := decodeList(t, getJSON(t, srv.URL+"/v1/agents?project=web"))
	if len(web) != 1 || web[0]["id"] != "triage" {
		t.Errorf("filtered = %+v", web)
	}
}

func TestGetAgentIncludesPrompt(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	body := decodeMap(t, getJSON(t, srv.URL+"/v1/agents/sre"))
	if body["prompt"] != "you are the sre role" {
		t.Errorf("prompt = %v", body["prompt"])
	}

	resp := getJSON(t, srv.URL+"/v1/agents/ghost")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if code := decodeErr(t, resp).Error.Code; code != "agent_not_found" {
		t.Errorf("code = %q, want agent_not_found", code)
	}
}

func TestPatchAgent(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	resp := patchJSON(t, srv.URL+"/v1/agents/sre", map[string]any{
		"prompt":  "updated policy",
		"cron":    "0 * * * *",
		"enabled": false,
		"subscriptions": []map[string]any{
			{"repo": "acme/platform", "labels": []string{"bug"}, "mention_only": true},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeMap(t, resp)
	if body["cron"] != "0 * * * *" || body["enabled"] != false {
		t.Errorf("body = %+v", body)
	}
	subs, ok := body["subscriptions"].([]any)
	if !ok || len(subs) != 1 {
		t.Fatalf("subscriptions = %+v", body["subscriptions"])
	}

	prompt, err := os.ReadFile(roles.PromptPath(d.Cfg.Home, "sre"))
	if err != nil {
		t.Fatalf("read role.md: %v", err)
	}
	if string(prompt) != "updated policy" {
		t.Errorf("role.md = %q, want the patched prompt", prompt)
	}
}

func TestEnableDisableAgent(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	body := decodeMap(t, postJSON(t, srv.URL+"/v1/agents/sre/disable", nil))
	if body["enabled"] != false {
		t.Errorf("enabled = %v, want false", body["enabled"])
	}
	body = decodeMap(t, postJSON(t, srv.URL+"/v1/agents/sre/enable", nil))
	if body["enabled"] != true {
		t.Errorf("enabled = %v, want true", body["enabled"])
	}
}

func TestDeleteAgent(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	resp := deleteReq(t, srv.URL+"/v1/agents/sre")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	if resp := getJSON(t, srv.URL+"/v1/agents/sre"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status after delete = %d, want 404", resp.StatusCode)
	}
}

func TestWakeEnqueuesInboxEvent(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	resp := postJSON(t, srv.URL+"/v1/agents/sre/wake", map[string]any{"text": "blocked by X"})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	body := decodeMap(t, resp)
	if body["kind"] != "message" {
		t.Errorf("kind = %v, want message", body["kind"])
	}
	if body["event_id"] == nil || body["event_id"] == float64(0) {
		t.Errorf("event_id = %v", body["event_id"])
	}

	events := decodeList(t, getJSON(t, srv.URL+"/v1/agents/sre/inbox"))
	if len(events) != 1 {
		t.Fatalf("inbox = %d events, want 1", len(events))
	}
	if events[0]["status"] != "queued" || events[0]["kind"] != "message" {
		t.Errorf("event = %+v", events[0])
	}
	payload, ok := events[0]["payload"].(map[string]any)
	if !ok || payload["text"] != "blocked by X" {
		t.Errorf("payload = %+v", events[0]["payload"])
	}

	queued := decodeList(t, getJSON(t, srv.URL+"/v1/agents/sre/inbox?status=queued"))
	if len(queued) != 1 {
		t.Errorf("queued = %d, want 1", len(queued))
	}
}

func TestWakeRejectsUnknownKind(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	resp := postJSON(t, srv.URL+"/v1/agents/sre/wake", map[string]any{"kind": "explode"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if code := decodeErr(t, resp).Error.Code; code != "invalid_kind" {
		t.Errorf("code = %q, want invalid_kind", code)
	}
}

func TestWakeUnknownAgent(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/agents/ghost/wake", map[string]any{"text": "hi"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestPutAndListAgentItems(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	resp := putJSON(t, srv.URL+"/v1/agents/sre/items", map[string]any{
		"kind": "issue", "ref": "acme/platform#12", "state": "deferred",
		"note": "waiting for migration", "snooze_until": 1800000000, "task_id": 45,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	item := decodeMap(t, resp)
	if item["state"] != "deferred" || item["ref"] != "acme/platform#12" ||
		item["task_id"] != float64(45) || item["snooze_until"] != float64(1800000000) {
		t.Errorf("item = %+v", item)
	}

	if resp := putJSON(t, srv.URL+"/v1/agents/sre/items", map[string]any{
		"kind": "task", "ref": "task:45", "state": "in_work",
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("second put status = %d", resp.StatusCode)
	}

	all := decodeList(t, getJSON(t, srv.URL+"/v1/agents/sre/items"))
	if len(all) != 2 {
		t.Fatalf("items = %d, want 2", len(all))
	}
	deferred := decodeList(t, getJSON(t, srv.URL+"/v1/agents/sre/items?state=deferred"))
	if len(deferred) != 1 || deferred[0]["ref"] != "acme/platform#12" {
		t.Errorf("deferred = %+v", deferred)
	}
}

func TestPutAgentItemValidation(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	cases := []struct {
		name    string
		payload map[string]any
		code    string
	}{
		{"missing ref", map[string]any{"kind": "issue", "state": "taken"}, "bad_request"},
		{"bad kind", map[string]any{"kind": "banana", "ref": "x", "state": "taken"}, "invalid_kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := putJSON(t, srv.URL+"/v1/agents/sre/items", tc.payload)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			if code := decodeErr(t, resp).Error.Code; code != tc.code {
				t.Errorf("code = %q, want %q", code, tc.code)
			}
		})
	}
}

// A role instance may only write its own dossier: the caller session must be
// kind=agent and its id must start with "<role>-run-".
func TestPutAgentItemSessionScope(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	addTestSession(t, d, "sre-run-1", "agent", "platform")
	addTestSession(t, d, "other-run-1", "agent", "platform")
	addTestSession(t, d, "platform-orch", "orchestrator", "platform")

	body := map[string]any{"kind": "issue", "ref": "acme/platform#7", "state": "taken"}

	if resp := putJSONWithHeader(t, srv.URL+"/v1/agents/sre/items", "sre-run-1", body); resp.StatusCode != http.StatusOK {
		t.Fatalf("own instance status = %d, want 200", resp.StatusCode)
	}
	resp := putJSONWithHeader(t, srv.URL+"/v1/agents/sre/items", "other-run-1", body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign role status = %d, want 403", resp.StatusCode)
	}
	if code := decodeErr(t, resp).Error.Code; code != "forbidden" {
		t.Errorf("code = %q, want forbidden", code)
	}
	if resp := putJSONWithHeader(t, srv.URL+"/v1/agents/sre/items", "platform-orch", body); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("orchestrator status = %d, want 403", resp.StatusCode)
	}
}

func TestAgentStoreItemRoundtrip(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	if _, err := d.Store.UpsertAgentItem(store.AgentItem{
		RoleID: "sre", Kind: "ping", ExternalRef: "msg:1", State: "triaged",
	}); err != nil {
		t.Fatalf("UpsertAgentItem: %v", err)
	}
	items := decodeList(t, getJSON(t, srv.URL+"/v1/agents/sre/items"))
	if len(items) != 1 || items[0]["kind"] != "ping" {
		t.Errorf("items = %+v", items)
	}
}

func TestListAgentsCarriesOpenQuestionCounts(t *testing.T) {
	d := agentsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	// Human-opened thread: open, but awaiting the role, not the user.
	if _, err := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", Body: "q1"}); err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	// Role-opened thread: awaits the user.
	if _, err := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", AskedBy: "sre-run-1", Body: "q2"}); err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}

	list := decodeList(t, getJSON(t, srv.URL+"/v1/agents"))
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	if list[0]["open_questions"] != float64(2) || list[0]["awaiting_user"] != float64(1) {
		t.Errorf("list entry = %+v", list[0])
	}

	one := decodeMap(t, getJSON(t, srv.URL+"/v1/agents/sre"))
	if one["open_questions"] != float64(2) || one["awaiting_user"] != float64(1) {
		t.Errorf("get = %+v", one)
	}
}
