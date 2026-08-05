package heartbeat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/store"
)

// TestStaleThread pins the staleness rule itself, away from delivery: what
// counts as movement, what counts as somebody's turn, and which threads are
// exempt from going stale at all.
func TestStaleThread(t *testing.T) {
	now := time.Unix(1_000_000_000, 0)
	old := now.Add(-30 * time.Hour).Unix()
	fresh := now.Add(-time.Hour).Unix()
	after := 24 * time.Hour

	base := func() store.OpenThread {
		return store.OpenThread{
			Question: store.Question{
				ID: 1, TaskID: 7, Status: "open",
				Type: store.QuestionTypeDecision, AskedAt: old,
			},
			Attention: []string{"cto"},
		}
	}

	t.Run("no messages: the question itself is the reference point", func(t *testing.T) {
		since, ok := StaleThread(base(), now, after)
		if !ok || since != 30*time.Hour {
			t.Fatalf("StaleThread = (%s, %v), want (30h, true)", since, ok)
		}
	})

	t.Run("the last entry is the reference point", func(t *testing.T) {
		th := base()
		th.LastMessage = &store.QuestionMessage{QuestionID: 1, CreatedAt: fresh}
		if since, ok := StaleThread(th, now, after); ok {
			t.Fatalf("StaleThread = (%s, true), want not stale after a fresh reply", since)
		}
	})

	t.Run("waiting on nobody is not stale", func(t *testing.T) {
		th := base()
		th.Attention = nil
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("a thread with an empty attention set must not be stale")
		}
	})

	t.Run("fyi never goes stale", func(t *testing.T) {
		th := base()
		th.Question.Type = store.QuestionTypeFYI
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("an fyi note must not be stale")
		}
	})

	t.Run("resolved never goes stale", func(t *testing.T) {
		th := base()
		th.Question.Status = "resolved"
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("a resolved thread must not be stale")
		}
	})

	t.Run("without a usable timestamp nothing is claimed", func(t *testing.T) {
		th := base()
		th.Question.AskedAt = 0
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("a thread without a timestamp must not be stale since the epoch")
		}
	})

	t.Run("exactly at the threshold is not yet stale", func(t *testing.T) {
		th := base()
		th.Question.AskedAt = now.Add(-after).Unix()
		if _, ok := StaleThread(th, now, after); ok {
			t.Fatal("the threshold is exclusive, like every other one in this package")
		}
	})
}

// sessionMessages returns the bodies queued to sessionID.
func sessionMessages(t *testing.T, st *store.Store, sessionID string) []string {
	t.Helper()
	msgs, err := st.ListMessages(sessionID, 0)
	if err != nil {
		t.Fatalf("ListMessages(%s): %v", sessionID, err)
	}
	var out []string
	for _, m := range msgs {
		out = append(out, m.Body)
	}
	return out
}

// TestRemindParticipant_LiveSessionGetsQueuedMessage: an ephemeral session in
// the attention set is reminded through the ordinary delivery queue.
func TestRemindParticipant_LiveSessionGetsQueuedMessage(t *testing.T) {
	st := openTestStore(t)
	seedOrchAndTask(t, st, "orch1", "in_progress")

	var woken []string
	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(to string) { woken = append(woken, to) })

	if !hb.remindParticipant("orch1", "reminder body") {
		t.Fatal("remindParticipant = false, want true for a live session")
	}
	bodies := sessionMessages(t, st, "orch1")
	if len(bodies) != 1 || !strings.Contains(bodies[0], "reminder body") {
		t.Fatalf("queued = %q, want one message carrying the body", bodies)
	}
	if len(woken) != 1 || woken[0] != "orch1" {
		t.Errorf("woken = %v, want [orch1]", woken)
	}
}

// TestRemindParticipant_SleepingAgentGetsInbox: a persistent agent that is not
// running still gets the reminder — its inbox is exactly the channel for that.
func TestRemindParticipant_SleepingAgentGetsInbox(t *testing.T) {
	st := openTestStore(t)
	addCTOAgent(t, st)

	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if !hb.remindParticipant(escalationAgent, "reminder body") {
		t.Fatal("remindParticipant = false, want true for a registered agent")
	}
	bodies := inboxBodies(t, st)
	if len(bodies) != 1 || !strings.Contains(bodies[0], "reminder body") {
		t.Fatalf("inbox = %q, want one message carrying the body", bodies)
	}
}

// TestRemindParticipant_HumanIsNotMessaged: the human has no inbox to deliver
// to — the stale flag on the thread is their channel.
func TestRemindParticipant_HumanIsNotMessaged(t *testing.T) {
	st := openTestStore(t)
	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})

	if hb.remindParticipant(store.ParticipantHuman, "reminder body") {
		t.Error("remindParticipant(human) = true, want false: the human is badged, not messaged")
	}
	if bodies := sessionMessages(t, st, store.ParticipantHuman); len(bodies) != 0 {
		t.Errorf("queued = %q, want nothing for the human", bodies)
	}
}

// TestRemindParticipant_UnknownRecipientIsDropped: a dead worker is neither a
// live session nor an agent with an inbox; there is nowhere to put it.
func TestRemindParticipant_UnknownRecipientIsDropped(t *testing.T) {
	st := openTestStore(t)
	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})

	if hb.remindParticipant("gone-worker", "reminder body") {
		t.Error("remindParticipant(unknown) = true, want false")
	}
}

// seedStaleThread opens a decision thread on taskID whose last movement is
// age ago, waiting on attention.
func seedStaleThread(t *testing.T, st *store.Store, taskID int64, body string, age time.Duration, attention ...string) int64 {
	t.Helper()
	qid, err := st.AddQuestion(store.Question{
		TaskID: taskID, AskedBy: "orch1", Body: body,
		Type: store.QuestionTypeDecision, AskedAt: time.Now().Add(-age).Unix(),
	})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := st.AddParticipants(qid, append([]string{"orch1"}, attention...)...); err != nil {
		t.Fatalf("AddParticipants: %v", err)
	}
	if err := st.SetAttention(qid, attention); err != nil {
		t.Fatalf("SetAttention: %v", err)
	}
	return qid
}

// TestSweepStaleThreads_RemindsAttentionOnce: criterion 8 — a thread idle
// longer than the threshold reminds exactly the members of its attention set,
// exactly once inside the anti-spam window.
func TestSweepStaleThreads_RemindsAttentionOnce(t *testing.T) {
	st := openTestStore(t)
	addCTOAgent(t, st)
	taskID := seedOrchAndTask(t, st, "orch1", "in_progress")
	seedStaleThread(t, st, taskID, "Ship or hold?", 30*time.Hour, escalationAgent)

	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	bodies := inboxBodies(t, st)
	if len(bodies) != 1 {
		t.Fatalf("inbox = %q, want exactly one reminder", bodies)
	}
	for _, want := range []string{"Ship or hold?", "30h", "rocket task reply", "rocket task close", "rocket task answer"} {
		if !strings.Contains(bodies[0], want) {
			t.Errorf("reminder %q must contain %q", bodies[0], want)
		}
	}
	ordinal := "1/Q1"
	if !strings.Contains(bodies[0], "/Q1") {
		t.Errorf("reminder %q must carry a local thread ref like %q", bodies[0], ordinal)
	}
	if !hasEvent(eventTypes(t, st), "question.stale") {
		t.Errorf("expected a question.stale event, got %v", eventTypes(t, st))
	}

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if bodies := inboxBodies(t, st); len(bodies) != 1 {
		t.Errorf("inbox = %q after the second tick, want still one (anti-spam)", bodies)
	}
}

// TestSweepStaleThreads_FreshThreadIsQuiet: a thread that moved recently is
// nobody's problem yet.
func TestSweepStaleThreads_FreshThreadIsQuiet(t *testing.T) {
	st := openTestStore(t)
	addCTOAgent(t, st)
	taskID := seedOrchAndTask(t, st, "orch1", "in_progress")
	seedStaleThread(t, st, taskID, "Fresh?", time.Hour, escalationAgent)

	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if bodies := inboxBodies(t, st); len(bodies) != 0 {
		t.Errorf("inbox = %q, want nothing for a fresh thread", bodies)
	}
	if hasEvent(eventTypes(t, st), "question.stale") {
		t.Error("a fresh thread must not publish question.stale")
	}
}

// TestSweepStaleThreads_HumanTurnIsBadgedNotMessaged: the human's turn still
// publishes the event (the dashboard badges from it) but queues nothing.
func TestSweepStaleThreads_HumanTurnIsBadgedNotMessaged(t *testing.T) {
	st := openTestStore(t)
	addCTOAgent(t, st)
	taskID := seedOrchAndTask(t, st, "orch1", "in_progress")
	seedStaleThread(t, st, taskID, "Your call?", 30*time.Hour, store.ParticipantHuman)

	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if bodies := inboxBodies(t, st); len(bodies) != 0 {
		t.Errorf("inbox = %q, want nothing: the human is badged, not messaged", bodies)
	}
	if !hasEvent(eventTypes(t, st), "question.stale") {
		t.Errorf("expected a question.stale event, got %v", eventTypes(t, st))
	}
}

// TestSweepStaleThreads_AnswerReplyResetsTheClock: movement is the thread's
// last entry, so a reply an hour ago keeps a day-old question quiet.
func TestSweepStaleThreads_AnswerReplyResetsTheClock(t *testing.T) {
	st := openTestStore(t)
	addCTOAgent(t, st)
	taskID := seedOrchAndTask(t, st, "orch1", "in_progress")
	qid := seedStaleThread(t, st, taskID, "Old question", 30*time.Hour, escalationAgent)
	if _, err := st.AddQuestionMessage(store.QuestionMessage{
		QuestionID: qid, Author: "orch1", Kind: "reply", Body: "still here",
		CreatedAt: time.Now().Add(-time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("AddQuestionMessage: %v", err)
	}

	hb := New(st, bus.New(st), testConfig(), unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if bodies := inboxBodies(t, st); len(bodies) != 0 {
		t.Errorf("inbox = %q, want nothing: the thread moved an hour ago", bodies)
	}
}
