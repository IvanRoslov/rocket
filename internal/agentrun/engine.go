package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// tickInterval is how often Run sweeps for expired snoozes, due cron
// schedules, idle instances and inbox events no Notify covered (e.g. rows
// written directly by another daemon component). Wakes themselves do not wait
// for it — Notify debounces and fires on its own timer.
const tickInterval = time.Minute

// idleActivityStates are the activity states an instance can sit in while the
// idle timeout runs down. "active" means the agent is working and is never
// killed, however long it takes.
var idleActivityStates = map[string]bool{
	"idle": true, "ready": true, "waiting_input": true, "blocked": true, "exited": true,
}

// Spawner is the slice of session.Manager the engine drives. Keeping it an
// interface documents exactly what the runtime is allowed to do with
// sessions, and lets engine tests run without tmux or git.
type Spawner interface {
	SpawnRole(ctx context.Context, role store.Agent, briefing string) (store.Session, error)
	LiveRoleInstance(roleID string) (store.Session, bool, error)
	Kill(ctx context.Context, id string, cleanup bool) error
}

// Engine is the wake engine: it turns queued inbox events into role runs.
//
// One role has at most one live instance. Events that arrive while an
// instance is alive are injected into it through the normal message queue;
// events that arrive while it is not spawn a fresh run, after a debounce
// window so a burst of events becomes one run rather than a stampede.
type Engine struct {
	st        *store.Store
	bus       *bus.Bus
	cfg       *config.Config
	sp        Spawner
	queueWake func(to string)

	// now returns the current time; overridable by tests.
	now func() time.Time

	mu sync.Mutex
	// debounce holds the pending wake timer per role.
	debounce map[string]*time.Timer
	// cronNext holds the next fire time per role, armed on the first tick
	// that sees the schedule. It is deliberately in-memory: a daemon
	// restart re-arms from "now", which at worst delays one tick of a
	// periodic sweep.
	cronNext map[string]time.Time
	// cronBroken remembers roles whose cron expression failed to parse, so
	// the daemon log gets one warning per role rather than one per tick.
	cronBroken map[string]bool

	ctxMu  sync.RWMutex
	runCtx context.Context
}

// New builds an Engine. queueWake is typically Queue.Wake: it kicks the
// delivery worker of an instance the engine just queued a message for.
func New(st *store.Store, b *bus.Bus, cfg *config.Config, sp Spawner, queueWake func(to string)) *Engine {
	if queueWake == nil {
		queueWake = func(string) {}
	}
	return &Engine{
		st: st, bus: b, cfg: cfg, sp: sp, queueWake: queueWake,
		now:        time.Now,
		debounce:   map[string]*time.Timer{},
		cronNext:   map[string]time.Time{},
		cronBroken: map[string]bool{},
	}
}

// Notify tells the engine that a role's inbox has grown. The role is
// processed once the debounce window closes; further notifications inside
// that window join the same batch.
func (e *Engine) Notify(roleID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, pending := e.debounce[roleID]; pending {
		return
	}

	e.debounce[roleID] = time.AfterFunc(e.cfg.AgentWakeDebounce, func() {
		e.mu.Lock()
		delete(e.debounce, roleID)
		e.mu.Unlock()

		if err := e.processRole(e.context(), roleID); err != nil {
			slog.Error("agentrun: process role", "role", roleID, "error", err)
		}
	})
}

// Run sweeps on a ticker until ctx is cancelled, starting with one immediate
// tick so events enqueued while the daemon was down are picked up.
func (e *Engine) Run(ctx context.Context) {
	e.ctxMu.Lock()
	e.runCtx = ctx
	e.ctxMu.Unlock()

	e.Tick(ctx)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.Tick(ctx)
		}
	}
}

// Tick runs one scheduler pass: expired snoozes, due cron schedules, idle
// instances, then every role that still has queued events.
func (e *Engine) Tick(ctx context.Context) {
	e.ctxMu.Lock()
	if e.runCtx == nil {
		e.runCtx = ctx
	}
	e.ctxMu.Unlock()

	e.fireSnoozes()
	e.fireCron()
	e.expireIdleInstances(ctx)

	roleIDs, err := e.st.RolesWithQueuedInbox()
	if err != nil {
		slog.Error("agentrun: list roles with queued inbox", "error", err)
		return
	}
	for _, id := range roleIDs {
		if err := e.processRole(ctx, id); err != nil {
			slog.Error("agentrun: process role", "role", id, "error", err)
		}
	}
}

// Done ends the run that sessionID belongs to: its delivered events are
// closed and the session is killed. The role's worktree is deliberately kept
// (cleanup=false) — it is persistent across runs.
func (e *Engine) Done(ctx context.Context, sessionID string) error {
	roleID, _, ok := session.RoleFromInstanceID(sessionID)
	if !ok {
		return fmt.Errorf("not a role instance: %s", sessionID)
	}
	return e.endRun(ctx, roleID, sessionID, "agent.run_done", nil)
}

// endRun closes a run: delivered events become done (the run has seen them —
// re-briefing them on the next wake would make the role redo its work),
// the session is killed, and event is published. Events still queued are
// untouched, so anything that arrived during the run wakes the role again.
func (e *Engine) endRun(ctx context.Context, roleID, sessionID, event string, data map[string]any) error {
	delivered, err := e.st.ListInboxEvents(roleID, store.InboxStatusDelivered, 0)
	if err != nil {
		return fmt.Errorf("list delivered events: %w", err)
	}
	ids := make([]int64, 0, len(delivered))
	for _, ev := range delivered {
		ids = append(ids, ev.ID)
	}
	if err := e.st.MarkInboxDone(ids); err != nil {
		return fmt.Errorf("mark events done: %w", err)
	}

	if err := e.sp.Kill(ctx, sessionID, false); err != nil {
		return err
	}
	if e.bus != nil {
		payload := map[string]any{"role": roleID, "events_closed": len(ids)}
		for k, v := range data {
			payload[k] = v
		}
		e.bus.Publish(event, sessionID, payload)
	}

	// Anything that arrived while the run was finishing deserves a new run.
	if n, err := e.st.CountQueuedInboxEvents(roleID); err == nil && n > 0 {
		e.Notify(roleID)
	}
	return nil
}

// processRole delivers a role's queued events: into its live instance if it
// has one, into a fresh instance otherwise.
func (e *Engine) processRole(ctx context.Context, roleID string) error {
	role, err := e.st.GetAgent(roleID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// The role was deleted; its events went with it.
			return nil
		}
		return err
	}
	if !role.Enabled {
		// Events stay queued: enabling the role picks them up.
		return nil
	}

	events, err := e.st.QueuedInboxEvents(roleID)
	if err != nil {
		return fmt.Errorf("queued events: %w", err)
	}
	if len(events) == 0 {
		return nil
	}

	live, ok, err := e.sp.LiveRoleInstance(roleID)
	if err != nil {
		return err
	}
	if ok {
		return e.injectIntoInstance(live, events)
	}

	briefing, err := BuildBriefing(e.st, e.cfg.Home, role, events)
	if err != nil {
		return err
	}
	if _, err := e.sp.SpawnRole(ctx, role, briefing); err != nil {
		// Events stay queued so the next tick retries — a failed spawn
		// (agent unavailable, git trouble, a racing instance) must not
		// swallow the role's inbox.
		return fmt.Errorf("spawn role instance: %w", err)
	}
	return e.markDelivered(events)
}

// injectIntoInstance hands events to a live run through the normal message
// queue, so they land in the same FIFO as everything else addressed to that
// session.
func (e *Engine) injectIntoInstance(live store.Session, events []store.AgentInboxEvent) error {
	for _, ev := range events {
		from, body := inboxMessage(ev)
		if from != "" {
			// Only a real session may be named as the sender: the queue
			// looks senders up to report delivery failures back to them.
			if _, err := e.st.GetSession(from); err != nil {
				body = "[from " + from + "] " + body
				from = ""
			}
		}
		if _, err := e.st.AddMessage(store.Message{
			FromSession: from,
			ToSession:   live.ID,
			Body:        body,
		}); err != nil {
			return fmt.Errorf("queue inbox event %d to %s: %w", ev.ID, live.ID, err)
		}
		if e.bus != nil {
			e.bus.Publish("message.queued", live.ID, map[string]any{"from": from, "to": live.ID})
		}
		e.queueWake(live.ID)
	}
	return e.markDelivered(events)
}

// inboxMessage renders an inbox event as an instance-facing message and
// returns the sender it should be attributed to (empty for system events).
func inboxMessage(ev store.AgentInboxEvent) (from, body string) {
	payload := decodePayload(ev.Payload)
	rendered := eventBody(ev, payload)
	if ev.Kind == "question" {
		// Q&A entries already carry their own "[role sre Q2 reply]" prefix
		// (the same one task threads use); a second frame would only add
		// noise.
		return "", rendered
	}
	return stringField(payload, "from"),
		fmt.Sprintf("[inbox #%d %s] %s", ev.ID, ev.Kind, rendered)
}

func (e *Engine) markDelivered(events []store.AgentInboxEvent) error {
	ids := make([]int64, 0, len(events))
	for _, ev := range events {
		ids = append(ids, ev.ID)
	}
	if err := e.st.MarkInboxDelivered(ids); err != nil {
		return fmt.Errorf("mark events delivered: %w", err)
	}
	return nil
}

// fireSnoozes turns every dossier entry whose snooze deadline has passed into
// a snooze_expired event and clears the deadline, so it fires exactly once.
func (e *Engine) fireSnoozes() {
	items, err := e.st.DueSnoozedItems(e.now().Unix())
	if err != nil {
		slog.Error("agentrun: due snoozed items", "error", err)
		return
	}

	for _, it := range items {
		payload, err := json.Marshal(map[string]any{
			"ref": it.ExternalRef, "item_kind": it.Kind, "state": it.State, "note": it.Note,
		})
		if err != nil {
			slog.Error("agentrun: marshal snooze payload", "item", it.ID, "error", err)
			continue
		}
		if _, err := e.st.EnqueueInboxEvent(store.AgentInboxEvent{
			RoleID: it.RoleID, Kind: "snooze_expired", Payload: string(payload),
		}); err != nil {
			slog.Error("agentrun: enqueue snooze event", "item", it.ID, "error", err)
			continue
		}
		if err := e.st.ClearAgentItemSnooze(it.ID); err != nil {
			slog.Error("agentrun: clear snooze", "item", it.ID, "error", err)
		}
		e.Notify(it.RoleID)
	}
}

// fireCron enqueues a cron event for every enabled role whose schedule has
// come due. The first tick that sees a schedule only arms it: rocket never
// fires a role's cron for a window it was not running in.
func (e *Engine) fireCron() {
	agents, err := e.st.ListAgents("")
	if err != nil {
		slog.Error("agentrun: list agents for cron", "error", err)
		return
	}

	now := e.now()
	for _, a := range agents {
		if !a.Enabled || a.Cron == "" {
			continue
		}

		sched, err := ParseCron(a.Cron)
		if err != nil {
			e.mu.Lock()
			warned := e.cronBroken[a.ID]
			e.cronBroken[a.ID] = true
			e.mu.Unlock()
			if !warned {
				slog.Warn("agentrun: role has an unparseable cron expression, schedule ignored",
					"role", a.ID, "cron", a.Cron, "error", err)
			}
			continue
		}

		e.mu.Lock()
		delete(e.cronBroken, a.ID)
		next, armed := e.cronNext[a.ID]
		due := armed && !now.Before(next)
		if !armed || due {
			e.cronNext[a.ID] = sched.Next(now)
		}
		e.mu.Unlock()

		if !due {
			continue
		}

		if _, err := e.st.EnqueueInboxEvent(store.AgentInboxEvent{
			RoleID: a.ID, Kind: "cron", Payload: `{"cron":"` + a.Cron + `"}`,
		}); err != nil {
			slog.Error("agentrun: enqueue cron event", "role", a.ID, "error", err)
			continue
		}
		e.Notify(a.ID)
	}
}

// expireIdleInstances kills role instances that have gone quiet for longer
// than the configured timeout. It is the safety net behind `rocket agent
// done`: a run that forgets to end itself must not hold the role's
// single-instance slot forever.
func (e *Engine) expireIdleInstances(ctx context.Context) {
	sessions, err := e.st.ListSessions(store.SessionFilter{Kind: "agent"})
	if err != nil {
		slog.Error("agentrun: list role instances", "error", err)
		return
	}

	cutoff := e.now().Add(-e.cfg.AgentIdleTimeout).Unix()
	for _, s := range sessions {
		roleID, _, ok := session.RoleFromInstanceID(s.ID)
		if !ok || s.ActivityTS == 0 || s.ActivityTS > cutoff {
			continue
		}
		if !idleActivityStates[s.Activity] {
			continue
		}

		idleFor := e.now().Sub(time.Unix(s.ActivityTS, 0))
		slog.Warn("agentrun: role instance idle past the timeout, killing",
			"session", s.ID, "role", roleID, "activity", s.Activity, "idle", idleFor)

		if err := e.endRun(ctx, roleID, s.ID, "agent.run_timeout", map[string]any{
			"activity": s.Activity, "idle_seconds": int64(idleFor.Seconds()),
		}); err != nil {
			slog.Error("agentrun: kill idle instance", "session", s.ID, "error", err)
		}
	}
}

// context returns the context Run was started with, falling back to a
// background context when a Notify fires before Run (or in tests).
func (e *Engine) context() context.Context {
	e.ctxMu.RLock()
	defer e.ctxMu.RUnlock()
	if e.runCtx != nil {
		return e.runCtx
	}
	return context.Background()
}
