package store

import (
	"errors"
	"testing"
)

func addGateTestTask(t *testing.T, s *Store) int64 {
	t.Helper()
	if err := s.AddRepo(Repo{ID: "r", Path: "/tmp/r"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := s.AddProject(Project{ID: "p", Name: "p", MainRepo: "r"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	id, err := s.AddTask(Task{Title: "T", ProjectID: "p", Status: "brainstorm"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	return id
}

func putGateTestDoc(t *testing.T, s *Store, taskID int64, kind string) TaskDoc {
	t.Helper()
	d, err := s.PutTaskDoc(TaskDoc{TaskID: taskID, Kind: kind, Title: kind, Body: "b"})
	if err != nil {
		t.Fatalf("PutTaskDoc: %v", err)
	}
	return d
}

func TestRequestTaskGateNoSpec(t *testing.T) {
	s := openTestStore(t)
	id := addGateTestTask(t, s)
	putGateTestDoc(t, s, id, "plan")

	if _, _, err := s.RequestTaskGate(id, "orch"); !errors.Is(err, ErrNoSpec) {
		t.Fatalf("err = %v, want ErrNoSpec", err)
	}
}

func TestRequestTaskGateSnapshotsLatestVersions(t *testing.T) {
	s := openTestStore(t)
	id := addGateTestTask(t, s)
	putGateTestDoc(t, s, id, "spec")
	putGateTestDoc(t, s, id, "spec")

	g, superseded, err := s.RequestTaskGate(id, "orch")
	if err != nil {
		t.Fatalf("RequestTaskGate: %v", err)
	}
	if len(superseded) != 0 {
		t.Errorf("superseded = %v, want none", superseded)
	}
	if g.ID == 0 || g.TaskID != id || g.SpecVersion != 2 || g.PlanVersion != nil {
		t.Errorf("gate = %+v, want spec v2, no plan", g)
	}
	if g.Status != "pending" || g.RequestedBy != "orch" || g.RequestedAt == 0 || g.DecidedAt != nil {
		t.Errorf("gate = %+v", g)
	}

	putGateTestDoc(t, s, id, "plan")
	g2, superseded, err := s.RequestTaskGate(id, "orch")
	if err != nil {
		t.Fatalf("RequestTaskGate 2: %v", err)
	}
	if g2.PlanVersion == nil || *g2.PlanVersion != 1 {
		t.Errorf("plan version = %v, want 1", g2.PlanVersion)
	}
	if len(superseded) != 1 || superseded[0].ID != g.ID || superseded[0].Status != "superseded" {
		t.Errorf("superseded = %+v, want first gate", superseded)
	}
	old, err := s.GetTaskGate(g.ID)
	if err != nil {
		t.Fatalf("GetTaskGate: %v", err)
	}
	if old.Status != "superseded" {
		t.Errorf("old status = %q, want superseded", old.Status)
	}
}

func TestDecideTaskGate(t *testing.T) {
	s := openTestStore(t)
	id := addGateTestTask(t, s)
	putGateTestDoc(t, s, id, "spec")
	g, _, err := s.RequestTaskGate(id, "orch")
	if err != nil {
		t.Fatalf("RequestTaskGate: %v", err)
	}

	d, err := s.DecideTaskGate(g.ID, "changes", "fix it", "user")
	if err != nil {
		t.Fatalf("DecideTaskGate: %v", err)
	}
	if d.Status != "changes" || d.Comment != "fix it" || d.DecidedBy != "user" || d.DecidedAt == nil {
		t.Errorf("decided = %+v", d)
	}

	if _, err := s.DecideTaskGate(g.ID, "go", "", "user"); !errors.Is(err, ErrGateNotPending) {
		t.Errorf("second decide err = %v, want ErrGateNotPending", err)
	}
	if _, err := s.DecideTaskGate(9999, "go", "", "user"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing gate err = %v, want ErrNotFound", err)
	}
}

func TestSupersedePendingGatesOnlyTouchesPending(t *testing.T) {
	s := openTestStore(t)
	id := addGateTestTask(t, s)
	putGateTestDoc(t, s, id, "spec")
	g1, _, _ := s.RequestTaskGate(id, "orch")
	if _, err := s.DecideTaskGate(g1.ID, "changes", "c", "user"); err != nil {
		t.Fatalf("DecideTaskGate: %v", err)
	}
	g2, _, _ := s.RequestTaskGate(id, "orch")

	got, err := s.SupersedePendingGates(id)
	if err != nil {
		t.Fatalf("SupersedePendingGates: %v", err)
	}
	if len(got) != 1 || got[0].ID != g2.ID || got[0].Status != "superseded" {
		t.Errorf("superseded = %+v, want only g2", got)
	}

	gates, err := s.ListTaskGates(id)
	if err != nil {
		t.Fatalf("ListTaskGates: %v", err)
	}
	if len(gates) != 2 || gates[0].ID != g2.ID || gates[1].ID != g1.ID {
		t.Fatalf("gates = %+v, want newest first", gates)
	}
	if gates[1].Status != "changes" {
		t.Errorf("decided gate status = %q, want changes", gates[1].Status)
	}

	again, err := s.SupersedePendingGates(id)
	if err != nil || len(again) != 0 {
		t.Errorf("second supersede = %v, %v; want none", again, err)
	}
}

// A spec written without going through the supersede path (or whose
// supersede failed) must still stop a Go on the old version.
func TestDecideTaskGateStaleSpecSupersedes(t *testing.T) {
	s := openTestStore(t)
	id := addGateTestTask(t, s)
	putGateTestDoc(t, s, id, "spec")
	g, _, err := s.RequestTaskGate(id, "orch")
	if err != nil {
		t.Fatalf("RequestTaskGate: %v", err)
	}
	putGateTestDoc(t, s, id, "spec")

	got, err := s.DecideTaskGate(g.ID, "go", "", "user")
	if !errors.Is(err, ErrGateSuperseded) {
		t.Fatalf("err = %v, want ErrGateSuperseded", err)
	}
	if got.Status != "superseded" {
		t.Errorf("returned status = %q, want superseded", got.Status)
	}
	stored, _ := s.GetTaskGate(g.ID)
	if stored.Status != "superseded" || stored.DecidedAt != nil {
		t.Errorf("stored = %+v, want superseded, undecided", stored)
	}
}

func TestReopenTaskGate(t *testing.T) {
	s := openTestStore(t)
	id := addGateTestTask(t, s)
	putGateTestDoc(t, s, id, "spec")
	g, _, _ := s.RequestTaskGate(id, "orch")
	if _, err := s.DecideTaskGate(g.ID, "go", "", "user"); err != nil {
		t.Fatalf("DecideTaskGate: %v", err)
	}
	if err := s.ReopenTaskGate(g.ID); err != nil {
		t.Fatalf("ReopenTaskGate: %v", err)
	}
	got, _ := s.GetTaskGate(g.ID)
	if got.Status != "pending" || got.DecidedBy != "" || got.DecidedAt != nil || got.Comment != "" {
		t.Errorf("reopened = %+v, want clean pending", got)
	}
	if _, err := s.DecideTaskGate(g.ID, "go", "", "user"); err != nil {
		t.Errorf("decide after reopen: %v", err)
	}
}

func TestOnePendingGatePerTask(t *testing.T) {
	s := openTestStore(t)
	id := addGateTestTask(t, s)
	putGateTestDoc(t, s, id, "spec")
	if _, _, err := s.RequestTaskGate(id, "orch"); err != nil {
		t.Fatalf("RequestTaskGate: %v", err)
	}
	_, err := s.db.Exec(`INSERT INTO task_gates (task_id, spec_version, status, requested_at) VALUES (?, 1, 'pending', 1)`, id)
	if err == nil {
		t.Fatal("second pending gate inserted, want unique violation")
	}
}

func TestRequestTaskGateConcurrent(t *testing.T) {
	s := openTestStore(t)
	id := addGateTestTask(t, s)
	putGateTestDoc(t, s, id, "spec")

	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, _, err := s.RequestTaskGate(id, "orch")
			errs <- err
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent request: %v", err)
		}
	}
	gates, _ := s.ListTaskGates(id)
	pending := 0
	for _, g := range gates {
		if g.Status == "pending" {
			pending++
		}
	}
	if len(gates) != n || pending != 1 {
		t.Errorf("gates = %d, pending = %d; want %d and 1", len(gates), pending, n)
	}
}
