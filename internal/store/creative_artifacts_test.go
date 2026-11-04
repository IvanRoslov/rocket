package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func artifactTestContext(t *testing.T, s *Store) (int64, int64, int64) {
	t.Helper()
	mustSetup(t, s)
	p, err := s.CreateCreativePipeline(CreativePipelineInput{ProjectID: "billing", Name: "Materials", Stages: []CreativeStageDefinition{{Name: "Shots", Repeated: true, Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateCreativeRun(CreativeRunInput{ProjectID: "billing", PipelineID: p.ID, Name: "Run", Brief: "Produce materials"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.CreateCreativeItem(r.ID, r.Stages[0].ID, CreativeItemInput{Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	return r.ID, r.Stages[0].ID, item.ID
}

func artifactPublication(run, stage, item int64, key string) CreativeArtifactPublication {
	return CreativeArtifactPublication{RunID: run, StageID: stage, ItemID: item, Name: "Master", Kind: "result", Filename: "master.mp4", MIME: "video/mp4", Size: 12, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", StorageKey: key}
}

func TestCreativeArtifactRetainedVersionsAndCurrent(t *testing.T) {
	s := openTestStore(t)
	run, stage, item := artifactTestContext(t, s)
	a, v1, err := s.PublishCreativeArtifact(artifactPublication(run, stage, item, "blob-one"))
	if err != nil {
		t.Fatal(err)
	}
	a2, v2, err := s.PublishCreativeArtifact(artifactPublication(run, stage, item, "blob-two"))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != a2.ID || v1.ID == v2.ID || v1.VersionNumber != 1 || v2.VersionNumber != 2 || a2.CurrentVersionID != v2.ID {
		t.Fatalf("publication identities: %+v %+v %+v", a2, v1, v2)
	}
	versions, err := s.ListCreativeArtifactVersions(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].StorageKey != "blob-one" || versions[1].StorageKey != "blob-two" {
		t.Fatalf("history: %+v", versions)
	}
	if err := s.SetCreativeArtifactCurrent(a.ID, v1.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCreativeArtifact(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentVersionID != v1.ID {
		t.Fatalf("current: %+v", got)
	}
	if err := s.ValidateCreativeArtifactVersions(run, stage, item, []int64{v1.ID, v2.ID}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListCreativeArtifacts(run, stage, item)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	runList, err := s.ListCreativeArtifacts(run, 0, 0)
	if err != nil || len(runList) != 0 {
		t.Fatalf("exact-context list: %+v %v", runList, err)
	}
}

func TestCreativeArtifactRejectsWrongContextAndSelection(t *testing.T) {
	s := openTestStore(t)
	run, stage, item := artifactTestContext(t, s)
	a, v, err := s.PublishCreativeArtifact(artifactPublication(run, stage, item, "first"))
	if err != nil {
		t.Fatal(err)
	}
	other := artifactPublication(run, stage, 0, "other")
	other.Name = "Storyboard"
	b, bv, err := s.PublishCreativeArtifact(other)
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]int64{{bv.ID}, {999999}, {0}, {-1}} {
		if err := s.ValidateCreativeArtifactVersions(run, stage, item, ids); err == nil {
			t.Fatalf("accepted version IDs %v", ids)
		}
	}
	if err := s.SetCreativeArtifactCurrent(a.ID, bv.ID); err == nil {
		t.Fatal("accepted other artifact version")
	}
	if err := s.SetCreativeArtifactCurrent(b.ID, v.ID); err == nil {
		t.Fatal("accepted other artifact version reverse")
	}
	bad := artifactPublication(run, stage+100, item, "wrong")
	if _, _, err := s.PublishCreativeArtifact(bad); err == nil {
		t.Fatal("accepted wrong ancestry")
	}
	if err := s.ValidateCreativeArtifactVersions(run, 0, item, nil); err == nil {
		t.Fatal("accepted item without stage")
	}
	if _, err := s.GetCreativeArtifactVersion(99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing version: %v", err)
	}
	for _, change := range []func(*CreativeArtifactPublication){func(p *CreativeArtifactPublication) { p.Name = " " }, func(p *CreativeArtifactPublication) { p.Kind = "working" }, func(p *CreativeArtifactPublication) { p.Size = 0 }, func(p *CreativeArtifactPublication) { p.StorageKey = "../escape" }} {
		p := artifactPublication(run, stage, item, "invalid")
		change(&p)
		if _, _, err := s.PublishCreativeArtifact(p); err == nil {
			t.Fatalf("accepted invalid publication %+v", p)
		}
	}
}

func TestCreativeArtifactConcurrentPublication(t *testing.T) {
	s := openTestStore(t)
	run, stage, item := artifactTestContext(t, s)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := s.PublishCreativeArtifact(artifactPublication(run, stage, item, fmt.Sprintf("blob-%d", i)))
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListCreativeArtifacts(run, stage, item)
	if err != nil || len(list) != 1 {
		t.Fatalf("artifacts %+v %v", list, err)
	}
	versions, err := s.ListCreativeArtifactVersions(list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 8 {
		t.Fatalf("lost versions %+v", versions)
	}
	for i, v := range versions {
		if v.VersionNumber != int64(i+1) {
			t.Fatalf("sequence %+v", versions)
		}
	}
	if list[0].CurrentVersionID != versions[7].ID {
		t.Fatal("current is not last committed publication")
	}
}
