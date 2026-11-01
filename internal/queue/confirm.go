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

// confirmPass walks accepted messages and durably claimed Claude delivering
// rows, grouped by recipient in id order, returning how many remain pending.
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
	sess, _ := q.getSession(to)
	// Read the restore marker once for BOTH route halves: one recipient can
	// have native and legacy accepted rows at once (a legacy message accepted
	// before its session became native), and consuming the marker in one
	// handler would hide the restore from the other.
	restored := st.restored[to]
	delete(st.restored, to)
	claude := make([]store.Message, 0, len(msgs))
	native := make([]store.Message, 0, len(msgs))
	unsupported := make([]store.Message, 0, len(msgs))
	legacy := msgs[:0]
	for _, msg := range msgs {
		switch routeOf(msg, sess) {
		case routeClaude:
			claude = append(claude, msg)
		case routeNative:
			native = append(native, msg)
		case routeUnsupported:
			unsupported = append(unsupported, msg)
		default:
			legacy = append(legacy, msg)
		}
	}
	pending += q.confirmClaudeRecipient(ctx, claude)
	pending += q.confirmNativeRecipient(ctx, st, to, restored, native)
	pending += q.failUnsupportedAccepted(unsupported)
	msgs = legacy
	if len(msgs) == 0 {
		return pending
	}

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
					q.injectOpts(sess).Dialect == runtime.PaneDialectKimi && (runtime.LooksLikeKimiQueued(pane, text) || runtime.KimiCacheHintDialog(pane)) ||
					q.injectOpts(sess).Dialect == runtime.PaneDialectPi && runtime.LooksLikePiQueued(pane, text)) {
					// The text is visibly parked in the agent's own queue or
					// stashed behind its cache dialog: pasting again would duplicate.
					pending++
					continue
				}
			}
		}
		q.giveUpAccepted(msg, exited, now)
	}
	return pending
}

func (q *Queue) confirmNativeRecipient(ctx context.Context, st *confirmState, to string, restored bool, msgs []store.Message) (pending int) {
	if len(msgs) == 0 {
		return 0
	}

	sess, err := q.getSession(to)
	gone := errors.Is(err, store.ErrNotFound)
	if err != nil && !gone {
		slog.Error("queue: get session for native accepted messages (transient)", "to", to, "error", err)
		return len(msgs)
	}
	state, known := q.getActivity(to)
	exited := gone || sess.State != "running" || (known && state == activity.Exited) || restored
	_, baseConfirmer := q.recipientCaps(sess)
	// Native confirmation uses only the Pi strict whole-record method; no
	// legacy substring/holdsLines fallback is allowed on this route.
	confirmer := nativePiProofConfirmer(baseConfirmer)
	ref := agent.ActivityRef{SessionID: to, WorktreePath: sess.WorktreePath}
	now := q.now()

	for _, msg := range msgs {
		if ctx.Err() != nil {
			return pending + 1
		}
		if confirmer != nil && q.confirmOne(ctx, st, confirmer, ref, to, msg, q.nativePiInjectedText(msg, sess)) {
			continue
		}
		timedOut := q.cfg != nil && q.cfg.QueueTimeout > 0 && now.Sub(time.Unix(msg.CreatedAt, 0)) >= q.cfg.QueueTimeout
		if !exited && !timedOut {
			pending++
			continue
		}
		ok, err := q.st.FailAccepted(msg.ID, nativeDeliveryUnconfirmed)
		if err != nil {
			slog.Error("queue: fail native accepted message", "id", msg.ID, "error", err)
			pending++
			continue
		}
		if ok {
			slog.Warn("queue: native delivery never confirmed", "id", msg.ID, "to", to, "recipient_exited", exited, "timed_out", timedOut)
			q.announceFailure(msg, nativeDeliveryUnconfirmed)
		}
	}
	return pending
}

// failUnsupportedAccepted settles accepted messages whose persisted transport
// Rocket does not implement. They were never handed to a supported transport,
// so the only safe outcome is an honest failure: no tmux probe, no requeue
// onto the visual path. Returns how many still need attention (store errors).
func (q *Queue) failUnsupportedAccepted(msgs []store.Message) (pending int) {
	for _, msg := range msgs {
		ok, err := q.st.FailAccepted(msg.ID, unsupportedDeliveryTransport)
		if err != nil {
			slog.Error("queue: fail unsupported-transport message", "id", msg.ID, "error", err)
			pending++
			continue
		}
		if ok {
			slog.Warn("queue: unsupported persisted delivery transport, failing closed",
				"id", msg.ID, "to", msg.ToSession, "transport", msg.DeliveryTransport)
			q.announceFailure(msg, unsupportedDeliveryTransport)
		}
	}
	return pending
}

// confirmOne asks the journal whether the accepted message was recorded and
// marks it delivered when found. It reports whether the message was confirmed.
func (q *Queue) confirmOne(ctx context.Context, st *confirmState, confirmer agent.DeliveryConfirmer, ref agent.ActivityRef, to string, msg store.Message, text string) bool {
	return q.confirmOneFrom(ctx, st, confirmer, ref, to, msg, text, false)
}

// confirmDeliveringOne is confirmOne for a message still in status
// "delivering" (the final scan of a failed native attempt).
func (q *Queue) confirmDeliveringOne(ctx context.Context, st *confirmState, confirmer agent.DeliveryConfirmer, ref agent.ActivityRef, to string, msg store.Message, text string) bool {
	return q.confirmOneFrom(ctx, st, confirmer, ref, to, msg, text, true)
}

// confirmOneFrom asks the journal for msg's text after its inject_at, skipping
// records already spent on an identical text, and settles the message through
// the status-appropriate CAS. Proof ownership is enforced atomically by the
// store: if another equal-text message won the same record while this scan
// ran, the CAS reports false and the scan moves on to a later record instead
// of pretending the message was settled.
func (q *Queue) confirmOneFrom(ctx context.Context, st *confirmState, confirmer agent.DeliveryConfirmer, ref agent.ActivityRef, to string, msg store.Message, text string, delivering bool) bool {
	since := time.UnixMilli(msg.InjectAt)
	norm := strings.Join(strings.Fields(text), " ")
	used := st.used[to]
	if used == nil {
		used = make(map[recordKey]struct{})
		st.used[to] = used
	}
	previous, err := q.st.ListMessages(to, 0)
	if err != nil {
		slog.Error("queue: rebuild consumed journal proofs", "id", msg.ID, "to", to, "error", err)
		return false
	}
	for _, prior := range previous {
		if prior.ID >= msg.ID {
			break
		}
		if prior.ToSession == to && prior.DeliveryProofAt > 0 && prior.DeliveryProofText == norm {
			used[recordKey{at: prior.DeliveryProofAt, text: norm}] = struct{}{}
		}
	}
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
		if _, dup := used[key]; dup {
			// This record already proved an earlier identical message;
			// the next one needs a record of its own.
			since = at
			continue
		}
		ok, err := q.settleMessage(msg.ID, delivering, q.now().Unix(), at.UnixNano(), norm)
		if err != nil {
			slog.Error("queue: confirm native/legacy message", "id", msg.ID, "delivering", delivering, "error", err)
			return false
		}
		if !ok {
			current, getErr := q.st.GetMessage(msg.ID)
			if getErr != nil || current.Status == "delivered" || current.Status == "failed" || current.Status == "queued" {
				return true // someone else settled this message; nothing more to do
			}
			// The row is still accepted/delivering: the record was atomically
			// spent on another equal-text message. Only a later record can
			// prove this one.
			since = at
			continue
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

// settleMessage runs the status-appropriate proof-consuming CAS.
func (q *Queue) settleMessage(id int64, delivering bool, deliveredAt, proofAt int64, proofText string) (bool, error) {
	if delivering {
		return q.st.ConfirmDelivering(id, deliveredAt, proofAt, proofText)
	}
	return q.st.ConfirmAccepted(id, deliveredAt, proofAt, proofText)
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
