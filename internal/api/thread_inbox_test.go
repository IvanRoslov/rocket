package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// getThreads GETs the unified inbox as the given caller ("" = the human).
func getThreads(t *testing.T, srv *httptest.Server, query, sessionID string) []threadInboxEntry {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/threads"+query, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if sessionID != "" {
		req.Header.Set(sessionHeader, sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET threads: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Threads []threadInboxEntry `json:"threads"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode threads: %v", err)
	}
	return body.Threads
}

// refsOf lists the local refs of an inbox listing, the field every assertion
// below is really about.
func refsOf(entries []threadInboxEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.LocalRef
	}
	return out
}

// setupInbox builds one open task thread, one open role thread and one
// resolved task thread, and returns the server and the task id.
func setupInbox(t *testing.T, d Deps) (*httptest.Server, int64) {
	t.Helper()
	taskID := setupQuestionTask(t, d)
	addTestProject(t, d, "platform") // the project every test role belongs to
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	open, err := d.Store.AddQuestion(store.Question{
		TaskID: taskID, AskedBy: "orch-1", Body: "деплоим сегодня?",
		Options: []string{"да", "нет"},
	})
	if err != nil {
		t.Fatalf("AddQuestion open: %v", err)
	}
	if err := d.Store.AddParticipants(open, store.ParticipantHuman, "orch-1"); err != nil {
		t.Fatalf("AddParticipants open: %v", err)
	}
	if err := d.Store.SetAttention(open, []string{store.ParticipantHuman}); err != nil {
		t.Fatalf("SetAttention open: %v", err)
	}

	role, err := d.Store.AddQuestion(store.Question{RoleID: "sre", Body: "нужен доступ"})
	if err != nil {
		t.Fatalf("AddQuestion role: %v", err)
	}
	if err := d.Store.AddParticipants(role, store.ParticipantHuman, "sre"); err != nil {
		t.Fatalf("AddParticipants role: %v", err)
	}
	if err := d.Store.SetAttention(role, []string{"sre"}); err != nil {
		t.Fatalf("SetAttention role: %v", err)
	}

	closed, err := d.Store.AddQuestion(store.Question{
		TaskID: taskID, AskedBy: "orch-1", Body: "выкатили", Type: store.QuestionTypeFYI,
	})
	if err != nil {
		t.Fatalf("AddQuestion closed: %v", err)
	}
	if err := d.Store.AddParticipants(closed, store.ParticipantHuman, "orch-1"); err != nil {
		t.Fatalf("AddParticipants closed: %v", err)
	}
	if err := d.Store.ResolveQuestion(closed, store.QuestionResolutionFYI); err != nil {
		t.Fatalf("ResolveQuestion closed: %v", err)
	}
	return srv, taskID
}

// TestGetThreads_UnifiesTaskAndRoleThreads: the whole point of the endpoint —
// one list covering both kinds of thread, each labelled by its local ref, so
// the CLI stops walking tasks in a loop.
func TestGetThreads_UnifiesTaskAndRoleThreads(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := setupInbox(t, d)

	got := getThreads(t, srv, "", "")
	if want := []string{itoa(taskID) + "/Q1", "sre/Q1"}; !reflect.DeepEqual(refsOf(got), want) {
		t.Fatalf("refs = %v, want %v", refsOf(got), want)
	}

	task := got[0]
	if task.Kind != "task" || task.TaskID != taskID || task.Subject != "task #"+itoa(taskID)+" \"Root\"" {
		t.Errorf("task entry = %+v", task)
	}
	if !reflect.DeepEqual(task.Options, []string{"да", "нет"}) {
		t.Errorf("options = %v, want [да нет]", task.Options)
	}
	if !reflect.DeepEqual(task.Attention, []string{store.ParticipantHuman}) {
		t.Errorf("attention = %v, want [human]", task.Attention)
	}
	if !task.YourTurn {
		t.Errorf("your_turn = false for the human on a thread awaiting the human")
	}
	if task.Type != store.QuestionTypeDecision || task.Status != "open" {
		t.Errorf("type/status = %q/%q, want decision/open", task.Type, task.Status)
	}

	role := got[1]
	if role.Kind != "role" || role.RoleID != "sre" || role.Subject != "role sre" {
		t.Errorf("role entry = %+v", role)
	}
	if role.YourTurn {
		t.Errorf("your_turn = true for the human on a thread awaiting sre")
	}
}

// TestGetThreads_AllIncludesResolved: the default listing is the open queue;
// --all is what surfaces fyi notes and answered threads.
func TestGetThreads_AllIncludesResolved(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := setupInbox(t, d)

	got := getThreads(t, srv, "?all=true", "")
	// Ordered by question id, which is creation order: open, role, fyi.
	want := []string{itoa(taskID) + "/Q1", "sre/Q1", itoa(taskID) + "/Q2"}
	if !reflect.DeepEqual(refsOf(got), want) {
		t.Fatalf("refs = %v, want %v", refsOf(got), want)
	}
	fyi := got[2]
	if fyi.Status != "resolved" || fyi.Type != store.QuestionTypeFYI ||
		fyi.Resolution != store.QuestionResolutionFYI {
		t.Errorf("fyi entry = %+v", fyi)
	}
	if len(fyi.Attention) != 0 {
		t.Errorf("attention = %v, want empty on a resolved thread", fyi.Attention)
	}
}

// TestGetThreads_WaitingOnFiltersByAttention: "what is waiting on me" is the
// question the inbox exists to answer, and it is answered from the stored
// attention set, not from the last message.
func TestGetThreads_WaitingOnFiltersByAttention(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := setupInbox(t, d)

	human := getThreads(t, srv, "?waiting_on=human", "")
	if want := []string{itoa(taskID) + "/Q1"}; !reflect.DeepEqual(refsOf(human), want) {
		t.Fatalf("waiting_on=human refs = %v, want %v", refsOf(human), want)
	}

	sre := getThreads(t, srv, "?waiting_on=sre", "")
	if want := []string{"sre/Q1"}; !reflect.DeepEqual(refsOf(sre), want) {
		t.Fatalf("waiting_on=sre refs = %v, want %v", refsOf(sre), want)
	}

	// A resolved thread waits on nobody, so --all plus a filter still yields
	// only threads with somebody in attention.
	both := getThreads(t, srv, "?all=true&waiting_on=human", "")
	if want := []string{itoa(taskID) + "/Q1"}; !reflect.DeepEqual(refsOf(both), want) {
		t.Fatalf("all+waiting_on refs = %v, want %v", refsOf(both), want)
	}
}

// TestGetThreads_StaleFlag: the inbox is the one screen that shows every
// thread at once, so "hanging for over a day" has to be readable there rather
// than only after opening each thread. The threshold is configurable
// (question_stale_after), so a client cannot recompute it — the daemon says it.
func TestGetThreads_StaleFlag(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := setupInbox(t, d)
	seedAgedThread(t, d, taskID, 30*time.Hour)
	seedAgedThread(t, d, taskID, time.Hour)

	got := getThreads(t, srv, "", "")
	byRef := map[string]threadInboxEntry{}
	for _, e := range got {
		byRef[e.LocalRef] = e
	}
	// setupInbox creates Q1 (open) and Q2 (fyi); the aged threads are Q3, Q4.
	if !byRef[itoa(taskID)+"/Q3"].Stale {
		t.Error("a thread idle for 30h with cto on the hook must be stale in the inbox")
	}
	if byRef[itoa(taskID)+"/Q4"].Stale {
		t.Error("a thread that moved an hour ago must not be stale")
	}
}

// TestGetThreads_TaskContext: the dashboard links an inbox row to the task
// page (/p/<project>/tasks/<id>) and labels it with the task title. Subject is
// a human-readable sentence, so those two travel as their own fields rather
// than being parsed back out of it. A role thread hangs off no project and
// carries neither.
func TestGetThreads_TaskContext(t *testing.T) {
	d := questionsTestDeps(t)
	srv, _ := setupInbox(t, d)

	got := getThreads(t, srv, "", "")
	task, role := got[0], got[1]
	if task.ProjectID != "proj1" || task.TaskTitle != "Root" {
		t.Errorf("task entry project/title = %q/%q, want proj1/Root", task.ProjectID, task.TaskTitle)
	}
	if role.ProjectID != "" || role.TaskTitle != "" {
		t.Errorf("role entry project/title = %q/%q, want both empty", role.ProjectID, role.TaskTitle)
	}
}

// TestGetThreads_HidesThreadsTheCallerMayNotRead: the inbox reuses the read
// permission of a single thread, so an unrelated session cannot enumerate
// other people's questions through it.
func TestGetThreads_HidesThreadsTheCallerMayNotRead(t *testing.T) {
	d := questionsTestDeps(t)
	srv, _ := setupInbox(t, d)
	addTestSession(t, d, "orch-2", "orchestrator", "proj1")

	if got := getThreads(t, srv, "", "orch-2"); len(got) != 0 {
		t.Fatalf("threads = %v, want none for an unrelated orchestrator", refsOf(got))
	}
	if got := getThreads(t, srv, "", "orch-1"); len(refsOf(got)) == 0 {
		t.Fatalf("threads = %v, want its own task's thread for orch-1", refsOf(got))
	}
}

// TestGetThreads_EmptySlicesMarshalAsArrays: a nil Go slice marshals to JSON
// `null`, and the dashboard crashed on exactly that — /questions did
// `entry.attention.filter(...)` on a thread whose attention set is empty
// (a resolved thread, or an open one nobody is waiting on). The wire contract
// is "always an array, possibly empty", so this asserts on the RAW JSON:
// decoding into []string would turn `null` back into an empty slice and hide
// the bug the way the typed helper above does.
func TestGetThreads_EmptySlicesMarshalAsArrays(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := setupInbox(t, d)

	// A thread with nobody in it: no participants, no attention. Nothing else
	// in the fixtures exercises the empty participants list.
	bare, err := d.Store.AddQuestion(store.Question{TaskID: taskID, AskedBy: "orch-1", Body: "без участников"})
	if err != nil {
		t.Fatalf("AddQuestion bare: %v", err)
	}
	if err := d.Store.SetAttention(bare, nil); err != nil {
		t.Fatalf("SetAttention bare: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/threads?all=true")
	if err != nil {
		t.Fatalf("GET threads: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Threads []map[string]json.RawMessage `json:"threads"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode threads: %v", err)
	}
	if len(body.Threads) == 0 {
		t.Fatalf("no threads returned")
	}
	for i, entry := range body.Threads {
		for _, field := range []string{"attention", "waiting_on", "participants"} {
			raw, ok := entry[field]
			if !ok {
				t.Errorf("thread[%d]: field %q missing; it has no omitempty and must always be present", i, field)
				continue
			}
			if string(raw) == "null" {
				t.Errorf("thread[%d].%s = null, want [] — a client does .filter() on it", i, field)
			}
		}
	}
}
