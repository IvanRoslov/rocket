package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// agentsTestDeps builds Deps with a real store, a temp home and one project to
// group agents under.
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

// errCode digs the error code out of an {"error":{"code":...}} response body.
func errCode(body map[string]any) string {
	e, ok := body["error"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := e["code"].(string)
	return code
}

// deleteJSON issues a DELETE request against url.
func deleteJSON(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("build DELETE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", url, err)
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
		"id":          id,
		"description": "the " + id + " agent",
		"project":     "platform",
		"dir":         "/tmp/agents/" + id,
		"command":     "claude",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /v1/agents = %d, want 201", resp.StatusCode)
	}
	return decodeMap(t, resp)
}

// addLiveAgentSession registers the session row that marks an agent's tmux
// session as alive, the way the daemon watcher does.
func addLiveAgentSession(t *testing.T, d Deps, id string) {
	t.Helper()
	if err := d.Store.AddSession(store.Session{
		ID: id, Kind: session.AgentSessionKind, ProjectID: "platform",
		FeatureSlug: id, Agent: "claude-code", TmuxName: id, State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
}

func TestPostAgentCreatesRegistration(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	body := createTestAgent(t, srv, "sre")

	if body["id"] != "sre" || body["project"] != "platform" {
		t.Errorf("agent = %+v", body)
	}
	if body["description"] != "the sre agent" || body["dir"] != "/tmp/agents/sre" || body["command"] != "claude" {
		t.Errorf("agent fields = %+v", body)
	}
	if body["enabled"] != true {
		t.Errorf("enabled = %v, want true", body["enabled"])
	}
	if body["session_alive"] != false {
		t.Errorf("session_alive = %v, want false", body["session_alive"])
	}
	if body["unread"] != float64(0) {
		t.Errorf("unread = %v, want 0", body["unread"])
	}
}

func TestPostAgentWithoutProject(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/v1/agents", map[string]any{"id": "solo"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /v1/agents = %d, want 201", resp.StatusCode)
	}
	body := decodeMap(t, resp)
	if body["project"] != "" {
		t.Errorf("project = %v, want empty", body["project"])
	}
}

func TestPostAgentValidation(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	cases := []struct {
		name    string
		payload map[string]any
		status  int
	}{
		{"bad id", map[string]any{"id": "Bad Id"}, http.StatusBadRequest},
		{"unknown project", map[string]any{"id": "sre", "project": "nope"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, srv.URL+"/v1/agents", tc.payload)
			resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.status)
			}
		})
	}

	createTestAgent(t, srv, "sre")
	resp := postJSON(t, srv.URL+"/v1/agents", map[string]any{"id": "sre"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate status = %d, want 409", resp.StatusCode)
	}
}

func TestGetAgentReportsLivenessAndUnread(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")
	if _, err := d.Store.AddInboxMessage(store.InboxMessage{AgentID: "sre", Body: "one"}); err != nil {
		t.Fatalf("AddInboxMessage: %v", err)
	}
	addLiveAgentSession(t, d, "sre")

	body := decodeMap(t, getJSON(t, srv.URL+"/v1/agents/sre"))
	if body["session_alive"] != true {
		t.Errorf("session_alive = %v, want true", body["session_alive"])
	}
	if body["unread"] != float64(1) {
		t.Errorf("unread = %v, want 1", body["unread"])
	}
}

func TestListAgentsFiltersByProject(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")
	resp := postJSON(t, srv.URL+"/v1/agents", map[string]any{"id": "solo"})
	resp.Body.Close()

	all := decodeList(t, getJSON(t, srv.URL+"/v1/agents"))
	if len(all) != 2 {
		t.Fatalf("agents = %d, want 2", len(all))
	}

	platform := decodeList(t, getJSON(t, srv.URL+"/v1/agents?project=platform"))
	if len(platform) != 1 || platform[0]["id"] != "sre" {
		t.Fatalf("platform agents = %+v", platform)
	}
}

func TestPatchAgentUpdatesFields(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")

	resp := patchJSON(t, srv.URL+"/v1/agents/sre", map[string]any{
		"description": "on call",
		"dir":         "/srv/sre",
		"command":     "",
		"enabled":     false,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH = %d, want 200", resp.StatusCode)
	}
	body := decodeMap(t, resp)
	if body["description"] != "on call" || body["dir"] != "/srv/sre" ||
		body["command"] != "" || body["enabled"] != false {
		t.Errorf("patched agent = %+v", body)
	}
}

func TestPatchAgentRejectsUnknownProject(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")
	resp := patchJSON(t, srv.URL+"/v1/agents/sre", map[string]any{"project": "nope"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestEnableDisableAgent(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")

	body := decodeMap(t, postJSON(t, srv.URL+"/v1/agents/sre/disable", nil))
	if body["enabled"] != false {
		t.Errorf("enabled after disable = %v", body["enabled"])
	}
	body = decodeMap(t, postJSON(t, srv.URL+"/v1/agents/sre/enable", nil))
	if body["enabled"] != true {
		t.Errorf("enabled after enable = %v", body["enabled"])
	}
}

func TestDeleteAgent(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")

	resp := deleteJSON(t, srv.URL+"/v1/agents/sre")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE = %d, want 200", resp.StatusCode)
	}
	resp = getJSON(t, srv.URL+"/v1/agents/sre")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET after delete = %d, want 404", resp.StatusCode)
	}
}

func TestUnknownAgentIs404(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	for _, url := range []string{
		srv.URL + "/v1/agents/ghost",
		srv.URL + "/v1/agents/ghost/inbox",
	} {
		resp := getJSON(t, url)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", url, resp.StatusCode)
		}
	}
}

func TestPostAgentMessageInboxesWhenSessionIsDead(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")

	resp := postJSON(t, srv.URL+"/v1/agents/sre/messages", map[string]any{"body": "deploy is stuck"})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST messages = %d, want 202", resp.StatusCode)
	}
	body := decodeMap(t, resp)
	if body["status"] != "inbox" || body["live"] != false {
		t.Errorf("result = %+v, want inbox", body)
	}

	msgs := decodeList(t, getJSON(t, srv.URL+"/v1/agents/sre/inbox"))
	if len(msgs) != 1 || msgs[0]["body"] != "deploy is stuck" || msgs[0]["status"] != "unread" {
		t.Fatalf("inbox = %+v", msgs)
	}
}

func TestPostAgentMessageQueuesWhenSessionIsLive(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")
	addLiveAgentSession(t, d, "sre")

	body := decodeMap(t, postJSON(t, srv.URL+"/v1/agents/sre/messages", map[string]any{"body": "ping"}))
	if body["status"] != "queued" || body["live"] != true {
		t.Errorf("result = %+v, want queued", body)
	}

	msgs, err := d.Store.ListMessages("sre", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Body != "ping" {
		t.Fatalf("messages = %+v", msgs)
	}
	inbox, err := d.Store.ListInboxMessages("sre", "", 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(inbox) != 0 {
		t.Errorf("inbox = %+v, want empty for a live agent", inbox)
	}
}

func TestPostAgentMessageRejectsEmptyBody(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")
	resp := postJSON(t, srv.URL+"/v1/agents/sre/messages", map[string]any{"body": "  "})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAgentInboxNextDrainsOneByOne(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")
	for _, text := range []string{"one", "two"} {
		resp := postJSON(t, srv.URL+"/v1/agents/sre/messages", map[string]any{"body": text})
		resp.Body.Close()
	}

	first := decodeMap(t, postJSON(t, srv.URL+"/v1/agents/sre/inbox/next", nil))
	if first["body"] != "one" || first["status"] != "read" {
		t.Fatalf("first next = %+v", first)
	}

	unread := decodeList(t, getJSON(t, srv.URL+"/v1/agents/sre/inbox?status=unread"))
	if len(unread) != 1 || unread[0]["body"] != "two" {
		t.Fatalf("unread after next = %+v", unread)
	}

	second := decodeMap(t, postJSON(t, srv.URL+"/v1/agents/sre/inbox/next", nil))
	if second["body"] != "two" {
		t.Fatalf("second next = %+v", second)
	}

	resp := postJSON(t, srv.URL+"/v1/agents/sre/inbox/next", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("drained next = %d, want 204", resp.StatusCode)
	}
}

func TestAgentInboxPeekDoesNotMarkRead(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")
	id, err := d.Store.AddInboxMessage(store.InboxMessage{AgentID: "sre", Body: "peek me"})
	if err != nil {
		t.Fatalf("AddInboxMessage: %v", err)
	}

	body := decodeMap(t, getJSON(t, srv.URL+"/v1/agents/sre/inbox/"+strconv.FormatInt(id, 10)))
	if body["body"] != "peek me" || body["status"] != "unread" {
		t.Fatalf("peek = %+v", body)
	}

	n, err := d.Store.CountUnreadInbox("sre")
	if err != nil {
		t.Fatalf("CountUnreadInbox: %v", err)
	}
	if n != 1 {
		t.Errorf("unread after peek = %d, want 1", n)
	}
}

func TestAgentInboxPeekOfAnotherAgentIs404(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")
	createTestAgent(t, srv, "triage")
	id, err := d.Store.AddInboxMessage(store.InboxMessage{AgentID: "sre", Body: "private"})
	if err != nil {
		t.Fatalf("AddInboxMessage: %v", err)
	}

	resp := getJSON(t, srv.URL+"/v1/agents/triage/inbox/"+strconv.FormatInt(id, 10))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAgentInboxRejectsUnknownStatus(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	createTestAgent(t, srv, "sre")
	resp := getJSON(t, srv.URL+"/v1/agents/sre/inbox?status=pending")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestStartAgentWithoutDirIsRejected(t *testing.T) {
	d := agentsTestDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp := postJSON(t, srv.URL+"/v1/agents", map[string]any{"id": "solo"})
	resp.Body.Close()

	resp = postJSON(t, srv.URL+"/v1/agents/solo/start", nil)
	body := decodeMap(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %+v)", resp.StatusCode, body)
	}
	if errCode(body) != "agent_no_dir" {
		t.Errorf("error = %+v, want agent_no_dir", body["error"])
	}
}
