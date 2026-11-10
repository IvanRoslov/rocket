package store

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func feedFixture(t *testing.T) (*Store, CreativeRun, CreativeItem) {
	t.Helper()
	s := openTestStore(t)
	mustSetup(t, s)
	p, err := s.CreateCreativePipeline(CreativePipelineInput{Name: "Feed", Stages: []CreativeStageDefinition{{Name: "Shots", Repeated: true, Approval: "stage"}, {Name: "Script", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateCreativeRun(CreativeRunInput{ProjectID: "billing", PipelineID: p.ID, Name: "Feed", Brief: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.CreateCreativeItem(r.ID, r.Stages[0].ID, CreativeItemInput{Name: "Scene 3"})
	if err != nil {
		t.Fatal(err)
	}
	return s, r, item
}
func addFeed(t *testing.T, s *Store, b CreativeBinding, author, kind, body string, replaces int64) CreativeFeedEntry {
	t.Helper()
	e, err := s.AddCreativeFeedEntry(CreativeFeedEntry{RunID: b.RunID, StageID: b.StageID, ItemID: b.ItemID, ArtifactVersionIDs: b.ArtifactVersionIDs, Author: author, Kind: kind, Body: body}, replaces)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestCreativeFeedOrderFilterUnread(t *testing.T) {
	s, r, item := feedFixture(t)
	stage := CreativeBinding{RunID: r.ID, StageID: r.Stages[0].ID}
	tagged := stage
	tagged.ItemID = item.ID
	a := addFeed(t, s, stage, "orchestrator", "message", "one", 0)
	b := addFeed(t, s, tagged, "orchestrator", "proposal", "two", 0)
	addFeed(t, s, stage, "human", "message", "three", 0)
	addFeed(t, s, CreativeBinding{RunID: r.ID}, "orchestrator", "message", "run", 0)
	entries, err := s.ListCreativeFeed(stage)
	if err != nil || len(entries) != 3 || entries[0].Body != "three" || entries[1].ItemID != item.ID {
		t.Fatalf("stage feed: %+v %v", entries, err)
	}
	entries, err = s.ListCreativeFeed(tagged)
	if err != nil || len(entries) != 1 || entries[0].ID != b.ID {
		t.Fatalf("filter: %+v %v", entries, err)
	}
	u, err := s.CreativeFeedUnread(r.ID)
	if err != nil || u.Total != 3 || u.RunFeed != 1 || len(u.Stages) != 2 || u.Stages[0].Unread != 2 {
		t.Fatalf("unread %+v %v", u, err)
	}
	if _, err = s.MarkCreativeFeedRead(stage, b.ID); err != nil {
		t.Fatal(err)
	}
	c := addFeed(t, s, stage, "orchestrator", "message", "later", 0)
	if cursor, err := s.MarkCreativeFeedRead(stage, a.ID); err != nil || cursor != b.ID {
		t.Fatalf("cursor regressed %d %v", cursor, err)
	}
	u, _ = s.CreativeFeedUnread(r.ID)
	if u.Total != 2 || u.Stages[0].Unread != 1 {
		t.Fatalf("read consumed concurrent entry %+v", u)
	}
	if _, err = s.MarkCreativeFeedRead(CreativeBinding{RunID: r.ID}, c.ID); err == nil {
		t.Fatal("foreign-feed cursor accepted")
	}
	if _, err = s.AddCreativeFeedEntry(CreativeFeedEntry{RunID: r.ID, Body: " ", Author: "human", Kind: "message"}, 0); err == nil {
		t.Fatal("blank entry")
	}
	if _, err = s.AddCreativeFeedEntry(CreativeFeedEntry{RunID: r.ID, StageID: stage.StageID, ItemID: 999, Body: "wrong item", Author: "human", Kind: "message"}, 0); err == nil {
		t.Fatal("foreign item")
	}
	if _, err = s.AddCreativeFeedEntry(CreativeFeedEntry{RunID: r.ID, Body: "bad version", ArtifactVersionIDs: []int64{999}, Author: "human", Kind: "message"}, 0); err == nil {
		t.Fatal("invalid versions")
	}
}
func TestCreativeFeedCardDecisionReplacement(t *testing.T) {
	s, r, _ := feedFixture(t)
	b := CreativeBinding{RunID: r.ID}
	old := addFeed(t, s, b, "orchestrator", "proposal", "4 calls", 0)
	replacement := addFeed(t, s, CreativeBinding{RunID: r.ID, StageID: r.Stages[0].ID}, "orchestrator", "proposal", "2 calls", old.ID)
	got, err := s.GetCreativeFeedCard(r.ID, old.ID)
	if err != nil || got.Card.Status != "replaced" || got.Card.ReplacedByEntryID != replacement.ID {
		t.Fatalf("old card %+v %v", got, err)
	}
	_, err = s.DecideCreativeFeedCard(r.ID, old.ID, "go")
	var conflict *CreativeFeedConflict
	if !errors.As(err, &conflict) || conflict.Entry.Card.Status != "replaced" {
		t.Fatalf("decided replaced card: %v", err)
	}
	got, err = s.DecideCreativeFeedCard(r.ID, replacement.ID, "go")
	if err != nil || got.Card.Decision != "go" || got.Card.DecidedAt == 0 {
		t.Fatalf("decision %+v %v", got, err)
	}
	_, err = s.DecideCreativeFeedCard(r.ID, replacement.ID, "deny")
	if !errors.As(err, &conflict) || conflict.Entry.Card.Decision != "go" {
		t.Fatalf("second decision: %v", err)
	}
	_, err = s.AddCreativeFeedEntry(CreativeFeedEntry{RunID: r.ID, Author: "orchestrator", Kind: "proposal", Body: "cannot replace decided"}, replacement.ID)
	if !errors.As(err, &conflict) {
		t.Fatalf("replaced decided card: %v", err)
	}
	got, _ = s.GetCreativeFeedCard(r.ID, replacement.ID)
	if got.Card.ReplacesEntryID != old.ID {
		t.Fatal("replacement link missing")
	}
	if _, err = s.GetCreativeFeedCard(r.ID+100, old.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign card leaked %v", err)
	}
}
func TestCreativeFeedCardRaces(t *testing.T) {
	for _, race := range []string{"decision-replacement", "two-replacements"} {
		t.Run(race, func(t *testing.T) {
			s, r, _ := feedFixture(t)
			old := addFeed(t, s, CreativeBinding{RunID: r.ID}, "orchestrator", "proposal", "old", 0)
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for n := 0; n < 2; n++ {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					<-start
					var err error
					if n == 0 && race == "decision-replacement" {
						_, err = s.DecideCreativeFeedCard(r.ID, old.ID, "go")
					} else {
						_, err = s.AddCreativeFeedEntry(CreativeFeedEntry{RunID: r.ID, Author: "orchestrator", Kind: "proposal", Body: fmt.Sprint(n)}, old.ID)
					}
					results <- err
				}(n)
			}
			close(start)
			wg.Wait()
			close(results)
			wins, conflicts := 0, 0
			for err := range results {
				var conflict *CreativeFeedConflict
				if err == nil {
					wins++
				} else if errors.As(err, &conflict) {
					conflicts++
				} else {
					t.Fatalf("unexpected race error: %v", err)
				}
			}
			if wins != 1 || conflicts != 1 {
				t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
			}
			entries, _ := s.ListCreativeFeed(CreativeBinding{RunID: r.ID})
			if len(entries) > 2 {
				t.Fatalf("orphan replacement %+v", entries)
			}
		})
	}
}
func TestCreativeFeedOfflineDelivery(t *testing.T) {
	s, r, item := feedFixture(t)
	b := CreativeBinding{RunID: r.ID, StageID: r.Stages[0].ID, ItemID: item.ID}
	e := addFeed(t, s, b, "human", "message", "make it 2", 0)
	queued, err := s.QueueCreativeFeedDeliveries(r.ID)
	if err != nil || len(queued) != 0 {
		t.Fatalf("offline enqueue %+v %v", queued, err)
	}
	orch := Session{ID: "feed-orch", Kind: "orchestrator", ProjectID: "billing", RepoID: "api", Agent: "fake", State: "killed"}
	if err = s.AddSession(orch); err != nil {
		t.Fatal(err)
	}
	if err = s.BindCreativeSession(orch.ID, CreativeBinding{RunID: r.ID}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetCreativeRunOrchestrator(r.ID, orch.ID); err != nil {
		t.Fatal(err)
	}
	queued, err = s.QueueCreativeFeedDeliveries(r.ID)
	if err != nil || len(queued) != 0 {
		t.Fatalf("terminal enqueue %+v %v", queued, err)
	}
	orch.State = "running"
	if err = s.UpdateSession(orch); err != nil {
		t.Fatal(err)
	}
	queued, err = s.QueueCreativeFeedDeliveries(r.ID)
	if err != nil || len(queued) != 1 {
		t.Fatalf("restored enqueue %+v %v", queued, err)
	}
	m, err := s.GetMessage(queued[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"make it 2", fmt.Sprintf("entry=%d", e.ID), fmt.Sprintf("run=%d stage=%d item=%d", r.ID, b.StageID, b.ItemID), fmt.Sprintf("rocket creative agent feed post %d --stage %d --item %d --body 'Your reply'", r.ID, b.StageID, b.ItemID), "artifact_versions=[]"} {
		if !strings.Contains(m.Body, want) {
			t.Fatalf("delivery missing %q: %s", want, m.Body)
		}
	}
	queued, err = s.QueueCreativeFeedDeliveries(r.ID)
	if err != nil || len(queued) != 0 {
		t.Fatalf("duplicate delivery %+v %v", queued, err)
	}
	card := addFeed(t, s, b, "orchestrator", "proposal", "exact two calls $0.20", 0)
	if _, err = s.DecideCreativeFeedCard(r.ID, card.ID, "go"); err != nil {
		t.Fatal(err)
	}
	queued, err = s.QueueCreativeFeedDeliveries(r.ID)
	if err != nil || len(queued) != 1 {
		t.Fatalf("decision enqueue %+v %v", queued, err)
	}
	m, _ = s.GetMessage(queued[0])
	if !strings.Contains(m.Body, fmt.Sprintf("card=%d decision=go", card.ID)) || !strings.Contains(m.Body, "exact two calls $0.20") {
		t.Fatalf("decision delivery: %s", m.Body)
	}
}

func TestCreativeFeedPendingDeliveryFollowsReplacement(t *testing.T) {
	s, r, _ := feedFixture(t)
	addFeed(t, s, CreativeBinding{RunID: r.ID}, "human", "message", "offline for replacement", 0)
	for _, id := range []string{"first-orch", "second-orch"} {
		if err := s.AddSession(Session{ID: id, Kind: "orchestrator", ProjectID: "billing", RepoID: "api", Agent: "fake", State: "running"}); err != nil {
			t.Fatal(err)
		}
		if err := s.BindCreativeSession(id, CreativeBinding{RunID: r.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetCreativeRunOrchestrator(r.ID, "first-orch"); err != nil {
		t.Fatal(err)
	}
	first, err := s.QueueCreativeFeedDeliveries(r.ID)
	if err != nil || len(first) != 1 {
		t.Fatalf("first %+v %v", first, err)
	}
	if err = s.SetCreativeRunOrchestrator(r.ID, "second-orch"); err != nil {
		t.Fatal(err)
	}
	second, err := s.QueueCreativeFeedDeliveries(r.ID)
	if err != nil || len(second) != 1 {
		t.Fatalf("replacement %+v %v", second, err)
	}
	m, _ := s.GetMessage(first[0])
	if m.Status != "failed" {
		t.Fatal("old recipient still has queued event")
	}
	m, _ = s.GetMessage(second[0])
	if m.ToSession != "second-orch" {
		t.Fatal("event not routed to replacement")
	}
}
