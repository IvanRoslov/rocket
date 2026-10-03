package api

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
	"github.com/IvanRoslov/rocket/internal/usage"
)

func usageAPISession(t *testing.T, d Deps, id, project, state string, taskID, subtaskID, created, updated int64) {
	t.Helper()
	if err := d.Store.AddSession(store.Session{
		ID: id, Kind: "worker", ProjectID: project, RepoID: project + "-repo", Agent: "codex",
		Branch: "feature/test/" + id, TmuxName: id, State: state, TaskID: taskID, SubtaskID: subtaskID,
		CreatedAt: created, UpdatedAt: updated,
	}); err != nil {
		t.Fatalf("AddSession %s: %v", id, err)
	}
}

func collectAPISession(t *testing.T, d Deps, id string, taskID, subtaskID int64, final bool, endedAt *int64, collectedAt int64, status, model string, input int64) {
	t.Helper()
	var models []store.ModelUsage
	if model != "" {
		models = []store.ModelUsage{{Model: model, Tokens: store.UsageTokens{Input: input}}}
	}
	if err := d.Store.ReplaceSessionUsage(store.SessionStats{
		SessionID: id, TaskID: taskID, SubtaskID: subtaskID, Status: status, Final: final,
		StartedAt: 100, EndedAt: endedAt, CollectedAt: collectedAt,
	}, models); err != nil {
		t.Fatalf("ReplaceSessionUsage %s: %v", id, err)
	}
}

func readUsagePeriod(t *testing.T, url string) (int, struct {
	From string `json:"from"`
	To   string `json:"to"`
	usage.PeriodSummary
	Pending int `json:"pending"`
}) {
	t.Helper()
	resp := getJSON(t, url)
	defer resp.Body.Close()
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
		usage.PeriodSummary
		Pending int `json:"pending"`
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, body
}

func TestUsagePeriodDefaultsAndLocalInclusiveEnd(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "p")
	addTestProject(t, d, "q")
	today := time.Now().In(time.Local)
	day := today.Format("2006-01-02")
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local)
	lastSecond := start.AddDate(0, 0, 1).Add(-time.Second).Unix()
	tomorrow := start.AddDate(0, 0, 1).Unix()
	for _, tc := range []struct {
		id, project, state string
		at                 int64
		final              bool
	}{
		{"included", "p", "done", lastSecond, true},
		{"excluded", "p", "done", tomorrow, true},
		{"snapshot", "p", "running", lastSecond, false},
		{"other", "q", "done", lastSecond, true},
	} {
		usageAPISession(t, d, tc.id, tc.project, tc.state, 0, 0, 100, tc.at)
		var ended *int64
		if tc.final {
			ended = &tc.at
		}
		collectAPISession(t, d, tc.id, 0, 0, tc.final, ended, tc.at, "ok", "m", 10)
	}
	usageAPISession(t, d, "pending", "p", "done", 0, 0, 100, lastSecond)
	status, got := readUsagePeriod(t, srv.URL+"/v1/stats/usage?from="+day+"&to="+day+"&project=p")
	if status != 200 || got.From != day || got.To != day || got.Totals.Sessions != 2 || got.Totals.Tokens.Billable != 20 || got.Pending != 1 {
		t.Fatalf("filtered period: status=%d body=%+v", status, got)
	}
	status, got = readUsagePeriod(t, srv.URL+"/v1/stats/usage")
	wantFrom := start.AddDate(0, 0, -29).Format("2006-01-02")
	if status != 200 || got.From != wantFrom || got.To != day || got.Totals.Sessions != 3 || got.Pending != 1 {
		t.Fatalf("default period: status=%d body=%+v", status, got)
	}
}

func TestUsagePeriodBadDates(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	for _, query := range []string{
		"?from=2026-02-30&to=2026-03-01",
		"?from=2026-03-02&to=2026-03-01",
		"?to=tomorrow",
		"?from=2026-1-1",
	} {
		resp := getJSON(t, srv.URL+"/v1/stats/usage"+query)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("query %q: status = %d", query, resp.StatusCode)
			resp.Body.Close()
			continue
		}
		if got := decodeErr(t, resp).Error.Code; got != "bad_request" {
			t.Errorf("query %q: code = %q", query, got)
		}
	}
}

func TestUsageDateWindowAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	from, to, _, _, err := usageDateWindow("2026-03-08", "2026-03-08", time.Date(2026, 3, 10, 12, 0, 0, 0, loc), loc)
	if err != nil {
		t.Fatal(err)
	}
	if to-from != int64(23*time.Hour/time.Second) {
		t.Fatalf("DST date window is %d seconds, want 23 hours", to-from)
	}
}

func TestUsagePeriodEmptyListsAreArrays(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	resp := getJSON(t, srv.URL+"/v1/stats/usage")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["models"]) != "[]" || string(raw["tasks"]) != "[]" {
		t.Fatalf("empty arrays: models=%s tasks=%s", raw["models"], raw["tasks"])
	}
}

func TestUsageTaskIncludesLinkedSessionsAndPRURL(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "p")
	repo, err := d.Store.GetRepo("p-repo")
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repo.Path, "remote", "add", "origin", "https://github.com/acme/widgets.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, output)
	}
	root, err := d.Store.AddTask(store.Task{Title: "Feature", ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := d.Store.AddTask(store.Task{Title: "Worker", ProjectID: "p", ParentID: root})
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct{ id, state string }{{"done", "done"}, {"missing", "done"}, {"live", "running"}, {"pending", "killed"}} {
		usageAPISession(t, d, tc.id, "p", tc.state, root, sub, int64(100+i*10), int64(200+i*10))
	}
	session, err := d.Store.GetSession("done")
	if err != nil {
		t.Fatal(err)
	}
	session.PRNumber = 42
	session.PRState = "merged"
	if err := d.Store.UpdateSession(session); err != nil {
		t.Fatal(err)
	}
	end := int64(160)
	collectAPISession(t, d, "done", root, sub, true, &end, 170, "ok", "m", 9)
	collectAPISession(t, d, "missing", root, sub, true, &end, 170, "missing", "", 0)
	resp := getJSON(t, srv.URL+"/v1/tasks/"+strconv.FormatInt(root, 10)+"/usage")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var got usage.TaskUsageSummary
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.TaskID != root || got.Totals.Sessions != 2 || len(got.Sessions) != 4 || got.Sessions[0].PRURL != "https://github.com/acme/widgets/pull/42" || got.Sessions[0].SubtaskID == nil || *got.Sessions[0].SubtaskID != sub {
		t.Fatalf("task usage = %+v", got)
	}
	if got.Sessions[1].Status != "missing" || got.Sessions[2].Status != "running" || got.Sessions[3].Status != "pending" || got.Sessions[2].CostUSD != nil || got.Sessions[3].CostUSD != nil {
		t.Errorf("session states = %+v", got.Sessions)
	}
	for path, wantCode := range map[string]string{
		"/v1/tasks/" + strconv.FormatInt(sub, 10) + "/usage": "not_root_task",
		"/v1/tasks/999999/usage":                             "task_not_found",
	} {
		resp := getJSON(t, srv.URL+path)
		if (wantCode == "not_root_task" && resp.StatusCode != 400) || (wantCode == "task_not_found" && resp.StatusCode != 404) {
			t.Errorf("%s status=%d", path, resp.StatusCode)
			resp.Body.Close()
			continue
		}
		if code := decodeErr(t, resp).Error.Code; code != wantCode {
			t.Errorf("%s code=%s", path, code)
		}
	}
}
