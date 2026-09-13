package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/mirror"
	"github.com/IvanRoslov/rocket/internal/store"
)

// seedMirrorRepo seeds a project whose repo lives under cfg.ReposDir, i.e. a
// mirror the daemon owns and is allowed to fast-forward.
func seedMirrorRepo(t *testing.T, st *store.Store, reposDir, projectID, repoID string) {
	t.Helper()
	if err := st.AddRepo(store.Repo{
		ID:            repoID,
		Path:          filepath.Join(reposDir, repoID),
		DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := st.AddProject(store.Project{ID: projectID, Name: projectID, MainRepo: repoID}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
}

// A worktree is cut from the mirror, so a mirror that is dozens of commits
// behind origin hands the new session a stale branch point. Spawn therefore
// fast-forwards the mirror first — synchronously, before ws.Create.
func TestSpawnSyncsMirrorBeforeWorkspace(t *testing.T) {
	m, st, _, _, ws, cfg := testManagerWithConfig(t)
	cfg.ReposDir = t.TempDir()
	seedMirrorRepo(t, st, cfg.ReposDir, "proj1", "repo1")

	var syncedRepos []string
	var createCallsAtSync int
	m.SetMirrorSyncer(func(ctx context.Context, repo store.Repo) (mirror.SyncResult, error) {
		syncedRepos = append(syncedRepos, repo.ID)
		createCallsAtSync = len(ws.createCalls)
		return mirror.SyncResult{}, nil
	})

	if _, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat", AgentName: "fake",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if len(syncedRepos) != 1 || syncedRepos[0] != "repo1" {
		t.Fatalf("synced repos = %v, want [repo1]", syncedRepos)
	}
	if createCallsAtSync != 0 {
		t.Errorf("workspace.Create ran before the mirror sync (%d calls at sync time), want sync first", createCallsAtSync)
	}
	if len(ws.createCalls) != 1 {
		t.Errorf("workspace.Create calls = %d, want 1", len(ws.createCalls))
	}
}

// `rocket repo add <path>` registers the user's own working copy, which
// rocket promises never to touch (docs/05-state.md). Spawning against one
// must not fast-forward it under its owner.
func TestSpawnSkipsMirrorSyncForUserWorkingCopy(t *testing.T) {
	m, st, _, _, _, cfg := testManagerWithConfig(t)
	cfg.ReposDir = t.TempDir()
	// seedProjectRepo registers a path outside ReposDir.
	seedProjectRepo(t, st, "proj1", "repo1")

	synced := 0
	m.SetMirrorSyncer(func(ctx context.Context, repo store.Repo) (mirror.SyncResult, error) {
		synced++
		return mirror.SyncResult{}, nil
	})

	if _, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat", AgentName: "fake",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if synced != 0 {
		t.Errorf("mirror sync ran %d times on a user working copy, want 0", synced)
	}
}

// The sync is best-effort: a stale mirror is a worse worktree, but a blocked
// spawn is a stopped feature. A failing sync must not fail the spawn.
func TestSpawnProceedsWhenMirrorSyncFails(t *testing.T) {
	m, st, _, _, ws, cfg := testManagerWithConfig(t)
	cfg.ReposDir = t.TempDir()
	seedMirrorRepo(t, st, cfg.ReposDir, "proj1", "repo1")

	m.SetMirrorSyncer(func(ctx context.Context, repo store.Repo) (mirror.SyncResult, error) {
		return mirror.SyncResult{}, errors.New("origin unreachable")
	})

	sess, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat", AgentName: "fake",
	})
	if err != nil {
		t.Fatalf("Spawn returned error for a failed mirror sync: %v", err)
	}
	if sess.State != "running" {
		t.Errorf("session state = %q, want running", sess.State)
	}
	if len(ws.createCalls) != 1 {
		t.Errorf("workspace.Create calls = %d, want 1", len(ws.createCalls))
	}
}

// A wedged fetch must not hang the spawn forever: the sync runs under
// mirrorSyncTimeout, so the syncer always receives a context with a
// deadline rather than the caller's open-ended one.
func TestSpawnMirrorSyncRunsUnderTimeout(t *testing.T) {
	m, st, _, _, _, cfg := testManagerWithConfig(t)
	cfg.ReposDir = t.TempDir()
	seedMirrorRepo(t, st, cfg.ReposDir, "proj1", "repo1")

	var deadline time.Time
	var hasDeadline bool
	m.SetMirrorSyncer(func(ctx context.Context, repo store.Repo) (mirror.SyncResult, error) {
		deadline, hasDeadline = ctx.Deadline()
		return mirror.SyncResult{}, nil
	})

	before := time.Now()
	if _, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat", AgentName: "fake",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if !hasDeadline {
		t.Fatal("mirror syncer got a context with no deadline; a wedged fetch would hang the spawn")
	}
	if got := deadline.Sub(before); got > mirrorSyncTimeout+time.Second || got < mirrorSyncTimeout-time.Second {
		t.Errorf("sync deadline is %v out, want ~%v", got, mirrorSyncTimeout)
	}
}

// A sync cancelled by that timeout is just another failure: logged, and the
// spawn goes on.
func TestSpawnProceedsWhenMirrorSyncIsCancelled(t *testing.T) {
	m, st, _, _, _, cfg := testManagerWithConfig(t)
	cfg.ReposDir = t.TempDir()
	seedMirrorRepo(t, st, cfg.ReposDir, "proj1", "repo1")

	m.SetMirrorSyncer(func(ctx context.Context, repo store.Repo) (mirror.SyncResult, error) {
		return mirror.SyncResult{}, context.DeadlineExceeded
	})

	sess, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat", AgentName: "fake",
	})
	if err != nil {
		t.Fatalf("Spawn returned error for a cancelled mirror sync: %v", err)
	}
	if sess.State != "running" {
		t.Errorf("session state = %q, want running", sess.State)
	}
}

// isMirrorPath is what keeps a user's own working copy out of the sync.
func TestIsMirrorPath(t *testing.T) {
	reposDir := t.TempDir()

	tests := []struct {
		name     string
		path     string
		reposDir string
		want     bool
	}{
		{"clone under repos_dir", filepath.Join(reposDir, "rocket"), reposDir, true},
		{"nested clone under repos_dir", filepath.Join(reposDir, "org", "rocket"), reposDir, true},
		{"user working copy outside repos_dir", filepath.Join(t.TempDir(), "proj"), reposDir, false},
		{"repos_dir itself is not a mirror", reposDir, reposDir, false},
		{"empty repos_dir claims nothing", filepath.Join(reposDir, "rocket"), "", false},
		{"empty path claims nothing", "", reposDir, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMirrorPath(tt.path, tt.reposDir); got != tt.want {
				t.Errorf("isMirrorPath(%q, %q) = %v, want %v", tt.path, tt.reposDir, got, tt.want)
			}
		})
	}
}

// The orchestrator's own worktree is cut from the same mirror, so its spawn
// path needs the same fast-forward.
func TestSpawnOrchestratorSyncsMirror(t *testing.T) {
	m, st, _, _, ws, cfg := testManagerWithConfig(t)
	cfg.ReposDir = t.TempDir()
	seedMirrorRepo(t, st, cfg.ReposDir, "proj1", "repo1")

	var createCallsAtSync = -1
	m.SetMirrorSyncer(func(ctx context.Context, repo store.Repo) (mirror.SyncResult, error) {
		createCallsAtSync = len(ws.createCalls)
		return mirror.SyncResult{}, nil
	})

	proj, err := st.GetProject("proj1")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	task := store.Task{ID: 42, Title: "Ship the thing", ProjectID: "proj1"}

	if _, err := m.SpawnOrchestrator(context.Background(), task, proj, "fake"); err != nil {
		t.Fatalf("SpawnOrchestrator: %v", err)
	}

	if createCallsAtSync != 0 {
		t.Errorf("mirror sync did not run before workspace.Create (createCallsAtSync=%d)", createCallsAtSync)
	}
}

// Restore re-cuts the worktree from the mirror as well, so it needs the same
// fast-forward — a session restored days later must not land on the branch
// point the mirror was stuck at.
func TestRestoreSyncsMirror(t *testing.T) {
	m, st, _, _, ws, cfg := testManagerWithConfig(t)
	cfg.ReposDir = t.TempDir()
	seedMirrorRepo(t, st, cfg.ReposDir, "proj1", "repo1")
	sess := seedRunningSession(t, st, "sess1")
	sess.State = "errored"
	if err := st.UpdateSession(sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	restoreCallsAtSync := -1
	m.SetMirrorSyncer(func(ctx context.Context, repo store.Repo) (mirror.SyncResult, error) {
		restoreCallsAtSync = ws.restoreCalls
		return mirror.SyncResult{}, nil
	})

	if err := m.Restore(context.Background(), "sess1"); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if restoreCallsAtSync != 0 {
		t.Errorf("mirror sync did not run before workspace.Restore (restoreCallsAtSync=%d)", restoreCallsAtSync)
	}
}
