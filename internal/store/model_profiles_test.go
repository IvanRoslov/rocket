package store

import (
	"errors"
	"reflect"
	"testing"
)

func profileNames(ps []ModelProfile) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func TestModelProfileCRUD(t *testing.T) {
	st := openTestStore(t)

	p := ModelProfile{Name: "fast", Agent: "claude-code", Model: "sonnet", Effort: "high",
		Description: "quick work", Enabled: true, Position: 3}
	if err := st.CreateModelProfile(p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := st.GetModelProfile("fast")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Agent != "claude-code" || got.Model != "sonnet" || got.Effort != "high" ||
		got.Description != "quick work" || !got.Enabled || got.Position != 3 {
		t.Errorf("Get = %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("timestamps not set: %+v", got)
	}

	got.Model = "opus"
	got.Enabled = false
	if err := st.UpdateModelProfile(got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got2, _ := st.GetModelProfile("fast")
	if got2.Model != "opus" || got2.Enabled {
		t.Errorf("after update = %+v", got2)
	}
	if !got2.CreatedAt.Equal(got.CreatedAt) {
		t.Errorf("created_at changed on update: %v -> %v", got.CreatedAt, got2.CreatedAt)
	}

	if err := st.DeleteModelProfile("fast"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.GetModelProfile("fast"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete err = %v, want ErrNotFound", err)
	}
}

func TestModelProfileErrors(t *testing.T) {
	st := openTestStore(t)
	p := ModelProfile{Name: "a", Agent: "codex", Enabled: true}
	if err := st.CreateModelProfile(p); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateModelProfile(p); !errors.Is(err, ErrExists) {
		t.Errorf("dup create err = %v, want ErrExists", err)
	}
	if err := st.UpdateModelProfile(ModelProfile{Name: "missing", Agent: "codex"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing err = %v, want ErrNotFound", err)
	}
	if err := st.DeleteModelProfile("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing err = %v, want ErrNotFound", err)
	}
}

func TestListModelProfilesOrder(t *testing.T) {
	st := openTestStore(t)
	for _, p := range []ModelProfile{
		{Name: "zeta", Agent: "codex", Position: 1},
		{Name: "beta", Agent: "codex", Position: 2},
		{Name: "alpha", Agent: "codex", Position: 1},
	} {
		if err := st.CreateModelProfile(p); err != nil {
			t.Fatal(err)
		}
	}
	ps, err := st.ListModelProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := profileNames(ps), []string{"alpha", "zeta", "beta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestSeedModelProfilesClaudeDefault(t *testing.T) {
	st := openTestStore(t)
	if err := st.SeedModelProfiles("claude-code"); err != nil {
		t.Fatal(err)
	}
	ps, _ := st.ListModelProfiles()
	if got, want := profileNames(ps), []string{"claude-opus", "claude-sonnet", "codex"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("seeded = %v, want %v", got, want)
	}
	if ps[0].Agent != "claude-code" || ps[0].Model != "opus" || !ps[0].Enabled || ps[0].Description == "" {
		t.Errorf("claude-opus = %+v", ps[0])
	}
	if ps[1].Model != "sonnet" || ps[2].Agent != "codex" || ps[2].Model != "" {
		t.Errorf("seeds = %+v", ps)
	}
	for _, k := range []string{SettingDefaultOrchestratorProfile, SettingDefaultWorkerProfile} {
		if v, _ := st.GetSetting(k); v != "claude-opus" {
			t.Errorf("%s = %q, want claude-opus", k, v)
		}
	}
	if v, _ := st.GetSetting(SettingModelProfilesSeeded); v == "" {
		t.Error("seed marker not written")
	}
}

func TestSeedModelProfilesCodexDefault(t *testing.T) {
	st := openTestStore(t)
	if err := st.SeedModelProfiles("codex"); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{SettingDefaultOrchestratorProfile, SettingDefaultWorkerProfile} {
		if v, _ := st.GetSetting(k); v != "codex" {
			t.Errorf("%s = %q, want codex", k, v)
		}
	}
}

func TestSeedModelProfilesUnknownAgentLeavesDefaultsUnset(t *testing.T) {
	st := openTestStore(t)
	if err := st.SeedModelProfiles("mystery"); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{SettingDefaultOrchestratorProfile, SettingDefaultWorkerProfile} {
		if _, err := st.GetSetting(k); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s set for unknown agent (err=%v)", k, err)
		}
	}
	if ps, _ := st.ListModelProfiles(); len(ps) != 3 {
		t.Errorf("seeded %d profiles, want 3", len(ps))
	}
}

// Review Focus 4: a human who deleted every profile must not get them back on
// the next daemon start.
func TestSeedModelProfilesOnlyOnce(t *testing.T) {
	st := openTestStore(t)
	if err := st.SeedModelProfiles("claude-code"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"claude-opus", "claude-sonnet", "codex"} {
		if err := st.DeleteModelProfile(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SeedModelProfiles("claude-code"); err != nil {
		t.Fatal(err)
	}
	if ps, _ := st.ListModelProfiles(); len(ps) != 0 {
		t.Errorf("re-seeded: %v", profileNames(ps))
	}
}

func TestSeedModelProfilesSkipsNonEmptyRegistry(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateModelProfile(ModelProfile{Name: "mine", Agent: "codex", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.SeedModelProfiles("claude-code"); err != nil {
		t.Fatal(err)
	}
	ps, _ := st.ListModelProfiles()
	if got := profileNames(ps); !reflect.DeepEqual(got, []string{"mine"}) {
		t.Errorf("profiles = %v, want [mine]", got)
	}
	if v, _ := st.GetSetting(SettingModelProfilesSeeded); v == "" {
		t.Error("seed marker not written")
	}
}

func TestTaskAllowedProfilesRoundTrip(t *testing.T) {
	st := openTestStore(t)
	id, err := st.AddTask(Task{Title: "f", ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	task, _ := st.GetTask(id)
	if task.AllowedProfiles != nil || task.OrchestratorProfile != "" {
		t.Errorf("fresh task = %v / %q", task.AllowedProfiles, task.OrchestratorProfile)
	}

	if err := st.SetTaskAllowedProfiles(id, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	task, _ = st.GetTask(id)
	if !reflect.DeepEqual(task.AllowedProfiles, []string{"a", "b"}) {
		t.Errorf("allowed = %v", task.AllowedProfiles)
	}

	if err := st.SetTaskAllowedProfiles(id, []string{}); err != nil {
		t.Fatal(err)
	}
	task, _ = st.GetTask(id)
	if task.AllowedProfiles != nil {
		t.Errorf("cleared allowed = %v, want nil", task.AllowedProfiles)
	}

	if err := st.SetTaskOrchestratorProfile(id, "claude-opus"); err != nil {
		t.Fatal(err)
	}
	task, _ = st.GetTask(id)
	if task.OrchestratorProfile != "claude-opus" {
		t.Errorf("orchestrator_profile = %q", task.OrchestratorProfile)
	}

	if err := st.SetTaskAllowedProfiles(999, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing task err = %v", err)
	}
	if err := st.SetTaskOrchestratorProfile(999, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing task err = %v", err)
	}
}

func TestSessionProfileSnapshotRoundTrip(t *testing.T) {
	st := openTestStore(t)
	sess := Session{ID: "s1", Kind: "worker", ProjectID: "p", RepoID: "r", FeatureSlug: "f",
		Agent: "claude-code", State: "running", Profile: "claude-opus", Model: "opus", Effort: "high"}
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile != "claude-opus" || got.Model != "opus" || got.Effort != "high" {
		t.Errorf("snapshot = %q/%q/%q", got.Profile, got.Model, got.Effort)
	}
	got.Effort = "max"
	if err := st.UpdateSession(got); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListSessions(SessionFilter{All: true})
	if len(list) != 1 || list[0].Effort != "max" || list[0].Profile != "claude-opus" {
		t.Errorf("listed = %+v", list)
	}
}
