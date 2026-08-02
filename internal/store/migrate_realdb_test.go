package store

import (
	"database/sql"
	"os"
	"testing"
)

// countRows returns the number of rows in table, or -1 if the table does not
// exist. It opens its own handle so it can be called before Open() migrates.
func countRows(t *testing.T, path, table string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+escapeDSNPath(path)+"?_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()

	var exists int
	err = db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("look up table %s: %v", table, err)
	}
	if exists == 0 {
		return -1
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestMigrateRealDBCopy is a manual harness: point ROCKET_MIGTEST_DB at a copy
// of a real database to verify migrations apply to it end to end. Beyond
// applying cleanly it asserts that migration 0009 lost no Q&A history: every
// task thread and role thread, and every message, is still readable afterwards
// and carries a participant list.
func TestMigrateRealDBCopy(t *testing.T) {
	path := os.Getenv("ROCKET_MIGTEST_DB")
	if path == "" {
		t.Skip("set ROCKET_MIGTEST_DB to a database copy")
	}

	// Thread volume before the migration. -1 means the table is already gone,
	// i.e. this copy has had 0009 applied to it before.
	taskQuestions := countRows(t, path, "task_questions")
	agentQuestions := countRows(t, path, "agent_questions")
	taskMessages := countRows(t, path, "question_messages")
	agentMessages := countRows(t, path, "agent_question_messages")
	alreadyMigrated := taskQuestions < 0

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ags, err := s.ListAgents("")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	t.Logf("agents: %d", len(ags))

	task, err := s.GetTask(639)
	if err != nil {
		t.Fatalf("GetTask(639): %v", err)
	}
	t.Logf("task 639: %s (%s)", task.Title, task.Status)

	if alreadyMigrated {
		t.Log("copy was already migrated past 0009; skipping the history check")
		return
	}
	t.Logf("before 0009: %d task threads (%d messages), %d role threads (%d messages)",
		taskQuestions, taskMessages, agentQuestions, agentMessages)

	if got := countRows(t, path, "questions"); got != taskQuestions+agentQuestions {
		t.Errorf("questions after migration = %d, want %d", got, taskQuestions+agentQuestions)
	}
	if got := countRows(t, path, "question_messages"); got != taskMessages+agentMessages {
		t.Errorf("question_messages after migration = %d, want %d", got, taskMessages+agentMessages)
	}

	rows, err := s.db.Query(`SELECT ` + questionColumns + ` FROM questions ORDER BY id`)
	if err != nil {
		t.Fatalf("query questions: %v", err)
	}
	var all []Question
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			rows.Close()
			t.Fatalf("scan question: %v", err)
		}
		all = append(all, q)
	}
	rows.Close()

	var withMessages int
	for _, q := range all {
		msgs, err := s.ListQuestionMessages(q.ID)
		if err != nil {
			t.Fatalf("ListQuestionMessages(%d): %v", q.ID, err)
		}
		if len(msgs) > 0 {
			withMessages++
		}
		for _, m := range msgs {
			if m.Author == "" {
				t.Errorf("question %d message %d has an empty author", q.ID, m.ID)
			}
		}

		parts, err := s.ListParticipants(q.ID)
		if err != nil {
			t.Fatalf("ListParticipants(%d): %v", q.ID, err)
		}
		var hasHuman bool
		for _, p := range parts {
			if p == ParticipantHuman {
				hasHuman = true
			}
			if p == "" {
				t.Errorf("question %d has an empty participant id", q.ID)
			}
		}
		if !hasHuman {
			t.Errorf("question %d participants = %v, want the human among them", q.ID, parts)
		}
		if q.TaskID == 0 && q.RoleID == "" {
			t.Errorf("question %d is bound to neither a task nor a role", q.ID)
		}
	}
	t.Logf("verified %d threads (%d carrying messages)", len(all), withMessages)
}
