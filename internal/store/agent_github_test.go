package store

import (
	"path/filepath"
	"testing"
)

// setupAgentGH opens a store with a project and one agent role, returning the
// store and the role id.
func setupAgentGH(t *testing.T) (*Store, string) {
	t.Helper()

	st, err := Open(filepath.Join(t.TempDir(), "rocket.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	addAgentFixtures(t, st)
	if err := st.AddAgent(Agent{ID: "sre", ProjectID: "platform", PromptPath: "role.md", Enabled: true}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	return st, "sre"
}

func TestAgentGHWatermarkRoundTrip(t *testing.T) {
	st, role := setupAgentGH(t)

	since, err := st.AgentGHWatermark(role, "o/r")
	if err != nil {
		t.Fatalf("AgentGHWatermark: %v", err)
	}
	if since != 0 {
		t.Fatalf("expected unseeded watermark 0, got %d", since)
	}

	if err := st.SetAgentGHWatermark(role, "o/r", 1000); err != nil {
		t.Fatalf("SetAgentGHWatermark: %v", err)
	}
	since, err = st.AgentGHWatermark(role, "o/r")
	if err != nil {
		t.Fatalf("AgentGHWatermark: %v", err)
	}
	if since != 1000 {
		t.Fatalf("expected watermark 1000, got %d", since)
	}

	// Overwriting an existing watermark keeps a single row.
	if err := st.SetAgentGHWatermark(role, "o/r", 2000); err != nil {
		t.Fatalf("SetAgentGHWatermark (update): %v", err)
	}
	since, _ = st.AgentGHWatermark(role, "o/r")
	if since != 2000 {
		t.Fatalf("expected watermark 2000, got %d", since)
	}

	// Watermarks are per repo.
	other, err := st.AgentGHWatermark(role, "o/other")
	if err != nil {
		t.Fatalf("AgentGHWatermark (other repo): %v", err)
	}
	if other != 0 {
		t.Fatalf("expected other repo unseeded, got %d", other)
	}
}

func TestAgentGHSeenIsIdempotent(t *testing.T) {
	st, role := setupAgentGH(t)

	first, err := st.MarkAgentGHSeen(role, "o/r", GHSeenIssue, 7)
	if err != nil {
		t.Fatalf("MarkAgentGHSeen: %v", err)
	}
	if !first {
		t.Fatal("expected the first mark to report a fresh id")
	}

	again, err := st.MarkAgentGHSeen(role, "o/r", GHSeenIssue, 7)
	if err != nil {
		t.Fatalf("MarkAgentGHSeen (repeat): %v", err)
	}
	if again {
		t.Fatal("expected the second mark to report an already-seen id")
	}

	// The same numeric id in a different kind, repo or role is a distinct row:
	// issue numbers and comment ids share a namespace otherwise.
	for _, tc := range []struct{ role, repo, kind string }{
		{role, "o/r", GHSeenComment},
		{role, "o/other", GHSeenIssue},
	} {
		fresh, err := st.MarkAgentGHSeen(tc.role, tc.repo, tc.kind, 7)
		if err != nil {
			t.Fatalf("MarkAgentGHSeen(%v): %v", tc, err)
		}
		if !fresh {
			t.Fatalf("expected %v to be a distinct unseen row", tc)
		}
	}
}

func TestPruneAgentGHSeen(t *testing.T) {
	st, role := setupAgentGH(t)

	if _, err := st.MarkAgentGHSeenAt(role, "o/r", GHSeenIssue, 7, 500); err != nil {
		t.Fatalf("MarkAgentGHSeenAt: %v", err)
	}
	if _, err := st.MarkAgentGHSeenAt(role, "o/r", GHSeenIssue, 8, 1500); err != nil {
		t.Fatalf("MarkAgentGHSeenAt: %v", err)
	}

	if err := st.PruneAgentGHSeen(1000); err != nil {
		t.Fatalf("PruneAgentGHSeen: %v", err)
	}

	pruned, err := st.MarkAgentGHSeen(role, "o/r", GHSeenIssue, 7)
	if err != nil {
		t.Fatalf("MarkAgentGHSeen: %v", err)
	}
	if !pruned {
		t.Fatal("expected the pruned id to look unseen again")
	}
	kept, err := st.MarkAgentGHSeen(role, "o/r", GHSeenIssue, 8)
	if err != nil {
		t.Fatalf("MarkAgentGHSeen: %v", err)
	}
	if kept {
		t.Fatal("expected the recent id to survive pruning")
	}
}
