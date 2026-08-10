package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/queue"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// msgFakeRuntime is a minimal runtime.Runtime whose Inject always succeeds,
// for driving the real queue.Queue end to end in these tests without a real
// tmux/agent.
type msgFakeRuntime struct{}

func (msgFakeRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	return runtime.Handle{Name: spec.Name}, nil
}
func (msgFakeRuntime) Inject(ctx context.Context, h runtime.Handle, text string, opts runtime.InjectOpts) error {
	return nil
}
func (msgFakeRuntime) SendKeys(ctx context.Context, h runtime.Handle, key string, literal bool) error {
	return nil
}
func (msgFakeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	return "", nil
}
func (msgFakeRuntime) Alive(ctx context.Context, h runtime.Handle) bool { return true }
func (msgFakeRuntime) PinWindowSize(ctx context.Context, h runtime.Handle, clientCols, clientRows int) error {
	return nil
}

func (msgFakeRuntime) UnpinWindowSize(ctx context.Context, h runtime.Handle) error { return nil }

func (msgFakeRuntime) Destroy(ctx context.Context, h runtime.Handle) error { return nil }
func (msgFakeRuntime) AttachCommand(h runtime.Handle) []string             { return nil }
func (msgFakeRuntime) List(ctx context.Context) ([]string, error)          { return nil, nil }

// messagesTestDeps builds Deps with a real store/bus on a temp SQLite db and
// a real queue.Queue wired to a fake Runtime whose Inject always succeeds
// and whose recipients are always reported "ready", for tests that exercise
// the /v1/messages endpoints end to end (including delivery).
func messagesTestDeps(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	b := bus.New(st)
	cfg := &config.Config{Home: dir, QueueTimeout: time.Hour}

	alwaysReady := func(sessionID string) (activity.State, bool) { return activity.Ready, true }
	q := queue.New(st, b, msgFakeRuntime{}, cfg, alwaysReady)

	d := testDeps(t, nil)
	d.Store = st
	d.Bus = b
	d.Cfg = cfg
	d.Queue = q
	return d
}

// addMsgTestSession inserts a session row with the given id and state.
func addMsgTestSession(t *testing.T, d Deps, id, state string) {
	t.Helper()
	if err := d.Store.AddSession(store.Session{
		ID: id, Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "claude-code",
		Branch: "main", WorktreePath: "/tmp/" + id, TmuxName: id, State: state,
	}); err != nil {
		t.Fatalf("AddSession(%s): %v", id, err)
	}
}

func waitForMsgStatus(t *testing.T, d Deps, id int64, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m, err := d.Store.GetMessage(id)
		if err != nil {
			t.Fatalf("GetMessage(%d): %v", id, err)
		}
		if m.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("message %d did not reach status %q within timeout", id, want)
}

func TestPostMessage_HappyPathDeliveredAndEventPublished(t *testing.T) {
	d := messagesTestDeps(t)
	srv := newTestServer(t, d)

	addMsgTestSession(t, d, "recv", "running")

	ch, cancel := d.Bus.Subscribe()
	defer cancel()

	resp := postJSON(t, srv.URL+"/v1/messages", map[string]any{
		"to": "recv", "body": "hello there",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	var body struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "queued" {
		t.Errorf("status = %q, want queued", body.Status)
	}
	if body.ID == 0 {
		t.Errorf("id = 0, want nonzero")
	}

	// message.queued event published with session_id = to.
	var gotQueuedEvent bool
	deadline := time.Now().Add(2 * time.Second)
	for !gotQueuedEvent && time.Now().Before(deadline) {
		select {
		case e := <-ch:
			if e.Type == "message.queued" && e.SessionID == "recv" {
				gotQueuedEvent = true
				// Data comes straight from the in-process bus fan-out (not a
				// JSON round trip), so the id retains its original int64 type.
				if gotID, ok := e.Data["id"].(int64); !ok || gotID != body.ID {
					t.Errorf("event id = %#v (ok=%v), want %d", e.Data["id"], ok, body.ID)
				}
				if e.Data["to"] != "recv" {
					t.Errorf("event to = %v, want recv", e.Data["to"])
				}
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !gotQueuedEvent {
		t.Fatal("did not observe message.queued event")
	}

	// Queue.Wake was called: the fake runtime always succeeds, and the
	// recipient always reports Ready, so the message should be delivered
	// shortly.
	waitForMsgStatus(t, d, body.ID, "delivered")
}

func TestPostMessage_EmptyBody(t *testing.T) {
	d := messagesTestDeps(t)
	srv := newTestServer(t, d)

	addMsgTestSession(t, d, "recv", "running")

	resp := postJSON(t, srv.URL+"/v1/messages", map[string]any{"to": "recv", "body": ""})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "empty_body" {
		t.Errorf("code = %q, want empty_body", eb.Error.Code)
	}
}

func TestPostMessage_UnknownTo(t *testing.T) {
	d := messagesTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/messages", map[string]any{"to": "nope", "body": "hi"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "session_not_found" {
		t.Errorf("code = %q, want session_not_found", eb.Error.Code)
	}
}

func TestPostMessage_ToTerminal(t *testing.T) {
	d := messagesTestDeps(t)
	srv := newTestServer(t, d)

	addMsgTestSession(t, d, "recv", "killed")

	resp := postJSON(t, srv.URL+"/v1/messages", map[string]any{"to": "recv", "body": "hi"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "recipient_terminal" {
		t.Errorf("code = %q, want recipient_terminal", eb.Error.Code)
	}
}

func TestPostMessage_UnknownFrom(t *testing.T) {
	d := messagesTestDeps(t)
	srv := newTestServer(t, d)

	addMsgTestSession(t, d, "recv", "running")

	resp := postJSON(t, srv.URL+"/v1/messages", map[string]any{"to": "recv", "from": "ghost", "body": "hi"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "from_unknown" {
		t.Errorf("code = %q, want from_unknown", eb.Error.Code)
	}
}

func TestPostMessage_NilQueueStillSucceeds(t *testing.T) {
	d := messagesTestDeps(t)
	d.Queue = nil
	srv := newTestServer(t, d)

	addMsgTestSession(t, d, "recv", "running")

	resp := postJSON(t, srv.URL+"/v1/messages", map[string]any{"to": "recv", "body": "hi"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
}

func TestGetMessages_HistoryBothDirectionsAndLimit(t *testing.T) {
	d := messagesTestDeps(t)
	srv := newTestServer(t, d)

	addMsgTestSession(t, d, "a", "running")
	addMsgTestSession(t, d, "b", "running")
	addMsgTestSession(t, d, "c", "running")

	if _, err := d.Store.AddMessage(store.Message{FromSession: "a", ToSession: "b", Body: "m1"}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if _, err := d.Store.AddMessage(store.Message{FromSession: "b", ToSession: "a", Body: "m2"}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if _, err := d.Store.AddMessage(store.Message{FromSession: "c", ToSession: "b", Body: "m3"}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if _, err := d.Store.AddMessage(store.Message{FromSession: "a", ToSession: "b", Body: "m4"}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/messages?session=a")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Messages []messageResponse `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Messages) != 3 {
		t.Fatalf("len = %d, want 3", len(body.Messages))
	}
	wantBodies := []string{"m1", "m2", "m4"}
	for i, want := range wantBodies {
		if body.Messages[i].Body != want {
			t.Errorf("messages[%d].Body = %q, want %q", i, body.Messages[i].Body, want)
		}
	}

	// limit applies to the most recent N, still ascending.
	limResp, err := http.Get(srv.URL + "/v1/messages?session=a&limit=2")
	if err != nil {
		t.Fatalf("GET limit: %v", err)
	}
	defer limResp.Body.Close()
	var limBody struct {
		Messages []messageResponse `json:"messages"`
	}
	if err := json.NewDecoder(limResp.Body).Decode(&limBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(limBody.Messages) != 2 {
		t.Fatalf("len = %d, want 2", len(limBody.Messages))
	}
	if limBody.Messages[0].Body != "m2" || limBody.Messages[1].Body != "m4" {
		t.Errorf("limited bodies = %q,%q, want m2,m4", limBody.Messages[0].Body, limBody.Messages[1].Body)
	}
}

func TestGetMessages_MissingSession(t *testing.T) {
	d := messagesTestDeps(t)
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/messages")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "missing_session" {
		t.Errorf("code = %q, want missing_session", eb.Error.Code)
	}
}

func TestGetMessageByID(t *testing.T) {
	d := messagesTestDeps(t)
	srv := newTestServer(t, d)

	addMsgTestSession(t, d, "b", "running")
	id, err := d.Store.AddMessage(store.Message{FromSession: "a", ToSession: "b", Body: "hi"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/messages/" + strconv.FormatInt(id, 10))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var m messageResponse
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.Body != "hi" || m.From != "a" || m.To != "b" {
		t.Errorf("message = %+v, want body=hi from=a to=b", m)
	}
}

func TestGetMessageByID_NotFound(t *testing.T) {
	d := messagesTestDeps(t)
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/messages/999")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "message_not_found" {
		t.Errorf("code = %q, want message_not_found", eb.Error.Code)
	}
}

// seedMessagesAgent registers project "platform" and agent "sre" so messages
// can be addressed to the agent by name.
func seedMessagesAgent(t *testing.T, d Deps) {
	t.Helper()
	if err := d.Store.AddRepo(store.Repo{ID: "platform-repo", Path: "/tmp/p"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := d.Store.AddProject(store.Project{ID: "platform", Name: "Platform", MainRepo: "platform-repo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := d.Store.AddAgent(store.Agent{ID: "sre", ProjectID: "platform", Enabled: true}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
}

func TestPostMessage_ToAgentWithoutSessionInboxesIt(t *testing.T) {
	d := messagesTestDeps(t)
	seedMessagesAgent(t, d)
	addMsgTestSession(t, d, "feat-orch", "running")

	srv := newTestServer(t, d)
	resp := postJSON(t, srv.URL+"/v1/messages", map[string]any{
		"from": "feat-orch", "to": "sre", "body": "blocked by X",
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	body := decodeMap(t, resp)
	if body["to"] != "sre" || body["status"] != "inbox" || body["live"] != false {
		t.Fatalf("response = %v, want the agent inbox shape", body)
	}

	msgs, err := d.Store.ListInboxMessages("sre", "", 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Body != "blocked by X" || msgs[0].From != "feat-orch" {
		t.Fatalf("inbox = %+v, want one message from feat-orch", msgs)
	}
}

func TestPostMessage_ToAgentWithLiveSessionQueuesIt(t *testing.T) {
	d := messagesTestDeps(t)
	seedMessagesAgent(t, d)
	addMsgTestSession(t, d, "feat-orch", "running")
	if err := d.Store.AddSession(store.Session{
		ID: "sre", Kind: "agent", ProjectID: "platform", FeatureSlug: "sre",
		Agent: "claude-code", TmuxName: "sre", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	srv := newTestServer(t, d)
	body := decodeMap(t, postJSON(t, srv.URL+"/v1/messages", map[string]any{
		"from": "feat-orch", "to": "sre", "body": "ping",
	}))
	if body["status"] != "queued" || body["live"] != true {
		t.Fatalf("response = %v, want queued", body)
	}

	msgs, err := d.Store.ListMessages("sre", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].FromSession != "feat-orch" || msgs[0].Body != "ping" {
		t.Fatalf("messages = %+v", msgs)
	}
}

// TestPostMessage_ToAgentWithDeadSessionInboxesIt guards the case a plain
// session recipient would fail on: a retired agent session row must send the
// message to the inbox rather than fail it as "recipient_terminal".
func TestPostMessage_ToAgentWithDeadSessionInboxesIt(t *testing.T) {
	d := messagesTestDeps(t)
	seedMessagesAgent(t, d)
	if err := d.Store.AddSession(store.Session{
		ID: "sre", Kind: "agent", ProjectID: "platform", FeatureSlug: "sre",
		Agent: "claude-code", TmuxName: "sre", State: "done",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	srv := newTestServer(t, d)
	resp := postJSON(t, srv.URL+"/v1/messages", map[string]any{"to": "sre", "body": "later"})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	if body := decodeMap(t, resp); body["status"] != "inbox" {
		t.Fatalf("response = %v, want inbox", body)
	}
}

func TestPostMessage_UnknownRecipientStill404(t *testing.T) {
	d := messagesTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/messages", map[string]any{"to": "nobody", "body": "hi"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

// CaptureEscaped serves the same pane as Capture: these fakes carry no
// escape sequences, which is what the draft guard treats conservatively.
func (r msgFakeRuntime) CaptureEscaped(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	return r.Capture(ctx, h, lines)
}
