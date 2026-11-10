package session

import (
	"context"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

func TestCreativeLaunchAndRestoreKeepCreativeLifecycle(t *testing.T) {
	m, st, _, rt, _, _ := testManagerWithConfig(t)
	seedProjectRepo(t, st, "p", "r")
	p, err := st.CreateCreativePipeline(store.CreativePipelineInput{Name: "Film", Stages: []store.CreativeStageDefinition{{Name: "Scene", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateCreativeRun(store.CreativeRunInput{ProjectID: "p", PipelineID: p.ID, Name: "Film", Brief: "Make a scene"})
	if err != nil {
		t.Fatal(err)
	}
	orch, err := m.SpawnCreative(context.Background(), CreativeSpawnReq{Context: store.CreativeBinding{RunID: run.ID}, AgentName: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if !testFakeAgent.setupCalls[0].Creative || testFakeAgent.setupCalls[0].DevelopmentOrchestrator() {
		t.Fatal("creative orchestrator receives development skills")
	}
	if orch.Kind != "orchestrator" || orch.TaskID != 0 {
		t.Fatalf("wrong lifecycle: %+v", orch)
	}
	if !strings.Contains(testFakeAgent.setupCalls[len(testFakeAgent.setupCalls)-1].SystemPrompt, "CREATIVE ORCHESTRATOR") {
		t.Fatalf("prompt: %+v", testFakeAgent.setupCalls[len(testFakeAgent.setupCalls)-1])
	}
	if _, err := m.SpawnCreative(context.Background(), CreativeSpawnReq{Context: store.CreativeBinding{RunID: run.ID}, AgentName: "fake"}); err == nil {
		t.Fatal("duplicate live orchestrator")
	}
	worker, err := m.SpawnCreative(context.Background(), CreativeSpawnReq{Context: store.CreativeBinding{RunID: run.ID, StageID: run.Stages[0].ID}, ParentID: orch.ID, Name: "scene", AgentName: "fake", Prompt: "Render scene"})
	if err != nil {
		t.Fatal(err)
	}
	if worker.TaskID != 0 || worker.SubtaskID != 0 || worker.ParentID != orch.ID {
		t.Fatalf("worker: %+v", worker)
	}
	if !strings.Contains(testFakeAgent.setupCalls[len(testFakeAgent.setupCalls)-1].SystemPrompt, "CREATIVE WORKER") {
		t.Fatal("worker got wrong prompt")
	}
	if err := m.Kill(context.Background(), worker.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(context.Background(), worker.ID, RestoreOpts{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(testFakeAgent.setupCalls[len(testFakeAgent.setupCalls)-1].SystemPrompt, "CREATIVE WORKER") || !strings.Contains(testFakeAgent.setupCalls[len(testFakeAgent.setupCalls)-1].FirstMessage, "Render scene") {
		t.Fatal("restore lost creative context")
	}
	if len(rt.created) != 3 {
		t.Fatalf("launches %d", len(rt.created))
	}
	tasks, err := st.ListTasks(store.TaskFilter{})
	if err != nil || len(tasks) != 0 {
		t.Fatalf("created development tasks: %v %v", tasks, err)
	}
}

func TestCreativeRestoreDoesNotDuplicateCurrentOrchestrator(t *testing.T) {
	m, st, _, _, _, _ := testManagerWithConfig(t)
	seedProjectRepo(t, st, "p", "r")
	p, err := st.CreateCreativePipeline(store.CreativePipelineInput{Name: "Film", Stages: []store.CreativeStageDefinition{{Name: "Scene", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateCreativeRun(store.CreativeRunInput{ProjectID: "p", PipelineID: p.ID, Name: "Film", Brief: "Create film"})
	if err != nil {
		t.Fatal(err)
	}
	req := CreativeSpawnReq{Context: store.CreativeBinding{RunID: run.ID}, AgentName: "fake"}
	old, err := m.SpawnCreative(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Kill(context.Background(), old.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SpawnCreative(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(context.Background(), old.ID, RestoreOpts{}); err == nil {
		t.Fatal("restored superseded orchestrator alongside current one")
	}
}

func TestCreativeFeedDeliveryAfterLaunchAndRestore(t *testing.T) {
	m, st, _, _, _, _ := testManagerWithConfig(t)
	seedProjectRepo(t, st, "p", "r")
	p, err := st.CreateCreativePipeline(store.CreativePipelineInput{Name: "Feed", Stages: []store.CreativeStageDefinition{{Name: "Scene", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateCreativeRun(store.CreativeRunInput{ProjectID: "p", PipelineID: p.ID, Name: "Feed", Brief: "Make scene"})
	if err != nil {
		t.Fatal(err)
	}
	add := func(body string) {
		t.Helper()
		_, err := st.AddCreativeFeedEntry(store.CreativeFeedEntry{RunID: run.ID, StageID: run.Stages[0].ID, Kind: "message", Author: "human", Body: body}, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	add("before launch")
	orch, err := m.SpawnCreative(context.Background(), CreativeSpawnReq{Context: store.CreativeBinding{RunID: run.ID}, AgentName: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := st.ListMessages(orch.ID, 0)
	if err != nil || len(messages) != 1 || !strings.Contains(messages[0].Body, "before launch") {
		t.Fatalf("launch lost feed %+v %v", messages, err)
	}
	if err = m.Kill(context.Background(), orch.ID, false); err != nil {
		t.Fatal(err)
	}
	add("while stopped")
	if err = m.Restore(context.Background(), orch.ID, RestoreOpts{}); err != nil {
		t.Fatal(err)
	}
	messages, err = st.ListMessages(orch.ID, 0)
	if err != nil || len(messages) != 2 || !strings.Contains(messages[1].Body, "while stopped") {
		t.Fatalf("restore lost feed %+v %v", messages, err)
	}
	entries, err := st.ListCreativeFeed(store.CreativeBinding{RunID: run.ID, StageID: run.Stages[0].ID})
	if err != nil || len(entries) != 2 {
		t.Fatalf("restore history %+v %v", entries, err)
	}
}
