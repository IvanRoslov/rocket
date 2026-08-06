package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// addSubtaskWithPR wires a root task, a worker session carrying a PR and a
// subtask pointing at that session, returning the root task id.
func addSubtaskWithPR(t *testing.T, d Deps, sessID string, checkedAt int64) int64 {
	t.Helper()
	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	sess := store.Session{
		ID: sessID, Kind: "worker", ProjectID: "proj1", RepoID: "proj1-repo",
		Agent: "fake", Branch: "feature/x", TmuxName: sessID, State: "running",
		PRNumber: 42, PRState: "open", CIState: "passing",
	}
	if err := d.Store.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if checkedAt != 0 {
		if err := d.Store.MarkSessionPRChecked(sessID, checkedAt); err != nil {
			t.Fatalf("MarkSessionPRChecked: %v", err)
		}
	}
	if _, err := d.Store.AddTask(store.Task{
		Title: "Sub", ProjectID: "proj1", ParentID: rootID, SessionID: sessID,
	}); err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}
	return rootID
}

func subtaskJSON(t *testing.T, srvURL string, rootID int64) map[string]any {
	t.Helper()
	resp := getJSON(t, srvURL+"/v1/tasks/"+itoa(rootID))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeRepo(t, resp)
	subs := body["subtasks"].([]any)
	if len(subs) != 1 {
		t.Fatalf("len(subtasks) = %d, want 1", len(subs))
	}
	return subs[0].(map[string]any)
}

func TestTaskDetail_FreshPRIsNotStale(t *testing.T) {
	d := tasksTestDeps(t)
	d.Cfg.GithubPollInterval = time.Minute
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	checkedAt := time.Now().Unix() - 30
	rootID := addSubtaskWithPR(t, d, "worker-fresh", checkedAt)

	sub := subtaskJSON(t, srv.URL, rootID)
	if sub["pr_checked_at"] != float64(checkedAt) {
		t.Errorf("pr_checked_at = %v, want %d", sub["pr_checked_at"], checkedAt)
	}
	if stale, ok := sub["pr_stale"]; ok && stale == true {
		t.Errorf("a PR checked 30s ago must not be stale (interval 1m)")
	}
}

func TestTaskDetail_StalePRPastThreePollIntervals(t *testing.T) {
	d := tasksTestDeps(t)
	d.Cfg.GithubPollInterval = time.Minute
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	// 4 minutes without a successful poll, threshold is 3×1m.
	checkedAt := time.Now().Unix() - 240
	rootID := addSubtaskWithPR(t, d, "worker-stale", checkedAt)

	sub := subtaskJSON(t, srv.URL, rootID)
	if sub["pr_stale"] != true {
		t.Errorf("pr_stale = %v, want true", sub["pr_stale"])
	}
}

// A PR that has never been polled at all is the worst case: it must be
// reported as stale, not as quietly fresh.
func TestTaskDetail_NeverPolledPRIsStale(t *testing.T) {
	d := tasksTestDeps(t)
	d.Cfg.GithubPollInterval = time.Minute
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	rootID := addSubtaskWithPR(t, d, "worker-never", 0)

	sub := subtaskJSON(t, srv.URL, rootID)
	if _, ok := sub["pr_checked_at"]; ok {
		t.Errorf("pr_checked_at should be omitted when never polled, got %v", sub["pr_checked_at"])
	}
	if sub["pr_stale"] != true {
		t.Errorf("pr_stale = %v, want true for a never-polled PR", sub["pr_stale"])
	}
}

// Staleness only makes sense for a subtask that actually has a PR; a subtask
// without one must not be flagged.
func TestTaskDetail_NoPRNoStaleness(t *testing.T) {
	d := tasksTestDeps(t)
	d.Cfg.GithubPollInterval = time.Minute
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	if _, err := d.Store.AddTask(store.Task{
		Title: "Sub", ProjectID: "proj1", ParentID: rootID,
	}); err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	sub := subtaskJSON(t, srv.URL, rootID)
	if stale, ok := sub["pr_stale"]; ok && stale == true {
		t.Errorf("subtask without a PR must not be marked stale")
	}
}

func TestPRStaleThreshold_DefaultsWhenUnconfigured(t *testing.T) {
	if got := prStaleThreshold(nil); got != 3*defaultGithubPollInterval {
		t.Errorf("prStaleThreshold(nil) = %v, want %v", got, 3*defaultGithubPollInterval)
	}
}
