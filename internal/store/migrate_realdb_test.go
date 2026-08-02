package store

import (
	"os"
	"testing"
)

// TestMigrateRealDBCopy is a manual harness: point ROCKET_MIGTEST_DB at a copy
// of a real database to verify migrations apply to it end to end.
func TestMigrateRealDBCopy(t *testing.T) {
	path := os.Getenv("ROCKET_MIGTEST_DB")
	if path == "" {
		t.Skip("set ROCKET_MIGTEST_DB to a database copy")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ags, err := s.ListAgents("")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	t.Logf("agents: %+v", ags)

	task, err := s.GetTask(639)
	if err != nil {
		t.Fatalf("GetTask(639): %v", err)
	}
	t.Logf("task 639: %s (%s)", task.Title, task.Status)
}
