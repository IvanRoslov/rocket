package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestUsageMigrationPreservesExistingSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rocket.db")
	db, err := sql.Open("sqlite", "file:"+escapeDSNPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		if name == "0023_session_usage.sql" {
			break
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO sessions
		(id,kind,project_id,repo_id,feature_slug,agent,branch,worktree_path,tmux_name,state,created_at,updated_at)
		VALUES ('old-orch','orchestrator','p','r','old','codex','orch/old','/tmp/old','old-orch','done',100,200)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("migrate populated database: %v", err)
	}
	defer s.Close()
	sess, err := s.GetSession("old-orch")
	if err != nil {
		t.Fatal(err)
	}
	if sess.TaskID != 0 || sess.SubtaskID != 0 || sess.CreatedAt != 100 {
		t.Fatalf("existing session changed: %+v", sess)
	}
	for _, table := range []string{"session_usage", "session_stats", "model_prices"} {
		var count int
		if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("table %s missing: count=%d err=%v", table, count, err)
		}
	}
}

func TestSessionTaskIDsRoundTrip(t *testing.T) {
	s := openTestStore(t)
	sess := Session{ID: "worker", Kind: "worker", State: "running", TaskID: 41, SubtaskID: 42}
	if err := s.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession("worker")
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != 41 || got.SubtaskID != 42 {
		t.Fatalf("GetSession task IDs = %d/%d, want 41/42", got.TaskID, got.SubtaskID)
	}
	got.TaskID, got.SubtaskID = 51, 52
	if err := s.UpdateSession(got); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListSessions(SessionFilter{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TaskID != 51 || rows[0].SubtaskID != 52 {
		t.Fatalf("ListSessions task IDs = %+v, want 51/52", rows)
	}
}

func TestReplaceSessionUsageReplacesModelsAndStats(t *testing.T) {
	s := openTestStore(t)
	addUsageTestSession(t, s, Session{ID: "s", Kind: "worker", State: "done"})
	first := SessionStats{SessionID: "s", TaskID: 10, SubtaskID: 11, Status: "ok", Final: false, StartedAt: 100, CollectedAt: 200}
	if err := s.ReplaceSessionUsage(first, []ModelUsage{
		{Model: "old", Tokens: UsageTokens{Input: 9}},
		{Model: "keep", Tokens: UsageTokens{Input: 2}},
	}); err != nil {
		t.Fatal(err)
	}
	end := int64(300)
	second := SessionStats{SessionID: "s", TaskID: 10, SubtaskID: 11, Status: "ok", Final: true, StartedAt: 100, EndedAt: &end, CollectedAt: 301}
	models := []ModelUsage{{Model: "keep", Tokens: UsageTokens{Input: 3, CacheWrite: 4, CacheRead: 5, Output: 6, Reasoning: 2, Messages: 7}}}
	if err := s.ReplaceSessionUsage(second, models); err != nil {
		t.Fatal(err)
	}
	got, gotModels, err := s.GetSessionStats("s")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, second) || !reflect.DeepEqual(gotModels, models) {
		t.Fatalf("replacement got stats=%+v models=%+v, want %+v %+v", got, gotModels, second, models)
	}
	if err := s.ReplaceSessionUsage(first, []ModelUsage{{Model: "dup"}, {Model: "dup"}}); err == nil {
		t.Fatal("duplicate model replacement unexpectedly succeeded")
	}
	got, gotModels, err = s.GetSessionStats("s")
	if err != nil || !reflect.DeepEqual(got, second) || !reflect.DeepEqual(gotModels, models) {
		t.Fatalf("failed replacement changed committed rows: stats=%+v models=%+v err=%v", got, gotModels, err)
	}
	if _, _, err := s.GetSessionStats("absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSessionStats(absent) = %v, want ErrNotFound", err)
	}
}

func TestSessionsNeedingUsageSelection(t *testing.T) {
	s := openTestStore(t)
	for _, id := range []string{"none", "snapshot", "missing", "ok", "retry", "fresh-error", "exhausted", "live"} {
		state := "done"
		if id == "live" {
			state = "running"
		}
		addUsageTestSession(t, s, Session{ID: id, Kind: "worker", State: state})
	}
	for _, tc := range []struct {
		id       string
		status   string
		final    bool
		attempts int
		at       int64
	}{
		{"snapshot", "ok", false, 0, 9900},
		{"missing", "missing", true, 0, 100},
		{"ok", "ok", true, 0, 100},
		{"retry", "error", true, 2, 6300},
		{"fresh-error", "error", true, 1, 6401},
		{"exhausted", "error", true, 3, 100},
	} {
		if err := s.ReplaceSessionUsage(SessionStats{SessionID: tc.id, Status: tc.status, Final: tc.final, StartedAt: 1, CollectedAt: tc.at, Attempts: tc.attempts}, nil); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.SessionsNeedingUsage(10, 10000)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(got))
	for _, sess := range got {
		ids = append(ids, sess.ID)
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"none", "retry", "snapshot"}) {
		t.Fatalf("SessionsNeedingUsage IDs = %v", ids)
	}
	limited, err := s.SessionsNeedingUsage(2, 10000)
	if err != nil || len(limited) != 2 {
		t.Fatalf("limit returned %d rows, err=%v", len(limited), err)
	}
}

func TestResolveSessionTaskFromDirectTaskAndLog(t *testing.T) {
	s := openTestStore(t)
	rootID, err := s.AddTask(Task{Title: "root", ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	subID, err := s.AddTask(Task{Title: "sub", ParentID: rootID, ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	addUsageTestSession(t, s, Session{ID: "direct", Kind: "worker", State: "done", TaskID: rootID, SubtaskID: subID})
	addUsageTestSession(t, s, Session{ID: "orch-old", Kind: "orchestrator", State: "done"})
	addUsageTestSession(t, s, Session{ID: "worker-old", Kind: "worker", State: "done"})
	addUsageTestSession(t, s, Session{ID: "worker-new", Kind: "worker", State: "running"})
	addUsageTestSession(t, s, Session{ID: "unlinked", Kind: "worker", State: "done"})

	root, err := s.GetTask(rootID)
	if err != nil {
		t.Fatal(err)
	}
	root.SessionID = "orch-old"
	if err := s.UpdateTask(root); err != nil {
		t.Fatal(err)
	}
	sub, err := s.GetTask(subID)
	if err != nil {
		t.Fatal(err)
	}
	sub.SessionID = "worker-new" // respawn overwrote the old worker link
	if err := s.UpdateTask(sub); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTaskLog(TaskLogEntry{TaskID: rootID, Kind: "status", Body: "spawned worker worker-old for subtask #" + itoaUsage(subID)}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		id, source string
		root, sub  int64
	}{
		{"direct", "column", rootID, subID},
		{"orch-old", "task", rootID, 0},
		{"worker-new", "task", rootID, subID},
		{"worker-old", "log", rootID, subID},
		{"unlinked", "none", 0, 0},
	} {
		gotRoot, gotSub, err := s.ResolveSessionTask(tc.id)
		if err != nil || gotRoot != tc.root || gotSub != tc.sub {
			t.Errorf("%s (%s): got %d/%d, err=%v; want %d/%d", tc.id, tc.source, gotRoot, gotSub, err, tc.root, tc.sub)
		}
		if tc.root != 0 {
			sess, err := s.GetSession(tc.id)
			if err != nil || sess.TaskID != tc.root || sess.SubtaskID != tc.sub {
				t.Errorf("%s persisted: %+v, err=%v", tc.id, sess, err)
			}
		}
	}
}

func TestUsageRowsFiltersExclusiveEndAndSnapshotTime(t *testing.T) {
	s := openTestStore(t)
	rootID, err := s.AddTask(Task{Title: "feature", ProjectID: "p", Status: "review"})
	if err != nil {
		t.Fatal(err)
	}
	subID, err := s.AddTask(Task{Title: "worker task", ParentID: rootID, ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id, project string
		final       bool
		ended, at   int64
	}{
		{"at-start", "p", true, 100, 300},
		{"at-end", "p", true, 200, 300},
		{"snapshot", "p", false, 0, 150},
		{"other-project", "q", true, 150, 300},
	} {
		addUsageTestSession(t, s, Session{ID: tc.id, Kind: "worker", ProjectID: tc.project, RepoID: "r", Agent: "codex", Profile: "fast", Effort: "high", State: "done", TaskID: rootID, SubtaskID: subID, PRNumber: 12, PRState: "merged"})
		st := SessionStats{SessionID: tc.id, TaskID: rootID, SubtaskID: subID, Status: "ok", Final: tc.final, StartedAt: 10, CollectedAt: tc.at}
		if tc.final {
			st.EndedAt = &tc.ended
		}
		if err := s.ReplaceSessionUsage(st, []ModelUsage{{Model: "m", Tokens: UsageTokens{Input: 4, CacheRead: 5, Output: 6}}}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.UsageRows(100, 200, "p")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.SessionID)
		if row.TaskID != rootID || row.SubtaskID != subID || row.TaskTitle != "feature" || row.SubtaskTitle != "worker task" || row.TaskStatus != "review" || row.Tokens.Input != 4 || row.Tokens.CacheRead != 5 || row.PRNumber != 12 {
			t.Errorf("incomplete usage row: %+v", row)
		}
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"at-start", "snapshot"}) {
		t.Fatalf("UsageRows IDs = %v, want at-start and snapshot", ids)
	}
}

func TestTaskUsageRowsIncludesRunningAndMissingSessions(t *testing.T) {
	s := openTestStore(t)
	rootID, err := s.AddTask(Task{Title: "root", ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	addUsageTestSession(t, s, Session{ID: "running", Kind: "orchestrator", State: "running", TaskID: rootID, CreatedAt: 100})
	addUsageTestSession(t, s, Session{ID: "missing", Kind: "worker", State: "done", TaskID: rootID, CreatedAt: 110})
	if err := s.ReplaceSessionUsage(SessionStats{SessionID: "missing", TaskID: rootID, Status: "missing", Final: true, StartedAt: 110, CollectedAt: 200}, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := s.TaskUsageRows(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].SessionID != "running" || rows[0].Status != "" || rows[0].StartedAt != 100 || rows[1].SessionID != "missing" || rows[1].Status != "missing" || rows[1].Model != "" {
		t.Fatalf("TaskUsageRows = %+v", rows)
	}
}

func TestTaskUsageRowsCarriesCollectionError(t *testing.T) {
	s := openTestStore(t)
	rootID, err := s.AddTask(Task{Title: "root", ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	addUsageTestSession(t, s, Session{ID: "failed", Kind: "worker", State: "errored", TaskID: rootID})
	if err := s.ReplaceSessionUsage(SessionStats{SessionID: "failed", TaskID: rootID, Status: "error", Error: "read denied", Final: true, StartedAt: 100, CollectedAt: 200}, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := s.TaskUsageRows(rootID)
	if err != nil || len(rows) != 1 || rows[0].Error != "read denied" {
		t.Fatalf("TaskUsageRows = %+v, err=%v", rows, err)
	}
}

func TestCountPendingUsageCountsTerminalUncollectedAndSnapshots(t *testing.T) {
	s := openTestStore(t)
	for _, tc := range []struct {
		id, state string
		updated   int64
	}{
		{"uncollected", "done", 100},
		{"snapshot", "killed", 150},
		{"at-end", "done", 200},
		{"live", "running", 100},
		{"final", "done", 100},
	} {
		addUsageTestSession(t, s, Session{ID: tc.id, Kind: "worker", State: tc.state, CreatedAt: 10, UpdatedAt: tc.updated})
	}
	if err := s.ReplaceSessionUsage(SessionStats{SessionID: "snapshot", Status: "ok", Final: false, StartedAt: 10, CollectedAt: 140}, nil); err != nil {
		t.Fatal(err)
	}
	end := int64(100)
	if err := s.ReplaceSessionUsage(SessionStats{SessionID: "final", Status: "ok", Final: true, StartedAt: 10, EndedAt: &end, CollectedAt: 101}, nil); err != nil {
		t.Fatal(err)
	}
	count, err := s.CountPendingUsage(100, 200, "")
	if err != nil || count != 2 {
		t.Fatalf("CountPendingUsage = %d, err=%v; want 2", count, err)
	}
}

func TestCountPendingUsageFiltersProject(t *testing.T) {
	s := openTestStore(t)
	addUsageTestSession(t, s, Session{ID: "p-pending", Kind: "worker", ProjectID: "p", State: "done", CreatedAt: 10, UpdatedAt: 150})
	addUsageTestSession(t, s, Session{ID: "q-pending", Kind: "worker", ProjectID: "q", State: "done", CreatedAt: 10, UpdatedAt: 150})
	all, err := s.CountPendingUsage(100, 200, "")
	if err != nil || all != 2 {
		t.Fatalf("all pending = %d, err=%v; want 2", all, err)
	}
	project, err := s.CountPendingUsage(100, 200, "p")
	if err != nil || project != 1 {
		t.Fatalf("project pending = %d, err=%v; want 1", project, err)
	}
}

func TestModelPricesAndObservedModels(t *testing.T) {
	s := openTestStore(t)
	addUsageTestSession(t, s, Session{ID: "s", Kind: "worker", State: "done"})
	if err := s.ReplaceSessionUsage(SessionStats{SessionID: "s", Status: "ok", Final: true, StartedAt: 1, CollectedAt: 2}, []ModelUsage{{Model: "unpriced"}}); err != nil {
		t.Fatal(err)
	}
	zero, two := 0.0, 2.0
	price := ModelPrice{Model: "priced", Input: &zero, Output: &two, UpdatedAt: 123}
	if err := s.UpsertModelPrice(price); err != nil {
		t.Fatal(err)
	}
	prices, err := s.ListModelPrices()
	if err != nil || len(prices) != 1 || !reflect.DeepEqual(prices[0], price) {
		t.Fatalf("ListModelPrices = %+v, err=%v", prices, err)
	}
	models, err := s.UsageModels()
	if err != nil || !reflect.DeepEqual(models, []string{"unpriced"}) {
		t.Fatalf("UsageModels = %v, err=%v", models, err)
	}
	price.Input = &two
	if err := s.UpsertModelPrice(price); err != nil {
		t.Fatal(err)
	}
	prices, err = s.ListModelPrices()
	if err != nil || len(prices) != 1 || *prices[0].Input != two {
		t.Fatalf("price upsert = %+v, err=%v", prices, err)
	}
	if err := s.DeleteModelPrice("priced"); err != nil {
		t.Fatal(err)
	}
	prices, err = s.ListModelPrices()
	if err != nil || len(prices) != 0 {
		t.Fatalf("price deletion = %+v, err=%v", prices, err)
	}
}

func TestUsageHistoryQueriesUseIndexes(t *testing.T) {
	s := openTestStore(t)
	for _, tc := range []struct {
		query string
		index string
	}{
		{`SELECT session_id FROM session_stats WHERE final=1 AND ended_at>=100 AND ended_at<200`, "session_stats_ended"},
		{`SELECT session_id FROM session_stats WHERE final=0 AND collected_at>=100 AND collected_at<200`, "session_stats_collected"},
		{`SELECT id FROM sessions WHERE task_id=42`, "idx_sessions_task"},
		{`SELECT id FROM tasks WHERE session_id='worker'`, "idx_tasks_session"},
	} {
		rows, err := s.db.Query(`EXPLAIN QUERY PLAN ` + tc.query)
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if !strings.Contains(strings.Join(details, " "), tc.index) {
			t.Errorf("query %q plan %v does not use %s", tc.query, details, tc.index)
		}
	}
}

func TestUsageReadStatementsUseIndexedCandidateSets(t *testing.T) {
	s := openTestStore(t)
	for _, tc := range []struct {
		query string
		args  []any
		want  []string
	}{
		{usageRowsStatement(false), []any{100, 200, 100, 200}, []string{"session_stats_ended", "session_stats_collected"}},
		{taskUsageRowsStatement(), []any{42, 42, 42, 42, 42}, []string{"idx_sessions_task", "session_stats_task"}},
	} {
		rows, err := s.db.Query(`EXPLAIN QUERY PLAN `+tc.query, tc.args...)
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		plan := strings.Join(details, " ")
		for _, index := range tc.want {
			if !strings.Contains(plan, index) {
				t.Errorf("query plan %v does not use %s", details, index)
			}
		}
	}
}

func addUsageTestSession(t *testing.T, s *Store, sess Session) {
	t.Helper()
	if err := s.AddSession(sess); err != nil {
		t.Fatal(err)
	}
}

func itoaUsage(n int64) string { return fmt.Sprintf("%d", n) }

func TestTerminalSessionIDsForManualRecollect(t *testing.T) {
	s := openTestStore(t)
	for _, id := range []string{"none", "ok", "missing", "error", "live"} {
		state := "killed"
		if id == "live" {
			state = "running"
		}
		addUsageTestSession(t, s, Session{ID: id, Kind: "worker", State: state})
	}
	for id, status := range map[string]string{"ok": "ok", "missing": "missing", "error": "error"} {
		if err := s.ReplaceSessionUsage(SessionStats{SessionID: id, Status: status, Final: true, StartedAt: 1, CollectedAt: 1, Attempts: 3}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		collected, missing bool
		want               []string
	}{
		{true, false, []string{"error", "none", "ok"}},
		{true, true, []string{"error", "missing", "none", "ok"}},
		{false, true, []string{"missing"}},
		{false, false, nil},
	} {
		got, err := s.TerminalSessionIDs(tc.collected, tc.missing)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("TerminalSessionIDs(%v,%v) = %v, want %v", tc.collected, tc.missing, got, tc.want)
		}
	}
}
