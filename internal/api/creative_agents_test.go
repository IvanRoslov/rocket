package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

func TestCreativeAgentLaunchAndContextualDiscussion(t *testing.T) {
	d := sessionsTestDeps(t)
	addTestProject(t, d, "film")
	p, err := d.Store.CreateCreativePipeline(store.CreativePipelineInput{ProjectID: "film", Name: "Film", Stages: []store.CreativeStageDefinition{{Name: "Scene", Approval: "auto"}}})
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
	resp = postJSONWithHeader(t, base+"/questions", orch.ID, map[string]any{"stage_id": run.Stages[0].ID, "body": "Paid proposal: exact calls and cost basis", "options": []string{"Go", "Revise"}})
	if resp.StatusCode != 201 {
		t.Fatalf("ask %d", resp.StatusCode)
	}
	var q creativeQuestionResponse
	json.NewDecoder(resp.Body).Decode(&q)
	resp.Body.Close()
	if q.TaskID != 0 || q.CreativeContext.StageID != run.Stages[0].ID {
		t.Fatalf("question %+v", q)
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
	if resp.StatusCode != 400 {
		t.Fatalf("invalid version: %d", resp.StatusCode)
	}
}
