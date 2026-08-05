package heartbeat

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
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

// staleReminderInterval is the anti-spam floor between two reminders about the
// same thread to the same recipient. It is deliberately a constant and not
// cfg.QuestionStaleAfter: shortening the staleness threshold should make
// threads go stale sooner, not make the reminder repeat every few hours.
const staleReminderInterval = 24 * time.Hour

// staleKeyPrefix namespaces per-(thread, recipient) reminder entries in
// lastSent, so a thread reminder and an orchestrator summary never suppress
// each other (same trick as escalationKeyPrefix).
const staleKeyPrefix = "stale-thread:"

// staleQuoteLimit is how much of a question's body a reminder quotes: enough
// to recognise the thread, short enough to stay on one line. Mirrors
// threadEchoLimit in internal/api/threads.go.
const staleQuoteLimit = 60

// sweepStaleThreads reminds every participant an open decision thread is
// waiting on, once the thread has gone longer than cfg.QuestionStaleAfter
// without movement. It runs once per tick over every open thread of every
// task and role — staleness is a property of the thread, not of any one
// orchestrator, so it deliberately sits outside the per-orchestrator sweep.
//
// The human is never messaged (see remindParticipant); the question.stale
// event and the thread's `stale` flag are what reaches them.
func (h *Heartbeat) sweepStaleThreads() error {
	threads, err := h.st.ListOpenThreads()
	if err != nil {
		return fmt.Errorf("list open threads: %w", err)
	}

	now := h.nowFunc()
	after := h.cfg.QuestionStaleAfter
	if after <= 0 {
		after = config.DefaultQuestionStaleAfter
	}

	for _, th := range threads {
		since, stale := StaleThread(th, now, after)
		if !stale {
			continue
		}

		ref, err := h.threadRef(th.Question)
		if err != nil {
			slog.Warn("heartbeat: local ref of stale thread",
				"question", th.Question.ID, "error", err)
			continue
		}

		body := staleBody(th.Question, ref, since)
		var reminded []string
		for _, participant := range th.Attention {
			key := staleKeyPrefix + ref + ":" + participant
			if !h.antiSpamOK(key, now, staleReminderInterval) {
				continue
			}
			if !h.remindParticipant(participant, body) {
				continue
			}
			h.mu.Lock()
			h.lastSent[key] = now
			h.mu.Unlock()
			reminded = append(reminded, participant)
		}

		if h.bus != nil {
			h.bus.Publish("question.stale", "", map[string]any{
				"question_id":   th.Question.ID,
				"task_id":       th.Question.TaskID,
				"role_id":       th.Question.RoleID,
				"local_ref":     ref,
				"since_seconds": int64(since.Seconds()),
				"attention":     th.Attention,
				"reminded":      reminded,
			})
		}
	}
	return nil
}

// threadRef renders the thread's one user-facing id — "1023/Q2" for a task
// thread, "cto/Q2" for a role thread. It restates the format of
// internal/api's threadLocalRef rather than importing it: heartbeat depends on
// the store only, and internal/api is being rewritten in parallel (task T3).
func (h *Heartbeat) threadRef(q store.Question) (string, error) {
	if q.RoleID != "" {
		ordinal, err := h.st.AgentQuestionOrdinal(store.AgentQuestion{ID: q.ID, RoleID: q.RoleID})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s/Q%d", q.RoleID, ordinal), nil
	}
	ordinal, err := h.st.QuestionOrdinal(q)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d/Q%d", q.TaskID, ordinal), nil
}

// staleBody assembles the reminder: which thread, how long it has been
// waiting, and the two commands that end the wait. It names `close` only —
// `answer` still works as its hidden alias, but a reminder that offers two
// spellings of the same verb invites the recipient to wonder which is meant.
func staleBody(q store.Question, ref string, since time.Duration) string {
	verb := "task"
	if q.RoleID != "" {
		verb = "agent"
	}
	return fmt.Sprintf(
		"[rocket stale thread] %s «%s» ждёт вашего хода %s.\n"+
			"Ответьте: rocket %s reply %s \"<текст>\" — или закройте: rocket %s close %s \"<резолюция>\".",
		ref, truncateForReminder(q.Body), humanSince(since), verb, ref, verb, ref)
}

// truncateForReminder shortens a question body to one readable line, counting
// runes so a Cyrillic question is not cut mid-character.
func truncateForReminder(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	r := []rune(s)
	if len(r) <= staleQuoteLimit {
		return s
	}
	return string(r[:staleQuoteLimit]) + "…"
}

// humanSince renders a waiting time: minutes below an hour, hours below two
// days, days above. The threshold for switching to days is 48h rather than
// 24h on purpose — "1d" reads as "since yesterday" and would hide the
// difference between a thread 25h idle and one idle 47h, which is exactly the
// range this reminder fires in.
func humanSince(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
