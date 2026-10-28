package queue

import (
	"context"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
)

// confirmCall records one ConfirmDelivered call made by the confirmer.
type confirmCall struct {
	text  string
	since time.Time
}

// fakeAgent stands in for the codex adapter: it takes input while busy and
// confirms delivery from a scripted journal.
type fakeAgent struct {
	mu        sync.Mutex
	busy      bool
	confirmFn func(text string, since time.Time) (bool, time.Time, error)
	calls     []confirmCall
}

var (
	_ agent.Agent             = (*fakeAgent)(nil)
	_ agent.BusyInputAccepter = (*fakeAgent)(nil)
	_ agent.DeliveryConfirmer = (*fakeAgent)(nil)
)

func (a *fakeAgent) setBusy(b bool) {
	a.mu.Lock()
	a.busy = b
	a.mu.Unlock()
}

func (a *fakeAgent) setConfirm(fn func(text string, since time.Time) (bool, time.Time, error)) {
	a.mu.Lock()
	a.confirmFn = fn
	a.mu.Unlock()
}

// confirmRecords scripts a journal: for each text, the timestamps of the
// records holding it, in order. ConfirmDelivered answers with the earliest
// record strictly after since, like the real adapter.
func (a *fakeAgent) confirmRecords(records map[string][]time.Time) {
	a.setConfirm(func(text string, since time.Time) (bool, time.Time, error) {
		for _, at := range records[text] {
			if at.After(since) {
				return true, at, nil
			}
		}
		return false, time.Time{}, nil
	})
}

func (a *fakeAgent) confirmCalls() []confirmCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]confirmCall(nil), a.calls...)
}

func (a *fakeAgent) AcceptsInputWhileBusy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.busy
}

func (a *fakeAgent) ConfirmDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	a.mu.Lock()
	a.calls = append(a.calls, confirmCall{text: text, since: since})
	fn := a.confirmFn
	a.mu.Unlock()
	if fn == nil {
		return false, time.Time{}, nil
	}
	return fn(text, since)
}

func (a *fakeAgent) Name() string                                       { return "codex" }
func (a *fakeAgent) Available() error                                   { return nil }
func (a *fakeAgent) SetupWorkspace(spec agent.LaunchSpec) error         { return nil }
func (a *fakeAgent) Efforts() []string                                  { return nil }
func (a *fakeAgent) Catalog(ctx context.Context) (agent.Catalog, error) { return agent.Catalog{}, nil }
func (a *fakeAgent) LaunchCommand(spec agent.LaunchSpec) []string       { return nil }
func (a *fakeAgent) Env(spec agent.LaunchSpec) map[string]string        { return nil }
func (a *fakeAgent) Activity(ctx context.Context, ref agent.ActivityRef) (activity.State, time.Time, error) {
	return activity.Ready, time.Time{}, nil
}
func (a *fakeAgent) TranscriptTail(ctx context.Context, ref agent.ActivityRef, cursor string) ([]agent.ChatEntry, string, error) {
	return nil, cursor, nil
}
func (a *fakeAgent) TranscriptStat(ctx context.Context, ref agent.ActivityRef) (int64, int64, error) {
	return 0, 0, nil
}

// fakeClock is the queue's notion of now in tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Now()} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}
