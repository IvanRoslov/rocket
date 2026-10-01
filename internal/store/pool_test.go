package store

import (
	"testing"
	"time"
)

// TestOpenBoundsThePool: an unbounded pool let a burst of requests open
// hundreds of SQLite connections, each with its own page cache, until the
// daemon drowned in memory and contention. Bounded, extra queries wait in
// line for a connection instead.
func TestOpenBoundsThePool(t *testing.T) {
	s := openTestStore(t)
	if got := s.db.Stats().MaxOpenConnections; got != maxOpenConns {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, maxOpenConns)
	}
}

// TestBoundedPoolSurvivesConcurrentReads: far more concurrent readers than
// pool slots, through a method that runs several queries in a row. Code that
// held a connection while taking another would deadlock here instead of in
// the daemon.
func TestBoundedPoolSurvivesConcurrentReads(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	for i := 0; i < 5; i++ {
		if _, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch", Body: "вопрос"}); err != nil {
			t.Fatalf("AddQuestion: %v", err)
		}
	}

	const readers = maxOpenConns * 4
	done := make(chan error, readers)
	for i := 0; i < readers; i++ {
		go func() {
			_, err := s.ListThreads(true)
			done <- err
		}()
	}
	timeout := time.After(10 * time.Second)
	for i := 0; i < readers; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("ListThreads: %v", err)
			}
		case <-timeout:
			t.Fatalf("only %d of %d readers finished: the bounded pool deadlocked", i, readers)
		}
	}
}
