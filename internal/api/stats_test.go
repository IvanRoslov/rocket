package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// statsFixture is a server with one storm: a task with the custom skill and
// one brainstorm question the human answered just now with the recommended
// option and a comment.
type statsFixture struct {
	d      Deps
	url    string
	taskID int64
}

func newStatsFixture(t *testing.T) statsFixture {
	t.Helper()
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	addTestSession(t, d, "worker-1", "worker", "proj1")
	id, err := d.Store.AddTask(store.Task{Title: "Storm", ProjectID: "proj1", SessionID: "orch-1", Status: "brainstorm"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := d.Store.SetTaskBrainstormSkill(id, "orchestrator-brainstorming"); err != nil {
		t.Fatalf("SetTaskBrainstormSkill: %v", err)
	}
	qid, err := d.Store.AddQuestion(store.Question{
		TaskID: id, AskedBy: "orch-1", Body: "Q?",
		Type: store.QuestionTypeBrainstorm, Options: []string{"A", "B"}, RecommendedOption: 1,
	})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if _, err := d.Store.ResolveBrainstormQuestion(qid, store.BrainstormAnswer{
		ChosenOption: 1, Comment: "да", Source: store.AnswerSourceUI,
	}); err != nil {
		t.Fatalf("ResolveBrainstormQuestion: %v", err)
	}
	if _, err := d.Store.AddQuestionMessage(store.QuestionMessage{
		QuestionID: qid, Author: store.ParticipantHuman, Kind: "answer", Body: "A",
	}); err != nil {
		t.Fatalf("AddQuestionMessage: %v", err)
	}
	return statsFixture{d: d, url: srv.URL, taskID: id}
}

func getAs(t *testing.T, url, sessionID string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if sessionID != "" {
		req.Header.Set(sessionHeader, sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func TestGetBrainstormStats(t *testing.T) {
	f := newStatsFixture(t)
	for _, caller := range []string{"", "worker-1"} {
		resp := getAs(t, f.url+"/v1/stats/brainstorm", caller)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("caller %q: status %d", caller, resp.StatusCode)
		}
		var got brainstormStatsResponse
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp.Body.Close()

		wantWeeks := []brainstormWeekResponse{{
			Week: store.ISOWeekLabel(time.Now()), Skill: "orchestrator-brainstorming",
			Answered: 1, Accepted: 1, AcceptedWithComment: 1,
		}}
		if !reflect.DeepEqual(got.Weeks, wantWeeks) {
			t.Errorf("caller %q weeks = %+v, want %+v", caller, got.Weeks, wantWeeks)
		}
		wantStorms := []brainstormStormResponse{{
			TaskID: f.taskID, Title: "Storm", ProjectID: "proj1", Skill: "orchestrator-brainstorming",
			Questions: 1, Answered: 1, Accepted: 1, AcceptedWithComment: 1,
		}}
		if !reflect.DeepEqual(got.Storms, wantStorms) {
			t.Errorf("caller %q storms = %+v, want %+v", caller, got.Storms, wantStorms)
		}
	}
}

func TestGetBrainstormStatsEmptyListsAreArrays(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	resp := getJSON(t, srv.URL+"/v1/stats/brainstorm?weeks=4")
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(raw["weeks"]) != "[]" || string(raw["storms"]) != "[]" {
		t.Errorf("empty stats = weeks %s storms %s, want [] []", raw["weeks"], raw["storms"])
	}
}

func TestGetBrainstormStatsBadWeeks(t *testing.T) {
	f := newStatsFixture(t)
	for _, weeks := range []string{"0", "-1", "x", "521"} {
		resp := getJSON(t, f.url+"/v1/stats/brainstorm?weeks="+weeks)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("weeks=%s: status %d, want 400", weeks, resp.StatusCode)
		}
	}
	resp := getJSON(t, f.url+"/v1/stats/brainstorm?weeks=520")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("weeks=520: status %d, want 200", resp.StatusCode)
	}
}

func TestGetTaskBrainstormStats(t *testing.T) {
	f := newStatsFixture(t)
	resp := getJSON(t, f.url+"/v1/tasks/"+itoa(f.taskID)+"/brainstorm/stats")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if v, ok := raw["go_at"]; !ok || v != nil {
		t.Errorf("go_at = %v (present %v), want null", v, ok)
	}
	if raw["accepted_with_comment"] != float64(1) || raw["skill"] != "orchestrator-brainstorming" {
		t.Errorf("storm = %v", raw)
	}

	quiet, err := f.d.Store.AddTask(store.Task{Title: "Quiet", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	resp = getJSON(t, f.url+"/v1/tasks/"+itoa(quiet)+"/brainstorm/stats")
	var got brainstormStormResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	want := brainstormStormResponse{TaskID: quiet, Title: "Quiet", ProjectID: "proj1", Skill: store.SkillUnknown}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("quiet = %+v, want %+v", got, want)
	}

	resp = getJSON(t, f.url+"/v1/tasks/99999/brainstorm/stats")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown task: status %d, want 404", resp.StatusCode)
	}
}
