package store

import (
	"errors"
	"sync"
	"testing"
)

func withdrawFixture(t *testing.T) (*Store, CreativeRun) {
	t.Helper()
	s, r := creativeFixture(t, "stage", false)
	for _, id := range []string{"orch", "old", "worker", "unbound"} {
		kind := "orchestrator"
		if id == "worker" {
			kind = "worker"
		}
		if err := s.AddSession(Session{ID: id, Kind: kind, ProjectID: "p", State: "running"}); err != nil {
			t.Fatal(err)
		}
		if id != "unbound" {
			if err := s.BindCreativeSession(id, CreativeBinding{RunID: r.ID}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.SetCreativeRunOrchestrator(r.ID, "orch"); err != nil {
		t.Fatal(err)
	}
	r = creativeAct(t, s, r, r.Stages[0].ID, 0, "start", 0)
	r = creativeAct(t, s, r, r.Stages[0].ID, 0, "submit", 0)
	return s, r
}
func TestCreativeWithdrawAuthorizationAndRevision(t *testing.T) {
	s, r := withdrawFixture(t)
	stage := r.Stages[0].ID
	a := CreativeAction{Action: "withdraw", ReviewRevision: 1, Reason: "Need corrected publication"}
	for _, id := range []string{"", "worker", "old", "missing"} {
		if _, err := s.WithdrawCreativeReview(r.ID, stage, 0, a, id); !errors.Is(err, ErrCreativeForbidden) {
			t.Fatalf("caller %q: %v", id, err)
		}
	}
	for _, human := range []bool{true, false} {
		if _, err := s.ApplyCreativeAction(r.ID, stage, 0, a, human); !errors.Is(err, ErrCreativeForbidden) {
			t.Fatalf("legacy human=%v: %v", human, err)
		}
	}
	bad := a
	bad.Reason = " \n "
	if _, err := s.WithdrawCreativeReview(r.ID, stage, 0, bad, "orch"); !errors.Is(err, ErrCreativeInvalid) {
		t.Fatalf("reason: %v", err)
	}
	bad = a
	bad.ReviewRevision = 2
	if _, err := s.WithdrawCreativeReview(r.ID, stage, 0, bad, "orch"); !errors.Is(err, ErrCreativeConflict) {
		t.Fatalf("revision: %v", err)
	}
	out, err := s.WithdrawCreativeReview(r.ID, stage, 0, a, "orch")
	if err != nil {
		t.Fatal(err)
	}
	if out.Stages[0].Status != "in_progress" || out.Stages[0].ReviewRevision != 1 {
		t.Fatalf("withdraw %+v", out.Stages[0])
	}
	if _, err = s.WithdrawCreativeReview(r.ID, stage, 0, a, "orch"); !errors.Is(err, ErrCreativeConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	out = creativeAct(t, s, out, stage, 0, "submit", 0)
	if out.Stages[0].ReviewRevision != 2 {
		t.Fatal("resubmit reused revision")
	}
	if _, err = s.ApplyCreativeAction(r.ID, stage, 0, CreativeAction{Action: "approve", ReviewRevision: 1}, true); !errors.Is(err, ErrCreativeConflict) {
		t.Fatalf("stale approve: %v", err)
	}
	out = creativeAct(t, s, out, stage, 0, "approve", 2)
	a.ReviewRevision = 2
	if _, err = s.WithdrawCreativeReview(r.ID, stage, 0, a, "orch"); !errors.Is(err, ErrCreativeConflict) {
		t.Fatalf("completed: %v", err)
	}
}
func TestCreativeWithdrawCurrentBoundOrchestrator(t *testing.T) {
	s, r := withdrawFixture(t)
	a := CreativeAction{Action: "withdraw", ReviewRevision: 1, Reason: "Correction"}
	if err := s.SetCreativeRunOrchestrator(r.ID, "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WithdrawCreativeReview(r.ID, r.Stages[0].ID, 0, a, "orch"); !errors.Is(err, ErrCreativeForbidden) {
		t.Fatalf("replaced: %v", err)
	}
	if err := s.SetCreativeRunOrchestrator(r.ID, "unbound"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WithdrawCreativeReview(r.ID, r.Stages[0].ID, 0, a, "unbound"); !errors.Is(err, ErrCreativeForbidden) {
		t.Fatalf("unbound: %v", err)
	}
	if err := s.SetCreativeRunOrchestrator(r.ID, "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE sessions SET state='exited' WHERE id='old'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WithdrawCreativeReview(r.ID, r.Stages[0].ID, 0, a, "old"); !errors.Is(err, ErrCreativeForbidden) {
		t.Fatalf("terminal: %v", err)
	}
}
func TestCreativeWithdrawRacesApprove(t *testing.T) {
	s, r := withdrawFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := s.WithdrawCreativeReview(r.ID, r.Stages[0].ID, 0, CreativeAction{Action: "withdraw", ReviewRevision: 1, Reason: "Correction"}, "orch")
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := s.ApplyCreativeAction(r.ID, r.Stages[0].ID, 0, CreativeAction{Action: "approve", ReviewRevision: 1}, true)
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrCreativeConflict) {
			t.Fatalf("race error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d", successes)
	}
}
