package session

import (
	"context"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// TestSpawnWithProfileFillsSpecAndSnapshot: a worker spawned with a resolved
// profile launches its agent with that model and effort and records the
// profile as the session's snapshot.
func TestSpawnWithProfileFillsSpecAndSnapshot(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	sess, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat", AgentName: "fake",
		Profile: LaunchProfile{Name: "deep", Model: "opus", Effort: "high"},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	spec := testFakeAgent.launchCalls[0]
	if spec.Model != "opus" || spec.Effort != "high" {
		t.Errorf("launch model/effort = %q/%q, want opus/high", spec.Model, spec.Effort)
	}
	assertSnapshot(t, st, sess.ID, "deep", "opus", "high")
}

// TestSpawnWithoutProfileIsLegacy: the zero LaunchProfile launches with the
// agent's own defaults and leaves the snapshot empty.
func TestSpawnWithoutProfileIsLegacy(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	sess, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat", AgentName: "fake",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	spec := testFakeAgent.launchCalls[0]
	if spec.Model != "" || spec.Effort != "" {
		t.Errorf("launch model/effort = %q/%q, want empty", spec.Model, spec.Effort)
	}
	assertSnapshot(t, st, sess.ID, "", "", "")
}

func TestSpawnOrchestratorWithProfile(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	proj, err := st.GetProject("proj1")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	task := store.Task{ID: 7, Title: "Pick a model", ProjectID: "proj1"}

	sess, err := m.SpawnOrchestrator(context.Background(), task, proj, "fake",
		LaunchProfile{Name: "orch", Model: "opus", Effort: "max"})
	if err != nil {
		t.Fatalf("SpawnOrchestrator: %v", err)
	}
	spec := testFakeAgent.launchCalls[0]
	if spec.Model != "opus" || spec.Effort != "max" {
		t.Errorf("launch model/effort = %q/%q, want opus/max", spec.Model, spec.Effort)
	}
	assertSnapshot(t, st, sess.ID, "orch", "opus", "max")
}

// TestRestoreUsesSessionSnapshot: restore relaunches with the model and
// effort the session was started with, even after its profile left the
// registry — it never re-resolves through the policy.
func TestRestoreUsesSessionSnapshot(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	if err := st.CreateModelProfile(store.ModelProfile{Name: "deep", Agent: "fake", Model: "opus", Effort: "high", Enabled: true}); err != nil {
		t.Fatalf("CreateModelProfile: %v", err)
	}

	sess, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat", AgentName: "fake",
		Profile: LaunchProfile{Name: "deep", Model: "opus", Effort: "high"},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := st.DeleteModelProfile("deep"); err != nil {
		t.Fatalf("DeleteModelProfile: %v", err)
	}
	if err := m.Kill(context.Background(), sess.ID, false); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	if err := m.Restore(context.Background(), sess.ID); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	last := testFakeAgent.launchCalls[len(testFakeAgent.launchCalls)-1]
	if last.Model != "opus" || last.Effort != "high" {
		t.Errorf("restore model/effort = %q/%q, want opus/high", last.Model, last.Effort)
	}
	assertSnapshot(t, st, sess.ID, "deep", "opus", "high")
}

func assertSnapshot(t *testing.T, st *store.Store, id, profile, model, effort string) {
	t.Helper()
	got, err := st.GetSession(id)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Profile != profile || got.Model != model || got.Effort != effort {
		t.Errorf("snapshot = %q/%q/%q, want %q/%q/%q", got.Profile, got.Model, got.Effort, profile, model, effort)
	}
}
