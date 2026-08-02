package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
)

// pre0009Migrations is the set of migrations that existed before
// 0009_threads.sql. seedPre0009 applies exactly these, so that a subsequent
// Open() runs 0009 alone against realistic data.
const pre0009Migrations = 8

// seedPre0009 builds a database at the schema version right before
// 0009_threads.sql and fills both thread families with history:
//
//	task thread 1 (task 1, asked by orch-1) — messages from the human and orch-1
//	task thread 2 (task 1, asked by the human)
//	role thread 1 (role cto, asked by the human)
//	role thread 2 (role cto, asked by cto-run-1) — one message from cto-run-1
//
// It returns the database path.
func seedPre0009(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rocket.db")

	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}

	names, err := migrationNames()
	if err != nil {
		t.Fatalf("migrationNames: %v", err)
	}
	if len(names) <= pre0009Migrations {
		t.Fatalf("expected more than %d migrations, found %d", pre0009Migrations, len(names))
	}
	for i := 0; i < pre0009Migrations; i++ {
		body, err := migrationsFS.ReadFile("migrations/" + names[i])
		if err != nil {
			t.Fatalf("read migration %s: %v", names[i], err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("apply migration %s: %v", names[i], err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, i+1); err != nil {
			t.Fatalf("record migration %s: %v", names[i], err)
		}
	}

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}

	exec(`INSERT INTO repos (id, path, created_at) VALUES ('api', '/r', 1)`)
	exec(`INSERT INTO projects (id, name, main_repo, created_at) VALUES ('billing', 'Billing', 'api', 1)`)
	exec(`INSERT INTO tasks (id, parent_id, title, description, status, project_id, created_at, updated_at)
	      VALUES (1, NULL, 'Root', '', 'in_progress', 'billing', 1, 1)`)
	exec(`INSERT INTO agents (id, description, project_id, dir, command, enabled, created_at, updated_at)
	      VALUES ('cto', '', '', '', '', 1, 1, 1)`)

	exec(`INSERT INTO task_questions (id, task_id, asked_by, body, context, status, resolution, asked_at, resolved_at)
	      VALUES (1, 1, 'orch-1', 'task q1', 'ctx', 'open', NULL, 100, NULL)`)
	exec(`INSERT INTO task_questions (id, task_id, asked_by, body, context, status, resolution, asked_at, resolved_at)
	      VALUES (2, 1, '', 'task q2', NULL, 'resolved', 'answered', 110, 120)`)
	exec(`INSERT INTO question_messages (id, question_id, author, kind, body, created_at)
	      VALUES (1, 1, '', 'reply', 'human on task q1', 101)`)
	exec(`INSERT INTO question_messages (id, question_id, author, kind, body, created_at)
	      VALUES (2, 1, 'orch-1', 'reply', 'orch on task q1', 102)`)

	exec(`INSERT INTO agent_questions (id, role_id, asked_by, body, context, status, resolution, asked_at, resolved_at)
	      VALUES (1, 'cto', '', 'role q1', NULL, 'open', NULL, 200, NULL)`)
	exec(`INSERT INTO agent_questions (id, role_id, asked_by, body, context, status, resolution, asked_at, resolved_at)
	      VALUES (2, 'cto', 'cto-run-1', 'role q2', 'rctx', 'open', NULL, 210, NULL)`)
	exec(`INSERT INTO agent_question_messages (id, question_id, author, kind, body, created_at)
	      VALUES (1, 2, 'cto-run-1', 'reply', 'cto on role q2', 211)`)

	return path
}

// queryStrings runs a single-column query and returns its rows.
func queryStrings(t *testing.T, s *Store, query string, args ...any) []string {
	t.Helper()
	rows, err := s.db.Query(query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan %q: %v", query, err)
		}
		out = append(out, v.String)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows %q: %v", query, err)
	}
	return out
}

func TestMigrate0009_TaskThreadsKeepTheirIdentity(t *testing.T) {
	path := seedPre0009(t)
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	got := queryStrings(t, s,
		`SELECT body || '|' || COALESCE(task_id, 0) || '|' || COALESCE(role_id, '')
		 FROM questions WHERE id IN (1, 2) ORDER BY id`)
	want := []string{"task q1|1|", "task q2|1|"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("task threads = %v, want %v", got, want)
	}
}

func TestMigrate0009_RoleThreadsMoveInWithHistory(t *testing.T) {
	path := seedPre0009(t)
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Both role threads landed with role_id set and no task, at ids beyond
	// every task thread.
	got := queryStrings(t, s,
		`SELECT body FROM questions WHERE role_id = 'cto' AND task_id IS NULL
		 AND id > (SELECT MAX(id) FROM questions WHERE task_id IS NOT NULL)
		 ORDER BY id`)
	if fmt.Sprint(got) != fmt.Sprint([]string{"role q1", "role q2"}) {
		t.Errorf("role threads = %v, want [role q1 role q2]", got)
	}

	// The moved message still points at the thread it belonged to — role q2,
	// not role q1 and not a task thread.
	got = queryStrings(t, s,
		`SELECT q.body FROM question_messages m JOIN questions q ON q.id = m.question_id
		 WHERE m.body = 'cto on role q2'`)
	if fmt.Sprint(got) != fmt.Sprint([]string{"role q2"}) {
		t.Errorf("moved message belongs to %v, want [role q2]", got)
	}

	// Task-thread messages are undisturbed.
	got = queryStrings(t, s,
		`SELECT m.body FROM question_messages m WHERE m.question_id = 1 ORDER BY m.id`)
	if fmt.Sprint(got) != fmt.Sprint([]string{"human on task q1", "orch on task q1"}) {
		t.Errorf("task q1 thread = %v", got)
	}
}

func TestMigrate0009_OrdinalsSurvive(t *testing.T) {
	path := seedPre0009(t)
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// "role q2" was the second question asked on role cto and must still be.
	got := queryStrings(t, s, `
		SELECT CAST((SELECT COUNT(*) FROM questions p
		             WHERE p.role_id = q.role_id AND p.id <= q.id) AS TEXT)
		FROM questions q WHERE q.body = 'role q2'`)
	if fmt.Sprint(got) != fmt.Sprint([]string{"2"}) {
		t.Errorf("ordinal of role q2 = %v, want [2]", got)
	}

	got = queryStrings(t, s, `
		SELECT CAST((SELECT COUNT(*) FROM questions p
		             WHERE p.task_id = q.task_id AND p.id <= q.id) AS TEXT)
		FROM questions q WHERE q.body = 'task q2'`)
	if fmt.Sprint(got) != fmt.Sprint([]string{"2"}) {
		t.Errorf("ordinal of task q2 = %v, want [2]", got)
	}
}

func TestMigrate0009_NormalisesHumanAuthors(t *testing.T) {
	path := seedPre0009(t)
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	got := queryStrings(t, s,
		`SELECT COALESCE(author, '<null>') FROM question_messages ORDER BY id`)
	sort.Strings(got)
	want := []string{"cto-run-1", "human", "orch-1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("authors = %v, want %v", got, want)
	}
}

func TestMigrate0009_BackfillsParticipants(t *testing.T) {
	path := seedPre0009(t)
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	cases := []struct {
		body string
		want []string
	}{
		// asked by orch-1, replies from the human and orch-1
		{"task q1", []string{"human", "orch-1"}},
		// asked by the human, no messages
		{"task q2", []string{"human"}},
		// asked by the human, no messages
		{"role q1", []string{"human"}},
		// asked by cto-run-1, one message from cto-run-1
		{"role q2", []string{"cto-run-1", "human"}},
	}
	for _, tc := range cases {
		got := queryStrings(t, s,
			`SELECT p.participant_id FROM question_participants p
			 JOIN questions q ON q.id = p.question_id
			 WHERE q.body = ? ORDER BY p.participant_id`, tc.body)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("participants of %q = %v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestMigrate0009_DropsTheOldTables(t *testing.T) {
	path := seedPre0009(t)
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	got := queryStrings(t, s,
		`SELECT name FROM sqlite_master WHERE type = 'table'
		 AND name IN ('task_questions', 'agent_questions', 'agent_question_messages')`)
	if len(got) != 0 {
		t.Errorf("old tables still present: %v", got)
	}
}

func TestMigrate0009_AppliesToAFreshDatabase(t *testing.T) {
	s := openTestStore(t)

	for _, name := range []string{"questions", "question_messages", "question_participants"} {
		got := queryStrings(t, s,
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name)
		if len(got) != 1 {
			t.Errorf("table %s missing on a fresh database", name)
		}
	}
}
