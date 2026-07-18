package session

import (
	"context"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// TestReconcileRunningSessionTmuxMissing tests that a running session
// with missing tmux is marked errored + event published.
func TestReconcileRunningSessionTmuxMissing(t *testing.T) {
	m, st, b, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	// Create a running session
	sess := store.Session{
		ID:           "sess1",
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat1",
		Agent:        "fake",
		Branch:       "feature/feat1/task1",
		WorktreePath: "/tmp/wt/sess1",
		TmuxName:     "sess1",
		State:        "running",
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	// Tmux list should be empty (tmux session doesn't exist)
	rt.listNames = []string{}

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Session should be marked errored
	stored, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.State != "errored" {
		t.Errorf("State = %q, want errored", stored.State)
	}

	// Event should be published with reason tmux_missing
	events := drainEvents(ch)
	var found bool
	for _, e := range events {
		if e.Type == "session.state_changed" && e.SessionID == "sess1" {
			found = true
			if e.Data["to"] != "errored" {
				t.Errorf("event to = %q, want errored", e.Data["to"])
			}
			if e.Data["reason"] != "tmux_missing" {
				t.Errorf("event reason = %q, want tmux_missing", e.Data["reason"])
			}
		}
	}
	if !found {
		t.Errorf("expected session.state_changed event not found. events: %v", events)
	}
}

// TestReconcileRunningSessionWorktreeMissing tests that a running session
// with missing worktree (but tmux alive) is marked errored + event published.
func TestReconcileRunningSessionWorktreeMissing(t *testing.T) {
	m, st, b, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	// Create a running session with non-existent worktree path
	sess := store.Session{
		ID:           "sess1",
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat1",
		Agent:        "fake",
		Branch:       "feature/feat1/task1",
		WorktreePath: "/nonexistent/wt/sess1",
		TmuxName:     "sess1",
		State:        "running",
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	// Tmux session exists
	rt.listNames = []string{"sess1"}

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Session should be marked errored
	stored, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.State != "errored" {
		t.Errorf("State = %q, want errored", stored.State)
	}

	// Event should be published with reason worktree_missing
	events := drainEvents(ch)
	var found bool
	for _, e := range events {
		if e.Type == "session.state_changed" && e.SessionID == "sess1" {
			found = true
			if e.Data["to"] != "errored" {
				t.Errorf("event to = %q, want errored", e.Data["to"])
			}
			if e.Data["reason"] != "worktree_missing" {
				t.Errorf("event reason = %q, want worktree_missing", e.Data["reason"])
			}
		}
	}
	if !found {
		t.Errorf("expected session.state_changed event not found. events: %v", events)
	}
}

// TestReconcileRunningSessionHealthy tests that a running session
// with tmux alive and worktree present is left untouched.
func TestReconcileRunningSessionHealthy(t *testing.T) {
	m, st, b, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	// Create a running session with valid worktree
	wtPath := t.TempDir()
	sess := store.Session{
		ID:           "sess1",
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat1",
		Agent:        "fake",
		Branch:       "feature/feat1/task1",
		WorktreePath: wtPath,
		TmuxName:     "sess1",
		State:        "running",
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	// Tmux session exists
	rt.listNames = []string{"sess1"}

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Session should still be running
	stored, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.State != "running" {
		t.Errorf("State = %q, want running", stored.State)
	}

	// No state change event should be published
	events := drainEvents(ch)
	for _, e := range events {
		if e.Type == "session.state_changed" && e.SessionID == "sess1" {
			t.Errorf("unexpected state_changed event for healthy session: %+v", e)
		}
	}
}

// TestReconcileOrphanTmuxSession tests that orphan tmux sessions
// (live but not in store) result in a single reconcile.orphan_tmux event.
func TestReconcileOrphanTmuxSession(t *testing.T) {
	m, st, b, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	// Create one stored session
	sess := store.Session{
		ID:           "sess1",
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat1",
		Agent:        "fake",
		Branch:       "feature/feat1/task1",
		WorktreePath: t.TempDir(),
		TmuxName:     "sess1",
		State:        "running",
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	// Add orphan tmux sessions (live but not in store)
	rt := &fakeRuntime{}
	m.rt = rt
	rt.listNames = []string{"sess1", "orphan-1", "orphan-2"}

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Should publish single reconcile.orphan_tmux event with both orphan names
	events := drainEvents(ch)
	var found bool
	for _, e := range events {
		if e.Type == "reconcile.orphan_tmux" {
			found = true
			names := e.Data["names"].([]string)
			if len(names) != 2 {
				t.Errorf("orphan names count = %d, want 2", len(names))
			}
			// Check names are present (order may vary)
			hasOrphan1 := false
			hasOrphan2 := false
			for _, name := range names {
				if name == "orphan-1" {
					hasOrphan1 = true
				}
				if name == "orphan-2" {
					hasOrphan2 = true
				}
			}
			if !hasOrphan1 {
				t.Errorf("orphan-1 not found in names: %v", names)
			}
			if !hasOrphan2 {
				t.Errorf("orphan-2 not found in names: %v", names)
			}
		}
	}
	if !found {
		t.Errorf("expected reconcile.orphan_tmux event not found. events: %v", events)
	}
}

// TestReconcileTerminalSessionsIgnored tests that sessions in terminal
// states (killed, done, errored) are not checked.
func TestReconcileTerminalSessionsIgnored(t *testing.T) {
	m, st, b, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	// Create a killed session with missing tmux
	sess := store.Session{
		ID:           "killed-sess",
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat1",
		Agent:        "fake",
		Branch:       "feature/feat1/task1",
		WorktreePath: "/nonexistent",
		TmuxName:     "killed-sess",
		State:        "killed",
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	// Create a done session with missing tmux
	sess2 := store.Session{
		ID:           "done-sess",
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat2",
		Agent:        "fake",
		Branch:       "feature/feat2/task2",
		WorktreePath: "/nonexistent",
		TmuxName:     "done-sess",
		State:        "done",
	}
	if err := st.AddSession(sess2); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	// Tmux list is empty
	rt.listNames = []string{}

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// No state changes should happen for terminal sessions
	events := drainEvents(ch)
	for _, e := range events {
		if e.Type == "session.state_changed" {
			t.Errorf("unexpected state_changed event for terminal session: %+v", e)
		}
	}

	// Terminal sessions should still be in their original states
	stored, err := st.GetSession("killed-sess")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.State != "killed" {
		t.Errorf("killed session state changed to %q", stored.State)
	}

	stored2, err := st.GetSession("done-sess")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored2.State != "done" {
		t.Errorf("done session state changed to %q", stored2.State)
	}
}

// TestReconcileMultipleSessions tests reconciliation of multiple sessions
// with mixed health states.
func TestReconcileMultipleSessions(t *testing.T) {
	m, st, b, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	// Session 1: running, tmux missing
	sess1 := store.Session{
		ID:           "sess1",
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat1",
		Agent:        "fake",
		Branch:       "feature/feat1/task1",
		WorktreePath: t.TempDir(),
		TmuxName:     "sess1",
		State:        "running",
	}
	if err := st.AddSession(sess1); err != nil {
		t.Fatalf("AddSession sess1: %v", err)
	}

	// Session 2: running, both tmux and worktree present
	sess2 := store.Session{
		ID:           "sess2",
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat2",
		Agent:        "fake",
		Branch:       "feature/feat2/task2",
		WorktreePath: t.TempDir(),
		TmuxName:     "sess2",
		State:        "running",
	}
	if err := st.AddSession(sess2); err != nil {
		t.Fatalf("AddSession sess2: %v", err)
	}

	// Session 3: spawning, tmux missing
	sess3 := store.Session{
		ID:           "sess3",
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat3",
		Agent:        "fake",
		Branch:       "feature/feat3/task3",
		WorktreePath: t.TempDir(),
		TmuxName:     "sess3",
		State:        "spawning",
	}
	if err := st.AddSession(sess3); err != nil {
		t.Fatalf("AddSession sess3: %v", err)
	}

	// Tmux list has sess2 and orphan-1
	rt.listNames = []string{"sess2", "orphan-1"}

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// sess1 should be errored (tmux missing)
	stored1, _ := st.GetSession("sess1")
	if stored1.State != "errored" {
		t.Errorf("sess1 state = %q, want errored", stored1.State)
	}

	// sess2 should still be running
	stored2, _ := st.GetSession("sess2")
	if stored2.State != "running" {
		t.Errorf("sess2 state = %q, want running", stored2.State)
	}

	// sess3 should be errored (tmux missing)
	stored3, _ := st.GetSession("sess3")
	if stored3.State != "errored" {
		t.Errorf("sess3 state = %q, want errored", stored3.State)
	}

	// Should have session.state_changed for sess1 and sess3, and one reconcile.orphan_tmux
	events := drainEvents(ch)
	stateChanges := 0
	orphanCount := 0
	for _, e := range events {
		if e.Type == "session.state_changed" {
			stateChanges++
		}
		if e.Type == "reconcile.orphan_tmux" {
			orphanCount++
		}
	}
	if stateChanges != 2 {
		t.Errorf("state_changed events = %d, want 2", stateChanges)
	}
	if orphanCount != 1 {
		t.Errorf("reconcile.orphan_tmux events = %d, want 1", orphanCount)
	}
}

// TestReconcileNoOrphanEventWhenNoneExist tests that no orphan event
// is published when there are no orphan tmux sessions.
func TestReconcileNoOrphanEventWhenNoneExist(t *testing.T) {
	m, st, b, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	// Create sessions that match tmux list exactly
	sess1 := store.Session{
		ID:           "sess1",
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat1",
		Agent:        "fake",
		Branch:       "feature/feat1/task1",
		WorktreePath: t.TempDir(),
		TmuxName:     "sess1",
		State:        "running",
	}
	if err := st.AddSession(sess1); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	// Tmux list matches exactly
	rt.listNames = []string{"sess1"}

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// No reconcile.orphan_tmux event should be published
	events := drainEvents(ch)
	for _, e := range events {
		if e.Type == "reconcile.orphan_tmux" {
			t.Errorf("unexpected reconcile.orphan_tmux event: %+v", e)
		}
	}
}

// TestReconcileKilledSessionTmuxNameNotOrphan tests that a tmux session
// whose name belongs to a killed store session is NOT reported as an orphan.
func TestReconcileKilledSessionTmuxNameNotOrphan(t *testing.T) {
	m, st, b, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	// Create a killed session
	sess := store.Session{
		ID:           "killed-sess",
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat1",
		Agent:        "fake",
		Branch:       "feature/feat1/task1",
		WorktreePath: "/nonexistent",
		TmuxName:     "killed-sess",
		State:        "killed",
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	// The tmux session exists in the runtime (this shouldn't normally happen,
	// but if it does, it should not be reported as an orphan)
	rt.listNames = []string{"killed-sess"}

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// No reconcile.orphan_tmux event should be published
	// (killed-sess is in storedSet even though killed session is not checked)
	events := drainEvents(ch)
	for _, e := range events {
		if e.Type == "reconcile.orphan_tmux" {
			t.Errorf("unexpected reconcile.orphan_tmux event for killed session's tmux: %+v", e)
		}
	}
}
