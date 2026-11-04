package store

import (
	"errors"
	"sync"
	"testing"
)

func creativeFixture(t *testing.T, approval string, repeated bool) (*Store, CreativeRun) {
	t.Helper()
	s := openTestStore(t)
	if err := s.AddRepo(Repo{ID: "repo", Path: "/tmp/repo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddProject(Project{ID: "p", MainRepo: "repo"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateCreativePipeline(CreativePipelineInput{ProjectID: "p", Name: "Pipeline", Stages: []CreativeStageDefinition{{Name: "First", Approval: approval, Repeated: repeated}, {Name: "Next", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateCreativeRun(CreativeRunInput{ProjectID: "p", PipelineID: p.ID, Name: "Run", Brief: "Brief"})
	if err != nil {
		t.Fatal(err)
	}
	return s, r
}
func creativeAct(t *testing.T, s *Store, r CreativeRun, stage, item int64, action string, revision int64) CreativeRun {
	t.Helper()
	got, err := s.ApplyCreativeAction(r.ID, stage, item, CreativeAction{Action: action, ReviewRevision: revision}, true)
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	return got
}
func TestCreativeApprovalCannotBeBypassed(t *testing.T) {
	s, r := creativeFixture(t, "stage", false)
	a, b := r.Stages[0].ID, r.Stages[1].ID
	if _, err := s.ApplyCreativeAction(r.ID, b, 0, CreativeAction{Action: "start"}, true); !errors.Is(err, ErrCreativeConflict) {
		t.Fatalf("skip stage: %v", err)
	}
	r = creativeAct(t, s, r, a, 0, "start", 0)
	if _, err := s.UpdateCreativeProgress(r.ID, a, 0, CreativeProgress{Progress: 101}); !errors.Is(err, ErrCreativeInvalid) {
		t.Fatalf("bad progress: %v", err)
	}
	if _, err := s.UpdateCreativeProgress(r.ID, a, 0, CreativeProgress{Progress: 100, ProgressNote: "Ready"}); err != nil {
		t.Fatal(err)
	}
	r = creativeAct(t, s, r, a, 0, "submit", 0)
	if r.Stages[0].Status != "awaiting_approval" || r.Stages[0].ReviewRevision != 1 {
		t.Fatalf("review: %+v", r.Stages[0])
	}
	for _, action := range []string{"start", "submit", "reopen"} {
		if _, err := s.ApplyCreativeAction(r.ID, a, 0, CreativeAction{Action: action}, true); !errors.Is(err, ErrCreativeConflict) {
			t.Fatalf("review bypass %s: %v", action, err)
		}
	}
	if _, err := s.ApplyCreativeAction(r.ID, a, 0, CreativeAction{Action: "approve", ReviewRevision: 1}, false); !errors.Is(err, ErrCreativeForbidden) {
		t.Fatalf("agent approved: %v", err)
	}
	if _, err := s.ApplyCreativeAction(r.ID, a, 0, CreativeAction{Action: "approve", ReviewRevision: 2}, true); !errors.Is(err, ErrCreativeConflict) {
		t.Fatalf("stale approval: %v", err)
	}
	r = creativeAct(t, s, r, a, 0, "reject", 1)
	r = creativeAct(t, s, r, a, 0, "submit", 0)
	if _, err := s.ApplyCreativeAction(r.ID, a, 0, CreativeAction{Action: "approve", ReviewRevision: 1}, true); !errors.Is(err, ErrCreativeConflict) {
		t.Fatalf("old review approved: %v", err)
	}
	r = creativeAct(t, s, r, a, 0, "approve", 2)
	r = creativeAct(t, s, r, b, 0, "start", 0)
	r = creativeAct(t, s, r, b, 0, "submit", 0)
	if r.Status != "completed" {
		t.Fatalf("run status %s", r.Status)
	}
	r = creativeAct(t, s, r, a, 0, "reopen", 0)
	if r.Stages[1].Status != "completed" || r.Stages[0].Status != "in_progress" {
		t.Fatalf("rework reset downstream: %+v", r)
	}
	r = creativeAct(t, s, r, a, 0, "submit", 0)
	if r.Status != "awaiting_approval" || r.Stages[0].ReviewRevision != 3 {
		t.Fatalf("rework bypass: %+v", r)
	}
}
func TestCreativeRepeatedItemGates(t *testing.T) {
	for _, mode := range []string{"auto", "stage", "item"} {
		t.Run(mode, func(t *testing.T) {
			s, r := creativeFixture(t, mode, true)
			stage := r.Stages[0].ID
			first, err := s.CreateCreativeItem(r.ID, stage, CreativeItemInput{Name: "One"})
			if err != nil {
				t.Fatal(err)
			}
			second, err := s.CreateCreativeItem(r.ID, stage, CreativeItemInput{Name: "Two"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.UpdateCreativeItem(r.ID, stage, second.ID, CreativeItemInput{Name: "Second"}); err != nil {
				t.Fatal(err)
			}
			r = creativeAct(t, s, r, stage, 0, "start", 0)
			if _, err = s.ApplyCreativeAction(r.ID, stage, 0, CreativeAction{Action: "submit"}, true); !errors.Is(err, ErrCreativeConflict) {
				t.Fatalf("unfinished stage: %v", err)
			}
			if mode == "item" {
				if _, err = s.ApplyCreativeAction(r.ID, stage, second.ID, CreativeAction{Action: "start"}, true); !errors.Is(err, ErrCreativeConflict) {
					t.Fatalf("skipped item: %v", err)
				}
			}
			r = creativeAct(t, s, r, stage, first.ID, "start", 0)
			r = creativeAct(t, s, r, stage, first.ID, "submit", 0)
			if mode == "item" {
				r = creativeAct(t, s, r, stage, first.ID, "approve", 1)
			}
			r = creativeAct(t, s, r, stage, second.ID, "start", 0)
			r = creativeAct(t, s, r, stage, second.ID, "submit", 0)
			if mode == "item" {
				r = creativeAct(t, s, r, stage, second.ID, "approve", 1)
			}
			r = creativeAct(t, s, r, stage, 0, "submit", 0)
			if mode == "stage" {
				r = creativeAct(t, s, r, stage, 0, "approve", 1)
			}
			if r.Stages[0].Status != "completed" {
				t.Fatal("stage not complete")
			}
			if err = s.DeleteCreativeItem(r.ID, stage, first.ID); !errors.Is(err, ErrCreativeConflict) {
				t.Fatalf("deleted completed item: %v", err)
			}
			if _, err = s.CreateCreativeItem(r.ID, stage, CreativeItemInput{Name: "Late"}); !errors.Is(err, ErrCreativeConflict) {
				t.Fatalf("changed completed stage: %v", err)
			}
			r = creativeAct(t, s, r, stage, first.ID, "reopen", 0)
			if r.Stages[0].Status != "in_progress" || r.Stages[0].Items[1].Status != "completed" {
				t.Fatalf("item rework: %+v", r.Stages[0])
			}
		})
	}
}
func TestCreativeConcurrentSubmit(t *testing.T) {
	s, r := creativeFixture(t, "stage", false)
	id := r.Stages[0].ID
	r = creativeAct(t, s, r, id, 0, "start", 0)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ApplyCreativeAction(r.ID, id, 0, CreativeAction{Action: "submit"}, false)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrCreativeConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", successes, conflicts)
	}
}
func TestCreativeReviewVersionsFailClosed(t *testing.T) {
	s, r := creativeFixture(t, "stage", false)
	id := r.Stages[0].ID
	r = creativeAct(t, s, r, id, 0, "start", 0)
	if _, err := s.ApplyCreativeAction(r.ID, id, 0, CreativeAction{Action: "submit", ArtifactVersionIDs: []int64{42}}, false); err == nil {
		t.Fatal("accepted unavailable version")
	}
	got, err := s.GetCreativeRun(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stages[0].Status != "in_progress" {
		t.Fatal("failed submit changed state")
	}
}
