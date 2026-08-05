// Core of the participant model shared by task threads (questions.go) and
// role threads (agent_questions.go). Everything here is subject-agnostic: a
// threadSubject says which of the two a thread is, and the turn, permission
// and delivery logic is written once against that.
package api

import (
	"time"

	"errors"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/heartbeat"
	"log/slog"
	"net/http"
	"strings"

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

// threadAttention returns the stored "whose turn" set of a thread — the
// replacement for the old waitingOn() derivation. A resolved thread waits on
// nobody regardless of what the table holds, so closing a thread can never
// leave a stale badge behind even if a clear were ever missed.
func threadAttention(d Deps, q store.Question) ([]string, error) {
	if q.Status != "open" {
		return nil, nil
	}
	return d.Store.ListAttention(q.ID)
}

// threadLocalRef renders the one user-facing thread id (spec v1 §«Тред и его
// id»): "1023/Q2" for a task thread, "cto/Q2" for a role thread. The global
// numeric id stays in the API but stops being what a human or an agent has to
// type — mistyping it across tasks was the original misdelivery bug.
func threadLocalRef(subj threadSubject, ordinal int) string {
	if subj.RoleID != "" {
		return fmt.Sprintf("%s/Q%d", subj.RoleID, ordinal)
	}
	return fmt.Sprintf("%d/Q%d", subj.TaskID, ordinal)
}

// threadSubjectLabel names what a thread hangs off, for echoes and guard
// errors: `task #1023 "Ship it"` or `role cto`.
func threadSubjectLabel(subj threadSubject, title string) string {
	if subj.RoleID != "" {
		return fmt.Sprintf("role %s", subj.RoleID)
	}
	return fmt.Sprintf("task #%d %q", subj.TaskID, title)
}

// threadEchoLimit is how much of a question's body an echo quotes: enough to
// recognise the thread, short enough to stay one line.
const threadEchoLimit = 60

// truncateForEcho shortens s to threadEchoLimit runes, appending an ellipsis
// when it had to cut. It counts runes, not bytes, so a Cyrillic question is not
// cut mid-character.
func truncateForEcho(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	r := []rune(s)
	if len(r) <= threadEchoLimit {
		return s
	}
	return string(r[:threadEchoLimit]) + "…"
}

// threadEcho renders the target confirmation every write prints back, so a
// misaddressed reply is visible immediately instead of hours later:
//
//	→ 1023/Q2 «Which approach?» (task #1023 "Ship it")
func threadEcho(subj threadSubject, ordinal int, body, title string) string {
	return fmt.Sprintf("→ %s «%s» (%s)",
		threadLocalRef(subj, ordinal), truncateForEcho(body), threadSubjectLabel(subj, title))
}

// threadWriteAccess decides whether caller may write into a thread, and — when
// it may not by default — whether an explicit join can let it through.
//
// Participants and the subject's own counterpart write as before. Everybody
// else is refused, but the refusal has two flavours (spec v1 §«Подтверждение
// цели»): the human and persistent agents are org-wide parties that legitimately
// get pulled into other people's threads, so they may retry with join=true and
// take responsibility for knowing where they are writing; an orchestrator or
// worker of another task stays refused outright, as today.
func threadWriteAccess(d Deps, caller *store.Session, subj threadSubject, participants []string) (allowed, joinable bool) {
	if contains(participants, callerParticipant(caller)) || callerIsCounterpart(caller, subj) {
		return true, true
	}
	if caller == nil || callerIsPersistentAgent(d, caller) {
		return false, true
	}
	return false, false
}

// notAParticipantMessage is the guard error: it names the thread the caller was
// about to write into, quotes it, lists who is in it, and says how to proceed on
// purpose. Reading it is meant to be enough to notice "this is not my thread".
func notAParticipantMessage(subj threadSubject, ordinal int, body, title string, participants []string) string {
	return fmt.Sprintf(
		"you are not a participant of %s: %s — participants: %s. If you really mean to write here, retry with join.",
		threadLocalRef(subj, ordinal), threadEcho(subj, ordinal, body, title), strings.Join(participants, ", "))
}

// threadCounts aggregates the open threads of one subject kind into per-subject
// counters. awaitingUser is deliberately not a second derivation: it is exactly
// "the human is in waiting_on", computed by the same waitingOn used to render a
// single thread, so a list badge and the thread it opens can never disagree.
// key picks the subject a thread belongs to and reports false for threads of
// the other kind.
func threadCounts[K comparable](threads []store.OpenThread, key func(store.OpenThread) (K, bool)) map[K]store.QuestionCounts {
	out := make(map[K]store.QuestionCounts)
	for _, th := range threads {
		k, ok := key(th)
		if !ok {
			continue
		}
		c := out[k]
		c.Open++
		if contains(th.Attention, store.ParticipantHuman) {
			c.AwaitingUser++
		}
		out[k] = c
	}
	return out
}

// taskQuestionCounts returns the open-question counters of every task with at
// least one open thread, in one aggregate read instead of a per-task query.
func taskQuestionCounts(d Deps) (map[int64]store.QuestionCounts, error) {
	threads, err := d.Store.ListOpenThreads()
	if err != nil {
		return nil, err
	}
	return threadCounts(threads, func(th store.OpenThread) (int64, bool) {
		return th.Question.TaskID, th.Question.TaskID != 0
	}), nil
}

// roleQuestionCounts is taskQuestionCounts for role threads, keyed by role id.
func roleQuestionCounts(d Deps) (map[string]store.QuestionCounts, error) {
	threads, err := d.Store.ListOpenThreads()
	if err != nil {
		return nil, err
	}
	return threadCounts(threads, func(th store.OpenThread) (string, bool) {
		return th.Question.RoleID, th.Question.RoleID != ""
	}), nil
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

// enforceThreadGuard applies the non-participant guard to a write. It returns
// the participant list the caller should carry on with — refreshed when an
// explicit join just added the caller — and false when it has already written
// the error response.
//
// The guard exists because answering used to check only WHO you are, never
// WHICH thread you were writing into: a persistent agent could resolve any
// thread in the system by id, and a mistyped id resolved somebody else's
// question silently and successfully.
func enforceThreadGuard(
	w http.ResponseWriter, d Deps, caller *store.Session, subj threadSubject,
	q store.Question, ordinal int, title string, participants []string, join bool,
) ([]string, bool) {
	allowed, joinable := threadWriteAccess(d, caller, subj, participants)
	if allowed {
		return participants, true
	}
	if !joinable {
		writeErr(w, http.StatusForbidden, "forbidden",
			"only a participant of this thread may write into it")
		return nil, false
	}
	if !join {
		writeErr(w, http.StatusForbidden, "not_a_participant",
			notAParticipantMessage(subj, ordinal, q.Body, title, participants))
		return nil, false
	}

	if err := d.Store.AddParticipants(q.ID, callerParticipant(caller)); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return nil, false
	}
	joined, err := d.Store.ListParticipants(q.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return nil, false
	}
	return joined, true
}

// chooseOptionBody resolves a 1-based --choose index into the option's own
// text, so a client never has to retype an answer the thread already offers.
// choose == 0 means "no choice made" and leaves body as it is.
func chooseOptionBody(w http.ResponseWriter, q store.Question, choose int, body string) (string, bool) {
	if choose == 0 {
		return body, true
	}
	if choose < 1 || choose > len(q.Options) {
		writeErr(w, http.StatusBadRequest, "invalid_choice",
			fmt.Sprintf("choose must be between 1 and %d for this thread", len(q.Options)))
		return "", false
	}
	return q.Options[choose-1], true
}

// threadPrefix renders the frame every delivered thread entry carries, so a
// recipient can tell a thread message from a plain one and see at a glance
// which thread, which entry and which author it came from. The author is named
// uniformly, including a human one: with several participants the frame is the
// only place it appears.
//
// The thread is named by its LOCAL ref — `[#1023/Q2 …]`, `[cto/Q1 …]` — which
// is exactly the string the recipient types back into reply/close. The earlier
// frame spelled the same thread differently from the id it accepted, and an
// agent reconstructing "task #1023 Q2" as a global id replied into somebody
// else's thread.
func threadPrefix(subj threadSubject, ordinal int, kind, author string) string {
	ref := threadLocalRef(subj, ordinal)
	if subj.RoleID == "" {
		// A task ref is spelled "#1023/Q2" here: the "#" reads as "task" and
		// keeps a bare number from looking like part of the surrounding text.
		ref = "#" + ref
	}
	return fmt.Sprintf("[%s %s from %s]", ref, kind, author)
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

// threadStale reports whether a thread has gone stale — open, of type
// decision, waiting on somebody, and without movement for longer than
// question_stale_after. The rule itself lives in internal/heartbeat (the
// package that acts on it) and is read here so a client can badge a thread
// without recomputing it; the heartbeat messages agents and sessions, but the
// human is only ever badged, and this field is that badge.
//
// It is derived on every read, like waiting_terminal, and never stored.
func threadStale(d Deps, q store.Question, lastMessage *store.QuestionMessage, attention []string) bool {
	after := config.DefaultQuestionStaleAfter
	if d.Cfg != nil && d.Cfg.QuestionStaleAfter > 0 {
		after = d.Cfg.QuestionStaleAfter
	}
	_, stale := heartbeat.StaleThread(store.OpenThread{
		Question:    q,
		LastMessage: lastMessage,
		Attention:   attention,
	}, time.Now(), after)
	return stale
}

// lastOf returns the last thread entry, or nil when nobody has replied yet.
func lastOf(msgs []store.QuestionMessage) *store.QuestionMessage {
	if len(msgs) == 0 {
		return nil
	}
	return &msgs[len(msgs)-1]
}
