package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// waitingTaskDeps builds deps with a project and returns them.
func waitingTaskDeps(t *testing.T) Deps {
	t.Helper()
	d := tasksTestDeps(t)
	addTestProject(t, d, "proj1")
	return d
}

// addWaitingTask creates a task owned by a live session that has been sitting
// on waiting_input for the given duration, and returns the task id.
func addWaitingTask(t *testing.T, d Deps, sessID, title string, waitingFor time.Duration) int64 {
	t.Helper()
	addTestSession(t, d, sessID, "orchestrator", "proj1")
	if err := d.Store.UpdateSessionActivity(sessID, string(activity.WaitingInput),
		time.Now().Add(-waitingFor).Unix()); err != nil {
		t.Fatalf("UpdateSessionActivity %s: %v", sessID, err)
	}
	id, err := d.Store.AddTask(store.Task{Title: title, ProjectID: "proj1", SessionID: sessID})
	if err != nil {
		t.Fatalf("AddTask %s: %v", title, err)
	}
	return id
}

// waitingRow is the slice of a task response this feature adds.
type waitingRow struct {
	ID              int64 `json:"id"`
	WaitingTerminal bool  `json:"waiting_terminal"`
}

func listWaitingRows(t *testing.T, srv *httptest.Server, query string) map[int64]bool {
	t.Helper()
	resp := getJSON(t, srv.URL+"/v1/tasks"+query)
	defer resp.Body.Close()
	var body struct {
		Tasks []waitingRow `json:"tasks"`
		Board struct {
			Backlog []waitingRow `json:"backlog"`
		} `json:"board"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	out := map[int64]bool{}
	for _, r := range append(body.Tasks, body.Board.Backlog...) {
		out[r.ID] = r.WaitingTerminal
	}
	return out
}

// TestListTasks_WaitingTerminal: a task whose session has been stalled on
// interactive input longer than the configured threshold is flagged; one that
// only just started waiting is not.
func TestListTasks_WaitingTerminal(t *testing.T) {
	d := waitingTaskDeps(t)
	srv := newTestServer(t, d)

	stalled := addWaitingTask(t, d, "orch-stalled", "Stalled", 20*time.Minute)
	fresh := addWaitingTask(t, d, "orch-fresh", "Fresh", time.Minute)

	rows := listWaitingRows(t, srv, "")
	if !rows[stalled] {
		t.Errorf("waiting_terminal = false for the stalled task, want true")
	}
	if rows[fresh] {
		t.Errorf("waiting_terminal = true for the freshly waiting task, want false")
	}
}

// TestListTasks_WaitingTerminalBoard: the board grouping carries the same flag.
func TestListTasks_WaitingTerminalBoard(t *testing.T) {
	d := waitingTaskDeps(t)
	srv := newTestServer(t, d)

	stalled := addWaitingTask(t, d, "orch-stalled", "Stalled", 20*time.Minute)

	rows := listWaitingRows(t, srv, "?board=true")
	if !rows[stalled] {
		t.Errorf("waiting_terminal = false on the board, want true")
	}
}

// TestListTasks_WaitingTerminalWithoutSession: a task with no session is
// never flagged. (A task pointing at a session that no longer exists isn't a
// reachable state: tasks.session_id is a foreign key into sessions, and the
// store has no DeleteSession — dead sessions merely change state, which
// TestListTasks_WaitingTerminalIgnoresDeadSession covers.)
func TestListTasks_WaitingTerminalWithoutSession(t *testing.T) {
	d := waitingTaskDeps(t)
	srv := newTestServer(t, d)

	id, err := d.Store.AddTask(store.Task{Title: "Sessionless", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if listWaitingRows(t, srv, "")[id] {
		t.Errorf("waiting_terminal = true for a task with no session, want false")
	}
}

// TestGetTask_WaitingTerminal: the detail response flags both the task itself
// and its subtasks.
func TestGetTask_WaitingTerminal(t *testing.T) {
	d := waitingTaskDeps(t)
	srv := newTestServer(t, d)

	root := addWaitingTask(t, d, "orch-stalled", "Stalled", 20*time.Minute)
	addTestSession(t, d, "wk-1", "worker", "proj1")
	if err := d.Store.UpdateSessionActivity("wk-1", string(activity.WaitingInput),
		time.Now().Add(-30*time.Minute).Unix()); err != nil {
		t.Fatalf("UpdateSessionActivity wk-1: %v", err)
	}
	if _, err := d.Store.AddTask(store.Task{
		Title: "Sub", ProjectID: "proj1", ParentID: root, SessionID: "wk-1",
	}); err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := getJSON(t, srv.URL+"/v1/tasks/"+itoa(root))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		waitingRow
		Subtasks []waitingRow `json:"subtasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if !body.WaitingTerminal {
		t.Errorf("waiting_terminal = false on the task detail, want true")
	}
	if len(body.Subtasks) != 1 || !body.Subtasks[0].WaitingTerminal {
		t.Errorf("subtasks = %+v, want one flagged subtask", body.Subtasks)
	}
}

// TestListSessions_WaitingTerminal: `rocket status` renders sessions, so the
// same derived flag rides along on the session response.
func TestListSessions_WaitingTerminal(t *testing.T) {
	d := waitingTaskDeps(t)
	srv := newTestServer(t, d)

	addWaitingTask(t, d, "orch-stalled", "Stalled", 20*time.Minute)
	addWaitingTask(t, d, "orch-fresh", "Fresh", time.Minute)

	resp := getJSON(t, srv.URL+"/v1/sessions")
	defer resp.Body.Close()
	var sessions []struct {
		ID              string `json:"id"`
		WaitingTerminal bool   `json:"waiting_terminal"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	got := map[string]bool{}
	for _, s := range sessions {
		got[s.ID] = s.WaitingTerminal
	}
	if !got["orch-stalled"] {
		t.Errorf("waiting_terminal = false for orch-stalled, want true")
	}
	if got["orch-fresh"] {
		t.Errorf("waiting_terminal = true for orch-fresh, want false")
	}
}

// TestListTasks_WaitingTerminalPendingQuiz: a stale AskUserQuestion quiz is
// the other half of the predicate — the activity timestamp keeps moving while
// a quiz is open, so the quiz's asked_at is what counts.
func TestListTasks_WaitingTerminalPendingQuiz(t *testing.T) {
	d := waitingTaskDeps(t)
	srv := newTestServer(t, d)

	addTestSession(t, d, "orch-quiz", "orchestrator", "proj1")
	if err := d.Store.UpdateSessionActivity("orch-quiz", "thinking", time.Now().Unix()); err != nil {
		t.Fatalf("UpdateSessionActivity: %v", err)
	}
	quiz := session.Quiz{
		Questions: []session.QuizQuestion{{Question: "Which schema?"}},
		AskedAt:   time.Now().Add(-25 * time.Minute).Unix(),
	}
	raw, err := json.Marshal(quiz)
	if err != nil {
		t.Fatalf("marshal quiz: %v", err)
	}
	if err := d.Store.SetPendingQuiz("orch-quiz", string(raw)); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}
	id, err := d.Store.AddTask(store.Task{Title: "Quizzed", ProjectID: "proj1", SessionID: "orch-quiz"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if !listWaitingRows(t, srv, "")[id] {
		t.Errorf("waiting_terminal = false for a task whose session holds a stale quiz, want true")
	}
}

// TestListTasks_WaitingTerminalOmittedWhenHealthy: the flag is omitempty —
// a healthy task carries no waiting_terminal key at all.
func TestListTasks_WaitingTerminalOmittedWhenHealthy(t *testing.T) {
	d := waitingTaskDeps(t)
	srv := newTestServer(t, d)
	addWaitingTask(t, d, "orch-fresh", "Fresh", time.Minute)

	resp := getJSON(t, srv.URL+"/v1/tasks")
	defer resp.Body.Close()
	var body struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	if len(body.Tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(body.Tasks))
	}
	if _, present := body.Tasks[0]["waiting_terminal"]; present {
		t.Errorf("waiting_terminal present on a healthy task: %+v", body.Tasks[0])
	}
}

// TestListTasks_WaitingTerminalIgnoresDeadSession: a killed session isn't
// waiting for a keystroke, however stale its last activity is.
func TestListTasks_WaitingTerminalIgnoresDeadSession(t *testing.T) {
	d := waitingTaskDeps(t)
	srv := newTestServer(t, d)

	id := addWaitingTask(t, d, "orch-dead", "Dead", 20*time.Minute)
	if err := d.Store.UpdateSessionState("orch-dead", "killed"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	if listWaitingRows(t, srv, "")[id] {
		t.Errorf("waiting_terminal = true for a killed session, want false")
	}
}
