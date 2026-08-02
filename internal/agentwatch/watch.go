// Package agentwatch keeps rocket's picture of persistent agents honest and
// tells a freshly appeared agent that mail is waiting.
//
// Rocket does not run agents: an agent's tmux session is either started by
// `rocket agent start` or created by hand, and it can disappear at any moment
// without telling anyone. So the watcher observes tmux instead: a session
// named after a registered agent is adopted (a session row appears, delivery
// and the dashboard see it as alive), and one that is gone is retired.
//
// When an agent's session appears with unread messages waiting, the watcher
// injects exactly one line — "You have N unread messages" — and then stays
// quiet. The agent pulls the messages itself with `rocket inbox next`; a new
// notification is only due once new unread messages have arrived since the
// last one, and never more often than cfg.AgentNotifyInterval.
package agentwatch

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// Sessions is the slice of session.Manager the watcher drives. Keeping it an
// interface documents what the watcher is allowed to do with sessions and lets
// its tests run without tmux or git.
type Sessions interface {
	// AdoptAgentSession registers an agent's live tmux session, reporting
	// whether this call is what brought it up (see session.Manager).
	AdoptAgentSession(a store.Agent) (sess store.Session, fresh bool, err error)
	RetireAgentSession(id string) error
}

// notifyState is what the watcher remembers per agent between ticks: the
// highest unread id it has already announced, and when it announced it.
type notifyState struct {
	lastID int64
	at     time.Time
}

// Watcher observes tmux and notifies agents of unread mail.
type Watcher struct {
	st   *store.Store
	rt   runtime.Runtime
	cfg  *config.Config
	sess Sessions
	// wake kicks the delivery worker of an agent the watcher queued a
	// notification for; typically Queue.Wake.
	wake func(to string)

	// now returns the current time; overridable by tests.
	now func() time.Time

	mu sync.Mutex
	// notified is the per-agent anti-spam bookkeeping. It is deliberately
	// in-memory: after a daemon restart, one extra notification per agent
	// with a non-empty inbox is the worst case, which is the right side to
	// err on — a missed notification is worse than a repeated one.
	notified map[string]notifyState
	// collisionWarned remembers agents whose adoption failed on a session
	// name collision, so the log gets one warning per agent rather than one
	// per tick.
	collisionWarned map[string]bool
}

// New builds a Watcher. wake is typically Queue.Wake.
func New(st *store.Store, rt runtime.Runtime, cfg *config.Config, sess Sessions, wake func(to string)) *Watcher {
	if wake == nil {
		wake = func(string) {}
	}
	return &Watcher{
		st: st, rt: rt, cfg: cfg, sess: sess, wake: wake,
		now:             time.Now,
		notified:        map[string]notifyState{},
		collisionWarned: map[string]bool{},
	}
}

// Run sweeps on a ticker until ctx is cancelled, starting with one immediate
// tick so agents already running when the daemon starts are picked up.
func (w *Watcher) Run(ctx context.Context) {
	w.Tick(ctx)

	ticker := time.NewTicker(w.cfg.ActivityPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Tick(ctx)
		}
	}
}

// Tick runs one sweep: reconcile every registered agent against the live tmux
// sessions, then notify the live ones that have unread mail.
func (w *Watcher) Tick(ctx context.Context) {
	agents, err := w.st.ListAgents("")
	if err != nil {
		slog.Error("agentwatch: list agents", "error", err)
		return
	}
	if len(agents) == 0 {
		return
	}

	names, err := w.rt.List(ctx)
	if err != nil {
		// Without a reliable picture of tmux, retiring sessions would
		// declare live agents dead. Skip the whole tick instead.
		slog.Error("agentwatch: list runtime sessions", "error", err)
		return
	}
	live := make(map[string]bool, len(names))
	for _, n := range names {
		live[n] = true
	}

	for _, a := range agents {
		if !live[a.ID] {
			w.forget(a.ID)
			if err := w.sess.RetireAgentSession(a.ID); err != nil {
				slog.Error("agentwatch: retire agent session", "agent", a.ID, "error", err)
			}
			continue
		}
		if !a.Enabled {
			// A disabled agent's session is left alone, but rocket neither
			// tracks nor notifies it.
			continue
		}
		_, fresh, err := w.sess.AdoptAgentSession(a)
		if err != nil {
			w.warnCollision(a.ID, err)
			continue
		}
		w.clearCollisionWarning(a.ID)
		if fresh {
			// A session rocket had not seen up until now: whatever it was
			// told in a previous life does not count, so the anti-spam
			// bookkeeping starts over. Without this, an agent restarted
			// between two ticks would never hear about the mail waiting for
			// it.
			w.forget(a.ID)
		}
		w.notifyUnread(a.ID)
	}
}

// notifyUnread injects the "N unread" line into an agent's live session, if
// one is due: unread messages exist, and at least one of them arrived after
// the last notification (and that notification is old enough).
func (w *Watcher) notifyUnread(agentID string) {
	maxID, err := w.st.MaxUnreadInboxID(agentID)
	if err != nil {
		slog.Error("agentwatch: max unread inbox id", "agent", agentID, "error", err)
		return
	}
	if maxID == 0 {
		return
	}

	now := w.now()
	w.mu.Lock()
	prev, seen := w.notified[agentID]
	due := !seen || (maxID > prev.lastID && now.Sub(prev.at) >= w.cfg.AgentNotifyInterval)
	if due {
		w.notified[agentID] = notifyState{lastID: maxID, at: now}
	}
	w.mu.Unlock()

	if !due {
		return
	}

	n, err := w.st.CountUnreadInbox(agentID)
	if err != nil {
		slog.Error("agentwatch: count unread inbox", "agent", agentID, "error", err)
		return
	}
	if n == 0 {
		return
	}

	// A system message (no sender) so the agent sees the bare line.
	if _, err := w.st.AddMessage(store.Message{
		ToSession: agentID,
		Body: fmt.Sprintf(
			"[rocket] You have %d unread message%s. Read them one by one with: rocket inbox next",
			n, plural(n)),
	}); err != nil {
		slog.Error("agentwatch: queue unread notification", "agent", agentID, "error", err)
		return
	}
	w.wake(agentID)
	slog.Info("agentwatch: notified agent of unread messages", "agent", agentID, "unread", n)
}

// forget drops an agent's notification bookkeeping, so the next session it
// comes up in is told about the mail waiting for it even if the pile has not
// grown since.
func (w *Watcher) forget(agentID string) {
	w.mu.Lock()
	delete(w.notified, agentID)
	w.mu.Unlock()
}

func (w *Watcher) warnCollision(agentID string, err error) {
	w.mu.Lock()
	warned := w.collisionWarned[agentID]
	w.collisionWarned[agentID] = true
	w.mu.Unlock()
	if !warned {
		slog.Warn("agentwatch: cannot track this agent's tmux session", "agent", agentID, "error", err)
	}
}

func (w *Watcher) clearCollisionWarning(agentID string) {
	w.mu.Lock()
	delete(w.collisionWarned, agentID)
	w.mu.Unlock()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
