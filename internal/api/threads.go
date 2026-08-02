// Core of the participant model shared by task threads (questions.go) and
// role threads (agent_questions.go). Everything here is subject-agnostic: a
// threadSubject says which of the two a thread is, and the turn, permission
// and delivery logic is written once against that.
package api

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// threadSubject describes what a thread hangs off. Exactly one of TaskID and
// RoleID is set. Counterpart is the participant id of the subject's own other
// side — the task's orchestrator session, or the role itself — which is seeded
// as a participant of every thread on that subject so a human-opened thread
// always has somebody to reach. It is empty when there is none, e.g. a task
// with no orchestrator attached.
type threadSubject struct {
	TaskID      int64
	RoleID      string
	Counterpart string
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

// sameParticipant reports whether two participant ids denote the same party,
// treating every spelling of the human as one.
func sameParticipant(a, b string) bool {
	return a == b || (store.IsHuman(a) && store.IsHuman(b))
}

// contains reports whether ids holds id, by the same identity rule.
func contains(ids []string, id string) bool {
	for _, got := range ids {
		if sameParticipant(got, id) {
			return true
		}
	}
	return false
}

// lastAuthor returns the participant id that spoke last in a thread. With no
// messages that is the question's own author, where an empty asked_by means
// the human — asked_by is not a participant-id column, so it still carries the
// legacy empty form (see T1, subtask #730).
func lastAuthor(q store.Question, msgs []store.QuestionMessage) string {
	author := q.AskedBy
	if len(msgs) > 0 {
		author = msgs[len(msgs)-1].Author
	}
	if store.IsHuman(author) {
		return store.ParticipantHuman
	}
	return author
}

// waitingOn derives who is expected to speak next, per spec v1 §2 of task
// #722: a resolved thread waits on nobody; an explicitly addressed last
// message names its own addressees; otherwise everyone but whoever spoke last.
// The result is sorted so the API response and its tests are deterministic.
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
		if sameParticipant(p, author) {
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

// callerIsPersistentAgent reports whether caller is an instance of a
// registered kind=agent session — the class of caller that spec v1 §3 grants
// the same thread rights as the human, including answer. d is unused today,
// but is part of the signature so a later fallback to the agents table does
// not have to touch every call site.
func callerIsPersistentAgent(d Deps, caller *store.Session) bool {
	return caller != nil && caller.Kind == session.AgentSessionKind
}

// canAnswerThread reports whether caller may resolve a thread (answer or
// dismiss). Spec v1 §3: the human and persistent agents only — an orchestrator
// or a worker gets 403 and is told to use reply instead, so a final decision
// always has a human or a standing role behind it.
func canAnswerThread(d Deps, caller *store.Session) bool {
	return caller == nil || callerIsPersistentAgent(d, caller)
}

// callerIsCounterpart reports whether caller is the subject's own other side:
// the orchestrator of the thread's task, or an instance of the thread's role.
func callerIsCounterpart(caller *store.Session, subj threadSubject) bool {
	if caller == nil || subj.Counterpart == "" {
		return false
	}
	return caller.ID == subj.Counterpart
}

// canOpenThread reports whether caller may open a thread on subj. Spec v1 §3:
// the human, any persistent agent, and the subject's own counterpart.
func canOpenThread(d Deps, caller *store.Session, subj threadSubject) bool {
	return caller == nil ||
		callerIsPersistentAgent(d, caller) ||
		callerIsCounterpart(caller, subj)
}

// canPostToThread reports whether caller may add a reply. Spec v1 §3: any
// participant may post. The human is a participant of every thread by
// construction, and the subject's counterpart is admitted even before it has
// spoken, which preserves today's "the task's orchestrator may always reply"
// behaviour on threads it has not yet touched.
func canPostToThread(d Deps, caller *store.Session, subj threadSubject, participants []string) bool {
	if caller == nil {
		return true
	}
	return contains(participants, caller.ID) || callerIsCounterpart(caller, subj)
}

// threadPrefix renders the frame every delivered thread entry carries, so a
// recipient can tell a thread message from a plain one and see at a glance
// which thread, which entry and which author it came from. Spec v1 §4 keeps
// the pre-participant shapes and appends " from <id>" — uniformly, including
// a human author: with several participants the frame is the only place the
// author is named.
func threadPrefix(subj threadSubject, ordinal int, kind, author string) string {
	if subj.RoleID != "" {
		return fmt.Sprintf("[role %s Q%d %s from %s]", subj.RoleID, ordinal, kind, author)
	}
	return fmt.Sprintf("[task #%d Q%d %s from %s]", subj.TaskID, ordinal, kind, author)
}

// participantFanOut delivers one thread entry to every participant except its
// author (spec v1 §4, acceptance criterion 4). A persistent agent goes through
// deliverToAgent, so a live one is injected and a dead one accumulates an
// inbox row it is told about on wake-up; an ephemeral session goes through
// deliverToSession; the human is never injected into — the thread itself is
// where the human reads.
//
// The recipient list is deliberately never narrowed to a message's
// addressed_to: "to" decides who must respond (waiting_on), not who is
// notified.
//
// A participant that resolves to neither an agent nor a session is logged and
// skipped: a vanished recipient must not fail a write already recorded in the
// thread.
func participantFanOut(d Deps, subj threadSubject, ordinal int, kind, author, body string, participants []string) error {
	framed := threadPrefix(subj, ordinal, kind, author) + " " + body

	for _, p := range participants {
		if sameParticipant(p, author) || store.IsHuman(p) {
			continue
		}

		_, err := d.Store.GetAgent(p)
		if err == nil {
			// The frame already names the author. Passing "human" as the
			// sender would make deliveryBody prefix a second "[from human]",
			// because no session is named "human"; a session-id author is
			// recorded on the message itself and adds no such prefix.
			from := author
			if store.IsHuman(from) {
				from = ""
			}
			if _, _, err := deliverToAgent(d, p, from, framed); err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, store.ErrNotFound) {
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

// callerOwnsRootTask reports whether caller's session belongs to the root task
// rootID — either directly, or as a worker on one of its subtasks. Questions
// only ever hang off root tasks, so a worker's own subtask never matches by
// id; its parent is what has to be compared. A session with no task at all (an
// agent, or one whose task is gone) owns nothing.
func callerOwnsRootTask(d Deps, caller *store.Session, rootID int64) bool {
	if caller == nil || rootID == 0 {
		return false
	}
	task, err := d.Store.GetTaskBySessionID(caller.ID)
	if err != nil {
		return false
	}
	return task.ID == rootID || (task.ParentID != 0 && task.ParentID == rootID)
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
	if contains(participants, caller.ID) || callerIsCounterpart(caller, subj) {
		return true
	}
	return callerOwnsRootTask(d, caller, subj.TaskID)
}
