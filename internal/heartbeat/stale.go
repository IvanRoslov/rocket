package heartbeat

import (
	"errors"
	"log/slog"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// StaleThread reports how long an open decision thread has gone without
// movement and whether that exceeds after.
//
// Movement is the thread's last entry — its last message, or the question
// itself when nobody has replied yet. Deliberately NOT question_attention's
// added_at: the attention set is rewritten by rules that need not touch the
// conversation (a `--to` handover), and staleness is about the conversation.
//
// Three kinds of thread never go stale: one waiting on nobody (an empty
// attention set — there is no one to remind), an fyi note (born resolved,
// waiting on nobody by construction), and a resolved thread. Neither does one
// without a usable timestamp: as in InputStalled, a missing reference point
// means "not stalled", never "stalled since the epoch".
//
// It is exported because the same rule is read twice: by the heartbeat sweep
// that sends the reminders, and by the API when it flags a thread `stale` for
// clients that render a badge instead of receiving a message.
func StaleThread(th store.OpenThread, now time.Time, after time.Duration) (since time.Duration, ok bool) {
	if th.Question.Status != "open" || th.Question.Type == store.QuestionTypeFYI {
		return 0, false
	}
	if len(th.Attention) == 0 {
		return 0, false
	}

	ref := th.Question.AskedAt
	if th.LastMessage != nil && th.LastMessage.CreatedAt > 0 {
		ref = th.LastMessage.CreatedAt
	}
	if ref <= 0 {
		return 0, false
	}

	since = now.Sub(time.Unix(ref, 0))
	return since, since > after
}

// remindParticipant delivers body to one member of a thread's attention set
// and reports whether it landed anywhere.
//
// The routing rules are the ones internal/api/agent_delivery.go already
// applies to every message addressed to a participant, restated here because
// the heartbeat depends on the store alone:
//
//   - the human is never messaged. There is no inbox to deliver to; their
//     channel is the thread's `stale` flag and the question.stale event, which
//     the dashboard and `rocket questions` render as a badge. Reported as not
//     delivered, since nothing was;
//   - a live (spawning|running) session — an orchestrator or worker — gets an
//     ordinary queued message and a wake;
//   - a persistent agent that is not running gets an inbox row, so a reminder
//     survives until the agent is next started;
//   - anything else (a finished worker, an unknown id) is dropped with a log:
//     an ephemeral session has no inbox, and there is nowhere else to put it.
//
// Failures are logged rather than returned: one undeliverable reminder must
// not abort the sweep over the remaining threads.
func (h *Heartbeat) remindParticipant(participant, body string) bool {
	if store.IsHuman(participant) {
		return false
	}

	sess, err := h.st.GetSession(participant)
	switch {
	case err == nil && liveSession(sess):
		id, err := h.st.AddMessage(store.Message{ToSession: participant, Body: body})
		if err != nil {
			slog.Warn("heartbeat: queue stale-thread reminder", "to", participant, "error", err)
			return false
		}
		if h.bus != nil {
			h.bus.Publish("message.queued", participant, map[string]any{
				"id": id, "from": "", "to": participant,
			})
		}
		if h.wake != nil {
			h.wake(participant)
		}
		return true
	case err != nil && !errors.Is(err, store.ErrNotFound):
		slog.Warn("heartbeat: look up stale-thread reminder recipient", "to", participant, "error", err)
		return false
	}

	if _, err := h.st.GetAgent(participant); err != nil {
		slog.Warn("heartbeat: stale-thread reminder has nowhere to go",
			"to", participant, "error", err)
		return false
	}
	if _, err := h.st.AddInboxMessage(store.InboxMessage{AgentID: participant, Body: body}); err != nil {
		slog.Warn("heartbeat: inbox stale-thread reminder", "to", participant, "error", err)
		return false
	}
	return true
}
