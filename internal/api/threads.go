// Core of the participant model shared by task threads (questions.go) and
// role threads (agent_questions.go). Everything here is subject-agnostic: a
// threadSubject says which of the two a thread is, and the turn, permission
// and delivery logic is written once against that.
package api

import (
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
