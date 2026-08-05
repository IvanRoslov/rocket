package api

import (
	"testing"

	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// agentCaller builds the caller a persistent agent presents: a session
// registered under the agent's own id with kind=agent.
func agentCaller(id string) *store.Session {
	return &store.Session{ID: id, Kind: session.AgentSessionKind}
}

func TestCallerParticipant_HumanIsCanonical(t *testing.T) {
	if got := callerParticipant(nil); got != store.ParticipantHuman {
		t.Errorf("callerParticipant(nil) = %q, want %q", got, store.ParticipantHuman)
	}
	if got := callerParticipant(&store.Session{ID: "orch-1"}); got != "orch-1" {
		t.Errorf("callerParticipant(orch-1) = %q, want orch-1", got)
	}
}

// The old TestWaitingOn covered the derived turn (last entry's addressed_to,
// else everyone but its author). Task #1023 replaced that derivation with the
// stored attention set, so those cases now live where the rules do:
// internal/store/attention_test.go.

func TestWhoseTurnCompat(t *testing.T) {
	if got := whoseTurnCompat([]string{"cto", "human"}, "orchestrator"); got != "user" {
		t.Errorf("with human = %q, want user", got)
	}
	if got := whoseTurnCompat([]string{"cto"}, "orchestrator"); got != "orchestrator" {
		t.Errorf("without human = %q, want orchestrator", got)
	}
	if got := whoseTurnCompat([]string{"cto"}, "role"); got != "role" {
		t.Errorf("role vocabulary = %q, want role", got)
	}
	if got := whoseTurnCompat(nil, "orchestrator"); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
}

func TestCanAnswerThread(t *testing.T) {
	d := Deps{}
	if !canAnswerThread(d, nil) {
		t.Error("the human must be able to answer")
	}
	if !canAnswerThread(d, agentCaller("cto")) {
		t.Error("a persistent agent must be able to answer")
	}
	if canAnswerThread(d, &store.Session{ID: "orch-1", Kind: "orchestrator"}) {
		t.Error("an orchestrator must not be able to answer")
	}
	if canAnswerThread(d, &store.Session{ID: "w-1", Kind: "worker"}) {
		t.Error("a worker must not be able to answer")
	}
}

func TestThreadWriteAccess(t *testing.T) {
	d := Deps{}
	subj := threadSubject{TaskID: 7, Counterpart: "orch-1"}
	parts := []string{"cto", "human", "orch-1"}

	for _, tt := range []struct {
		name              string
		caller            *store.Session
		parts             []string
		allowed, joinable bool
	}{
		{"the human is a participant of every thread", nil, parts, true, true},
		{"a participant agent writes as before", agentCaller("cto"), parts, true, true},
		{"the task's own orchestrator", &store.Session{ID: "orch-1", Kind: "orchestrator"}, parts, true, true},
		{"a participant worker", &store.Session{ID: "w-9", Kind: "worker"}, append(append([]string(nil), parts...), "w-9"), true, true},
		{"an outside agent may join on purpose", agentCaller("sre"), parts, false, true},
		{"the human of a thread they are not in may join", nil, []string{"cto"}, false, true},
		{"an outside worker stays refused", &store.Session{ID: "w-9", Kind: "worker"}, parts, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			allowed, joinable := threadWriteAccess(d, tt.caller, subj, tt.parts)
			if allowed != tt.allowed || joinable != tt.joinable {
				t.Errorf("threadWriteAccess = %v/%v, want %v/%v", allowed, joinable, tt.allowed, tt.joinable)
			}
		})
	}
}

func TestCanOpenThread(t *testing.T) {
	d := Deps{}
	subj := threadSubject{TaskID: 7, Counterpart: "orch-1"}

	if !canOpenThread(d, nil, subj) {
		t.Error("the human must be able to open a thread")
	}
	if !canOpenThread(d, agentCaller("cto"), subj) {
		t.Error("a persistent agent must be able to open a thread")
	}
	if !canOpenThread(d, &store.Session{ID: "orch-1", Kind: "orchestrator"}, subj) {
		t.Error("the task's own orchestrator must be able to open a thread")
	}
	if canOpenThread(d, &store.Session{ID: "orch-2", Kind: "orchestrator"}, subj) {
		t.Error("another task's orchestrator must not be able to open a thread")
	}
	if canOpenThread(d, &store.Session{ID: "w-1", Kind: "worker"}, subj) {
		t.Error("a worker must not be able to open a thread")
	}
}

func TestThreadPrefix(t *testing.T) {
	task := threadSubject{TaskID: 722, Counterpart: "orch-1"}
	if got := threadPrefix(task, 3, "reply", "cto"); got != "[task #722 Q3 reply from cto]" {
		t.Errorf("task prefix = %q", got)
	}
	role := threadSubject{RoleID: "cto", Counterpart: "cto"}
	if got := threadPrefix(role, 2, "answer", "human"); got != "[role cto Q2 answer from human]" {
		t.Errorf("role prefix = %q", got)
	}
}

// TestParticipantFanOut_SkipsAuthorAndHuman drives the real delivery plumbing:
// a live orchestrator session receives the framed body, the author does not,
// and the human is never injected into.
func TestParticipantFanOut_SkipsAuthorAndHuman(t *testing.T) {
	d := messagesTestDeps(t)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	addLiveAgentSession(t, d, "cto")
	if err := d.Store.AddAgent(store.Agent{ID: "cto", Dir: "/tmp/cto", Command: "claude", Enabled: true}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	subj := threadSubject{TaskID: 7, Counterpart: "orch-1"}
	if err := participantFanOut(d, subj, 1, "reply", "cto", "the body",
		[]string{"cto", "human", "orch-1"}); err != nil {
		t.Fatalf("participantFanOut: %v", err)
	}

	orchMsgs, err := d.Store.ListMessages("orch-1", 0)
	if err != nil {
		t.Fatalf("ListMessages(orch-1): %v", err)
	}
	if len(orchMsgs) != 1 {
		t.Fatalf("orch-1 got %d messages, want 1", len(orchMsgs))
	}
	if want := "[task #7 Q1 reply from cto] the body"; orchMsgs[0].Body != want {
		t.Errorf("orch-1 body = %q, want %q", orchMsgs[0].Body, want)
	}

	ctoMsgs, err := d.Store.ListMessages("cto", 0)
	if err != nil {
		t.Fatalf("ListMessages(cto): %v", err)
	}
	if len(ctoMsgs) != 0 {
		t.Errorf("the author must not be delivered to, got %d messages", len(ctoMsgs))
	}
}

// TestParticipantFanOut_DeadAgentGetsInbox covers acceptance criterion 2: an
// agent with no live session finds the message in its inbox instead.
func TestParticipantFanOut_DeadAgentGetsInbox(t *testing.T) {
	d := messagesTestDeps(t)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	if err := d.Store.AddAgent(store.Agent{ID: "cto", Dir: "/tmp/cto", Command: "claude", Enabled: true}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	subj := threadSubject{TaskID: 7, Counterpart: "orch-1"}
	if err := participantFanOut(d, subj, 1, "question", "orch-1", "wake up",
		[]string{"cto", "human", "orch-1"}); err != nil {
		t.Fatalf("participantFanOut: %v", err)
	}

	inbox, err := d.Store.ListInboxMessages("cto", store.InboxUnread, 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("cto inbox has %d messages, want 1", len(inbox))
	}
	if want := "[task #7 Q1 question from orch-1] wake up"; inbox[0].Body != want {
		t.Errorf("inbox body = %q, want %q", inbox[0].Body, want)
	}
}

// TestParticipantFanOut_TerminalSessionSkipped: an ephemeral session has no
// inbox, so a dead one is logged and skipped rather than queued forever.
func TestParticipantFanOut_TerminalSessionSkipped(t *testing.T) {
	d := messagesTestDeps(t)
	addTestProject(t, d, "proj1")
	sess := addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	if err := d.Store.UpdateSessionState(sess.ID, "done"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	subj := threadSubject{TaskID: 7, Counterpart: "orch-1"}
	if err := participantFanOut(d, subj, 1, "answer", "human", "final",
		[]string{"human", "orch-1"}); err != nil {
		t.Fatalf("participantFanOut: %v", err)
	}

	msgs, err := d.Store.ListMessages("orch-1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("a terminal session must not be queued to, got %d messages", len(msgs))
	}
}

// TestCanReadThread_WorkerReadsItsRootTask is the clause that matters: a
// worker owns a subtask, the thread hangs off the ROOT task above it, and the
// worker must still be able to read it.
func TestCanReadThread_WorkerReadsItsRootTask(t *testing.T) {
	d := messagesTestDeps(t)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	addTestSession(t, d, "w-1", "worker", "proj1")

	rootID, err := d.Store.AddTask(store.Task{
		Title: "Root", ProjectID: "proj1", SessionID: "orch-1",
	})
	if err != nil {
		t.Fatalf("AddTask root: %v", err)
	}
	if _, err := d.Store.AddTask(store.Task{
		Title: "Sub", ProjectID: "proj1", ParentID: rootID, SessionID: "w-1",
	}); err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	subj := threadSubject{TaskID: rootID, Counterpart: "orch-1"}
	worker := &store.Session{ID: "w-1", Kind: "worker"}
	if !canReadThread(d, worker, subj, []string{"human", "orch-1"}) {
		t.Error("a worker must be able to read its root task's threads")
	}
}

func TestCanReadThread_Matrix(t *testing.T) {
	d := messagesTestDeps(t)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	addTestSession(t, d, "orch-2", "orchestrator", "proj1")

	rootID, err := d.Store.AddTask(store.Task{
		Title: "Root", ProjectID: "proj1", SessionID: "orch-1",
	})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if _, err := d.Store.AddTask(store.Task{
		Title: "Other", ProjectID: "proj1", SessionID: "orch-2",
	}); err != nil {
		t.Fatalf("AddTask other: %v", err)
	}

	subj := threadSubject{TaskID: rootID, Counterpart: "orch-1"}
	parts := []string{"human", "orch-1"}

	if !canReadThread(d, nil, subj, parts) {
		t.Error("the human must read every thread")
	}
	if !canReadThread(d, agentCaller("cto"), subj, parts) {
		t.Error("a persistent agent must read every thread")
	}
	if !canReadThread(d, &store.Session{ID: "orch-1", Kind: "orchestrator"}, subj, parts) {
		t.Error("the task's own orchestrator must read its threads")
	}
	if canReadThread(d, &store.Session{ID: "orch-2", Kind: "orchestrator"}, subj, parts) {
		t.Error("an unrelated orchestrator must not read another task's threads")
	}
	if !canReadThread(d, &store.Session{ID: "orch-2", Kind: "orchestrator"},
		subj, append(parts, "orch-2")) {
		t.Error("a participant reads the thread it takes part in, whatever its task")
	}
}
