package queue

// This file verifies the equal-text correlation hypothesis from CTO5902/Q5.
// It is the only file added under that release. Everything it needs lives
// here; no shared helper, production path or fixture is edited.
//
// Scenario: two different Rocket message IDs A and B, the same sender and
// the same short injected envelope, delivered to one native Pi target. A's
// send is accepted but appends no user record (the supported Pi API is
// fire-and-forget; a deferred handler error produces exactly this). B's send
// appends the sole matching record after both send boundaries. The safety
// assertion is that B's record must never confirm A. If the fixture cannot
// identify the record's owner unambiguously, the only acceptable outcome is
// an honest unconfirmed state, never an arbitrary allocation.

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/nativeinput"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// corrRecord is one user record in the isolated transcript oracle. owner is
// the Rocket message ID whose send appended it; the queue never sees it.
type corrRecord struct {
	text  string
	at    time.Time
	owner int64
}

// corrJournal is the deterministic transcript fixture.
type corrJournal struct {
	mu      sync.Mutex
	records []corrRecord
	appends int
}

func (j *corrJournal) appendRecord(text string, owner int64, at time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.records = append(j.records, corrRecord{text: normalizeCorrText(text), at: at, owner: owner})
	j.appends++
}

func (j *corrJournal) snapshot() []corrRecord {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]corrRecord(nil), j.records...)
}

func (j *corrJournal) appendCount() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.appends
}

func (j *corrJournal) only(t *testing.T) corrRecord {
	t.Helper()
	records := j.snapshot()
	if len(records) != 1 {
		t.Fatalf("oracle: journal holds %d records, want exactly 1: %+v", len(records), records)
	}
	return records[0]
}

func normalizeCorrText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// corrClock is the queue's and the fixture's notion of now.
type corrClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *corrClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *corrClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// corrAgent is the delivery confirmer over corrJournal. It implements the
// optional capabilities the queue resolves: DeliveryConfirmer and
// BusyInputAccepter (Pi accepts followUp mid-turn).
type corrAgent struct {
	journal *corrJournal
}

var (
	_ agent.Agent             = (*corrAgent)(nil)
	_ agent.BusyInputAccepter = (*corrAgent)(nil)
	_ agent.DeliveryConfirmer = (*corrAgent)(nil)
)

func (a *corrAgent) ConfirmDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	norm := normalizeCorrText(text)
	for _, record := range a.journal.snapshot() {
		if record.text == norm && record.at.After(since) {
			return true, record.at, nil
		}
	}
	return false, time.Time{}, nil
}

func (a *corrAgent) AcceptsInputWhileBusy() bool { return true }
func (a *corrAgent) Name() string               { return "pi" }
func (a *corrAgent) Available() error           { return nil }
func (a *corrAgent) SetupWorkspace(spec agent.LaunchSpec) error {
	return nil
}
func (a *corrAgent) Efforts() []string { return nil }
func (a *corrAgent) Catalog(ctx context.Context) (agent.Catalog, error) {
	return agent.Catalog{}, nil
}
func (a *corrAgent) LaunchCommand(spec agent.LaunchSpec) []string { return nil }
func (a *corrAgent) Env(spec agent.LaunchSpec) map[string]string  { return nil }
func (a *corrAgent) Activity(ctx context.Context, ref agent.ActivityRef) (activity.State, time.Time, error) {
	return activity.Ready, time.Time{}, nil
}
func (a *corrAgent) TranscriptTail(ctx context.Context, ref agent.ActivityRef, cursor string) ([]agent.ChatEntry, string, error) {
	return nil, cursor, nil
}
func (a *corrAgent) TranscriptStat(ctx context.Context, ref agent.ActivityRef) (int64, int64, error) {
	return 0, 0, nil
}

// corrRuntime counts every visual/tmux entry point so the test can prove the
// native route made zero of them.
type corrRuntime struct {
	mu       sync.Mutex
	injects  int
	sendKeys int
	captures int
	escapes  int
}

func (r *corrRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	return runtime.Handle{Name: spec.Name}, nil
}
func (r *corrRuntime) Inject(ctx context.Context, h runtime.Handle, text string, opts runtime.InjectOpts) error {
	r.mu.Lock()
	r.injects++
	r.mu.Unlock()
	return nil
}
func (r *corrRuntime) SendKeys(ctx context.Context, h runtime.Handle, key string, literal bool) error {
	r.mu.Lock()
	r.sendKeys++
	r.mu.Unlock()
	return nil
}
func (r *corrRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	r.mu.Lock()
	r.captures++
	r.mu.Unlock()
	return "", nil
}
func (r *corrRuntime) CaptureEscaped(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	r.mu.Lock()
	r.escapes++
	r.mu.Unlock()
	return "", nil
}
func (r *corrRuntime) Alive(ctx context.Context, h runtime.Handle) bool { return true }
func (r *corrRuntime) Destroy(ctx context.Context, h runtime.Handle) error {
	return nil
}
func (r *corrRuntime) AttachCommand(h runtime.Handle) []string { return nil }
func (r *corrRuntime) PinWindowSize(ctx context.Context, h runtime.Handle, clientCols, clientRows int) error {
	return nil
}
func (r *corrRuntime) UnpinWindowSize(ctx context.Context, h runtime.Handle) error { return nil }
func (r *corrRuntime) ScrollHistory(ctx context.Context, h runtime.Handle, lines int) error {
	return nil
}
func (r *corrRuntime) ExitHistory(ctx context.Context, h runtime.Handle) error { return nil }
func (r *corrRuntime) List(ctx context.Context) ([]string, error)             { return nil, nil }

func (r *corrRuntime) tmuxCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.injects + r.sendKeys + r.captures + r.escapes
}

// corrSender is the deterministic native sender. It records every call and
// appends a journal record only for the configured message ID; the other
// message's send is accepted with no record, as the supported Pi API allows.
type corrSender struct {
	mu        sync.Mutex
	calls     []int64
	texts     []string
	journal   *corrJournal
	clock     *corrClock
	recordFor int64
}

func (s *corrSender) Send(ctx context.Context, target nativeinput.Target, messageID int64, text string) (nativeinput.Outcome, error) {
	s.mu.Lock()
	s.calls = append(s.calls, messageID)
	s.texts = append(s.texts, text)
	record := messageID == s.recordFor
	s.mu.Unlock()

	// Every send boundary advances the fixture clock; the record belongs to
	// the recipient's turn and appears after the boundary of its own send.
	s.clock.advance(time.Second)
	if record {
		s.journal.appendRecord(text, messageID, s.clock.now().Add(time.Second))
	}
	return nativeinput.Accepted, nil
}

func (s *corrSender) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *corrSender) callIDs() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.calls...)
}

func (s *corrSender) callTexts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.texts...)
}

// corrHarness bundles the real store/queue with the isolated fixtures.
type corrHarness struct {
	q       *Queue
	st      *store.Store
	journal *corrJournal
	sender  *corrSender
	clock   *corrClock
	rt      *corrRuntime
	ctx     context.Context
}

func newCorrHarness(t *testing.T) *corrHarness {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "correlation.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	b := bus.New(st)
	rt := &corrRuntime{}
	clock := &corrClock{t: time.Now()}
	journal := &corrJournal{}
	ag := &corrAgent{journal: journal}
	cfg := &config.Config{QueueTimeout: 30 * time.Minute, LargeMessageThreshold: 2048, SocketDelivery: true}

	q := New(st, b, rt, cfg, func(string) (activity.State, bool) {
		return activity.Ready, true
	})
	q.now = clock.now
	q.backoff = func(int) time.Duration { return time.Millisecond }
	q.SetAgentResolver(func(name string) (agent.Agent, error) { return ag, nil })

	ctx, cancel := context.WithCancel(context.Background())
	q.ctxMu.Lock()
	q.runCtxVal = ctx
	q.ctxMu.Unlock()
	t.Cleanup(func() {
		cancel()
		corrWait(t, 2*time.Second, func() bool {
			q.mu.Lock()
			defer q.mu.Unlock()
			return len(q.workers) == 0
		}, "delivery workers to exit")
	})

	return &corrHarness{
		q: q, st: st, journal: journal, clock: clock, rt: rt, ctx: ctx,
		sender: &corrSender{journal: journal, clock: clock},
	}
}

func (h *corrHarness) addNativeSession(t *testing.T, id string) {
	t.Helper()
	if err := h.st.AddSession(store.Session{
		ID: id, Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "pi",
		Branch: "main", WorktreePath: "/tmp/" + id, TmuxName: id, State: "running",
		DeliveryTransport: "pi-uds-v1", DeliveryInstance: "launch-1", DeliveryEndpoint: "/tmp/corr-native.sock",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
}

func (h *corrHarness) addMessage(t *testing.T, from, to, body string) int64 {
	t.Helper()
	id, err := h.st.AddMessage(store.Message{FromSession: from, ToSession: to, Body: body})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	return id
}

func (h *corrHarness) status(t *testing.T, id int64) string {
	t.Helper()
	msg, err := h.st.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage(%d): %v", id, err)
	}
	return msg.Status
}

func (h *corrHarness) confirmPass() {
	h.q.confirmPass(h.ctx, &confirmState{
		used:     make(map[string]map[recordKey]struct{}),
		restored: make(map[string]bool),
	})
}

func corrWait(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func logCorrMessage(t *testing.T, label string, msg store.Message) {
	t.Helper()
	t.Logf("%s id=%d status=%s attempts=%d inject_at=%d proof_at=%d proof_text=%q reason=%q",
		label, msg.ID, msg.Status, msg.Attempts, msg.InjectAt, msg.DeliveryProofAt, msg.DeliveryProofText, msg.Reason)
}

// TestPiEqualTextCorrelationBRecordDoesNotConfirmA reproduces the Q5 case.
// RED while the queue lets B's sole record settle A.
func TestPiEqualTextCorrelationBRecordDoesNotConfirmA(t *testing.T) {
	h := newCorrHarness(t)
	h.addNativeSession(t, "native")
	const body = "одинаковый короткий текст"
	aID := h.addMessage(t, "sender", "native", body)
	bID := h.addMessage(t, "sender", "native", body)
	if aID == bID {
		t.Fatalf("oracle: A and B must have different Rocket IDs, both are %d", aID)
	}
	h.sender.recordFor = bID
	h.q.SetNativeSender(h.sender)
	h.q.Wake("native")

	corrWait(t, 5*time.Second, func() bool {
		return h.status(t, aID) == "accepted" && h.status(t, bID) == "accepted"
	}, "both native sends accepted")
	if got := h.sender.callCount(); got != 2 {
		t.Fatalf("send boundaries = %d, want 2 (A and B): %v", got, h.sender.callIDs())
	}
	if got := h.journal.appendCount(); got != 1 {
		t.Fatalf("journal appends = %d, want exactly B's record", got)
	}
	record := h.journal.only(t)
	if record.owner != bID {
		t.Fatalf("oracle: the only record was appended by %d, want B=%d", record.owner, bID)
	}
	if got := h.rt.tmuxCalls(); got != 0 {
		t.Fatalf("tmux calls on the native route = %d, want 0", got)
	}

	h.confirmPass()

	a, _ := h.st.GetMessage(aID)
	b, _ := h.st.GetMessage(bID)
	logCorrMessage(t, "A", a)
	logCorrMessage(t, "B", b)
	t.Logf("record text=%q at=%d owner=B(%d); sender calls=%v texts=%v",
		record.text, record.at.UnixNano(), record.owner, h.sender.callIDs(), h.sender.callTexts())

	if a.Status == "delivered" {
		t.Fatalf("SAFETY VIOLATION: B's only record (owner=%d at=%d) confirmed A (id=%d proof_at=%d proof_text=%q); A=%s B=%s",
			record.owner, record.at.UnixNano(), aID, a.DeliveryProofAt, a.DeliveryProofText, a.Status, b.Status)
	}
	// Passing branch: A must stay unconfirmed, and B may only be settled by
	// its own record.
	if b.Status == "delivered" && b.DeliveryProofAt != record.at.UnixNano() {
		t.Fatalf("B delivered by a proof that is not its own record: proof_at=%d record_at=%d", b.DeliveryProofAt, record.at.UnixNano())
	}
}

// TestPiEqualTextCorrelationControls pins the fixture invariants that make
// the main assertion meaningful.
func TestPiEqualTextCorrelationControls(t *testing.T) {
	t.Run("distinct text does not cross-confirm", func(t *testing.T) {
		h := newCorrHarness(t)
		h.addNativeSession(t, "native")
		aID := h.addMessage(t, "sender", "native", "первый текст")
		bID := h.addMessage(t, "sender", "native", "второй текст")
		h.sender.recordFor = bID
		h.q.SetNativeSender(h.sender)
		h.q.Wake("native")
		corrWait(t, 5*time.Second, func() bool {
			return h.status(t, aID) == "accepted" && h.status(t, bID) == "accepted"
		}, "both sends accepted")
		h.confirmPass()
		if got := h.status(t, aID); got == "delivered" {
			t.Fatalf("A with a distinct text was delivered from B's record")
		}
		if got := h.status(t, bID); got != "delivered" {
			t.Fatalf("B with its own record status = %q, want delivered", got)
		}
	})

	t.Run("record before both boundaries is never proof", func(t *testing.T) {
		h := newCorrHarness(t)
		h.addNativeSession(t, "native")
		const body = "старая запись"
		oldAt := h.clock.now().Add(-time.Minute)
		h.journal.appendRecord(body, 0, oldAt)
		aID := h.addMessage(t, "sender", "native", body)
		bID := h.addMessage(t, "sender", "native", body)
		h.sender.recordFor = bID
		h.q.SetNativeSender(h.sender)
		h.q.Wake("native")
		corrWait(t, 5*time.Second, func() bool {
			return h.status(t, aID) == "accepted" && h.status(t, bID) == "accepted"
		}, "both sends accepted")
		h.confirmPass()
		a, _ := h.st.GetMessage(aID)
		b, _ := h.st.GetMessage(bID)
		if a.DeliveryProofAt == oldAt.UnixNano() || b.DeliveryProofAt == oldAt.UnixNano() {
			t.Fatalf("a record before the inject_at boundary was used as proof: A=%+v B=%+v", a, b)
		}
	})

	t.Run("second pass appends nothing and resends nothing", func(t *testing.T) {
		h := newCorrHarness(t)
		h.addNativeSession(t, "native")
		const body = "повторный проход"
		aID := h.addMessage(t, "sender", "native", body)
		bID := h.addMessage(t, "sender", "native", body)
		h.sender.recordFor = bID
		h.q.SetNativeSender(h.sender)
		h.q.Wake("native")
		corrWait(t, 5*time.Second, func() bool {
			return h.status(t, aID) == "accepted" && h.status(t, bID) == "accepted"
		}, "both sends accepted")
		h.confirmPass()
		h.confirmPass()
		if got := h.sender.callCount(); got != 2 {
			t.Fatalf("sender calls after two passes = %d, want 2 (no resend)", got)
		}
		if got := h.journal.appendCount(); got != 1 {
			t.Fatalf("journal appends after two passes = %d, want 1", got)
		}
	})

	t.Run("timeout and Recover do not resend", func(t *testing.T) {
		h := newCorrHarness(t)
		h.addNativeSession(t, "native")
		const body = "таймаут и восстановление"
		aID := h.addMessage(t, "sender", "native", body)
		bID := h.addMessage(t, "sender", "native", body)
		h.sender.recordFor = bID
		h.q.SetNativeSender(h.sender)
		h.q.Wake("native")
		corrWait(t, 5*time.Second, func() bool {
			return h.status(t, aID) == "accepted" && h.status(t, bID) == "accepted"
		}, "both sends accepted")
		h.confirmPass()

		h.clock.advance(31 * time.Minute)
		h.q.Recover(h.ctx)
		h.confirmPass()
		if got := h.sender.callCount(); got != 2 {
			t.Fatalf("sender calls after timeout/Recover = %d, want 2 (no resend)", got)
		}
		if got := h.journal.appendCount(); got != 1 {
			t.Fatalf("journal appends after timeout/Recover = %d, want 1", got)
		}
	})
}
