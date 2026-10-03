// Package usage collects per-session token usage from agent transcripts into
// the store: on terminal session events, as an orchestrator snapshot when its
// feature goes to review, and in a background sweeper that also backfills
// sessions that ended before the collector existed.
package usage

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/store"
)

const (
	// DefaultDelay gives an agent time to flush its transcript after the
	// session was killed or completed.
	DefaultDelay = 15 * time.Second
	// DefaultSweepInterval is how often the sweeper looks for terminal
	// sessions without a final collection (dropped events, restarts, backfill).
	DefaultSweepInterval = 10 * time.Minute
	// DefaultBatchSize bounds one sweeper query; explicit jobs run between batches.
	DefaultBatchSize = 50
)

// ReaderFunc returns the usage reader for an agent adapter name, false when
// the agent is unknown or cannot read usage.
type ReaderFunc func(agentName string) (agent.UsageReader, bool)

// Options tunes a Collector; zero values take the defaults.
type Options struct {
	Delay         time.Duration
	SweepInterval time.Duration
	BatchSize     int
	Readers       ReaderFunc
	Now           func() time.Time
}

// Collector writes session_usage/session_stats rows. All collection runs on
// the single goroutine started by Run; the public enqueue methods never block.
type Collector struct {
	st   *store.Store
	bus  *bus.Bus
	opts Options

	mu     sync.Mutex
	queue  []string
	queued map[string]int64 // session id -> terminal event time (0 = none)

	wake       chan struct{}
	subscribed chan struct{}
	subOnce    sync.Once
}

// New builds a Collector; call Run to start it.
func New(st *store.Store, b *bus.Bus, opts Options) *Collector {
	if opts.Delay <= 0 {
		opts.Delay = DefaultDelay
	}
	if opts.SweepInterval <= 0 {
		opts.SweepInterval = DefaultSweepInterval
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = DefaultBatchSize
	}
	if opts.Readers == nil {
		opts.Readers = registryReader
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Collector{
		st: st, bus: b, opts: opts,
		queued:     make(map[string]int64),
		wake:       make(chan struct{}, 1),
		subscribed: make(chan struct{}),
	}
}

func registryReader(name string) (agent.UsageReader, bool) {
	a, err := agent.Get(name)
	if err != nil {
		return nil, false
	}
	r, ok := a.(agent.UsageReader)
	return r, ok
}

// Snapshot queues an immediate collection of an orchestrator whose feature
// went to review: a live session gets a final=0 snapshot, an already
// terminal one its ordinary final collection.
func (c *Collector) Snapshot(sessionID string) { c.enqueue(sessionID, 0) }

// Enqueue queues an immediate (re)collection of one session.
func (c *Collector) Enqueue(sessionID string) { c.enqueue(sessionID, 0) }

// EnqueueTerminal queues terminal sessions for re-collection (see
// store.TerminalSessionIDs) and returns how many were newly queued.
func (c *Collector) EnqueueTerminal(includeCollected, includeMissing bool) (int, error) {
	ids, err := c.st.TerminalSessionIDs(includeCollected, includeMissing)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if c.enqueue(id, 0) {
			n++
		}
	}
	return n, nil
}

// enqueue adds id to the FIFO unless it is already waiting; a known event
// time is kept. Reports whether id was newly queued.
func (c *Collector) enqueue(id string, eventAt int64) bool {
	c.mu.Lock()
	prev, exists := c.queued[id]
	if !exists {
		c.queue = append(c.queue, id)
	}
	if !exists || eventAt > prev {
		c.queued[id] = eventAt
	}
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return !exists
}

func (c *Collector) pop() (string, int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queue) == 0 {
		return "", 0, false
	}
	id := c.queue[0]
	c.queue = c.queue[1:]
	at := c.queued[id]
	delete(c.queued, id)
	return id, at, true
}

// Run subscribes to the bus and processes the queue and the sweeper on one
// goroutine until ctx is cancelled. It sweeps once at start.
func (c *Collector) Run(ctx context.Context) {
	events, unsubscribe := c.bus.Subscribe()
	defer unsubscribe()
	c.subOnce.Do(func() { close(c.subscribed) })

	go c.watchEvents(ctx, events)

	ticker := time.NewTicker(c.opts.SweepInterval)
	defer ticker.Stop()
	sweeping := true
	seen := map[string]bool{}
	for {
		if ctx.Err() != nil {
			return
		}
		if id, at, ok := c.pop(); ok {
			if err := c.CollectNow(ctx, id, at); err != nil && ctx.Err() == nil {
				slog.Error("usage: collect failed", "session", id, "error", err)
			}
			continue
		}
		if sweeping {
			_, more, err := c.sweepBatch(ctx, seen)
			if err != nil && ctx.Err() == nil {
				slog.Error("usage: sweep failed", "error", err)
			}
			if err != nil || !more {
				sweeping = false
				seen = map[string]bool{}
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-ticker.C:
			sweeping = true
		}
	}
}

// watchEvents turns terminal session events into delayed collections.
func (c *Collector) watchEvents(ctx context.Context, events <-chan store.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}
			if !isTerminalEvent(e) {
				continue
			}
			id, at := e.SessionID, e.TS
			time.AfterFunc(c.opts.Delay, func() {
				if ctx.Err() == nil {
					c.enqueue(id, at)
				}
			})
		}
	}
}

func isTerminalEvent(e store.Event) bool {
	if e.SessionID == "" {
		return false
	}
	switch e.Type {
	case "session.killed":
		return true
	case "session.state_changed":
		to, _ := e.Data["to"].(string)
		return isTerminal(to)
	}
	return false
}

func isTerminal(state string) bool {
	return state == "done" || state == "killed" || state == "errored"
}

// Sweep collects every terminal session still needing usage, in batches, and
// returns the number of non-empty batches processed.
func (c *Collector) Sweep(ctx context.Context) (int, error) {
	seen := map[string]bool{}
	batches := 0
	for {
		n, more, err := c.sweepBatch(ctx, seen)
		if n > 0 {
			batches++
		}
		if err != nil || !more {
			return batches, err
		}
	}
}

// sweepBatch collects one batch and reports how many sessions it handled.
// more is false once the sweep is done: the batch was short, or it only
// returned sessions this sweep already handled (a session that cannot leave
// the selection must not spin the sweeper).
func (c *Collector) sweepBatch(ctx context.Context, seen map[string]bool) (n int, more bool, err error) {
	sessions, err := c.st.SessionsNeedingUsage(c.opts.BatchSize, c.opts.Now().Unix())
	if err != nil {
		return 0, false, err
	}
	for _, sess := range sessions {
		if seen[sess.ID] {
			continue
		}
		seen[sess.ID] = true
		n++
		if err := c.CollectNow(ctx, sess.ID, 0); err != nil {
			return n, false, err
		}
	}
	return n, n > 0 && len(sessions) == c.opts.BatchSize, nil
}

// CollectNow reads the session's transcripts and replaces its usage rows.
// eventAt is the terminal event time (0 when unknown, e.g. backfill). Reader
// failures are recorded in the row; only store failures are returned.
func (c *Collector) CollectNow(ctx context.Context, sessionID string, eventAt int64) error {
	sess, err := c.st.GetSession(sessionID)
	if err != nil {
		return err
	}
	taskID, subtaskID, err := c.st.ResolveSessionTask(sessionID)
	if err != nil {
		return err
	}
	prev, prevModels, err := c.st.GetSessionStats(sessionID)
	hasPrev := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}

	terminal := isTerminal(sess.State)
	st := store.SessionStats{
		SessionID:   sessionID,
		TaskID:      taskID,
		SubtaskID:   subtaskID,
		Final:       terminal,
		StartedAt:   sess.CreatedAt,
		CollectedAt: c.opts.Now().Unix(),
	}

	var u agent.Usage
	var readErr error
	if reader, ok := c.opts.Readers(sess.Agent); ok {
		u, readErr = reader.Usage(ctx, sess.WorktreePath, time.Unix(sess.CreatedAt, 0))
		if readErr != nil && ctx.Err() != nil {
			return ctx.Err() // shutting down is not a collection failure
		}
	}

	var models []store.ModelUsage
	switch {
	case readErr != nil:
		// A partial result is never stored: the previous rows stay.
		st.Status, st.Error, st.Attempts = "error", readErr.Error(), 1
		if hasPrev && prev.Status == "error" {
			st.Attempts = prev.Attempts + 1
		}
		models = prevModels
	case !u.Found:
		st.Status = "missing"
	default:
		st.Status = "ok"
		models = toStoreModels(u.Models)
	}

	if terminal {
		ended := sess.UpdatedAt
		switch {
		case eventAt > 0:
			ended = eventAt
		case hasPrev && prev.Final && prev.EndedAt != nil:
			ended = *prev.EndedAt
		case !u.LastAt.IsZero():
			ended = u.LastAt.Unix()
		}
		st.EndedAt = &ended
	}

	if err := c.st.ReplaceSessionUsage(st, models); err != nil {
		return err
	}
	var task any
	if taskID != 0 {
		task = taskID
	}
	c.bus.Publish("usage.collected", sessionID, map[string]any{"task_id": task})
	return nil
}

// toStoreModels is the single agent.Tokens -> store.UsageTokens mapping.
func toStoreModels(in map[string]agent.Tokens) []store.ModelUsage {
	out := make([]store.ModelUsage, 0, len(in))
	for model, t := range in {
		out = append(out, store.ModelUsage{Model: model, Tokens: store.UsageTokens{
			Input: t.Input, CacheWrite: t.CacheWrite, CacheRead: t.CacheRead,
			Output: t.Output, Reasoning: t.Reasoning, Messages: t.Messages,
		}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}
