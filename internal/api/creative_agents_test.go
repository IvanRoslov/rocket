package api

import (
	"encoding/json"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/prompts"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

func TestCreativeAgentLaunchAndContextualDiscussion(t *testing.T) {
	d := sessionsTestDeps(t)
	addTestProject(t, d, "film")
	p, err := d.Store.CreateCreativePipeline(store.CreativePipelineInput{Name: "Film", Stages: []store.CreativeStageDefinition{{Name: "Scene", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.Store.CreateCreativeRun(store.CreativeRunInput{ProjectID: "film", PipelineID: p.ID, Name: "Film", Brief: "Make film"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()
	base := fmt.Sprintf("%s/v1/creative/runs/%d", srv.URL, run.ID)
	resp := postJSONWithHeader(t, base+"/sessions", "", map[string]any{"agent": "fake"})
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("launch %d", resp.StatusCode)
	}
	var orch sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&orch); err != nil {
		t.Fatal(err)
	}
	resp = postJSONWithHeader(t, base+"/messages", "", map[string]any{"stage_id": run.Stages[0].ID, "body": "Review this scene"})
	if resp.StatusCode != 201 {
		t.Fatalf("send %d", resp.StatusCode)
	}
	var msg creativeMessageResponse
	json.NewDecoder(resp.Body).Decode(&msg)
	resp.Body.Close()
	if msg.CreativeContext.StageID != run.Stages[0].ID || !strings.Contains(msg.Body, "Review this scene") {
		t.Fatalf("message %+v", msg)
	}
	// Existing questions are an archive: seed a pre-upgrade question, then
	// exercise unchanged reply/answer and delivery behavior.
	qid, err := d.Store.AddCreativeQuestion(store.Question{AskedBy: orch.ID, Body: "Paid proposal: exact calls and cost basis", Options: []string{"Go", "Revise"}}, store.CreativeBinding{RunID: run.ID, StageID: run.Stages[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Store.AddParticipants(qid, store.ParticipantHuman, orch.ID); err != nil {
		t.Fatal(err)
	}
	q, err := creativeQuestionView(d, nil, qid)
	if err != nil {
		t.Fatal(err)
	}
	resp = postJSONWithHeader(t, fmt.Sprintf("%s/v1/questions/%d/answer", srv.URL, q.ID), orch.ID, map[string]any{"body": "Go"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("agent authorized own Go: %d", resp.StatusCode)
	}
	resp = postJSONWithHeader(t, fmt.Sprintf("%s/v1/questions/%d/answer", srv.URL, q.ID), "", map[string]any{"choose": 1})
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("answer %d", resp.StatusCode)
	}
	stored, err := d.Store.GetQuestion(q.ID)
	if err != nil || stored.Status != "resolved" {
		t.Fatalf("answer not stored %v %v", stored, err)
	}
	context, err := d.Store.GetCreativeDiscussionContext("question", q.ID)
	if err != nil || context.StageID != run.Stages[0].ID {
		t.Fatal("lost question context")
	}
	messages, err := d.Store.ListMessages(orch.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) < 2 || !strings.Contains(messages[len(messages)-1].Body, "Go") {
		t.Fatalf("answer not delivered: %+v", messages)
	}
	// Fabricated shown versions fail before any discussion is recorded.
	resp = postJSONWithHeader(t, base+"/messages", "", map[string]any{"stage_id": run.Stages[0].ID, "artifact_version_ids": []int64{999}, "body": "bad version"})
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("invalid version: %d", resp.StatusCode)
	}
}

func TestCreativeQuestionSurvivesOrchestratorReplacement(t *testing.T) {
	d := sessionsTestDeps(t)
	addTestProject(t, d, "film")
	p, err := d.Store.CreateCreativePipeline(store.CreativePipelineInput{Name: "Film", Stages: []store.CreativeStageDefinition{{Name: "Scene", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.Store.CreateCreativeRun(store.CreativeRunInput{ProjectID: "film", PipelineID: p.ID, Name: "Film", Brief: "Create film"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()
	base := fmt.Sprintf("%s/v1/creative/runs/%d", srv.URL, run.ID)
	// A pre-upgrade question remains readable without a live session.
	qid, err := d.Store.AddCreativeQuestion(store.Question{Body: "Which scene?"}, store.CreativeBinding{RunID: run.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Store.AddParticipants(qid, store.ParticipantHuman); err != nil {
		t.Fatal(err)
	}
	q, err := creativeQuestionView(d, nil, qid)
	if err != nil {
		t.Fatal(err)
	}
	var resp *http.Response
	resp = postJSONWithHeader(t, base+"/sessions", "", map[string]any{"agent": "fake"})
	if resp.StatusCode != 201 {
		t.Fatalf("launch %d", resp.StatusCode)
	}
	var orch sessionResponse
	json.NewDecoder(resp.Body).Decode(&orch)
	resp.Body.Close()
	resp = postJSONWithHeader(t, fmt.Sprintf("%s/v1/questions/%d/reply", srv.URL, q.ID), orch.ID, map[string]any{"body": "I suggest scene one"})
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("new orchestrator cannot continue retained question: %d", resp.StatusCode)
	}
	resp = postJSONWithHeader(t, fmt.Sprintf("%s/v1/questions/%d/answer", srv.URL, q.ID), "", map[string]any{"body": "Use scene one"})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("answer %d", resp.StatusCode)
	}
	messages, err := d.Store.ListMessages(orch.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 || !strings.Contains(messages[len(messages)-1].Body, "Use scene one") {
		t.Fatal("current orchestrator missed decision")
	}
}

func TestCreativeMessageRecipients(t *testing.T) {
	d, _, _ := creativeArtifactDeps(t)
	// Observe enqueue events without a background delivery race.
	d.Queue = nil
	// A repeated stage exercises exact run/stage/item/version preservation.
	pipeline, err := d.Store.CreateCreativePipeline(store.CreativePipelineInput{Name: "Shots", Stages: []store.CreativeStageDefinition{{Name: "Scenes", Repeated: true, Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.Store.CreateCreativeRun(store.CreativeRunInput{ProjectID: "p", PipelineID: pipeline.ID, Name: "Film", Brief: "Create scenes"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := d.Store.CreateCreativeItem(run.ID, run.Stages[0].ID, store.CreativeItemInput{Name: "Scene"})
	if err != nil {
		t.Fatal(err)
	}
	binding := store.CreativeBinding{RunID: run.ID, StageID: run.Stages[0].ID, ItemID: item.ID}
	for _, id := range []string{"creative-orch", "creative-worker"} {
		kind := "worker"
		scope := binding
		if id == "creative-orch" {
			kind = "orchestrator"
			scope.StageID = 0
			scope.ItemID = 0
		}
		if err := d.Store.AddSession(store.Session{ID: id, Kind: kind, ProjectID: "p", RepoID: "r", Agent: "claude-code", State: "running"}); err != nil {
			t.Fatal(err)
		}
		if err := d.Store.BindCreativeSession(id, scope); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Store.SetCreativeRunOrchestrator(run.ID, "creative-orch"); err != nil {
		t.Fatal(err)
	}
	_, version, err := d.Store.PublishCreativeArtifact(store.CreativeArtifactPublication{RunID: run.ID, StageID: binding.StageID, ItemID: item.ID, Name: "Scene", Kind: "result", Filename: "scene.txt", MIME: "text/plain", Size: 1, SHA256: strings.Repeat("a", 64), StorageKey: "scene"})
	if err != nil {
		t.Fatal(err)
	}
	binding.ArtifactVersionIDs = []int64{version.ID}
	h := NewHandler(d)
	path := fmt.Sprintf("/v1/creative/runs/%d/messages", run.ID)
	for _, tc := range []struct {
		from, to string
		reject   bool
	}{
		{"creative-orch", "", true}, {"creative-orch", "creative-orch", true}, {"creative-worker", "creative-worker", true},
		{"creative-worker", "", false}, {"creative-orch", "creative-worker", false},
	} {
		t.Run(tc.from+"_to_"+tc.to, func(t *testing.T) {
			events, cancel := d.Bus.Subscribe()
			defer cancel()
			input, err := json.Marshal(map[string]any{"stage_id": binding.StageID, "item_id": item.ID, "artifact_version_ids": []int64{version.ID}, "to": tc.to, "body": "exact scene"})
			if err != nil {
				t.Fatal(err)
			}
			before, err := d.Store.ListCreativeDiscussionIDs("message", binding)
			if err != nil {
				t.Fatal(err)
			}
			w := artifactRequest(h, "POST", path, "application/json", tc.from, strings.NewReader(string(input)))
			if tc.reject {
				if w.Code != 400 || !strings.Contains(w.Body.String(), "--to") {
					t.Fatalf("self-send %d: %s", w.Code, w.Body.String())
				}
				after, err := d.Store.ListCreativeDiscussionIDs("message", binding)
				if err != nil || len(after) != len(before) {
					t.Fatalf("self-send persisted: %v %v", after, err)
				}
				select {
				case e := <-events:
					t.Fatalf("self-send published event: %+v", e)
				default:
				}
				return
			}
			if w.Code != 201 {
				t.Fatalf("send %d: %s", w.Code, w.Body.String())
			}
			var msg creativeMessageResponse
			if err := json.Unmarshal(w.Body.Bytes(), &msg); err != nil {
				t.Fatal(err)
			}
			wantTo := tc.to
			if wantTo == "" {
				wantTo = "creative-orch"
			}
			if msg.ID == 0 || msg.To != wantTo || msg.From != tc.from || msg.Status != "queued" || !strings.Contains(msg.Body, "exact scene") {
				t.Fatalf("message: %+v", msg)
			}
			stored, err := d.Store.GetCreativeDiscussionContext("message", msg.ID)
			if err != nil || stored.RunID != binding.RunID || stored.StageID != binding.StageID || stored.ItemID != item.ID || len(stored.ArtifactVersionIDs) != 1 || stored.ArtifactVersionIDs[0] != version.ID {
				t.Fatalf("context lost: %+v %v", stored, err)
			}
			select {
			case e := <-events:
				if e.Type != "message.queued" || e.SessionID != wantTo {
					t.Fatalf("wrong queue event: %+v", e)
				}
			default:
				t.Fatal("no queue wake event")
			}
		})
	}
}

// Follow the actual worker report instruction after a restore/replacement. A
// report pinned to ParentID fails even though the run's current orchestrator lives.
func TestCreativeWorkerReportFollowsReplacement(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	d.Queue = nil
	for _, id := range []string{"original-orch", "replacement-orch", "worker"} {
		kind := "orchestrator"
		parent := ""
		scope := store.CreativeBinding{RunID: run}
		if id == "worker" {
			kind = "worker"
			parent = "original-orch"
			scope.StageID = stage
		}
		if err := d.Store.AddSession(store.Session{ID: id, Kind: kind, ParentID: parent, ProjectID: "p", RepoID: "r", Agent: "claude-code", State: "running"}); err != nil {
			t.Fatal(err)
		}
		if err := d.Store.BindCreativeSession(id, scope); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Store.SetCreativeRunOrchestrator(run, "original-orch"); err != nil {
		t.Fatal(err)
	}
	original, err := d.Store.GetSession("original-orch")
	if err != nil {
		t.Fatal(err)
	}
	original.State = "terminated"
	if err := d.Store.UpdateSession(original); err != nil {
		t.Fatal(err)
	}
	if err := d.Store.SetCreativeRunOrchestrator(run, "replacement-orch"); err != nil {
		t.Fatal(err)
	}
	worker, err := d.Store.GetSession("worker")
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := prompts.Render("", "creative-worker", prompts.Vars{"session_id": worker.ID, "parent_id": worker.ParentID, "project_name": "Project", "worktree_path": "/tmp/work", "allowed_repos": "r", "creative_context": "assigned stage", "run_snapshot": "current replacement", "tools_home": "/tmp/tools", "brief": "Report result"})
	if err != nil {
		t.Fatal(err)
	}
	command := regexp.MustCompile("(?s)`rocket creative agent send RUN ([^`]+)`").FindStringSubmatch(rendered)
	if len(command) != 2 {
		t.Fatal("worker has no report command")
	}
	to := ""
	words := strings.Fields(command[1])
	for i, word := range words {
		if word == "--to" && i+1 < len(words) {
			to = words[i+1]
		}
	}
	input, err := json.Marshal(map[string]any{"stage_id": stage, "to": to, "body": "Report after restore"})
	if err != nil {
		t.Fatal(err)
	}
	w := artifactRequest(NewHandler(d), "POST", fmt.Sprintf("/v1/creative/runs/%d/messages", run), "application/json", worker.ID, strings.NewReader(string(input)))
	if w.Code != 201 {
		t.Fatalf("following worker report command targets stale parent: %d %s", w.Code, w.Body.String())
	}
	var msg creativeMessageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.To != "replacement-orch" || msg.From != "worker" || msg.CreativeContext.RunID != run || msg.CreativeContext.StageID != stage {
		t.Fatalf("report misrouted: %+v", msg)
	}
}
