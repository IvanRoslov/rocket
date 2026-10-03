package store

import (
	"database/sql"
	"path/filepath"
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
