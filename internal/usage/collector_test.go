package usage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/store"
)

// fakeReader returns a scripted usage per worktree and counts calls.
type fakeReader struct {
	mu     sync.Mutex
	usage  map[string]agent.Usage
	err    map[string]error
	calls  map[string]int
	since  map[string]time.Time
	defUse *agent.Usage
}

func newFakeReader() *fakeReader {
	return &fakeReader{usage: map[string]agent.Usage{}, err: map[string]error{},
		calls: map[string]int{}, since: map[string]time.Time{}}
}

func (f *fakeReader) set(wt string, u agent.Usage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usage[wt] = u
}

func (f *fakeReader) setErr(wt string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err[wt] = err
}

func (f *fakeReader) Usage(_ context.Context, wt string, since time.Time) (agent.Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[wt]++
	f.since[wt] = since
	if err := f.err[wt]; err != nil {
		return agent.Usage{}, err
	}
	if u, ok := f.usage[wt]; ok {
		return u, nil
	}
	if f.defUse != nil {
		return *f.defUse, nil
	}
	return agent.Usage{}, nil
}

type fixture struct {
	st     *store.Store
	bus    *bus.Bus
	reader *fakeReader
	c      *Collector
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/rocket.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := bus.New(st)
	r := newFakeReader()
	c := New(st, b, Options{
		Delay:         10 * time.Millisecond,
		SweepInterval: time.Hour,
		Readers: func(name string) (agent.UsageReader, bool) {
			if name == "claude-code" {
				return r, true
			}
			return nil, false
		},
	})
	return &fixture{st: st, bus: b, reader: r, c: c}
}

func (f *fixture) addSession(t *testing.T, id, state string) store.Session {
	t.Helper()
	s := store.Session{ID: id, Kind: "worker", ProjectID: "p", Agent: "claude-code",
		WorktreePath: "/wt/" + id, State: state, CreatedAt: 1000, UpdatedAt: 2000}
	if err := f.st.AddSession(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func twoModels() agent.Usage {
	return agent.Usage{Found: true,
		FirstAt: time.Unix(1100, 0), LastAt: time.Unix(1900, 0),
		Models: map[string]agent.Tokens{
			"claude-opus-5-5":  {Input: 10, CacheWrite: 20, CacheRead: 30, Output: 40, Messages: 2},
			"claude-haiku-4-5": {Input: 1, Output: 2, Messages: 1},
		}}
}

func getStats(t *testing.T, st *store.Store, id string) (store.SessionStats, map[string]store.UsageTokens) {
	t.Helper()
	s, models, err := st.GetSessionStats(id)
	if err != nil {
		t.Fatalf("GetSessionStats(%s): %v", id, err)
	}
	out := map[string]store.UsageTokens{}
	for _, m := range models {
		out[m.Model] = m.Tokens
	}
	return s, out
}

func TestCollectTerminalSessionWritesFinalRow(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "w1", "killed")
	root, _ := f.st.AddTask(store.Task{Title: "feature", ProjectID: "p", Status: "in_progress"})
	sub, _ := f.st.AddTask(store.Task{Title: "sub", ProjectID: "p", ParentID: root, SessionID: "w1", Status: "in_progress"})
	f.reader.set("/wt/w1", twoModels())

	if err := f.c.CollectNow(context.Background(), "w1", 5000); err != nil {
		t.Fatal(err)
	}
	st, models := getStats(t, f.st, "w1")
	if st.Status != "ok" || !st.Final || st.EndedAt == nil || *st.EndedAt != 5000 {
		t.Fatalf("stats = %+v", st)
	}
	if st.TaskID != root || st.SubtaskID != sub || st.StartedAt != 1000 {
		t.Fatalf("task keys/start = %+v", st)
	}
	if got := models["claude-opus-5-5"]; got != (store.UsageTokens{Input: 10, CacheWrite: 20, CacheRead: 30, Output: 40, Messages: 2}) {
		t.Fatalf("opus = %+v", got)
	}
	if len(models) != 2 {
		t.Fatalf("models = %v", models)
	}
	if since := f.reader.since["/wt/w1"]; since.Unix() != 1000 {
		t.Fatalf("since = %v, want created_at", since)
	}
}

func TestSnapshotThenFinalReplacesRows(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "o1", "running")
	f.reader.set("/wt/o1", agent.Usage{Found: true, Models: map[string]agent.Tokens{
		"old-model": {Input: 100}, "claude-opus-5-5": {Input: 5}}})
	if err := f.c.CollectNow(context.Background(), "o1", 0); err != nil {
		t.Fatal(err)
	}
	st, models := getStats(t, f.st, "o1")
	if st.Final || st.EndedAt != nil || len(models) != 2 {
		t.Fatalf("snapshot = %+v %v", st, models)
	}

	if err := f.st.UpdateSessionState("o1", "done"); err != nil {
		t.Fatal(err)
	}
	f.reader.set("/wt/o1", agent.Usage{Found: true, Models: map[string]agent.Tokens{"claude-opus-5-5": {Input: 7}}})
	if err := f.c.CollectNow(context.Background(), "o1", 6000); err != nil {
		t.Fatal(err)
	}
	st, models = getStats(t, f.st, "o1")
	if !st.Final || *st.EndedAt != 6000 {
		t.Fatalf("final = %+v", st)
	}
	if len(models) != 1 || models["claude-opus-5-5"].Input != 7 {
		t.Fatalf("rows not replaced: %v", models)
	}
}

func TestRepeatedFinalCollectionIsIdempotent(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "w1", "done")
	f.reader.set("/wt/w1", twoModels())
	for i := 0; i < 2; i++ {
		if err := f.c.CollectNow(context.Background(), "w1", 5000); err != nil {
			t.Fatal(err)
		}
	}
	first, m1 := getStats(t, f.st, "w1")
	if err := f.c.CollectNow(context.Background(), "w1", 0); err != nil {
		t.Fatal(err)
	}
	second, m2 := getStats(t, f.st, "w1")
	if fmt.Sprint(m1) != fmt.Sprint(m2) || *first.EndedAt != *second.EndedAt {
		t.Fatalf("not idempotent: %+v %v vs %+v %v", first, m1, second, m2)
	}
}

func TestRestoredSessionGetsSnapshotNotFinal(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "w1", "running") // terminal event arrived, but session was restored
	f.reader.set("/wt/w1", twoModels())
	if err := f.c.CollectNow(context.Background(), "w1", 5000); err != nil {
		t.Fatal(err)
	}
	st, _ := getStats(t, f.st, "w1")
	if st.Final || st.EndedAt != nil {
		t.Fatalf("live session written as final: %+v", st)
	}
}

func TestMissingTranscriptAndUnknownAgent(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "w1", "killed")
	if err := f.st.AddSession(store.Session{ID: "x1", Kind: "worker", Agent: "gemini", State: "killed",
		WorktreePath: "/wt/x1", CreatedAt: 1000, UpdatedAt: 2000}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"w1", "x1"} {
		if err := f.c.CollectNow(context.Background(), id, 0); err != nil {
			t.Fatal(err)
		}
		st, models := getStats(t, f.st, id)
		if st.Status != "missing" || !st.Final || len(models) != 0 {
			t.Fatalf("%s = %+v %v", id, st, models)
		}
		if st.EndedAt == nil || *st.EndedAt != 2000 {
			t.Fatalf("%s ended_at = %v, want updated_at", id, st.EndedAt)
		}
	}
	batches, err := f.c.Sweep(context.Background())
	if err != nil || batches != 0 {
		t.Fatalf("sweeper re-picked missing: %d %v", batches, err)
	}
}

func TestReaderErrorCountsAttempts(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "w1", "killed")
	f.reader.setErr("/wt/w1", errors.New("permission denied"))
	for want := 1; want <= 3; want++ {
		if err := f.c.CollectNow(context.Background(), "w1", 5000); err != nil {
			t.Fatal(err)
		}
		st, models := getStats(t, f.st, "w1")
		if st.Status != "error" || st.Attempts != want || st.Error != "permission denied" || !st.Final || len(models) != 0 {
			t.Fatalf("attempt %d: %+v %v", want, st, models)
		}
	}
	// attempts=3: the sweeper never retries it, even an hour later.
	got, err := f.st.SessionsNeedingUsage(50, time.Now().Unix()+7200)
	if err != nil || len(got) != 0 {
		t.Fatalf("needing = %v %v", got, err)
	}
	f.reader.setErr("/wt/w1", nil)
	f.reader.set("/wt/w1", twoModels())
	if err := f.c.CollectNow(context.Background(), "w1", 5000); err != nil {
		t.Fatal(err)
	}
	if st, models := getStats(t, f.st, "w1"); st.Status != "ok" || st.Attempts != 0 || st.Error != "" || len(models) != 2 {
		t.Fatalf("success did not reset: %+v %v", st, models)
	}
}

// Claude Code deletes old transcripts: a later re-collection must never
// replace collected history with missing or error.
func TestOkRowIsNeverDowngraded(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(f *fixture)
	}{
		{"transcript gone", func(f *fixture) { f.reader.set("/wt/w1", agent.Usage{}) }},
		{"read error", func(f *fixture) { f.reader.setErr("/wt/w1", errors.New("boom")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.addSession(t, "w1", "done")
			f.reader.set("/wt/w1", twoModels())
			clock := int64(10000)
			f.c.opts.Now = func() time.Time { return time.Unix(clock, 0) }
			if err := f.c.CollectNow(context.Background(), "w1", 5000); err != nil {
				t.Fatal(err)
			}
			before, m1 := getStats(t, f.st, "w1")
			tc.set(f)
			clock = 20000
			if err := f.c.CollectNow(context.Background(), "w1", 7000); err != nil {
				t.Fatal(err)
			}
			after, m2 := getStats(t, f.st, "w1")
			if after.Status != "ok" || after.Attempts != 0 || after.Error != "" || !after.Final {
				t.Fatalf("downgraded: %+v", after)
			}
			if fmt.Sprint(m1) != fmt.Sprint(m2) {
				t.Fatalf("rows changed: %v -> %v", m1, m2)
			}
			if *after.EndedAt != *before.EndedAt || after.CollectedAt != 20000 {
				t.Fatalf("ended_at/collected_at = %d/%d, want %d/20000", *after.EndedAt, after.CollectedAt, *before.EndedAt)
			}
		})
	}
}

func TestOkSnapshotThenTranscriptGoneBecomesFinalOk(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "o1", "running")
	f.reader.set("/wt/o1", twoModels())
	if err := f.c.CollectNow(context.Background(), "o1", 0); err != nil {
		t.Fatal(err)
	}
	_ = f.st.UpdateSessionState("o1", "killed")
	f.reader.set("/wt/o1", agent.Usage{})
	if err := f.c.CollectNow(context.Background(), "o1", 6000); err != nil {
		t.Fatal(err)
	}
	st, models := getStats(t, f.st, "o1")
	if st.Status != "ok" || !st.Final || st.EndedAt == nil || *st.EndedAt != 6000 || len(models) != 2 {
		t.Fatalf("snapshot not finalised: %+v %v", st, models)
	}
	if batches, _ := f.c.Sweep(context.Background()); batches != 0 {
		t.Fatalf("sweeper re-picks it: %d", batches)
	}
}

func TestBackfillEndedAtFallsBackToLastAtThenUpdatedAt(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "a", "done")
	f.addSession(t, "b", "done")
	f.reader.set("/wt/a", twoModels())                                                                // LastAt 1900
	f.reader.set("/wt/b", agent.Usage{Found: true, Models: map[string]agent.Tokens{"m": {Input: 1}}}) // no LastAt
	for _, id := range []string{"a", "b"} {
		if err := f.c.CollectNow(context.Background(), id, 0); err != nil {
			t.Fatal(err)
		}
	}
	if st, _ := getStats(t, f.st, "a"); *st.EndedAt != 1900 {
		t.Fatalf("a ended_at = %d", *st.EndedAt)
	}
	if st, _ := getStats(t, f.st, "b"); *st.EndedAt != 2000 {
		t.Fatalf("b ended_at = %d", *st.EndedAt)
	}
}

func TestCollectPublishesUsageCollected(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "o1", "killed")
	root, _ := f.st.AddTask(store.Task{Title: "feature", ProjectID: "p", SessionID: "o1", Status: "in_progress"})
	ch, cancel := f.bus.Subscribe()
	defer cancel()
	if err := f.c.CollectNow(context.Background(), "o1", 0); err != nil {
		t.Fatal(err)
	}
	e := waitEvent(t, ch, "usage.collected")
	if e.SessionID != "o1" || e.Data["task_id"] != root {
		t.Fatalf("event = %+v", e)
	}
}

// waitSession waits for usage.collected of one session, skipping others.
func waitSession(t *testing.T, ch <-chan store.Event, id string) store.Event {
	t.Helper()
	for {
		if e := waitEvent(t, ch, "usage.collected"); e.SessionID == id {
			return e
		}
	}
}

func waitEvent(t *testing.T, ch <-chan store.Event, typ string) store.Event {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Type == typ {
				return e
			}
		case <-timeout:
			t.Fatalf("no %s event", typ)
		}
	}
}

func TestSweepProcessesInBatches(t *testing.T) {
	f := newFixture(t)
	f.reader.defUse = &agent.Usage{Found: true, Models: map[string]agent.Tokens{"m": {Input: 1}}}
	for i := 0; i < 120; i++ {
		f.addSession(t, fmt.Sprintf("s%03d", i), "done")
	}
	f.addSession(t, "live", "running")
	batches, err := f.c.Sweep(context.Background())
	if err != nil || batches != 3 {
		t.Fatalf("batches = %d, err = %v", batches, err)
	}
	for i := 0; i < 120; i++ {
		if st, _ := getStats(t, f.st, fmt.Sprintf("s%03d", i)); !st.Final || st.Status != "ok" {
			t.Fatalf("s%03d = %+v", i, st)
		}
	}
	if _, _, err := f.st.GetSessionStats("live"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("live session swept: %v", err)
	}
	if batches, _ := f.c.Sweep(context.Background()); batches != 0 {
		t.Fatalf("second sweep batches = %d", batches)
	}
}

func TestSweepStopsOnStoreFailure(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "s1", "done")
	f.st.Close()
	done := make(chan struct{})
	go func() {
		_, _ = f.c.Sweep(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sweep loops on a broken store")
	}
}

func TestRunCollectsTerminalEventsAfterDelay(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "w1", "running")
	f.addSession(t, "w2", "running")
	f.reader.set("/wt/w1", twoModels())
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { f.c.Run(ctx); close(stopped) }()
	ch, unsub := f.bus.Subscribe()
	defer unsub()
	f.c.waitSubscribed(t)

	_ = f.st.UpdateSessionState("w1", "killed")
	f.bus.Publish("session.killed", "w1", nil)
	// A non-terminal transition is ignored.
	f.bus.Publish("session.state_changed", "w2", map[string]any{"to": "running"})

	waitSession(t, ch, "w1")
	if st, _ := getStats(t, f.st, "w1"); !st.Final || st.Status != "ok" {
		t.Fatalf("w1 = %+v", st)
	}
	if _, _, err := f.st.GetSessionStats("w2"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("w2 collected on non-terminal event: %v", err)
	}

	_ = f.st.UpdateSessionState("w2", "errored")
	f.bus.Publish("session.state_changed", "w2", map[string]any{"from": "spawning", "to": "errored"})
	waitSession(t, ch, "w2")
	if st, _ := getStats(t, f.st, "w2"); !st.Final || st.Status != "missing" {
		t.Fatalf("w2 = %+v", st)
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancel")
	}
}

func TestRunSweepsAtStart(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "old", "done") // terminal before the daemon started: no event ever
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, unsub := f.bus.Subscribe()
	defer unsub()
	go f.c.Run(ctx)
	e := waitEvent(t, ch, "usage.collected")
	if e.SessionID != "" || e.Data["task_id"] != nil || e.Data["batch"] != 1 {
		t.Fatalf("start-up sweep event = %+v", e)
	}
	if st, _ := getStats(t, f.st, "old"); !st.Final {
		t.Fatalf("old = %+v", st)
	}
}

func TestSweepPublishesOneEventPerBatch(t *testing.T) {
	f := newFixture(t)
	f.reader.defUse = &agent.Usage{Found: true, Models: map[string]agent.Tokens{"m": {Input: 1}}}
	for i := 0; i < 120; i++ {
		f.addSession(t, fmt.Sprintf("s%03d", i), "done")
	}
	ch, unsub := f.bus.Subscribe()
	defer unsub()
	if _, err := f.c.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got []any
	for len(ch) > 0 {
		e := <-ch
		if e.Type != "usage.collected" {
			continue
		}
		if e.SessionID != "" || e.Data["task_id"] != nil {
			t.Fatalf("per-session event from sweeper: %+v", e)
		}
		got = append(got, e.Data["batch"])
	}
	if fmt.Sprint(got) != "[50 50 20]" {
		t.Fatalf("batch events = %v", got)
	}
}

func TestBulkRecollectPublishesBatchEvents(t *testing.T) {
	f := newFixture(t)
	f.c.opts.BatchSize = 2
	for _, id := range []string{"a", "b", "c"} {
		f.addSession(t, id, "done")
		if err := f.c.CollectNow(context.Background(), id, 0); err != nil {
			t.Fatal(err)
		}
	}
	ch, unsub := f.bus.Subscribe()
	defer unsub()
	if n, err := f.c.EnqueueTerminal(false, true); err != nil || n != 3 {
		t.Fatalf("EnqueueTerminal = %d %v", n, err)
	}
	f.c.Enqueue("a") // already queued in bulk: an explicit request still announces it
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.c.Run(ctx)
	var batches []any
	perSession := map[string]int{}
	for len(batches) < 1 || sumBatches(batches) < 2 {
		e := waitEvent(t, ch, "usage.collected")
		if e.SessionID == "" {
			batches = append(batches, e.Data["batch"])
		} else {
			perSession[e.SessionID]++
		}
	}
	if fmt.Sprint(batches) != "[2]" || perSession["a"] != 1 || len(perSession) != 1 {
		t.Fatalf("batches = %v, per-session = %v", batches, perSession)
	}
}

func sumBatches(b []any) int {
	n := 0
	for _, v := range b {
		n += v.(int)
	}
	return n
}

func TestSnapshotAndEnqueueAll(t *testing.T) {
	f := newFixture(t)
	f.addSession(t, "ok", "done")
	f.addSession(t, "miss", "done")
	f.addSession(t, "live", "running")
	f.reader.set("/wt/ok", twoModels())
	ctx := context.Background()
	for _, id := range []string{"ok", "miss"} {
		if err := f.c.CollectNow(ctx, id, 0); err != nil {
			t.Fatal(err)
		}
	}
	n, err := f.c.EnqueueTerminal(true, false)
	if err != nil || n != 1 {
		t.Fatalf("EnqueueTerminal(true,false) = %d %v", n, err)
	}
	n, err = f.c.EnqueueTerminal(true, true)
	if err != nil || n != 1 { // "ok" is already queued; only "miss" is new
		t.Fatalf("EnqueueTerminal(true,true) = %d %v", n, err)
	}
	f.c.Snapshot("live")
	if got := f.c.queuedIDs(); fmt.Sprint(got) != "[ok miss live]" {
		t.Fatalf("queue = %v", got)
	}
}

func (c *Collector) waitSubscribed(t *testing.T) {
	t.Helper()
	select {
	case <-c.subscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("collector never subscribed")
	}
}

func (c *Collector) queuedIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.queue...)
}
