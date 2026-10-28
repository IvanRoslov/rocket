package session

import (
	"context"
	"errors"
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

func TestRestoreUnavailableAgentDoesNotDestroyRuntimeOrQueueNotice(t *testing.T) {
	m, st, _, rt, ws := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	testFakeAgent.firstByInput = true
	testFakeAgent.availableErr = errors.New("missing binary")
	sess := seedRunningSession(t, st, "worker")
	if _, err := st.AddTask(store.Task{Title: "work", ProjectID: "proj1", RepoID: "repo1", SessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	rt.aliveMap = map[string]bool{sess.ID: true}
	if err := m.Restore(context.Background(), sess.ID, RestoreOpts{Force: true}); err == nil {
		t.Fatal("Restore succeeded with unavailable agent")
	}
	if len(rt.destroyed) != 0 || len(rt.created) != 0 || ws.restoreCalls != 0 {
		t.Fatalf("unavailable restore mutated runtime/workspace: destroyed=%v created=%v restores=%d", rt.destroyed, rt.created, ws.restoreCalls)
	}
	if _, ok, err := st.NextQueuedMessage(sess.ID); err != nil || ok {
		t.Fatalf("restore queue: ok=%v err=%v", ok, err)
	}
}

func TestRestoreEmptyLaunchCommandDoesNotQueueNotice(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	testFakeAgent.firstByInput = true
	testFakeAgent.launchEmpty = true
	sess := seedRunningSession(t, st, "worker")
	sess.State = "errored"
	if err := st.UpdateSession(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddTask(store.Task{Title: "work", ProjectID: "proj1", RepoID: "repo1", SessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(context.Background(), sess.ID, RestoreOpts{}); err == nil {
		t.Fatal("Restore succeeded with empty launch command")
	}
	if len(rt.created) != 0 {
		t.Fatalf("runtime created with empty command: %+v", rt.created)
	}
	if _, ok, err := st.NextQueuedMessage(sess.ID); err != nil || ok {
		t.Fatalf("restore queue: ok=%v err=%v", ok, err)
	}
}

type testAgentWithoutInput struct{ agent.Agent }

func TestSpawnQueuesInitialMessageForInputAgent(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	testFakeAgent.firstByInput = true
	var woken []string
	m.SetWake(func(id string) {
		woken = append(woken, id)
		stored, err := st.GetSession(id)
		if err != nil || stored.State != "running" {
			t.Fatalf("wake state = %+v, err %v", stored, err)
		}
		if msg, ok, err := st.NextQueuedMessage(id); err != nil || !ok || msg.Body != "first brief" {
			t.Fatalf("wake queue = %+v, ok %v, err %v", msg, ok, err)
		}
	})
	seedProjectRepo(t, st, "project", "repo")
	sess, err := m.Spawn(context.Background(), SpawnReq{Project: "project", Repo: "repo", Task: "work", Prompt: "first brief", AgentName: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if got := testFakeAgent.launchCalls[0].FirstMessage; got != "" {
		t.Fatalf("launch FirstMessage = %q, want empty", got)
	}
	assertFirstQueuedMessage(t, st, sess.ID, "first brief")
	if len(woken) != 1 || woken[0] != sess.ID {
		t.Fatalf("woken = %v", woken)
	}
}

func TestStartQueuesKickoffForInputAgent(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	testFakeAgent.firstByInput = true
	var woken []string
	m.SetWake(func(id string) { woken = append(woken, id) })
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
	if len(woken) != 1 || woken[0] != sess.ID {
		t.Fatalf("woken = %v", woken)
	}
}

func TestRestoreQueuesNoticeForInputAgent(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	testFakeAgent.firstByInput = true
	var woken []string
	m.SetWake(func(id string) { woken = append(woken, id) })
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
	if _, err := st.AddMessage(store.Message{FromSession: "orch", ToSession: sess.ID, Body: "pending before restore"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(context.Background(), sess.ID, RestoreOpts{}); err != nil {
		t.Fatal(err)
	}
	if got := testFakeAgent.launchCalls[0].FirstMessage; got != "" {
		t.Fatalf("launch FirstMessage = %q, want empty", got)
	}
	msgs, err := st.ListMessages(sess.ID, 0)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("messages = %+v, err = %v", msgs, err)
	}
	if msgs[0].Body != "pending before restore" || msgs[0].Status != "queued" {
		t.Fatalf("pending message order changed: %+v", msgs)
	}
	msg := msgs[1]
	if msg.FromSession != "rocket" || msg.Status != "queued" {
		t.Fatalf("restore message = %+v", msg)
	}
	if !strings.Contains(msg.Body, "[rocket restore]") || !strings.Contains(msg.Body, sess.Prompt) {
		t.Fatalf("queued restore message = %q", msg.Body)
	}
	if len(woken) != 1 || woken[0] != sess.ID {
		t.Fatalf("woken = %v", woken)
	}
}

func TestSpawnKeepsFirstMessageInLegacyLaunch(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	var woken []string
	m.SetWake(func(id string) { woken = append(woken, id) })
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
	if len(woken) != 0 {
		t.Fatalf("legacy launch woke queue: %v", woken)
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
