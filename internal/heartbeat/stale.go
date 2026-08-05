package heartbeat

import (
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
