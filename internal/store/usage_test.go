package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
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

func addUsageTestSession(t *testing.T, s *Store, sess Session) {
	t.Helper()
	if err := s.AddSession(sess); err != nil {
		t.Fatal(err)
	}
}

func itoaUsage(n int64) string { return fmt.Sprintf("%d", n) }
