package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// preTitleMigrations is the number of migrations that existed before
// 00NN_question_title.sql — everything up to, but not including, it. seedPreTitle
// applies exactly these, so a subsequent Open() runs the title migration alone
// against realistic data.
const preTitleMigrations = 13

// seedPreTitle builds a database at the schema version right before the title
// migration and fills it with two questions: one carrying a legacy context and
// one without. It returns the database path.
func seedPreTitle(t *testing.T) string {
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
	if len(names) <= preTitleMigrations {
		t.Fatalf("expected more than %d migrations, found %d", preTitleMigrations, len(names))
	}
	for i := 0; i < preTitleMigrations; i++ {
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
	exec(`INSERT INTO tasks (id, title, project_id, status, created_by, created_at, updated_at)
	      VALUES (1, 'T', 'p', 'backlog', 'human', 1, 1)`)
	exec(`INSERT INTO questions (id, task_id, asked_by, body, context, status, addressed_to, type, asked_at)
	      VALUES (1, 1, 'orch', '## Какой CIDR выставить', 'детали контекста', 'open', '', 'decision', 1)`)
	exec(`INSERT INTO questions (id, task_id, asked_by, body, context, status, addressed_to, type, asked_at)
	      VALUES (2, 1, 'orch', 'Нужен ли Cloudflare? Иначе не соберём.', NULL, 'open', '', 'decision', 1)`)
	exec(`INSERT INTO questions (id, task_id, asked_by, body, context, status, addressed_to, type, asked_at)
	      VALUES (3, 1, 'orch', 'Пустой контекст.', '   ', 'open', '', 'decision', 1)`)

	return path
}

// TestMigrateQuestionTitle checks the migration end to end: the legacy context
// is appended to the body with the canonical separator, an empty one changes
// nothing, and every row comes out with a derived title.
func TestMigrateQuestionTitle(t *testing.T) {
	path := seedPreTitle(t)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	withContext, err := s.GetQuestion(1)
	if err != nil {
		t.Fatalf("GetQuestion(1): %v", err)
	}
	wantBody := "## Какой CIDR выставить\n\n---\n\nдетали контекста"
	if withContext.Body != wantBody {
		t.Fatalf("body = %q, want %q", withContext.Body, wantBody)
	}
	if withContext.Title != "Какой CIDR выставить" {
		t.Fatalf("title = %q, want %q", withContext.Title, "Какой CIDR выставить")
	}

	plain, err := s.GetQuestion(2)
	if err != nil {
		t.Fatalf("GetQuestion(2): %v", err)
	}
	if plain.Body != "Нужен ли Cloudflare? Иначе не соберём." {
		t.Fatalf("body = %q, want it unchanged", plain.Body)
	}
	if plain.Title != "Нужен ли Cloudflare?" {
		t.Fatalf("title = %q, want %q", plain.Title, "Нужен ли Cloudflare?")
	}

	blank, err := s.GetQuestion(3)
	if err != nil {
		t.Fatalf("GetQuestion(3): %v", err)
	}
	if blank.Body != "Пустой контекст." {
		t.Fatalf("body = %q, want it unchanged", blank.Body)
	}

	// The context column survives the migration: dropping it is out of scope.
	var ctx sql.NullString
	if err := s.db.QueryRow(`SELECT context FROM questions WHERE id = 1`).Scan(&ctx); err != nil {
		t.Fatalf("select context: %v", err)
	}
	if ctx.String != "детали контекста" {
		t.Fatalf("context column = %q, want it preserved", ctx.String)
	}
}

// TestMigrateQuestionTitleBackfillIsIdempotent checks that reopening the store
// does not re-append the context or overwrite a title somebody set by hand.
func TestMigrateQuestionTitleBackfillIsIdempotent(t *testing.T) {
	path := seedPreTitle(t)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE questions SET title = 'вручную' WHERE id = 2`); err != nil {
		t.Fatalf("set title by hand: %v", err)
	}
	first, err := s.GetQuestion(1)
	if err != nil {
		t.Fatalf("GetQuestion(1): %v", err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	again, err := s2.GetQuestion(1)
	if err != nil {
		t.Fatalf("GetQuestion(1) after reopen: %v", err)
	}
	if again.Body != first.Body {
		t.Fatalf("body changed on reopen: %q → %q", first.Body, again.Body)
	}
	manual, err := s2.GetQuestion(2)
	if err != nil {
		t.Fatalf("GetQuestion(2) after reopen: %v", err)
	}
	if manual.Title != "вручную" {
		t.Fatalf("title = %q, want it left alone", manual.Title)
	}
}
