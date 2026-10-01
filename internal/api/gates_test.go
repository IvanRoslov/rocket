package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// gateFixture is a brainstorm task owned by a live orchestrator "orch-1",
// plus a persistent agent "cto" and a worker "worker-1" for permission tests.
type gateFixture struct {
	d      Deps
	url    string
	taskID int64
}

func newGateFixture(t *testing.T, status string) gateFixture {
	t.Helper()
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	addTestSession(t, d, "cto", "agent", "proj1")
	addTestSession(t, d, "worker-1", "worker", "proj1")
	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1", SessionID: "orch-1", Status: status})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	return gateFixture{d: d, url: srv.URL, taskID: id}
}

func (f gateFixture) putDoc(t *testing.T, kind string) {
	t.Helper()
	resp := putJSONWithHeader(t, f.url+"/v1/tasks/"+itoa(f.taskID)+"/docs", "orch-1",
		map[string]any{"kind": kind, "title": kind, "body": "b"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put %s doc: status %d", kind, resp.StatusCode)
	}
}

func decodeGate(t *testing.T, resp *http.Response) gateResponse {
	t.Helper()
	var g gateResponse
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		t.Fatalf("decode gate: %v", err)
	}
	return g
}

func (f gateFixture) request(t *testing.T, caller string) gateResponse {
	t.Helper()
	resp := postJSONWithHeader(t, f.url+"/v1/tasks/"+itoa(f.taskID)+"/gates", caller, map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("request gate: status %d", resp.StatusCode)
	}
	return decodeGate(t, resp)
}

func (f gateFixture) decide(t *testing.T, caller string, gateID int64, decision, comment string) *http.Response {
	t.Helper()
	return postJSONWithHeader(t, f.url+"/v1/gates/"+itoa(gateID)+"/decide", caller,
		map[string]any{"decision": decision, "comment": comment})
}

func (f gateFixture) eventTypes(t *testing.T) map[string][]map[string]any {
	t.Helper()
	events, err := f.d.Store.ListEventsTail(100, "")
	if err != nil {
		t.Fatalf("ListEventsTail: %v", err)
	}
	out := map[string][]map[string]any{}
	for _, e := range events {
		out[e.Type] = append(out[e.Type], e.Data)
	}
	return out
}

func (f gateFixture) orchMessages(t *testing.T) []string {
	t.Helper()
	msgs, err := f.d.Store.ListMessages("orch-1", 50)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	var out []string
	for _, m := range msgs {
		if m.ToSession == "orch-1" {
			out = append(out, m.Body)
		}
	}
	return out
}

func TestRequestGateNoSpec(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	resp := postJSONWithHeader(t, f.url+"/v1/tasks/"+itoa(f.taskID)+"/gates", "orch-1", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "no_spec" {
		t.Errorf("code = %q, want no_spec", eb.Error.Code)
	}
}

func TestRequestGateForbiddenForForeignWorker(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "spec")
	resp := postJSONWithHeader(t, f.url+"/v1/tasks/"+itoa(f.taskID)+"/gates", "worker-1", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestRequestGateSnapshotsAndSupersedesPending(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "spec")

	g1 := f.request(t, "orch-1")
	if g1.Status != "pending" || g1.SpecVersion != 1 || g1.PlanVersion != nil || g1.RequestedBy != "orch-1" {
		t.Errorf("g1 = %+v", g1)
	}

	f.putDoc(t, "plan")
	g2 := f.request(t, "") // the human may request too
	if g2.PlanVersion == nil || *g2.PlanVersion != 1 || g2.RequestedBy != "user" {
		t.Errorf("g2 = %+v", g2)
	}

	resp := getJSON(t, f.url+"/v1/tasks/"+itoa(f.taskID)+"/gates")
	defer resp.Body.Close()
	var list struct {
		Gates []gateResponse `json:"gates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Gates) != 2 || list.Gates[0].ID != g2.ID || list.Gates[1].Status != "superseded" {
		t.Fatalf("gates = %+v, want g2 then superseded g1", list.Gates)
	}

	ev := f.eventTypes(t)
	if len(ev["task.gate_requested"]) != 2 {
		t.Errorf("gate_requested events = %v, want 2", ev["task.gate_requested"])
	}
	if s := ev["task.gate_superseded"]; len(s) != 1 || s[0]["status"] != "superseded" {
		t.Errorf("gate_superseded events = %v", s)
	}
}

func TestNewSpecSupersedesPendingGate(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "spec")
	g := f.request(t, "orch-1")

	f.putDoc(t, "plan") // a plan alone does not invalidate the gate
	if got, _ := f.d.Store.GetTaskGate(g.ID); got.Status != "pending" {
		t.Fatalf("after plan put: status = %q, want pending", got.Status)
	}

	f.putDoc(t, "spec")
	if got, _ := f.d.Store.GetTaskGate(g.ID); got.Status != "superseded" {
		t.Fatalf("after spec put: status = %q, want superseded", got.Status)
	}
	if len(f.eventTypes(t)["task.gate_superseded"]) != 1 {
		t.Errorf("want one task.gate_superseded event")
	}

	// Go on the stale gate is refused and the task does not move.
	resp := f.decide(t, "", g.ID, "go", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("decide stale: status = %d, want 409", resp.StatusCode)
	}
	if task, _ := f.d.Store.GetTask(f.taskID); task.Status != "brainstorm" {
		t.Errorf("task status = %q, want brainstorm", task.Status)
	}
}

func TestDocPutPublishesEventAndAcceptsProblem(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "problem")
	f.putDoc(t, "problem")

	ev := f.eventTypes(t)["task.doc_put"]
	if len(ev) != 2 {
		t.Fatalf("doc_put events = %v, want 2", ev)
	}
	last := ev[0]
	if last["kind"] != "problem" || last["task_id"] != float64(f.taskID) {
		t.Errorf("doc_put = %v", last)
	}
	versions := map[float64]bool{}
	for _, e := range ev {
		versions[e["version"].(float64)] = true
	}
	if !versions[1] || !versions[2] {
		t.Errorf("doc_put versions = %v, want 1 and 2", versions)
	}
}

func TestDecideGatePermissions(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "spec")
	g := f.request(t, "orch-1")

	for _, caller := range []string{"orch-1", "cto", "worker-1"} {
		resp := f.decide(t, caller, g.ID, "go", "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", caller, resp.StatusCode)
		}
	}
	if got, _ := f.d.Store.GetTaskGate(g.ID); got.Status != "pending" {
		t.Errorf("gate status = %q after refused decisions, want pending", got.Status)
	}
}

func TestDecideGateValidation(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "spec")
	g := f.request(t, "orch-1")

	resp := f.decide(t, "", g.ID, "changes", "   ")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("changes without comment: status = %d, want 400", resp.StatusCode)
	} else if eb := decodeErr(t, resp); eb.Error.Code != "comment_required" {
		t.Errorf("code = %q, want comment_required", eb.Error.Code)
	}
	resp.Body.Close()

	resp = f.decide(t, "", g.ID, "maybe", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad decision: status = %d, want 400", resp.StatusCode)
	}

	resp = f.decide(t, "", 9999, "go", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown gate: status = %d, want 404", resp.StatusCode)
	}
}

func TestDecideGateGoMovesBrainstormAndDelivers(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "spec")
	f.putDoc(t, "plan")
	f.putDoc(t, "spec")
	g := f.request(t, "orch-1")

	resp := f.decide(t, "", g.ID, "go", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeGate(t, resp)
	if got.Status != "go" || got.DecidedBy != "user" || got.DecidedAt == nil {
		t.Errorf("gate = %+v", got)
	}

	task, _ := f.d.Store.GetTask(f.taskID)
	if task.Status != "in_progress" {
		t.Errorf("task status = %q, want in_progress", task.Status)
	}
	logs, _ := f.d.Store.ListTaskLog(f.taskID, "status")
	if len(logs) != 1 || logs[0].Body != "status: brainstorm → in_progress (by user)" {
		t.Errorf("status log = %+v", logs)
	}

	msgs := f.orchMessages(t)
	want := "[rocket gate] Go по спеке v2 (план v1) — начинай реализацию."
	if len(msgs) != 1 || msgs[0] != want {
		t.Errorf("messages = %q, want [%q]", msgs, want)
	}

	ev := f.eventTypes(t)
	if d := ev["task.gate_decided"]; len(d) != 1 || d[0]["status"] != "go" || d[0]["gate_id"] != float64(g.ID) {
		t.Errorf("gate_decided events = %v", d)
	}
	if len(ev["task.status_changed"]) != 1 {
		t.Errorf("want one task.status_changed event, got %v", ev["task.status_changed"])
	}

	// Already decided → 409.
	resp2 := f.decide(t, "", g.ID, "go", "")
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Errorf("second decide: status = %d, want 409", resp2.StatusCode)
	}
}

func TestDecideGateGoOnNonBrainstormLeavesStatus(t *testing.T) {
	f := newGateFixture(t, "in_progress")
	f.putDoc(t, "spec")
	g := f.request(t, "orch-1")

	resp := f.decide(t, "", g.ID, "go", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	task, _ := f.d.Store.GetTask(f.taskID)
	if task.Status != "in_progress" {
		t.Errorf("task status = %q, want in_progress", task.Status)
	}
	if logs, _ := f.d.Store.ListTaskLog(f.taskID, "status"); len(logs) != 0 {
		t.Errorf("status log = %+v, want none", logs)
	}
	notes, _ := f.d.Store.ListTaskLog(f.taskID, "note")
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "in_progress") {
		t.Errorf("note log = %+v, want one note mentioning the status", notes)
	}
	want := "[rocket gate] Go по спеке v1 (план —) — начинай реализацию."
	if msgs := f.orchMessages(t); len(msgs) != 1 || msgs[0] != want {
		t.Errorf("messages = %q, want [%q]", msgs, want)
	}
}

func TestDecideGateChangesDelivers(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "spec")
	g := f.request(t, "orch-1")

	resp := f.decide(t, "", g.ID, "changes", "добавь раздел про ошибки")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if task, _ := f.d.Store.GetTask(f.taskID); task.Status != "brainstorm" {
		t.Errorf("task status = %q, want brainstorm", task.Status)
	}
	want := "[rocket gate] Нужны правки по спеке v1: добавь раздел про ошибки"
	if msgs := f.orchMessages(t); len(msgs) != 1 || msgs[0] != want {
		t.Errorf("messages = %q, want [%q]", msgs, want)
	}
}

func TestDecideGateWithoutLiveOrchestrator(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "spec")
	g := f.request(t, "orch-1")
	if err := f.d.Store.UpdateSessionState("orch-1", "killed"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	resp := f.decide(t, "", g.ID, "go", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if task, _ := f.d.Store.GetTask(f.taskID); task.Status != "in_progress" {
		t.Errorf("task status = %q, want in_progress", task.Status)
	}
}
