package heartbeat

import (
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// TestStaleThread pins the staleness rule itself, away from delivery: what
// counts as movement, what counts as somebody's turn, and which threads are
// exempt from going stale at all.
func TestStaleThread(t *testing.T) {
	now := time.Unix(1_000_000_000, 0)
	old := now.Add(-30 * time.Hour).Unix()
	fresh := now.Add(-time.Hour).Unix()
	after := 24 * time.Hour

	base := func() store.OpenThread {
		return store.OpenThread{
			Question: store.Question{
				ID: 1, TaskID: 7, Status: "open",
				Type: store.QuestionTypeDecision, AskedAt: old,
			},
			Attention: []string{"cto"},
		}
	}

	t.Run("no messages: the question itself is the reference point", func(t *testing.T) {
		since, ok := StaleThread(base(), now, after)
		if !ok || since != 30*time.Hour {
			t.Fatalf("StaleThread = (%s, %v), want (30h, true)", since, ok)
		}
	})

	t.Run("the last entry is the reference point", func(t *testing.T) {
		th := base()
		th.LastMessage = &store.QuestionMessage{QuestionID: 1, CreatedAt: fresh}
		if since, ok := StaleThread(th, now, after); ok {
			t.Fatalf("StaleThread = (%s, true), want not stale after a fresh reply", since)
		}
	})

	t.Run("waiting on nobody is not stale", func(t *testing.T) {
		th := base()
		th.Attention = nil
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("a thread with an empty attention set must not be stale")
		}
	})

	t.Run("fyi never goes stale", func(t *testing.T) {
		th := base()
		th.Question.Type = store.QuestionTypeFYI
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("an fyi note must not be stale")
		}
	})

	t.Run("resolved never goes stale", func(t *testing.T) {
		th := base()
		th.Question.Status = "resolved"
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("a resolved thread must not be stale")
		}
	})

	t.Run("without a usable timestamp nothing is claimed", func(t *testing.T) {
		th := base()
		th.Question.AskedAt = 0
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("a thread without a timestamp must not be stale since the epoch")
		}
	})

	t.Run("exactly at the threshold is not yet stale", func(t *testing.T) {
		th := base()
		th.Question.AskedAt = now.Add(-after).Unix()
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("the threshold is exclusive, like every other one in this package")
		}
	})
}
