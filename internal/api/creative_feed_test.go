package api

import (
	"encoding/json"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/store"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreativeFeedHTTPContractAndPermissions(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	d.Queue = nil
	for _, id := range []string{"feed-orch", "feed-worker", "stale-orch", "foreign-orch"} {
		kind := "orchestrator"
		scope := store.CreativeBinding{RunID: run}
		if id == "feed-worker" {
			kind = "worker"
			scope.StageID = stage
		}
		if err := d.Store.AddSession(store.Session{ID: id, Kind: kind, ProjectID: "p", RepoID: "r", Agent: "fake", State: "running"}); err != nil {
			t.Fatal(err)
		}
		if id != "foreign-orch" {
			if err := d.Store.BindCreativeSession(id, scope); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := d.Store.SetCreativeRunOrchestrator(run, "feed-orch"); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(d)
	base := fmt.Sprintf("/v1/creative/runs/%d/feed", run)
	request := func(method, path, from, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := artifactRequest(h, method, path, "application/json", from, strings.NewReader(body))
		if w.Code != want {
			t.Fatalf("%s %s from %s: %d %s", method, path, from, w.Code, w.Body.String())
		}
		return w
	}
	for _, from := range []string{"feed-worker", "stale-orch", "foreign-orch"} {
		w := request("POST", base, from, `{"body":"no"}`, 403)
		if !strings.Contains(w.Body.String(), "rocket creative agent") {
			t.Fatal("refusal has no command hint")
		}
	}
	request("POST", base, "", `{"body":" "}`, 400)
	human := request("POST", base, "", fmt.Sprintf(`{"stage_id":%d,"body":"make ending darker"}`, stage), 201)
	var entry store.CreativeFeedEntry
	if err := json.Unmarshal(human.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Author != "human" || entry.Card != nil || entry.AuthorSessionID != "" || entry.ArtifactVersionIDs == nil {
		t.Fatalf("human shape %+v", entry)
	}
	request("POST", base, "feed-orch", fmt.Sprintf(`{"stage_id":%d,"body":"done"}`, stage), 201)
	card := request("POST", base+"/cards", "feed-orch", fmt.Sprintf(`{"stage_id":%d,"body":"4 images, exact calls and cost"}`, stage), 201)
	var first store.CreativeFeedEntry
	json.Unmarshal(card.Body.Bytes(), &first)
	replacement := request("POST", base+"/cards", "feed-orch", fmt.Sprintf(`{"stage_id":%d,"body":"2 images, exact calls and cost","replaces_entry_id":%d}`, stage, first.ID), 201)
	var second store.CreativeFeedEntry
	json.Unmarshal(replacement.Body.Bytes(), &second)
	oldPath := fmt.Sprintf("%s/cards/%d", base, first.ID)
	newPath := fmt.Sprintf("%s/cards/%d", base, second.ID)
	conflict := request("POST", oldPath+"/decision", "", `{"decision":"go"}`, 409)
	var current struct {
		Entry store.CreativeFeedEntry `json:"entry"`
	}
	json.Unmarshal(conflict.Body.Bytes(), &current)
	if current.Entry.Card == nil || current.Entry.Card.ReplacedByEntryID != second.ID {
		t.Fatalf("missing current card: %s", conflict.Body.String())
	}
	request("POST", newPath+"/decision", "feed-orch", `{"decision":"go"}`, 403)
	request("POST", newPath+"/decision", "", `{"decision":"go"}`, 200)
	request("POST", newPath+"/decision", "", `{"decision":"deny"}`, 409)
	request("GET", newPath, "foreign-orch", "", 403)
	request("GET", newPath, "feed-orch", "", 200)
	unread := request("GET", base+"/unread", "", "", 200)
	var u store.CreativeFeedUnread
	json.Unmarshal(unread.Body.Bytes(), &u)
	if u.Total != 3 || u.Stages[0].Unread != 3 {
		t.Fatalf("unread %s", unread.Body.String())
	}
	request("POST", base+"/read", "feed-orch", fmt.Sprintf(`{"stage_id":%d,"through_entry_id":%d}`, stage, second.ID), 403)
	request("POST", base+"/read", "", fmt.Sprintf(`{"stage_id":%d,"through_entry_id":%d}`, stage, second.ID), 200)
	request("POST", base+"/read", "", `{}`, 400)
	unread = request("GET", base+"/unread", "", "", 200)
	json.Unmarshal(unread.Body.Bytes(), &u)
	if u.Total != 0 {
		t.Fatalf("unread not reset: %s", unread.Body.String())
	}
	got := request("GET", base+fmt.Sprintf("?stage_id=%d", stage), "feed-orch", "", 200)
	var entries []store.CreativeFeedEntry
	json.Unmarshal(got.Body.Bytes(), &entries)
	if len(entries) != 4 || entries[0].ID != second.ID || entries[1].Card.Status != "replaced" {
		t.Fatalf("feed order/state: %s", got.Body.String())
	}
	for _, from := range []string{"feed-orch", "feed-worker", ""} {
		w := request("POST", strings.TrimSuffix(base, "/feed")+"/questions", from, `{"body":"legacy ask"}`, 409)
		if !strings.Contains(w.Body.String(), "creative_questions_retired") || !strings.Contains(w.Body.String(), "rocket creative agent") {
			t.Fatal("missing ask refusal guidance")
		}
	}
	messages, err := d.Store.ListMessages("feed-orch", 0)
	if err != nil || len(messages) != 2 {
		t.Fatalf("human event deliveries %+v %v", messages, err)
	}
	for _, m := range messages {
		if !strings.Contains(m.Body, fmt.Sprintf("rocket creative agent feed post %d --stage %d", run, stage)) {
			t.Fatalf("reply context absent: %s", m.Body)
		}
	}
	stored, err := d.Store.GetCreativeRun(run)
	if err != nil || stored.Stages[0].Status != "pending" {
		t.Fatal("feed changed pipeline state")
	}
}

func TestCreativeFeedWorkerRefusalHasExecutableScope(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	d.Queue = nil
	if err := d.Store.AddSession(store.Session{ID: "worker-hint", Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "fake", State: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Store.BindCreativeSession("worker-hint", store.CreativeBinding{RunID: run, StageID: stage}); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(d)
	for _, suffix := range []string{"feed", "questions"} {
		w := artifactRequest(h, "POST", fmt.Sprintf("/v1/creative/runs/%d/%s", run, suffix), "application/json", "worker-hint", strings.NewReader(`{"body":"question"}`))
		want := fmt.Sprintf("rocket creative agent send %d --stage %d", run, stage)
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("worker hint loses assigned scope; want %s: %s", want, w.Body.String())
		}
	}
}
