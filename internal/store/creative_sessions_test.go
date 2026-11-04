package store

import (
	"errors"
	"testing"
)

func TestCreativeSessionContextAndPRIsolation(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	// Core's pipeline/run creation supplies real ancestry; no runtime or generation.
	pipeline, err := s.CreateCreativePipeline(CreativePipelineInput{Name: "Film", Stages: []CreativeStageDefinition{{Name: "Scenes", Repeated: true, Approval: "item"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateCreativeRun(CreativeRunInput{ProjectID: "billing", PipelineID: pipeline.ID, Name: "Film", Brief: "Create film"})
	if err != nil {
		t.Fatal(err)
	}
	sess := Session{ID: "creative-test", Kind: "worker", ProjectID: "billing", RepoID: "api", FeatureSlug: "creative", Agent: "fake", State: "running"}
	if err := s.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	binding := CreativeBinding{RunID: run.ID, StageID: run.Stages[0].ID}
	if err := s.BindCreativeSession(sess.ID, binding); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCreativeSessionContext(sess.ID)
	if err != nil || got.RunID != run.ID {
		t.Fatalf("context: %+v %v", got, err)
	}
	polls, err := s.ListSessionsForPRPoll()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range polls {
		if p.ID == sess.ID {
			t.Fatal("creative worker entered PR lifecycle")
		}
	}
	if err := s.BindCreativeSession(sess.ID, CreativeBinding{RunID: run.ID, StageID: 999}); err == nil {
		t.Fatal("accepted foreign stage")
	}
	_, err = s.GetCreativeSessionContext("absent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestCreativeDiscussionPinsVersionsAndStageIncludesItems(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	p, err := s.CreateCreativePipeline(CreativePipelineInput{Name: "Film", Stages: []CreativeStageDefinition{{Name: "Scenes", Repeated: true, Approval: "stage"}, {Name: "Other", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateCreativeRun(CreativeRunInput{ProjectID: "billing", PipelineID: p.ID, Name: "Film", Brief: "Create film"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.CreateCreativeItem(run.ID, run.Stages[0].ID, CreativeItemInput{Name: "Scene", Description: "Scene", ExpectedArtifacts: "image"})
	if err != nil {
		t.Fatal(err)
	}
	// Immutable asset metadata fixtures; no generated media or provider calls.
	_, err = s.db.Exec(`INSERT INTO creative_artifacts(id,run_id,stage_id,item_id,name,kind,created_at) VALUES(100,?,?,?,'scene','result',1)`, run.ID, run.Stages[0].ID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO creative_artifact_versions(id,artifact_id,version_number,filename,mime,size,sha256,storage_key,author_session_id,created_at) VALUES(200,100,1,'scene.png','image/png',1,'hash','key-one','',1),(201,100,2,'scene.png','image/png',1,'hash','key-two','',2)`)
	if err != nil {
		t.Fatal(err)
	}
	stage := CreativeBinding{RunID: run.ID, StageID: run.Stages[0].ID, ArtifactVersionIDs: []int64{200}}
	if _, err := s.ValidateCreativeBinding(stage); err != nil {
		t.Fatalf("stage rejects its item: %v", err)
	}
	exact := stage
	exact.ItemID = item.ID
	if _, err := s.ValidateCreativeBinding(exact); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []CreativeBinding{{RunID: run.ID, ArtifactVersionIDs: []int64{200}}, {RunID: run.ID, StageID: run.Stages[1].ID, ArtifactVersionIDs: []int64{200}}} {
		if _, err := s.ValidateCreativeBinding(bad); err == nil {
			t.Fatalf("accepted wrong context %+v", bad)
		}
	}
	qid, err := s.AddCreativeQuestion(Question{Body: "Review version one"}, stage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE creative_artifacts SET current_version_id=201 WHERE id=100`); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCreativeDiscussionContext("question", qid)
	if err != nil || len(got.ArtifactVersionIDs) != 1 || got.ArtifactVersionIDs[0] != 200 {
		t.Fatalf("binding drifted: %+v %v", got, err)
	}
}
