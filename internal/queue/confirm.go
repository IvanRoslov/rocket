package queue

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// The confirmer finishes deliveries the worker left in status "accepted":
// the text was pasted into a recipient whose agent (Codex) proves delivery
// from its own journal (agent.DeliveryConfirmer). It polls those messages,
// marks the proven ones delivered and applies the requeue/fail rules of
// docs/06-messaging.md «Статус accepted» to the rest.

// confirmInterval is how often accepted messages are re-checked while any
// exist; bus events (activity change, restore) trigger a pass sooner.
const confirmInterval = 3 * time.Second

// acceptedTurnGrace is how long after the paste an accepted message may stay
// unconfirmed while its recipient is NOT mid-turn before it is requeued. A
// recipient that is still "active" keeps it indefinitely: Codex hands queued
// text to the model only at the next tool call or turn end, and a foreground
// command can legitimately take tens of minutes.
const acceptedTurnGrace = 60 * time.Second

// acceptedPaneLines is how much of the recipient's screen the requeue check
// captures to look for the agent's pending-queue block (whole visible
// screen; the block can be tall).
const acceptedPaneLines = 60

// recordKey identifies one journal record's proof: a record may confirm
// several DIFFERENT messages (Codex merges consecutive queued messages into
// one user record), but the same text twice needs two records.
type recordKey struct {
	at   int64  // record timestamp, unix nanoseconds
	text string // whitespace-normalized injected text
}

// confirmState is the loop's private bookkeeping, touched only from the
// confirmer goroutine.
type confirmState struct {
	// used holds, per recipient, the journal records already spent on a
	// message of that text. Dropped once the recipient has nothing accepted.
	used map[string]map[recordKey]struct{}
	// restored marks recipients whose session was restarted since the last
	// pass: a new process cannot have our text in its queue, so every
	// accepted message of theirs goes back to "queued".
	restored map[string]bool
}

// confirmLoop runs until ctx is cancelled. It wakes on a kick (a message
// just became accepted, Recover), on session.activity_changed /
// session.restored / session.state_changed bus events, and — only while
// accepted messages exist — every confirmInterval.
func (q *Queue) confirmLoop(ctx context.Context) {
	events, cancel := q.bus.Subscribe()
	defer cancel()

	st := &confirmState{used: make(map[string]map[recordKey]struct{}), restored: make(map[string]bool)}
	pending := q.confirmPass(ctx, st)
	for {
		var tick <-chan time.Time
		var timer *time.Timer
		if pending > 0 {
			timer = time.NewTimer(confirmInterval)
			tick = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-tick:
		case <-q.confirmKick:
		case e, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			switch e.Type {
			case "session.restored":
				st.restored[e.SessionID] = true
			case "session.activity_changed", "session.state_changed":
			default:
				continue
			}
		}
		if timer != nil {
			timer.Stop()
		}
		pending = q.confirmPass(ctx, st)
	}
}

// confirmPass walks every accepted message, grouped by recipient in id
// order, and returns how many are still accepted afterwards.
func (q *Queue) confirmPass(ctx context.Context, st *confirmState) (pending int) {
	msgs, err := q.st.ListAcceptedMessages()
	if err != nil {
		slog.Error("queue: list accepted messages", "error", err)
		return 1 // keep ticking: a transient store error must not park the loop
	}
	seen := make(map[string]bool)
	for i := 0; i < len(msgs); {
		to := msgs[i].ToSession
		j := i
		for j < len(msgs) && msgs[j].ToSession == to {
			j++
		}
		seen[to] = true
		pending += q.confirmRecipient(ctx, st, to, msgs[i:j])
		i = j
	}
	for to := range st.used {
		if !seen[to] {
			delete(st.used, to)
		}
	}
	for to := range st.restored {
		if !seen[to] {
			delete(st.restored, to)
		}
	}
	return pending
}

// confirmRecipient handles the accepted messages of one recipient (oldest
// first) and returns how many stay accepted.
func (q *Queue) confirmRecipient(ctx context.Context, st *confirmState, to string, msgs []store.Message) (pending int) {
	restored := st.restored[to]
	delete(st.restored, to)

	sess, err := q.getSession(to)
	gone := errors.Is(err, store.ErrNotFound)
	if err != nil && !gone {
		// Transient lookup failure: look again next tick.
		slog.Error("queue: get session for accepted messages (transient)", "to", to, "error", err)
		return len(msgs)
	}
	state, known := q.getActivity(to)
	exited := gone || sess.State != "running" || (known && state == activity.Exited) || restored

	var confirmer agent.DeliveryConfirmer
	if !exited {
		_, confirmer = q.recipientCaps(sess)
	}
	ref := agent.ActivityRef{SessionID: to, WorktreePath: sess.WorktreePath}
	now := q.now()

	for _, msg := range msgs {
		if ctx.Err() != nil {
			return pending + 1
		}
		if !exited {
			text := q.injectedText(msg, sess)
			if confirmer != nil && q.confirmOne(ctx, st, confirmer, ref, to, msg, text) {
				continue
			}
			// Not proven yet. Still mid-turn: the agent has not had a
			// chance to hand it over — keep waiting, however long.
			if known && state == activity.Active {
				pending++
				continue
			}
			if now.Sub(time.UnixMilli(msg.InjectAt)) < acceptedTurnGrace {
				pending++
				continue
			}
			if confirmer != nil {
				pane, capErr := q.rt.Capture(ctx, runtime.Handle{Name: sess.TmuxName}, acceptedPaneLines)
				if capErr == nil && (runtime.LooksLikeAgentQueued(pane, text) ||
					q.injectOpts(sess).Dialect == runtime.PaneDialectKimi && runtime.LooksLikeKimiQueued(pane, text)) {
					// The text is visibly parked in the agent's own queue:
					// it will reach the model, pasting again would duplicate.
					pending++
					continue
				}
			}
		}
		q.giveUpAccepted(msg, exited, now)
	}
	return pending
}

// confirmOne asks the journal for msg's text after its inject_at, skipping
// records already spent on an identical text, and marks it delivered when
// found. It reports whether the message was confirmed.
func (q *Queue) confirmOne(ctx context.Context, st *confirmState, confirmer agent.DeliveryConfirmer, ref agent.ActivityRef, to string, msg store.Message, text string) bool {
	since := time.UnixMilli(msg.InjectAt)
	norm := strings.Join(strings.Fields(text), " ")
	for {
		found, at, err := confirmer.ConfirmDelivered(ctx, ref, text, since)
		if err != nil {
			if !errors.Is(err, agent.ErrNoSignal) {
				slog.Warn("queue: confirm delivery from journal", "id", msg.ID, "to", to, "error", err)
			}
			return false
		}
		if !found {
			return false
		}
		key := recordKey{at: at.UnixNano(), text: norm}
		used := st.used[to]
		if used == nil {
			used = make(map[recordKey]struct{})
			st.used[to] = used
		}
		if _, dup := used[key]; dup {
			// This record already proved an earlier identical message;
			// the next one needs a record of its own.
			since = at
			continue
		}
		ok, err := q.st.ConfirmAccepted(msg.ID, q.now().Unix())
		if err != nil {
			slog.Error("queue: confirm accepted message", "id", msg.ID, "error", err)
			return false
		}
		if !ok {
			return true // someone else settled it; nothing more to do here
		}
		used[key] = struct{}{}
		slog.Info("queue: delivery confirmed from the recipient's journal", "id", msg.ID, "to", to, "at", at)
		q.bus.Publish("message.delivered", msg.ToSession, map[string]any{
			"id":   msg.ID,
			"to":   msg.ToSession,
			"from": msg.FromSession,
		})
		return true
	}
}

// giveUpAccepted requeues an unconfirmed accepted message for one more
// paste or, when all maxAttempts pastes are spent (Attempts counts pastes;
// the worker increments it before each) or the message is older than
// queue_timeout, fails it with reason not_confirmed and tells the sender.
func (q *Queue) giveUpAccepted(msg store.Message, exited bool, now time.Time) {
	timedOut := q.cfg != nil && q.cfg.QueueTimeout > 0 && now.Sub(time.Unix(msg.CreatedAt, 0)) >= q.cfg.QueueTimeout
	if msg.Attempts >= maxAttempts || timedOut {
		ok, err := q.st.FailAccepted(msg.ID, "not_confirmed")
		if err != nil {
			slog.Error("queue: fail accepted message", "id", msg.ID, "error", err)
			return
		}
		if ok {
			slog.Warn("queue: accepted message never confirmed, giving up",
				"id", msg.ID, "to", msg.ToSession, "attempts", msg.Attempts, "timed_out", timedOut)
			q.announceFailure(msg, "not_confirmed")
		}
		return
	}
	ok, err := q.st.RequeueAccepted(msg.ID)
	if err != nil {
		slog.Error("queue: requeue accepted message", "id", msg.ID, "error", err)
		return
	}
	if ok {
		slog.Warn("queue: accepted message not confirmed, requeued",
			"id", msg.ID, "to", msg.ToSession, "attempt", msg.Attempts+1, "recipient_exited", exited)
		q.Wake(msg.ToSession)
	}
}

// injectedText returns the text that was pasted for msg — what the journal
// must hold: the sender-prefixed body, or for a large body the pointer to
// the inbox file prepareText wrote (when that file exists). It never writes.
func (q *Queue) injectedText(msg store.Message, sess store.Session) string {
	if !q.isLarge(msg) || sess.WorktreePath == "" {
		return formatBody(msg)
	}
	relPath := inboxRelPath(msg.ID)
	if _, err := os.Stat(filepath.Join(sess.WorktreePath, relPath)); err != nil {
		return formatBody(msg)
	}
	return inboxPointer(msg, relPath)
}
