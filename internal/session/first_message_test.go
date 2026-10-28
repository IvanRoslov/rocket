package session

import (
	"context"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/store"
)

func TestFirstMessageCapabilityDefaultsOff(t *testing.T) {
	if agent.TakesFirstMessageByInput(testAgentWithoutInput{&fakeAgent{}}) {
		t.Fatal("agent without capability takes first message by input")
	}
	if !agent.TakesFirstMessageByInput(&fakeAgent{firstByInput: true}) {
		t.Fatal("opted-in agent does not take first message by input")
	}
	if agent.WantsBracketedPaste(testAgentWithoutInput{&fakeAgent{}}) {
		t.Fatal("agent without capability wants bracketed paste")
	}
	if !agent.WantsBracketedPaste(&fakeAgent{bracketed: true}) {
		t.Fatal("opted-in agent does not want bracketed paste")
	}
}

type testAgentWithoutInput struct{ agent.Agent }

func TestSpawnQueuesInitialMessageForInputAgent(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	testFakeAgent.firstByInput = true
	seedProjectRepo(t, st, "project", "repo")
	sess, err := m.Spawn(context.Background(), SpawnReq{Project: "project", Repo: "repo", Task: "work", Prompt: "first brief", AgentName: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if got := testFakeAgent.launchCalls[0].FirstMessage; got != "" {
		t.Fatalf("launch FirstMessage = %q, want empty", got)
	}
	assertFirstQueuedMessage(t, st, sess.ID, "first brief")
}

func TestStartQueuesKickoffForInputAgent(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	testFakeAgent.firstByInput = true
	seedProjectRepo(t, st, "project", "repo")
	project, err := st.GetProject("project")
	if err != nil {
		t.Fatal(err)
	}
	task := store.Task{ID: 42, Title: "Start a task", Description: "Do the work.", ProjectID: "project"}
	sess, err := m.SpawnOrchestrator(context.Background(), task, project, "fake", LaunchProfile{})
	if err != nil {
		t.Fatal(err)
	}
	if got := testFakeAgent.launchCalls[0].FirstMessage; got != "" {
		t.Fatalf("launch FirstMessage = %q, want empty", got)
	}
	msg := assertQueuedMessage(t, st, sess.ID)
	if !strings.Contains(msg.Body, task.Title) || !strings.Contains(msg.Body, task.Description) {
		t.Fatalf("queued kickoff = %q, want title and description", msg.Body)
	}
}

func TestRestoreQueuesNoticeForInputAgent(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	testFakeAgent.firstByInput = true
	seedProjectRepo(t, st, "proj1", "repo1")
	sess := seedRunningSession(t, st, "worker")
	sess.State = "errored"
	sess.Prompt = "original brief"
	if err := st.UpdateSession(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddTask(store.Task{Title: "worker", ProjectID: "proj1", RepoID: "repo1", SessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(context.Background(), sess.ID, RestoreOpts{}); err != nil {
		t.Fatal(err)
	}
	if got := testFakeAgent.launchCalls[0].FirstMessage; got != "" {
		t.Fatalf("launch FirstMessage = %q, want empty", got)
	}
	msg := assertQueuedMessage(t, st, sess.ID)
	if !strings.Contains(msg.Body, "[rocket restore]") || !strings.Contains(msg.Body, sess.Prompt) {
		t.Fatalf("queued restore message = %q", msg.Body)
	}
}

func TestSpawnKeepsFirstMessageInLegacyLaunch(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "project", "repo")
	sess, err := m.Spawn(context.Background(), SpawnReq{Project: "project", Repo: "repo", Task: "work", Prompt: "first brief", AgentName: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if got := testFakeAgent.launchCalls[0].FirstMessage; got != "first brief" {
		t.Fatalf("legacy launch FirstMessage = %q", got)
	}
	if _, ok, err := st.NextQueuedMessage(sess.ID); err != nil || ok {
		t.Fatalf("legacy queue: ok=%v err=%v", ok, err)
	}
}

func assertQueuedMessage(t *testing.T, st *store.Store, id string) store.Message {
	t.Helper()
	msg, ok, err := st.NextQueuedMessage(id)
	if err != nil || !ok {
		t.Fatalf("NextQueuedMessage: ok=%v err=%v", ok, err)
	}
	if msg.FromSession != "rocket" || msg.Status != "queued" {
		t.Fatalf("queued message = %+v, want rocket sender and queued status", msg)
	}
	return msg
}

func assertFirstQueuedMessage(t *testing.T, st *store.Store, id, want string) {
	t.Helper()
	msg := assertQueuedMessage(t, st, id)
	if msg.Body != want {
		t.Fatalf("queued body = %q, want %q", msg.Body, want)
	}
}
