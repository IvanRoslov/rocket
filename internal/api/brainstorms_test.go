package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

type brainstormsListResp struct {
	Brainstorms []brainstormEntry `json:"brainstorms"`
}

func (f gateFixture) askStorm(t *testing.T, taskID int64) questionResponse {
	t.Helper()
	resp := postJSONWithHeader(t, f.url+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{
		"body": "Какую схему?", "type": "brainstorm",
		"options": []string{"A", "B", "C"}, "recommend": 2,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("ask brainstorm = %d, want 201", resp.StatusCode)
	}
	return decodeQuestion(t, resp)
}

// askAgedStorm opens a storm thread on taskID directly in the store, dated age
// ago — the API cannot backdate a question, so ordering tests seed it here.
func (f gateFixture) askAgedStorm(t *testing.T, taskID int64, age time.Duration) {
	t.Helper()
	id, err := f.d.Store.AddQuestion(store.Question{
		TaskID: taskID, AskedBy: "orch-1", Body: "Какую схему?", Type: store.QuestionTypeBrainstorm,
		Options: []string{"A", "B", "C"}, RecommendedOption: 2, AskedAt: time.Now().Add(-age).Unix(),
	})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := f.d.Store.AddParticipants(id, store.ParticipantHuman, "orch-1"); err != nil {
		t.Fatalf("AddParticipants: %v", err)
	}
	if err := f.d.Store.SetAttention(id, []string{store.ParticipantHuman}); err != nil {
		t.Fatalf("SetAttention: %v", err)
	}
}

func (f gateFixture) brainstorms(t *testing.T) []brainstormEntry {
	t.Helper()
	resp := getJSON(t, f.url+"/v1/brainstorms")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/brainstorms = %d, want 200", resp.StatusCode)
	}
	var out brainstormsListResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode brainstorms: %v", err)
	}
	return out.Brainstorms
}

func TestBrainstorms_EmptyIsArray(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	resp := getJSON(t, f.url+"/v1/brainstorms")
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(raw["brainstorms"]) != "[]" {
		t.Fatalf("brainstorms = %s, want []", raw["brainstorms"])
	}
}

func TestBrainstorms_QuestionOnHuman(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.askStorm(t, f.taskID)

	got := f.brainstorms(t)
	if len(got) != 1 {
		t.Fatalf("entries = %+v, want 1", got)
	}
	e := got[0]
	if e.TaskID != f.taskID || e.QuestionsWaiting != 1 || e.PendingGate != nil ||
		e.TaskStatus != "brainstorm" || e.TaskTitle != "T" || e.ProjectID != "proj1" || e.WaitingSince == 0 {
		t.Errorf("entry = %+v", e)
	}
}

func TestBrainstorms_QuestionOnAgentHidden(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	q := f.askStorm(t, f.taskID)

	// The human replies without closing: the turn passes back to orch-1.
	resp := postJSONWithHeader(t, f.url+"/v1/questions/"+itoa(q.ID)+"/reply", "", map[string]any{"body": "а почему B?"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("human reply = %d, want 201", resp.StatusCode)
	}
	// Precondition: the thread is still open and no longer waits on the human.
	threads, err := f.d.Store.ListThreads(false)
	if err != nil || len(threads) != 1 || contains(threads[0].Attention, "human") {
		t.Fatalf("threads/attention = %+v / %v, want one open thread not on the human", threads, err)
	}

	if got := f.brainstorms(t); len(got) != 0 {
		t.Fatalf("entries = %+v, want none", got)
	}
}

func TestBrainstorms_PendingGate(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "spec")
	g := f.request(t, "orch-1")

	got := f.brainstorms(t)
	if len(got) != 1 {
		t.Fatalf("entries = %+v, want 1", got)
	}
	e := got[0]
	if e.QuestionsWaiting != 0 || e.PendingGate == nil || e.PendingGate.SpecVersion != 1 ||
		e.PendingGate.ID != g.ID || e.WaitingSince != e.PendingGate.RequestedAt {
		t.Errorf("entry = %+v gate=%+v", e, e.PendingGate)
	}
}

func TestBrainstorms_GateWithoutThreads(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "spec")
	f.request(t, "orch-1")
	if threads, _ := f.d.Store.ListThreads(false); len(threads) != 0 {
		t.Fatalf("precondition: %d threads, want 0", len(threads))
	}
	if got := f.brainstorms(t); len(got) != 1 || got[0].PendingGate == nil {
		t.Fatalf("entries = %+v, want the gate-only task", got)
	}
}

func TestBrainstorms_InProgressGateShown(t *testing.T) {
	f := newGateFixture(t, "in_progress")
	f.putDoc(t, "spec")
	f.request(t, "orch-1")
	got := f.brainstorms(t)
	if len(got) != 1 || got[0].TaskStatus != "in_progress" {
		t.Fatalf("entries = %+v, want the in_progress task", got)
	}
}

func TestBrainstorms_DecidedOrSupersededGateHidden(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.putDoc(t, "spec")
	g := f.request(t, "orch-1")
	resp := f.decide(t, "", g.ID, "go", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("decide = %d, want 200", resp.StatusCode)
	}
	if err := f.d.Store.UpdateTaskStatus(f.taskID, "brainstorm"); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}
	if got := f.brainstorms(t); len(got) != 0 {
		t.Fatalf("decided gate: entries = %+v, want none", got)
	}

	// A fresh gate, then a new spec version supersedes it.
	f.request(t, "orch-1")
	f.putDoc(t, "spec")
	if got := f.brainstorms(t); len(got) != 0 {
		t.Fatalf("superseded gate: entries = %+v, want none", got)
	}
}

func TestBrainstorms_HiddenStatuses(t *testing.T) {
	for _, status := range []string{"review", "done", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			f := newGateFixture(t, "brainstorm")
			f.putDoc(t, "spec")
			f.request(t, "orch-1")
			f.askStorm(t, f.taskID)
			if got := f.brainstorms(t); len(got) != 1 {
				t.Fatalf("precondition: entries = %+v, want 1", got)
			}
			if err := f.d.Store.UpdateTaskStatus(f.taskID, status); err != nil {
				t.Fatalf("UpdateTaskStatus: %v", err)
			}
			if got := f.brainstorms(t); len(got) != 0 {
				t.Fatalf("entries = %+v, want none", got)
			}
		})
	}
}

func TestBrainstorms_BothOneEntry(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	// The question is older than the gate, so min and max differ.
	f.askAgedStorm(t, f.taskID, 100*time.Second)
	f.putDoc(t, "spec")
	g := f.request(t, "orch-1")

	got := f.brainstorms(t)
	if len(got) != 1 {
		t.Fatalf("entries = %+v, want 1", got)
	}
	e := got[0]
	if e.QuestionsWaiting != 1 || e.PendingGate == nil {
		t.Fatalf("entry = %+v, want both question and gate", e)
	}
	threads, _ := f.d.Store.ListThreads(false)
	asked := threads[0].Question.AskedAt
	if asked >= g.RequestedAt {
		t.Fatalf("precondition: asked %d should predate the gate %d", asked, g.RequestedAt)
	}
	if e.WaitingSince != asked {
		t.Errorf("waiting_since = %d, want the earlier wait (asked %d, gate %d)", e.WaitingSince, asked, g.RequestedAt)
	}
}

func TestBrainstorms_Order(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	f.d.Cfg.QuestionStaleAfter = time.Hour
	second, err := f.d.Store.AddTask(store.Task{Title: "T2", ProjectID: "proj1", SessionID: "orch-1", Status: "brainstorm"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	// The higher-id task's question aged past the threshold; the lower-id
	// task's is fresh.
	f.askAgedStorm(t, second, 2*time.Hour)
	f.askStorm(t, f.taskID)

	got := f.brainstorms(t)
	if len(got) != 2 {
		t.Fatalf("entries = %+v, want 2", got)
	}
	if got[0].TaskID != second || !got[0].QuestionsStale || got[1].QuestionsStale {
		t.Fatalf("order = %+v, want the stale task first", got)
	}
}

func TestSortBrainstorms(t *testing.T) {
	es := []brainstormEntry{
		{TaskID: 4, WaitingSince: 10},
		{TaskID: 3, WaitingSince: 20, QuestionsStale: true},
		{TaskID: 2, WaitingSince: 10},
		{TaskID: 1, WaitingSince: 5},
	}
	sortBrainstorms(es)
	var ids []int64
	for _, e := range es {
		ids = append(ids, e.TaskID)
	}
	want := []int64{3, 1, 2, 4}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("order = %v, want %v", ids, want)
		}
	}
}

func TestBrainstorms_AgentForbidden(t *testing.T) {
	f := newGateFixture(t, "brainstorm")
	req, _ := http.NewRequest(http.MethodGet, f.url+"/v1/brainstorms", nil)
	req.Header.Set(sessionHeader, "orch-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}
