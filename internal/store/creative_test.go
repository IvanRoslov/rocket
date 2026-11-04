package store

import "testing"

func TestCreativePipelineSnapshot(t *testing.T) {
	s := openTestStore(t)
	if err := s.AddRepo(Repo{ID: "repo", Path: "/tmp/repo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddProject(Project{ID: "project", Name: "Project", MainRepo: "repo"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateCreativePipeline(CreativePipelineInput{ProjectID: "project", Name: "Video", Stages: []CreativeStageDefinition{{Name: "Plan", Description: "Write plan", ExpectedArtifacts: "Plan text", Approval: "stage"}, {Name: "Scenes", Repeated: true, Approval: "item"}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateCreativeRun(CreativeRunInput{ProjectID: "project", PipelineID: p.ID, Name: "Launch", Brief: "Brief"})
	if err != nil {
		t.Fatal(err)
	}
	p.Stages[0].Name = "Changed"
	p.Stages[0], p.Stages[1] = p.Stages[1], p.Stages[0]
	if _, err = s.UpdateCreativePipeline(p.ID, CreativePipelineInput{ProjectID: p.ProjectID, Name: p.Name, Stages: p.Stages}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCreativeRun(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Stages) != 2 || got.Stages[0].Name != "Plan" || got.Stages[0].Approval != "stage" || got.Status != "pending" {
		t.Fatalf("snapshot: %+v", got)
	}
	tasks, err := s.ListTasks(TaskFilter{})
	if err != nil || len(tasks) != 0 {
		t.Fatalf("creative leaked into tasks: %v %v", tasks, err)
	}
}

func TestCreativeContextAndAssociation(t *testing.T) {
	s := openTestStore(t)
	if err := s.AddRepo(Repo{ID: "r", Path: "/tmp/r"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddProject(Project{ID: "p", MainRepo: "r"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateCreativePipeline(CreativePipelineInput{ProjectID: "p", Name: "P", Stages: []CreativeStageDefinition{{Name: "A", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateCreativeRun(CreativeRunInput{ProjectID: "p", PipelineID: p.ID, Name: "A", Brief: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateCreativeRun(CreativeRunInput{ProjectID: "p", PipelineID: p.ID, Name: "B", Brief: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveCreativeContext(a.ID, b.Stages[0].ID, 0); err == nil {
		t.Fatal("accepted cross-run stage")
	}
	if _, err = s.ResolveCreativeContext(a.ID, 0, 1); err == nil {
		t.Fatal("accepted item without stage")
	}
	if err = s.AddSession(Session{ID: "creative", Kind: "orchestrator", ProjectID: "p", Agent: "fake", State: "running"}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetCreativeRunOrchestrator(a.ID, "creative"); err != nil {
		t.Fatal(err)
	}
	c, err := s.ResolveCreativeContext(a.ID, a.Stages[0].ID, 0)
	if err != nil || c.ProjectID != "p" || c.OrchestratorSessionID != "creative" {
		t.Fatalf("context: %+v %v", c, err)
	}
	if err = s.SetCreativeRunOrchestrator(a.ID, "missing"); err == nil {
		t.Fatal("accepted nonexistent session")
	}
}
