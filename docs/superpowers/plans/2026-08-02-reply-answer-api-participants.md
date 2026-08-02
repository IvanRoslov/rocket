# T2 — Participant permissions, waiting_on and fan-out delivery (API layer)

> **For agentic workers:** REQUIRED SUB-SKILL: use `superpowers:test-driven-development`
> for every task below. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the question API on T1's participant model: permissions derived
from participation instead of the "human or my own orchestrator" pair, a
`waiting_on` / `your_turn` turn model, an optional `to` addressee list, and
fan-out of every thread message to all participants except its author.

**Architecture:** A new `internal/api/threads.go` holds the subject-agnostic
core — participant identity of a caller, the permission predicates, the
`waiting_on` derivation and the fan-out delivery loop — parameterised by a
`threadSubject` that says whether the thread hangs off a task or off a role.
`questions.go` and `agent_questions.go` become thin handlers over it, so the
two route families share one implementation while keeping their two distinct
wire shapes.

**Tech Stack:** Go, `net/http` (Go 1.22 `ServeMux` patterns), SQLite via
`internal/store`, the existing `queue.Queue` / `bus` delivery plumbing.

## Global Constraints

- Spec v1 of task #722 (`task_docs`, kind `spec`) is the source of truth; a
  divergence is a question for the orchestrator, not a local decision.
- Code, identifiers, comments and commit messages in English.
- Scope is `internal/api`, plus the minimum outside it needed to keep
  `go build ./...` and `go test ./...` green. Do **not** touch `internal/cli`,
  `web/`, `mobile/` or `docs/*.md` — those are T3–T6.
- Backward compatibility: `/v1/agents/{id}/questions` and the task question
  routes keep their existing fields and their existing meanings. New fields are
  additive only.
- `messages[].author` stays `""` on the wire for the human — `wireAuthor()` in
  `questions.go:29` survives T2 untouched. Flipping it is subtask #736.
  `questions.asked_by` likewise stays `""` for the human.
- Branch `feature/reply-answer/api-participants`, one PR.

## Participant identifiers

| Value | Who |
|---|---|
| `human` (`store.ParticipantHuman`) | the human user |
| persistent agent id (`cto`) | a `kind=agent` session |
| session id (`reply-answer-orch`) | an orchestrator or a worker |

`store.IsHuman(id)` accepts both `"human"` and the legacy `""`.

## Decisions confirmed by the orchestrator (messages 8072, and the replies to 8073/8078)

1. **The subject's own counterpart is seeded as a participant at creation.** On
   `ask` the participant set is `{human} ∪ {caller} ∪ {counterpart} ∪ to`, where
   the counterpart is `task.SessionID` for a task thread and the role id for a
   role thread. The orchestrator reads this as spec §1's "the human is always a
   participant" applied symmetrically to the other endpoint, not as a third way
   of joining — `--to` and writing-in remain the only ways a *third party*
   joins. Two details it insisted on: **seed only if the counterpart exists**
   (a task with an empty `SessionID` seeds nothing — do not invent a
   participant), and seed **through `AddParticipants`** so it stays idempotent.
2. **The ` from <id>` suffix is uniform**, including a human author:
   `[task #7 Q1 reply from human] ...`. With several participants the reader can
   no longer infer the author from the frame. Safe for existing readers:
   anything keying on the `[task #N QM reply` prefix still matches, only the
   tail grows.
3. **Role prefixes keep their `role ` word**: `[role cto Q2 reply from X]`. The
   spec's rule ("existing prefixes preserved") and its example (`[cto Q2 ...]`)
   contradict each other; the orchestrator confirmed the rule binds and the
   example is an error.
4. **Read permission IS enforced in T2** — Task 8 is mandatory, with the precise
   rule the orchestrator dictated (see that task).
5. **The mixed wire representation is intended.** `questions.asked_by` stays
   `""` for the human and `messages[].author` stays `""` via `wireAuthor()`,
   while `participants` / `waiting_on` / `your_turn` / `addressed_to` carry
   canonical ids including `human`. Subtask #736 unifies both, after web and
   mobile merge. Do not touch either here.
6. **Fan-out is never narrowed by `to`.** Acceptance criterion 4 is literal:
   every new message goes to every participant except its author. `to` sets
   `waiting_on` — who must *respond* — not who gets *notified*.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/api/threads.go` (create) | `threadSubject`, `callerParticipant`, permission predicates, `waitingOn`, `participantFanOut` |
| `internal/api/threads_test.go` (create) | unit tests for the pure functions above |
| `internal/api/questions.go` (modify) | task-thread handlers on the new core; `participants`/`waiting_on`/`your_turn`/`addressed_to` on the wire; `to` in request bodies |
| `internal/api/agent_questions.go` (modify) | role-thread handlers on the same core, same additive fields |
| `internal/api/agent_delivery.go` (modify) | `deliverToSession`, the generalised "deliver to an ephemeral session" path |
| `internal/api/questions_test.go` (modify) | new behaviour on task threads |
| `internal/api/agent_questions_test.go` (modify) | new behaviour on role threads |

---

## Interfaces produced (consumed by T3/T4/T5)

Wire shape, task threads (`questionResponse`) — additive fields only:

```json
{
  "id": 262, "task_id": 722, "ordinal": 3, "asked_by": "",
  "body": "...", "status": "open",
  "participants": ["cto", "human", "reply-answer-orch"],
  "waiting_on": ["cto", "human"],
  "your_turn": true,
  "whose_turn": "user",
  "messages": [
    {"id": 1, "author": "cto", "kind": "reply",
     "addressed_to": ["reply-answer-orch"], "body": "...", "created_at": 0}
  ]
}
```

`agentQuestionResponse` gains exactly the same four thread-level fields, and
its messages gain `addressed_to`. Its `whose_turn` keeps its own vocabulary
(`user` / `role`).

Request bodies for `POST /v1/tasks/{id}/questions`, `POST /v1/questions/{id}/reply`,
`POST /v1/questions/{id}/answer`, `POST /v1/agents/{id}/questions`,
`POST /v1/agent-questions/{id}/reply`, `POST /v1/agent-questions/{id}/answer`
all accept an optional `"to": ["cto", "human"]`.

Go interfaces the later tasks in this plan rely on:

```go
type threadSubject struct {
    TaskID    int64  // 0 for a role thread
    RoleID    string // "" for a task thread
    OwnerSide string // task.SessionID, or the role id
}

func callerParticipant(caller *store.Session) string
func callerIsPersistentAgent(d Deps, caller *store.Session) bool
func waitingOn(q store.Question, msgs []store.QuestionMessage, participants []string) []string
func contains(ids []string, id string) bool
func whoseTurnCompat(waiting []string, agentWord string) string
func canPostToThread(caller *store.Session, subj threadSubject, participants []string, d Deps) bool
func canAnswerThread(d Deps, caller *store.Session) bool
func participantFanOut(d Deps, subj threadSubject, ordinal int, kind, author, body string, participants []string) error
```

---

## Task 1: `threadSubject`, caller identity and `waitingOn`

**Files:**
- Create: `internal/api/threads.go`
- Test: `internal/api/threads_test.go`

**Interfaces:**
- Consumes: `store.Question`, `store.QuestionMessage`, `store.IsHuman`,
  `store.ParticipantHuman` from T1.
- Produces: `threadSubject`, `callerParticipant`, `contains`, `waitingOn`,
  `whoseTurnCompat`.

These are pure functions with no store access, so they get a plain table test
and no fixtures. `waitingOn` implements spec §2 exactly:

- resolved thread → `nil`;
- last message has a non-empty `AddressedTo` → that list;
- otherwise → every participant except the author of the last entry;
- a thread with no messages → the "last entry" is the question itself, authored
  by `q.AskedBy` (empty means the human).

Output is sorted so responses and tests are deterministic —
`ListParticipants` already sorts, and the `AddressedTo` branch is sorted here.

- [ ] **Step 1: Write the failing test**

`internal/api/threads_test.go`:

```go
package api

import (
	"reflect"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

func TestCallerParticipant_HumanIsCanonical(t *testing.T) {
	if got := callerParticipant(nil); got != store.ParticipantHuman {
		t.Errorf("callerParticipant(nil) = %q, want %q", got, store.ParticipantHuman)
	}
	if got := callerParticipant(&store.Session{ID: "orch-1"}); got != "orch-1" {
		t.Errorf("callerParticipant(orch-1) = %q, want orch-1", got)
	}
}

func TestWaitingOn(t *testing.T) {
	parts := []string{"cto", "human", "orch-1"}

	tests := []struct {
		name string
		q    store.Question
		msgs []store.QuestionMessage
		want []string
	}{
		{
			name: "resolved thread waits on nobody",
			q:    store.Question{Status: "resolved", AskedBy: "orch-1"},
			msgs: []store.QuestionMessage{{Author: "human"}},
			want: nil,
		},
		{
			name: "no messages, orchestrator asked: everyone but the asker",
			q:    store.Question{Status: "open", AskedBy: "orch-1"},
			want: []string{"cto", "human"},
		},
		{
			name: "no messages, human asked (asked_by is empty)",
			q:    store.Question{Status: "open", AskedBy: ""},
			want: []string{"cto", "orch-1"},
		},
		{
			name: "last message unaddressed: everyone but its author",
			q:    store.Question{Status: "open", AskedBy: "orch-1"},
			msgs: []store.QuestionMessage{{Author: "cto"}},
			want: []string{"human", "orch-1"},
		},
		{
			name: "last message addressed: exactly its addressees",
			q:    store.Question{Status: "open", AskedBy: "orch-1"},
			msgs: []store.QuestionMessage{
				{Author: "human"},
				{Author: "cto", AddressedTo: []string{"orch-1"}},
			},
			want: []string{"orch-1"},
		},
		{
			name: "human author stored as legacy empty string",
			q:    store.Question{Status: "open", AskedBy: "orch-1"},
			msgs: []store.QuestionMessage{{Author: ""}},
			want: []string{"cto", "orch-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := waitingOn(tt.q, tt.msgs, parts)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("waitingOn = %v, want %v", got, tt.want)
			}
		})
	}
}

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
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/api/ -run 'TestCallerParticipant|TestWaitingOn|TestWhoseTurnCompat' -v`
Expected: FAIL — `undefined: callerParticipant`, `undefined: waitingOn`,
`undefined: whoseTurnCompat`.

- [ ] **Step 3: Write the implementation**

`internal/api/threads.go`:

```go
// Package-internal core of the participant model shared by task threads
// (questions.go) and role threads (agent_questions.go). Everything here is
// subject-agnostic: a threadSubject says which of the two a thread is, and the
// permission, turn and delivery logic is written once against that.
package api

import (
	"sort"

	"github.com/IvanRoslov/rocket/internal/store"
)

// threadSubject describes what a thread hangs off. Exactly one of TaskID and
// RoleID is set. OwnerSide is the participant id of the subject's own side —
// the task's orchestrator session, or the role itself — which is a participant
// of every thread on that subject so a human-opened thread always has somebody
// to reach.
type threadSubject struct {
	TaskID    int64
	RoleID    string
	OwnerSide string
}

// callerParticipant maps an API caller to its participant id. A nil caller is
// the human; every other caller is identified by its session id, which for a
// kind=agent session is the agent's own id.
func callerParticipant(caller *store.Session) string {
	if caller == nil {
		return store.ParticipantHuman
	}
	return caller.ID
}

// contains reports whether ids holds id, treating every spelling of the human
// as the same participant.
func contains(ids []string, id string) bool {
	for _, got := range ids {
		if got == id || (store.IsHuman(got) && store.IsHuman(id)) {
			return true
		}
	}
	return false
}

// lastAuthor returns the participant id that spoke last in a thread. With no
// messages that is the question's own author, where an empty asked_by means
// the human (asked_by is not a participant-id column — see T1).
func lastAuthor(q store.Question, msgs []store.QuestionMessage) string {
	if len(msgs) == 0 {
		if store.IsHuman(q.AskedBy) {
			return store.ParticipantHuman
		}
		return q.AskedBy
	}
	last := msgs[len(msgs)-1]
	if store.IsHuman(last.Author) {
		return store.ParticipantHuman
	}
	return last.Author
}

// waitingOn derives who is expected to speak next, per spec §2 of task #722: a
// resolved thread waits on nobody; an explicitly addressed last message names
// its own addressees; otherwise everyone but whoever spoke last. The result is
// sorted so the API response and its tests are deterministic.
func waitingOn(q store.Question, msgs []store.QuestionMessage, participants []string) []string {
	if q.Status != "open" {
		return nil
	}

	if len(msgs) > 0 {
		if to := msgs[len(msgs)-1].AddressedTo; len(to) > 0 {
			out := append([]string(nil), to...)
			sort.Strings(out)
			return out
		}
	}

	author := lastAuthor(q, msgs)
	var out []string
	for _, p := range participants {
		if p == author || (store.IsHuman(p) && store.IsHuman(author)) {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// whoseTurnCompat renders waiting_on into the pre-participant whose_turn field
// the clients still read. The human's turn is "user"; anybody else's is the
// subject's own word for its agent side — "orchestrator" for a task thread,
// "role" for a role thread. Nobody waiting renders as empty, as before.
func whoseTurnCompat(waiting []string, agentWord string) string {
	if len(waiting) == 0 {
		return ""
	}
	if contains(waiting, store.ParticipantHuman) {
		return "user"
	}
	return agentWord
}
```

- [ ] **Step 4: Run the test — expect PASS**

Run: `go test ./internal/api/ -run 'TestCallerParticipant|TestWaitingOn|TestWhoseTurnCompat' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/threads.go internal/api/threads_test.go
git commit -m "api: thread subjects, participant identity and waiting_on"
```

---

## Task 2: Permission predicates

**Files:**
- Modify: `internal/api/threads.go`
- Test: `internal/api/threads_test.go`

**Interfaces:**
- Consumes: Task 1's `threadSubject`, `callerParticipant`, `contains`.
- Produces: `callerIsPersistentAgent`, `canAnswerThread`, `canOpenThread`,
  `canPostToThread`.

Spec §3, as a table:

| Action | Who |
|---|---|
| `ask` | the human; a `kind=agent` caller; the orchestrator of the thread's own task (role threads: an instance of that role) |
| `reply` | any participant of the thread, or a caller naming itself nowhere but listed in the request's `to` — no: a caller becomes a participant by *writing*, so any participant, plus the subject's own side, may post |
| `answer` / `dismiss` | the human and a `kind=agent` caller only |

`canPostToThread` therefore admits: the human, any current participant, and the
subject's own side (an orchestrator of the thread's task, or an instance of the
thread's role) even before it has spoken — the latter keeps today's behaviour
where the task's orchestrator can always reply.

`callerIsPersistentAgent` decides "is this a `kind=agent` caller" from the
session kind, which `internal/session` sets when the agent's tmux session is
registered. `d` is unused today but is threaded through so a later change can
fall back to the `agents` table without touching every call site.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/threads_test.go`:

```go
func agentCaller(id string) *store.Session {
	return &store.Session{ID: id, Kind: session.AgentSessionKind}
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

func TestCanPostToThread(t *testing.T) {
	d := Deps{}
	subj := threadSubject{TaskID: 7, OwnerSide: "orch-1"}
	parts := []string{"cto", "human", "orch-1"}

	if !canPostToThread(d, nil, subj, parts) {
		t.Error("the human must be able to post")
	}
	if !canPostToThread(d, agentCaller("cto"), subj, parts) {
		t.Error("a participant agent must be able to post")
	}
	if !canPostToThread(d, &store.Session{ID: "orch-1", Kind: "orchestrator"}, subj, parts) {
		t.Error("the task's own orchestrator must be able to post")
	}
	if canPostToThread(d, &store.Session{ID: "w-9", Kind: "worker"}, subj, parts) {
		t.Error("a non-participant worker must not be able to post")
	}
	if !canPostToThread(d, &store.Session{ID: "w-9", Kind: "worker"},
		subj, append(parts, "w-9")) {
		t.Error("a worker that is a participant must be able to post")
	}
}

func TestCanOpenThread(t *testing.T) {
	d := Deps{}
	subj := threadSubject{TaskID: 7, OwnerSide: "orch-1"}

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
```

Add `"github.com/IvanRoslov/rocket/internal/session"` to the test file's
imports.

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/api/ -run 'TestCanAnswerThread|TestCanPostToThread|TestCanOpenThread' -v`
Expected: FAIL — `undefined: canAnswerThread`, `undefined: canPostToThread`,
`undefined: canOpenThread`, `undefined: agentCaller`.

- [ ] **Step 3: Write the implementation**

Append to `internal/api/threads.go` (and add
`"github.com/IvanRoslov/rocket/internal/session"` to its imports):

```go
// callerIsPersistentAgent reports whether caller is an instance of a
// registered kind=agent session — the class of caller that spec §3 grants the
// same thread rights as the human, including answer.
func callerIsPersistentAgent(d Deps, caller *store.Session) bool {
	return caller != nil && caller.Kind == session.AgentSessionKind
}

// canAnswerThread reports whether caller may resolve a thread (answer or
// dismiss). Spec §3: the human and persistent agents only — an orchestrator or
// a worker gets 403 and is told to use reply instead, so that a final decision
// always has a human or a standing agent behind it.
func canAnswerThread(d Deps, caller *store.Session) bool {
	return caller == nil || callerIsPersistentAgent(d, caller)
}

// callerIsOwnerSide reports whether caller is the subject's own side: the
// orchestrator of the thread's task, or an instance of the thread's role.
func callerIsOwnerSide(caller *store.Session, subj threadSubject) bool {
	if caller == nil || subj.OwnerSide == "" {
		return false
	}
	return caller.ID == subj.OwnerSide
}

// canOpenThread reports whether caller may open a thread on subj. Spec §3:
// the human, any persistent agent, and the subject's own side.
func canOpenThread(d Deps, caller *store.Session, subj threadSubject) bool {
	return caller == nil ||
		callerIsPersistentAgent(d, caller) ||
		callerIsOwnerSide(caller, subj)
}

// canPostToThread reports whether caller may add a reply to the thread. Spec
// §3: any participant may post. The human is a participant of every thread by
// construction, and the subject's own side is admitted even before it has
// spoken, which is what preserves today's "the task's orchestrator may always
// reply" behaviour on threads it has not yet touched.
func canPostToThread(d Deps, caller *store.Session, subj threadSubject, participants []string) bool {
	if caller == nil {
		return true
	}
	return contains(participants, caller.ID) || callerIsOwnerSide(caller, subj)
}
```

- [ ] **Step 4: Run the test — expect PASS**

Run: `go test ./internal/api/ -run 'TestCanAnswerThread|TestCanPostToThread|TestCanOpenThread' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/threads.go internal/api/threads_test.go
git commit -m "api: participant-based thread permissions"
```

---

## Task 3: `deliverToSession` and the fan-out loop

**Files:**
- Modify: `internal/api/agent_delivery.go`, `internal/api/threads.go`
- Test: `internal/api/threads_test.go`

**Interfaces:**
- Consumes: `deliverToAgent` (`agent_delivery.go:21`), `store.GetSession`,
  `isSessionTerminal` (`tasks.go:765`), Task 1's `threadSubject`.
- Produces: `deliverToSession(d Deps, sessionID, body string) error`,
  `threadPrefix(subj threadSubject, ordinal int, kind, author string) string`,
  `participantFanOut(d Deps, subj threadSubject, ordinal int, kind, author, body string, participants []string) error`.

`deliverToSession` is `deliverToOrchestrator` (`questions.go:238`) with the
task argument replaced by a plain session id, so it can serve any ephemeral
participant. `deliverToOrchestrator` is deleted and its two call sites move
over in Tasks 5 and 6.

Routing per participant, spec §4:

| Participant | Path |
|---|---|
| the author | skipped |
| `human` | nothing is injected; the human reads the thread in CLI/dashboard |
| a `kind=agent` session id | `deliverToAgent` — live session gets the body, dead one gets an inbox row |
| any other session id | `deliverToSession` — live session gets the body, terminal one is logged and skipped |

Distinguishing an agent from an ephemeral session is a `GetAgent` lookup: the
`agents` table holds exactly the registered persistent agents, and an id absent
from it is a session id. A participant that is neither is skipped with a log —
a thread must not fail to record a message because one recipient has vanished.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/threads_test.go`:

```go
func TestThreadPrefix(t *testing.T) {
	task := threadSubject{TaskID: 722, OwnerSide: "orch-1"}
	if got := threadPrefix(task, 3, "reply", "cto"); got != "[task #722 Q3 reply from cto]" {
		t.Errorf("task prefix = %q", got)
	}
	role := threadSubject{RoleID: "cto", OwnerSide: "cto"}
	if got := threadPrefix(role, 2, "answer", "human"); got != "[role cto Q2 answer from human]" {
		t.Errorf("role prefix = %q", got)
	}
}

// TestParticipantFanOut_SkipsAuthorAndHuman drives the real delivery plumbing:
// a live orchestrator session and a live agent session both receive the body,
// the author does not, and the human is never injected into.
func TestParticipantFanOut_SkipsAuthorAndHuman(t *testing.T) {
	d := messagesTestDeps(t)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	addLiveAgentSession(t, d, "cto")
	if err := d.Store.AddAgent(store.Agent{
		ID: "cto", ProjectID: "platform", Dir: "/tmp/cto", Command: "claude", Enabled: true,
	}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	subj := threadSubject{TaskID: 7, OwnerSide: "orch-1"}
	err := participantFanOut(d, subj, 1, "reply", "cto", "the body",
		[]string{"cto", "human", "orch-1"})
	if err != nil {
		t.Fatalf("participantFanOut: %v", err)
	}

	orchMsgs, err := d.Store.ListMessagesForSession("orch-1")
	if err != nil {
		t.Fatalf("ListMessagesForSession(orch-1): %v", err)
	}
	if len(orchMsgs) != 1 {
		t.Fatalf("orch-1 got %d messages, want 1", len(orchMsgs))
	}
	if want := "[task #7 Q1 reply from cto] the body"; orchMsgs[0].Body != want {
		t.Errorf("orch-1 body = %q, want %q", orchMsgs[0].Body, want)
	}

	ctoMsgs, err := d.Store.ListMessagesForSession("cto")
	if err != nil {
		t.Fatalf("ListMessagesForSession(cto): %v", err)
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
	if err := d.Store.AddAgent(store.Agent{
		ID: "cto", ProjectID: "platform", Dir: "/tmp/cto", Command: "claude", Enabled: true,
	}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	subj := threadSubject{TaskID: 7, OwnerSide: "orch-1"}
	if err := participantFanOut(d, subj, 1, "question", "orch-1", "wake up",
		[]string{"cto", "human", "orch-1"}); err != nil {
		t.Fatalf("participantFanOut: %v", err)
	}

	inbox, err := d.Store.ListInboxMessages("cto", true)
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
```

Before writing these, confirm the exact signatures of `AddAgent`,
`ListMessagesForSession` and `ListInboxMessages` with
`grep -n 'func (s \*Store) \(AddAgent\|ListMessagesForSession\|ListInboxMessages\)' internal/store/*.go`
and adjust the calls to match — the store is the authority, not this plan.

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/api/ -run 'TestThreadPrefix|TestParticipantFanOut' -v`
Expected: FAIL — `undefined: threadPrefix`, `undefined: participantFanOut`.

- [ ] **Step 3: Write the implementation**

In `internal/api/agent_delivery.go`, add:

```go
// deliverToSession enqueues body to an ephemeral session — an orchestrator or
// a worker taking part in a thread. It is the generalisation of the old
// deliverToOrchestrator: the same enqueue pattern POST /v1/messages uses
// (insert queued, publish message.queued, wake the delivery worker), with the
// recipient named directly instead of derived from a task.
//
// Unlike an agent, an ephemeral session has no inbox: if it is gone or
// terminal the message is dropped with a log, because there is nothing to
// deliver it to later. The thread record itself is written regardless.
func deliverToSession(d Deps, sessionID, body string) error {
	if sessionID == "" {
		return nil
	}
	body = rewriteAttachmentLinks(d, body)

	sess, err := d.Store.GetSession(sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			slog.Warn("api: thread delivery target session not found, skipping", "session_id", sessionID)
			return nil
		}
		return err
	}
	if isSessionTerminal(sess.State) {
		slog.Warn("api: thread delivery target session is terminal, skipping",
			"session_id", sessionID, "state", sess.State)
		return nil
	}

	id, err := d.Store.AddMessage(store.Message{ToSession: sessionID, Body: body})
	if err != nil {
		return err
	}

	if d.Bus != nil {
		d.Bus.Publish("message.queued", sessionID, map[string]any{
			"id": id, "from": "", "to": sessionID,
		})
	}
	if d.Queue != nil {
		d.Queue.Wake(sessionID)
	} else {
		slog.Warn("api: thread message queued with nil Queue, will not be delivered until daemon restart",
			"id", id, "to", sessionID)
	}
	return nil
}
```

Append to `internal/api/threads.go` (imports gain `fmt`, `log/slog`, `errors`):

```go
// threadPrefix renders the frame every delivered thread entry carries, so a
// recipient can tell a thread message from a plain one and see at a glance
// which thread, which entry and which author it came from. Spec §4 keeps the
// pre-participant shapes and appends " from <id>".
func threadPrefix(subj threadSubject, ordinal int, kind, author string) string {
	if subj.RoleID != "" {
		return fmt.Sprintf("[role %s Q%d %s from %s]", subj.RoleID, ordinal, kind, author)
	}
	return fmt.Sprintf("[task #%d Q%d %s from %s]", subj.TaskID, ordinal, kind, author)
}

// participantFanOut delivers one thread entry to every participant except its
// author (spec §4, acceptance criterion 4). A persistent agent goes through
// deliverToAgent, so a live one is injected and a dead one accumulates an
// inbox row it will be told about on wake-up; an ephemeral session goes
// through deliverToSession; the human is never injected into — the thread is
// the human's inbox.
//
// A participant that resolves to neither an agent nor a session is logged and
// skipped: a vanished recipient must not fail the write that has already been
// recorded in the thread.
func participantFanOut(d Deps, subj threadSubject, ordinal int, kind, author, body string, participants []string) error {
	framed := threadPrefix(subj, ordinal, kind, author) + " " + body

	for _, p := range participants {
		if p == author || (store.IsHuman(p) && store.IsHuman(author)) {
			continue
		}
		if store.IsHuman(p) {
			continue
		}

		if _, err := d.Store.GetAgent(p); err == nil {
			if _, _, err := deliverToAgent(d, p, author, framed); err != nil {
				return err
			}
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}

		if _, err := d.Store.GetSession(p); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				slog.Warn("api: thread participant is neither an agent nor a session, skipping",
					"participant", p)
				continue
			}
			return err
		}
		if err := deliverToSession(d, p, framed); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the test — expect PASS**

Run: `go test ./internal/api/ -run 'TestThreadPrefix|TestParticipantFanOut' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/threads.go internal/api/agent_delivery.go internal/api/threads_test.go
git commit -m "api: fan out thread entries to every participant but the author"
```

---

## Task 4: `participants` / `waiting_on` / `your_turn` / `addressed_to` on task-thread reads

**Files:**
- Modify: `internal/api/questions.go:14-118`
- Test: `internal/api/questions_test.go`

**Interfaces:**
- Consumes: Task 1's `waitingOn`, `whoseTurnCompat`, `callerParticipant`,
  `contains`; `store.ListParticipants` from T1.
- Produces: the wire shape in "Interfaces produced" above.

`buildQuestionResponse` gains the caller, because `your_turn` is
caller-relative. The old `whoseTurn(q, msgs)` helper is deleted and its two
unit tests in `questions_test.go:42-55` are rewritten against `waitingOn` —
`TestWhoseTurn_UserOpenedNoMessages` and `TestWhoseTurn_OrchestratorOpenedNoMessages`
already assert exactly the two no-message cases Task 1's table covers, so they
become assertions on the assembled response instead.

- [ ] **Step 1: Write the failing test**

In `internal/api/questions_test.go`, delete `TestWhoseTurn_UserOpenedNoMessages`
and `TestWhoseTurn_OrchestratorOpenedNoMessages` (lines 42-55) and add:

```go
// TestGetTaskQuestions_ParticipantFields covers the additive wire contract:
// an orchestrator-opened thread has the human and the orchestrator as
// participants, waits on the human, and reports your_turn for a human caller.
func TestGetTaskQuestions_ParticipantFields(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which approach?"})
	resp.Body.Close()

	got := getQuestions(t, srv, taskID, "")
	if len(got) != 1 {
		t.Fatalf("got %d questions, want 1", len(got))
	}
	q := got[0]
	if !reflect.DeepEqual(q.Participants, []string{"human", "orch-1"}) {
		t.Errorf("participants = %v, want [human orch-1]", q.Participants)
	}
	if !reflect.DeepEqual(q.WaitingOn, []string{"human"}) {
		t.Errorf("waiting_on = %v, want [human]", q.WaitingOn)
	}
	if !q.YourTurn {
		t.Error("your_turn = false for a human caller, want true")
	}
	if q.WhoseTurn != "user" {
		t.Errorf("whose_turn = %q, want user (compat)", q.WhoseTurn)
	}
}

// TestGetTaskQuestions_YourTurnIsCallerRelative: the same thread reads as
// not-your-turn for the orchestrator that opened it.
func TestGetTaskQuestions_YourTurnIsCallerRelative(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which approach?"})
	resp.Body.Close()

	got := getQuestions(t, srv, taskID, "orch-1")
	if len(got) != 1 {
		t.Fatalf("got %d questions, want 1", len(got))
	}
	if got[0].YourTurn {
		t.Error("your_turn = true for the asker, want false")
	}
}
```

Add a helper next to `decodeQuestion`, and `"reflect"` to the imports:

```go
// getQuestions GETs a task's threads as the given caller ("" = the human).
func getQuestions(t *testing.T, srv *httptest.Server, taskID int64, sessionID string) []questionResponse {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if sessionID != "" {
		req.Header.Set(sessionHeader, sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET questions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET questions = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Questions []questionResponse `json:"questions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode questions: %v", err)
	}
	return body.Questions
}
```

Add `"net/http/httptest"` to the imports if it is not already there.

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/api/ -run TestGetTaskQuestions_ -v`
Expected: FAIL — `q.Participants undefined`, `q.WaitingOn undefined`,
`q.YourTurn undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/api/questions.go`:

```go
type questionMessageResponse struct {
	ID          int64    `json:"id"`
	Author      string   `json:"author,omitempty"`
	Kind        string   `json:"kind"`
	Body        string   `json:"body"`
	AddressedTo []string `json:"addressed_to,omitempty"`
	CreatedAt   int64    `json:"created_at"`
}

type questionResponse struct {
	ID           int64                     `json:"id"`
	TaskID       int64                     `json:"task_id"`
	Ordinal      int                       `json:"ordinal"`
	AskedBy      string                    `json:"asked_by"`
	Body         string                    `json:"body"`
	Context      string                    `json:"context,omitempty"`
	Status       string                    `json:"status"`
	Resolution   string                    `json:"resolution,omitempty"`
	Participants []string                  `json:"participants"`
	WaitingOn    []string                  `json:"waiting_on"`
	YourTurn     bool                      `json:"your_turn"`
	WhoseTurn    string                    `json:"whose_turn,omitempty"`
	AskedAt      int64                     `json:"asked_at"`
	ResolvedAt   int64                     `json:"resolved_at,omitempty"`
	Messages     []questionMessageResponse `json:"messages"`
}
```

`toQuestionMessageResponse` gains `AddressedTo: m.AddressedTo`. Delete
`whoseTurn` entirely and rewrite the builder:

```go
// buildQuestionResponse loads a thread's messages, participants and ordinal
// and assembles the full API response. caller decides your_turn, which is the
// one caller-relative field in the shape.
func buildQuestionResponse(d Deps, caller *store.Session, q store.Question) (questionResponse, error) {
	msgs, err := d.Store.ListQuestionMessages(q.ID)
	if err != nil {
		return questionResponse{}, err
	}
	participants, err := d.Store.ListParticipants(q.ID)
	if err != nil {
		return questionResponse{}, err
	}
	ordinal, err := d.Store.QuestionOrdinal(q)
	if err != nil {
		return questionResponse{}, err
	}

	msgOut := make([]questionMessageResponse, len(msgs))
	for i, m := range msgs {
		msgOut[i] = toQuestionMessageResponse(m)
	}

	waiting := waitingOn(q, msgs, participants)
	return questionResponse{
		ID:           q.ID,
		TaskID:       q.TaskID,
		Ordinal:      ordinal,
		AskedBy:      q.AskedBy,
		Body:         q.Body,
		Context:      q.Context,
		Status:       q.Status,
		Resolution:   q.Resolution,
		Participants: participants,
		WaitingOn:    waiting,
		YourTurn:     contains(waiting, callerParticipant(caller)),
		WhoseTurn:    whoseTurnCompat(waiting, "orchestrator"),
		AskedAt:      q.AskedAt,
		ResolvedAt:   q.ResolvedAt,
		Messages:     msgOut,
	}, nil
}
```

Thread the caller through every `buildQuestionResponse` call site. Four of the
five handlers already resolve `caller`; `handleGetTaskQuestions` and
`handleGetAllQuestions` do not, so add at the top of each:

```go
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
```

- [ ] **Step 4: Run the test — expect PASS**

Run: `go test ./internal/api/ -run TestGetTaskQuestions_ -v`
Expected: PASS.

- [ ] **Step 5: Run the whole API suite**

Run: `go test ./internal/api/`
Expected: PASS. Failures here are compile errors at `buildQuestionResponse`
call sites — fix them, do not weaken the tests.

- [ ] **Step 6: Commit**

```bash
git add internal/api/questions.go internal/api/questions_test.go
git commit -m "api: task threads expose participants, waiting_on and your_turn"
```

---

## Task 5: `to`, participant-based permissions and fan-out on task threads

**Files:**
- Modify: `internal/api/questions.go:275-620`
- Test: `internal/api/questions_test.go`

**Interfaces:**
- Consumes: Tasks 1-4; `store.AddParticipants` from T1.
- Produces: the request-body contract (`"to": [...]`) and acceptance criteria
  1-5 on task threads.

Three handlers change. In all three the shape is the same: resolve the caller,
build the `threadSubject`, check the right predicate, write the row, register
participants, fan out.

`deliverToOrchestrator` (`questions.go:238`) is deleted; its work is now
`participantFanOut`, which reaches the orchestrator because the orchestrator is
a participant.

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/questions_test.go`:

```go
// setupQuestionAgent registers persistent agent "cto" with no live session.
func setupQuestionAgent(t *testing.T, d Deps) {
	t.Helper()
	if err := d.Store.AddAgent(store.Agent{
		ID: "cto", ProjectID: "platform", Dir: "/tmp/cto", Command: "claude", Enabled: true,
	}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
}

// TestPostTaskQuestions_ToAddsParticipant covers acceptance criterion 2: an
// orchestrator addresses cto, who becomes a participant and is waited on.
func TestPostTaskQuestions_ToAddsParticipant(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	setupQuestionAgent(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Approve the schema?", "to": []string{"cto"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeQuestion(t, resp)

	if !reflect.DeepEqual(q.Participants, []string{"cto", "human", "orch-1"}) {
		t.Errorf("participants = %v, want [cto human orch-1]", q.Participants)
	}
	if !reflect.DeepEqual(q.WaitingOn, []string{"cto"}) {
		t.Errorf("waiting_on = %v, want [cto]", q.WaitingOn)
	}

	inbox, err := d.Store.ListInboxMessages("cto", true)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("cto inbox has %d messages, want 1", len(inbox))
	}
}

// TestPostQuestionAnswer_AgentMayAnswer covers acceptance criterion 1: a
// persistent agent with ROCKET_SESSION_ID set closes the thread.
func TestPostQuestionAnswer_AgentMayAnswer(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	setupQuestionAgent(t, d)
	addLiveAgentSession(t, d, "cto")

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Approve the schema?", "to": []string{"cto"}})
	q := decodeQuestion(t, resp)
	resp.Body.Close()

	ans := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "cto",
		map[string]any{"body": "Approved."})
	defer ans.Body.Close()
	if ans.StatusCode != http.StatusOK {
		t.Fatalf("agent answer = %d, want 200", ans.StatusCode)
	}
	got := decodeQuestion(t, ans)
	if got.Status != "resolved" || got.Resolution != "answered" {
		t.Errorf("status/resolution = %q/%q, want resolved/answered", got.Status, got.Resolution)
	}
	if len(got.WaitingOn) != 0 {
		t.Errorf("waiting_on = %v, want empty for a resolved thread", got.WaitingOn)
	}
}

// TestPostQuestionAnswer_OrchestratorForbidden covers acceptance criterion 5.
func TestPostQuestionAnswer_OrchestratorForbidden(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions",
		map[string]any{"body": "What now?"})
	q := decodeQuestion(t, resp)
	resp.Body.Close()

	ans := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "orch-1",
		map[string]any{"body": "Done."})
	defer ans.Body.Close()
	if ans.StatusCode != http.StatusForbidden {
		t.Fatalf("orchestrator answer = %d, want 403", ans.StatusCode)
	}
	var e struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(ans.Body).Decode(&e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if !strings.Contains(e.Message, "reply") {
		t.Errorf("403 message = %q, want it to point at reply", e.Message)
	}
}

// TestPostQuestionReply_HumanDelegatesWithTo covers acceptance criterion 3.
func TestPostQuestionReply_HumanDelegatesWithTo(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	setupQuestionAgent(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which approach?"})
	q := decodeQuestion(t, resp)
	resp.Body.Close()

	rep := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply",
		map[string]any{"body": "cto decides.", "to": []string{"cto"}})
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusCreated {
		t.Fatalf("human reply = %d, want 201", rep.StatusCode)
	}
	got := decodeQuestion(t, rep)
	if !contains(got.Participants, "cto") {
		t.Errorf("participants = %v, want cto included", got.Participants)
	}
	if !reflect.DeepEqual(got.WaitingOn, []string{"cto"}) {
		t.Errorf("waiting_on = %v, want [cto]", got.WaitingOn)
	}
	if got.Messages[0].AddressedTo == nil {
		t.Error("addressed_to must round-trip on the wire")
	}

	inbox, err := d.Store.ListInboxMessages("cto", true)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(inbox) != 1 {
		t.Errorf("cto inbox has %d messages, want 1", len(inbox))
	}
}

// TestPostQuestionReply_NonParticipantWorkerForbidden: participation, not the
// old "human or my own orchestrator" pair, is what grants reply.
func TestPostQuestionReply_NonParticipantWorkerForbidden(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	addTestSession(t, d, "w-9", "worker", "proj1")

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which approach?"})
	q := decodeQuestion(t, resp)
	resp.Body.Close()

	rep := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "w-9",
		map[string]any{"body": "me too"})
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusForbidden {
		t.Errorf("non-participant worker reply = %d, want 403", rep.StatusCode)
	}
}
```

Add `"strings"` to the test file's imports.

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/api/ -run 'TestPostTaskQuestions_To|TestPostQuestionAnswer_|TestPostQuestionReply_' -v`
Expected: FAIL — `to` is ignored so `participants` lacks `cto`; the agent
answer is 403; the orchestrator answer is 403 but with the old message.

- [ ] **Step 3: Implement `handlePostTaskQuestions`**

```go
type postQuestionRequest struct {
	Body    string   `json:"body"`
	Context string   `json:"context"`
	To      []string `json:"to"`
}
```

Replace the permission check with the subject-based one, and replace the
`caller == nil` delivery block with an unconditional fan-out:

```go
	subj := threadSubject{TaskID: task.ID, OwnerSide: task.SessionID}
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if !canOpenThread(d, caller, subj) {
		writeErr(w, http.StatusForbidden, "forbidden",
			"only the human user, a persistent agent or the task's own orchestrator may ask questions")
		return
	}
```

after the row is inserted:

```go
	// Every thread has the human and the subject's own counterpart in it from
	// the start, so a human-opened thread always has somebody to reach and the
	// orchestrator never has to be added by hand. --to joins the addressees.
	// An unattached task has no counterpart to seed — seeding "" would be
	// canonicalised to "human" and invent a participant that is not there.
	author := callerParticipant(caller)
	ids := []string{store.ParticipantHuman, author}
	if task.SessionID != "" {
		ids = append(ids, task.SessionID)
	}
	ids = append(ids, req.To...)
	if err := d.Store.AddParticipants(qid, ids...); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	q, ok := getQuestionOr404(w, d, qid)
	if !ok {
		return
	}
	ordinal, err := d.Store.QuestionOrdinal(q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	participants, err := d.Store.ListParticipants(qid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	text := req.Body
	if req.Context != "" {
		text += "\n\n" + req.Context
	}
	if err := participantFanOut(d, subj, ordinal, "question", author, text, participants); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
```

Note what is **not** here: the recipient list is never narrowed to `req.To`.
Acceptance criterion 4 is literal — every message reaches every participant but
its author. `to` decides who must *respond* (`waiting_on`), not who is
*notified*.

- [ ] **Step 4: Implement `handlePostQuestionReply`**

```go
type postQuestionReplyRequest struct {
	Body string   `json:"body"`
	To   []string `json:"to"`
}
```

Replace the permission check:

```go
	subj := threadSubject{TaskID: task.ID, OwnerSide: task.SessionID}
	participants, err := d.Store.ListParticipants(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !canPostToThread(d, caller, subj, participants) {
		writeErr(w, http.StatusForbidden, "forbidden",
			"only a participant of this thread may reply")
		return
	}
```

The reopen rule is unchanged: a resolved thread is final for the human (409),
any other caller reopens it. Store the message with its addressees, register
participants, fan out:

```go
	author := callerParticipant(caller)
	if _, err := d.Store.AddQuestionMessage(store.QuestionMessage{
		QuestionID:  id,
		Author:      author,
		Kind:        "reply",
		Body:        req.Body,
		AddressedTo: req.To,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	ids := append([]string{author}, req.To...)
	if err := d.Store.AddParticipants(id, ids...); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
```

and after the reopen event, replacing the `caller == nil` delivery block:

```go
	ordinal, err := d.Store.QuestionOrdinal(q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	// Re-read: req.To has just joined and must be notified too.
	recipients, err := d.Store.ListParticipants(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if err := participantFanOut(d, subj, ordinal, "reply", author, req.Body, recipients); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
```

- [ ] **Step 5: Implement `handlePostQuestionAnswer`**

```go
type postQuestionAnswerRequest struct {
	Body    string   `json:"body"`
	Dismiss bool     `json:"dismiss"`
	To      []string `json:"to"`
}
```

Replace the human-only check:

```go
	if !canAnswerThread(d, caller) {
		writeErr(w, http.StatusForbidden, "forbidden",
			"only the human user or a persistent agent may answer; use reply")
		return
	}
```

The answer message is stored with `Author: callerParticipant(caller)` — it used
to be left empty, which was correct only while the human was the only answerer.
The delivery block becomes the same `participantFanOut` call as in Step 4, with
kind `"answer"`. `dismiss` still writes no message and delivers nothing.

- [ ] **Step 6: Run the tests — expect PASS**

Run: `go test ./internal/api/ -run 'TestPostTaskQuestions|TestPostQuestionAnswer|TestPostQuestionReply' -v`
Expected: PASS.

- [ ] **Step 7: Run the whole API suite**

Run: `go test ./internal/api/`
Expected: PASS. Existing tests that assert the old delivery body
(`[task #N QM reply] ...` without ` from <id>`) must be updated to the new
frame — that is the intended contract change, not a regression.

- [ ] **Step 8: Commit**

```bash
git add internal/api/questions.go internal/api/questions_test.go
git commit -m "api: task threads take --to, participant permissions and fan-out"
```

---

## Task 6: The same on role threads

**Files:**
- Modify: `internal/api/agent_questions.go`
- Test: `internal/api/agent_questions_test.go`

**Interfaces:**
- Consumes: Tasks 1-5.
- Produces: acceptance criterion 7 — `/v1/agents/{id}/questions` keeps its
  contract while gaining the same additive fields.

The role handlers mirror the task ones with `threadSubject{RoleID: a.ID,
OwnerSide: a.ID}`. Two differences from Task 5:

- `whoseTurnCompat(waiting, "role")` — role threads say `role`, not
  `orchestrator`;
- the store facade is the `AgentQuestion*` family, whose `AgentQuestionMessage`
  is a distinct type. Check whether it carries `AddressedTo`
  (`grep -n 'AddressedTo' internal/store/agent_questions.go`). If it does not,
  use the unified `store.AddQuestionMessage` / `ListQuestionMessages` /
  `ListParticipants` directly against the same ids — T1 made both families one
  table, so the unified DAO reads role threads correctly, and the facade is
  only needed where its extra ordinal scoping matters.

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/agent_questions_test.go`:

```go
// TestAgentQuestion_ParticipantFields: a role thread carries the same
// additive fields as a task thread, with the role's own whose_turn word.
func TestAgentQuestion_ParticipantFields(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "cto")

	resp := postJSON(t, srv.URL+"/v1/agents/cto/questions",
		map[string]any{"body": "Status?"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var q agentQuestionResponse
	if err := json.NewDecoder(resp.Body).Decode(&q); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !reflect.DeepEqual(q.Participants, []string{"cto", "human"}) {
		t.Errorf("participants = %v, want [cto human]", q.Participants)
	}
	if !reflect.DeepEqual(q.WaitingOn, []string{"cto"}) {
		t.Errorf("waiting_on = %v, want [cto]", q.WaitingOn)
	}
	if q.WhoseTurn != "role" {
		t.Errorf("whose_turn = %q, want role (compat)", q.WhoseTurn)
	}
	if q.YourTurn {
		t.Error("your_turn = true for the human asker, want false")
	}
}

// TestAgentQuestion_OrchestratorMayReplyWhenAddressed: an orchestrator named
// in --to joins a role thread and may reply, but still may not answer it.
func TestAgentQuestion_OrchestratorMayReplyWhenAddressed(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "cto")
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	resp := postJSON(t, srv.URL+"/v1/agents/cto/questions",
		map[string]any{"body": "Status?", "to": []string{"orch-1"}})
	var q agentQuestionResponse
	if err := json.NewDecoder(resp.Body).Decode(&q); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()

	rep := postJSONWithHeader(t, srv.URL+"/v1/agent-questions/"+itoa(q.ID)+"/reply", "orch-1",
		map[string]any{"body": "green"})
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusCreated {
		t.Fatalf("addressed orchestrator reply = %d, want 201", rep.StatusCode)
	}

	ans := postJSONWithHeader(t, srv.URL+"/v1/agent-questions/"+itoa(q.ID)+"/answer", "orch-1",
		map[string]any{"body": "done"})
	defer ans.Body.Close()
	if ans.StatusCode != http.StatusForbidden {
		t.Errorf("orchestrator answer = %d, want 403", ans.StatusCode)
	}
}
```

Confirm the deps helper's real name with
`grep -n 'func agentQuestionsTestDeps\|func agentsTestDeps' internal/api/*_test.go`
and use whichever exists.

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/api/ -run TestAgentQuestion_ -v`
Expected: FAIL — `q.Participants undefined`; the addressed orchestrator gets
403 from `callerIsRoleInstance`.

- [ ] **Step 3: Implement**

`agentQuestionResponse` gains `Participants []string \`json:"participants"\``,
`WaitingOn []string \`json:"waiting_on"\`` and `YourTurn bool \`json:"your_turn"\``,
in the same positions as `questionResponse`.

`buildAgentQuestionResponse` gains the caller argument and mirrors Task 4's
builder, ending in
`WhoseTurn: whoseTurnCompat(waiting, "role")`. `whoseTurnAgent`
(`agent_questions.go:37`) is deleted along with any unit tests that call it
directly.

The three write handlers take `subj := threadSubject{RoleID: a.ID, OwnerSide: a.ID}`
and the predicates from Task 2, replacing every `callerIsRoleInstance` /
`writeAgentQuestionForbidden` check. `deliverHumanEntry`
(`agent_questions.go:129`) is deleted; `participantFanOut` replaces it and
already produces the `[role cto Q2 reply from X]` frame.

The request structs gain `To []string \`json:"to"\``, and the ask/reply/answer
bodies register participants exactly as in Task 5:
`{human, author, roleID} ∪ to` on ask, `{author} ∪ to` on reply and answer.

- [ ] **Step 4: Run the tests — expect PASS**

Run: `go test ./internal/api/ -run TestAgentQuestion_ -v`
Expected: PASS.

- [ ] **Step 5: Run the whole API suite**

Run: `go test ./internal/api/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/agent_questions.go internal/api/agent_questions_test.go
git commit -m "api: role threads on the shared participant layer"
```

---

## Task 7: End-to-end acceptance scenario

**Files:**
- Test: `internal/api/questions_test.go`

**Interfaces:**
- Consumes: everything above. Produces nothing — this is the gate.

One test walking the exact scenario spec §"Проверка" names: an orchestrator
asks `--to cto`, cto replies, cto answers and the thread closes. It exists to
catch the seams the per-task tests each see only one side of.

- [ ] **Step 1: Write the test**

```go
// TestQuestionThread_OrchestratorAsksAgentAnswers walks acceptance criteria
// 1, 2 and 4 end to end: the orchestrator addresses cto, cto is notified and
// becomes a participant, cto replies (which notifies the orchestrator), and
// cto's answer closes the thread.
func TestQuestionThread_OrchestratorAsksAgentAnswers(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	setupQuestionAgent(t, d)
	addLiveAgentSession(t, d, "cto")

	ask := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Approve the schema?", "to": []string{"cto"}})
	q := decodeQuestion(t, ask)
	ask.Body.Close()
	if !reflect.DeepEqual(q.WaitingOn, []string{"cto"}) {
		t.Fatalf("after ask waiting_on = %v, want [cto]", q.WaitingOn)
	}

	ctoMsgs, err := d.Store.ListMessagesForSession("cto")
	if err != nil {
		t.Fatalf("ListMessagesForSession(cto): %v", err)
	}
	if len(ctoMsgs) != 1 {
		t.Fatalf("cto got %d queued messages after ask, want 1", len(ctoMsgs))
	}

	rep := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "cto",
		map[string]any{"body": "One question first."})
	got := decodeQuestion(t, rep)
	rep.Body.Close()
	if !reflect.DeepEqual(got.WaitingOn, []string{"human", "orch-1"}) {
		t.Errorf("after cto reply waiting_on = %v, want [human orch-1]", got.WaitingOn)
	}

	orchMsgs, err := d.Store.ListMessagesForSession("orch-1")
	if err != nil {
		t.Fatalf("ListMessagesForSession(orch-1): %v", err)
	}
	if len(orchMsgs) == 0 {
		t.Fatal("the orchestrator was not notified of cto's reply")
	}
	if want := "[task #" + itoa(taskID) + " Q1 reply from cto] One question first."; orchMsgs[len(orchMsgs)-1].Body != want {
		t.Errorf("orchestrator body = %q, want %q", orchMsgs[len(orchMsgs)-1].Body, want)
	}

	ans := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "cto",
		map[string]any{"body": "Approved."})
	final := decodeQuestion(t, ans)
	ans.Body.Close()
	if final.Status != "resolved" || final.Resolution != "answered" {
		t.Fatalf("status/resolution = %q/%q, want resolved/answered", final.Status, final.Resolution)
	}
	if len(final.WaitingOn) != 0 {
		t.Errorf("waiting_on = %v, want empty", final.WaitingOn)
	}
	if final.WhoseTurn != "" {
		t.Errorf("whose_turn = %q, want empty for a resolved thread", final.WhoseTurn)
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./internal/api/ -run TestQuestionThread_OrchestratorAsksAgentAnswers -v`
Expected: PASS if Tasks 1-6 are right. A failure here is a real integration
bug — use `superpowers:systematic-debugging`, do not adjust the assertions to
match the behaviour.

- [ ] **Step 3: Commit**

```bash
git add internal/api/questions_test.go
git commit -m "api: end-to-end participant thread scenario"
```

---

## Task 8: Read permissions

**Files:**
- Modify: `internal/api/threads.go`, `internal/api/questions.go`,
  `internal/api/agent_questions.go`
- Test: `internal/api/threads_test.go`, `internal/api/questions_test.go`

**Interfaces:**
- Consumes: Task 2's predicates; `store.GetTaskBySessionID`
  (`internal/store/tasks.go:98`), `store.Task.ParentID`.
- Produces: `canReadThread`, `callerOwnsRootTask`.

Today `GET /v1/tasks/{id}/questions` and `GET /v1/agents/{id}/questions` have
**no** caller check. The orchestrator dictated the exact rule to enforce, and
its wording goes on `canReadThread` as the doc comment:

- a human caller (no session header): every thread;
- a `kind=agent` caller: every thread — persistent agents are org-wide roles and
  need to see what they may be pulled into;
- an orchestrator or worker: a thread it participates in, **or** a thread of
  "its own task", where own task means the **root** task its session belongs
  to — for a worker that is the root task above its subtask, not only the
  subtask itself.

That last clause carries the weight. Questions only exist on root tasks, so a
naive `task.SessionID == caller.ID` check would 403 every worker reading its
feature's threads for context, which is real current usage. Resolve the
worker's subtask up to its parent and compare against that. With this rule the
only thing that becomes forbidden is cross-task snooping by an unrelated
session.

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/threads_test.go`:

```go
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

	subj := threadSubject{TaskID: rootID, OwnerSide: "orch-1"}
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

	subj := threadSubject{TaskID: rootID, OwnerSide: "orch-1"}
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
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/api/ -run TestCanReadThread -v`
Expected: FAIL — `undefined: canReadThread`.

- [ ] **Step 3: Implement**

Append to `internal/api/threads.go`:

```go
// callerOwnsRootTask reports whether caller's session belongs to the root task
// rootID — either directly, or as a worker on one of its subtasks. Questions
// only ever hang off root tasks, so a worker's own subtask never matches by
// id; its parent is what has to be compared. A session with no task at all
// (an agent, or a session whose task is gone) owns nothing.
func callerOwnsRootTask(d Deps, caller *store.Session, rootID int64) bool {
	if caller == nil || rootID == 0 {
		return false
	}
	task, err := d.Store.GetTaskBySessionID(caller.ID)
	if err != nil {
		return false
	}
	if task.ID == rootID {
		return true
	}
	if task.ParentID == 0 {
		return false
	}
	return task.ParentID == rootID
}

// canReadThread reports whether caller may read a thread on subj:
//
//   - a human caller (no session header): every thread;
//   - a kind=agent caller: every thread — persistent agents are org-wide roles,
//     and they need to see what they may be pulled into;
//   - an orchestrator or worker: a thread it participates in, or a thread of
//     its own task, where own task means the ROOT task its session belongs to.
//     For a worker that is the root task above its subtask, not only the
//     subtask itself: questions only exist on root tasks, so comparing against
//     the subtask alone would forbid every worker from reading its feature's
//     threads for context, which is real current usage.
//
// The only thing this forbids that used to be allowed is cross-task snooping
// by an unrelated session.
func canReadThread(d Deps, caller *store.Session, subj threadSubject, participants []string) bool {
	if caller == nil || callerIsPersistentAgent(d, caller) {
		return true
	}
	if contains(participants, caller.ID) || callerIsOwnerSide(caller, subj) {
		return true
	}
	return callerOwnsRootTask(d, caller, subj.TaskID)
}
```

Gate `handleGetTaskQuestions` per thread — a task's listing drops the threads
the caller may not see rather than 403-ing the whole request, so a legitimate
caller is never blocked by one unrelated thread:

```go
	participants, err := d.Store.ListParticipants(q.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !canReadThread(d, caller, subj, participants) {
		continue
	}
```

Apply the same filter in `handleGetAllQuestions` (its `subj` comes from the
task it already loads) and in `handleGetAgentQuestions` (where
`subj = threadSubject{RoleID: a.ID, OwnerSide: a.ID}`). The single-thread
`reply`/`answer` paths need no read gate: their write predicates are strictly
narrower.

- [ ] **Step 4: Run the tests — expect PASS**

Run: `go test ./internal/api/ -run TestCanReadThread -v`
Expected: PASS.

- [ ] **Step 5: Run the whole API suite**

Run: `go test ./internal/api/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/threads.go internal/api/questions.go \
        internal/api/agent_questions.go internal/api/threads_test.go
git commit -m "api: participant-based thread read permissions"
```

---

## Task 9: Verification and PR

This repository has **no CI** (`.github/` does not exist), so local
verification is the only gate. Run it honestly and put the real commands and
their real output in the PR body.

- [ ] `gofmt -l internal/` — must print nothing.
- [ ] `go build ./...`
- [ ] `go vet ./...`
- [ ] `go test ./...` — the whole tree, not just `internal/api`. `internal/cli`
      and `internal/heartbeat` consume these handlers and may need a compile fix
      (not a behaviour change — that is T3's).
- [ ] Walk the brief's nine acceptance criteria and name the test covering
      each. Criterion 7 (`messages[].author` still `""` for the human) has no
      new test of its own — confirm `wireAuthor()` is still called from both
      builders and that an existing test asserts it.
- [ ] `superpowers:verification-before-completion` before claiming done.
- [ ] `gh pr create` referencing feature `reply-answer` and subtask #731.
- [ ] `rocket send reply-answer-orch "<PR URL>"`.

---

## Self-review notes

- **Spec coverage:** §2 (`waiting_on` / `your_turn` / `whose_turn`) → Tasks 1,
  4, 6. §3 write rights → Tasks 2, 5, 6; §3 read rights → Task 8. §4 (fan-out
  and prefixes) → Tasks 3, 5, 6. §1 is T1's, §5 is T3's, §6 is T4/T5's, §7 is
  T6's.
- **Brief acceptance criteria:** 1 → Task 5 (`TestPostQuestionAnswer_AgentMayAnswer`)
  and Task 7; 2 → Task 5 (`TestPostTaskQuestions_ToAddsParticipant`) and Task 3
  (`TestParticipantFanOut_DeadAgentGetsInbox`); 3 → Task 5
  (`TestPostQuestionReply_HumanDelegatesWithTo`); 4 → Task 3 and Task 7; 5 →
  Task 5 (`TestPostQuestionAnswer_OrchestratorForbidden`); 6 → Task 1
  (`TestWaitingOn`, `TestWhoseTurnCompat`) and Task 4; 7 → Task 9's check that
  `wireAuthor()` survives; 8 → Task 6; 9 → Task 9.
- **All four earlier open questions are now decided** by the orchestrator; see
  "Decisions confirmed" above. Nothing in this plan is left hanging on a reply.
- **Deliberate contract change:** existing tests asserting
  `[task #N QM reply] ...` without ` from <id>` are updated, per spec §4 —
  confirmed safe because readers key on the prefix, not the whole frame.
