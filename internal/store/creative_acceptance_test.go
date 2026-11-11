package store

import (
	"errors"
	"testing"
)

func acceptanceRun(t *testing.T) (*Store, CreativeRun) {
	t.Helper()
	s := openTestStore(t)
	if err := s.AddRepo(Repo{ID: "r", Path: "/tmp/r"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddProject(Project{ID: "p", MainRepo: "r"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateCreativePipeline(CreativePipelineInput{Name: "P", Stages: []CreativeStageDefinition{{Name: "A", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateCreativeRun(CreativeRunInput{ProjectID: "p", PipelineID: p.ID, Name: "A", Brief: "Make A"})
	if err != nil {
		t.Fatal(err)
	}
	return s, r
}
func completeAcceptanceRun(t *testing.T, s *Store, r CreativeRun) CreativeRun {
	t.Helper()
	for _, action := range []string{"start", "submit"} {
		var err error
		r, err = s.ApplyCreativeAction(r.ID, r.Stages[0].ID, 0, CreativeAction{Action: action}, false)
		if err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// Removing the eligibility check or conflating stage completion with acceptance breaks this contract.
func TestCreativeAcceptanceEligibilityAndDurability(t *testing.T) {
	s, r := acceptanceRun(t)
	if _, err := s.BeginCreativeAcceptance(r.ID, true); !errors.Is(err, ErrCreativeConflict) {
		t.Fatalf("unfinished: %v", err)
	}
	r = completeAcceptanceRun(t, s, r)
	got, _ := s.GetCreativeRun(r.ID)
	if got.ClosureState != "open" || got.Status != "completed" || got.AcceptedAt != 0 {
		t.Fatalf("legacy completed must stay open: %+v", got)
	}
	if _, err := s.BeginCreativeAcceptance(r.ID, false); !errors.Is(err, ErrCreativeForbidden) {
		t.Fatalf("agent: %v", err)
	}
	got, err := s.BeginCreativeAcceptance(r.ID, true)
	if err != nil || got.ClosureState != "closing" {
		t.Fatalf("begin: %+v %v", got, err)
	}
	if err = s.RecordCreativeClosureError(r.ID, "stop failed"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetCreativeRun(r.ID)
	if got.ClosureError != "stop failed" || got.AcceptedAt != 0 {
		t.Fatalf("lost error: %+v", got)
	}
	if err = s.FinishCreativeAcceptance(r.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetCreativeRun(r.ID)
	if got.ClosureState != "accepted" || got.AcceptedAt == 0 || got.ClosureError != "" {
		t.Fatalf("finish: %+v", got)
	}
	again, err := s.BeginCreativeAcceptance(r.ID, true)
	if err != nil || again.AcceptedAt != got.AcceptedAt {
		t.Fatalf("duplicate: %+v %v", again, err)
	}
}

// A stale write must fail for both incomplete closure and a finished archive.
func TestCreativeAcceptanceFreezesWork(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(map[bool]string{false: "closing", true: "accepted"}[accepted], func(t *testing.T) {
			s, r := acceptanceRun(t)
			r = completeAcceptanceRun(t, s, r)
			if _, err := s.BeginCreativeAcceptance(r.ID, true); err != nil {
				t.Fatal(err)
			}
			if accepted {
				if err := s.FinishCreativeAcceptance(r.ID); err != nil {
					t.Fatal(err)
				}
			}
			checks := []struct {
				name  string
				write func() error
			}{
				{"reopen", func() error {
					_, e := s.ApplyCreativeAction(r.ID, r.Stages[0].ID, 0, CreativeAction{Action: "reopen"}, true)
					return e
				}},
				{"feed", func() error {
					_, e := s.AddCreativeFeedEntry(CreativeFeedEntry{RunID: r.ID, Author: "human", Kind: "message", Body: "late"}, 0)
					return e
				}},
				{"profiles", func() error { return s.SetCreativeRunAllowedProfiles(r.ID, []string{"changed"}) }},
				{"message", func() error {
					_, e := s.AddCreativeMessage(Message{ToSession: "none", Body: "late"}, CreativeBinding{RunID: r.ID})
					return e
				}},
				{"question", func() error {
					_, e := s.AddCreativeQuestion(Question{AskedBy: "none", Body: "late"}, CreativeBinding{RunID: r.ID})
					return e
				}},
			}
			for _, c := range checks {
				t.Run(c.name, func(t *testing.T) {
					if err := c.write(); !errors.Is(err, ErrCreativeConflict) {
						t.Fatalf("write: %v", err)
					}
				})
			}
			if _, err := s.ResolveCreativeContext(r.ID, r.Stages[0].ID, 0); err != nil {
				t.Fatalf("archive context: %v", err)
			}
			if _, err := s.ListCreativeFeed(CreativeBinding{RunID: r.ID}); err != nil {
				t.Fatalf("archive feed: %v", err)
			}
		})
	}
}

// Retention and generic write APIs must not erase or extend archived history.
func TestCreativeAcceptanceGenericHistoryAndRetention(t *testing.T) {
	s, r := acceptanceRun(t)
	if err := s.AddSession(Session{ID: "orch", Kind: "orchestrator", ProjectID: "p", Agent: "fake", State: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := s.BindCreativeSession("orch", CreativeBinding{RunID: r.ID}); err != nil {
		t.Fatal(err)
	}
	msg, err := s.AddCreativeMessage(Message{ToSession: "orch", Body: "retained result discussion"}, CreativeBinding{RunID: r.ID})
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.AddCreativeQuestion(Question{AskedBy: "orch", Body: "retained decision"}, CreativeBinding{RunID: r.ID})
	if err != nil {
		t.Fatal(err)
	}
	r = completeAcceptanceRun(t, s, r)
	if _, err = s.BeginCreativeAcceptance(r.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddMessage(Message{ToSession: "orch", Body: "late"}); err == nil {
		t.Error("generic message extends frozen run")
	}
	if _, err = s.AddQuestionMessage(QuestionMessage{QuestionID: q, Author: "human", Kind: "reply", Body: "late"}); err == nil {
		t.Error("generic question reply extends frozen run")
	}
	if err = s.ResolveQuestion(q, "answered"); err == nil {
		t.Error("generic question decision changes frozen run")
	}
	if err = s.UpdateSessionState("orch", "killed"); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishCreativeAcceptance(r.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.PurgeOld(9999999999); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetMessage(msg); err != nil || got.Body != "retained result discussion" {
		t.Fatalf("lost archive message: %+v %v", got, err)
	}
}

func TestCreativeAcceptanceArtifactHistoryIsReadOnly(t *testing.T) {
	s, r := acceptanceRun(t)
	p := CreativeArtifactPublication{RunID: r.ID, Name: "Result", Kind: "result", Filename: "result.txt", MIME: "text/plain", Size: 4, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", StorageKey: "blob"}
	a, v, err := s.PublishCreativeArtifact(p)
	if err != nil {
		t.Fatal(err)
	}
	r = completeAcceptanceRun(t, s, r)
	if _, err = s.BeginCreativeAcceptance(r.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.PublishCreativeArtifact(p); !errors.Is(err, ErrCreativeConflict) {
		t.Fatalf("publish: %v", err)
	}
	if err = s.SetCreativeArtifactCurrent(a.ID, v.ID); !errors.Is(err, ErrCreativeConflict) {
		t.Fatalf("current: %v", err)
	}
	if got, err := s.ListCreativeArtifactVersions(a.ID); err != nil || len(got) != 1 || got[0].ID != v.ID {
		t.Fatalf("lost versions: %v %v", got, err)
	}
}
