package queue

// This file verifies the equal-text correlation hypothesis from CTO5902/Q5.
// It is the only file added under that release. Everything it needs lives
// here; no shared helper, production path or fixture is edited.
//
// Scenario: two different Rocket message IDs A and B, the same sender and
// the same short injected envelope, delivered to one native Pi target. A's
// send is accepted but appends no user record. This is the real supported-API
// observation from the stock-Pi probe: the public input handler returns
// {action:"handled"} for the first extension-sourced input, and Pi returns
// before appending a user record. A thrown input-handler error is NOT such
// evidence: installed Pi 1.4.0 runner.js emitInput catches handler exceptions,
// emits the error and continues to the next handler (ultimately
// continue/transform), so a thrown error alone still records the message.
// B's send appends the sole matching record after both send boundaries. The
// safety assertion is that B's record must never confirm A. If the fixture
// cannot identify the record's owner unambiguously, the only acceptable
// outcome is an honest unconfirmed state, never an arbitrary allocation.

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

// ConfirmNativeDelivered is the strict capability used by the Pi adapter in
// this fixture; its whole-record matching is already exact.
func (a *corrAgent) ConfirmNativeDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	return a.ConfirmDelivered(ctx, ref, text, since)
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
// message's send is accepted with no record, matching the real public-API
// observation where the first input is answered with {action:"handled"}.
type corrSender struct {
	mu        sync.Mutex
	calls     []int64
	texts     []string
	journal   *corrJournal
	clock     *corrClock
	recordFor int64
	recordAll bool
	// recordText, when non-empty, is appended instead of the actual wire text;
	// it models an old unsigned sender in the compatibility test.
	recordText string
}

func (s *corrSender) Send(ctx context.Context, target nativeinput.Target, messageID int64, text string) (nativeinput.Outcome, error) {
	s.mu.Lock()
	s.calls = append(s.calls, messageID)
	s.texts = append(s.texts, text)
	record := messageID == s.recordFor || s.recordAll
	recordText := text
	if s.recordText != "" {
		recordText = s.recordText
	}
	s.mu.Unlock()

	// Every send boundary advances the fixture clock. The record belongs to
	// the recipient's turn and is appended at the clock's current value, so
	// it is strictly after both persisted send boundaries and never in the
	// future relative to the confirmation pass.
	s.clock.advance(time.Second)
	if record {
		s.journal.appendRecord(recordText, messageID, s.clock.now())
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
	aPersisted, _ := h.st.GetMessage(aID)
	bPersisted, _ := h.st.GetMessage(bID)
	if !record.at.After(time.UnixMilli(aPersisted.InjectAt)) || !record.at.After(time.UnixMilli(bPersisted.InjectAt)) {
		t.Fatalf("oracle: record at=%d must be strictly after both persisted boundaries A=%d B=%d",
			record.at.UnixNano(), aPersisted.InjectAt, bPersisted.InjectAt)
	}
	if record.at.After(h.clock.now()) {
		t.Fatalf("oracle: record at=%d is in the future relative to the confirmation clock %d",
			record.at.UnixNano(), h.clock.now().UnixNano())
	}
	if aPersisted.DeliveryProofAt != 0 || bPersisted.DeliveryProofAt != 0 {
		t.Fatalf("oracle: messages already carry proof before confirmation: A=%d B=%d",
			aPersisted.DeliveryProofAt, bPersisted.DeliveryProofAt)
	}
	if got := h.sender.callIDs(); len(got) != 2 || got[0] == got[1] {
		t.Fatalf("oracle: send IDs must be distinct, got %v", got)
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
	if b.Status != "delivered" || b.DeliveryProofAt != record.at.UnixNano() {
		t.Fatalf("B was not settled by its own record: status=%s proof_at=%d record_at=%d", b.Status, b.DeliveryProofAt, record.at.UnixNano())
	}

	// The store keeps the same original Body; only the wire text differs by
	// the persisted ID prefix.
	if a.Body != b.Body {
		t.Fatalf("oracle: original Bodies differ: %q vs %q", a.Body, b.Body)
	}
	sent := h.sender.callTexts()
	wantA := nativePiEnvelope(aID, formatBody(a))
	wantB := nativePiEnvelope(bID, formatBody(b))
	if len(sent) != 2 || sent[0] != wantA || sent[1] != wantB {
		t.Fatalf("sent wire texts = %q, want %q and %q", sent, wantA, wantB)
	}
	if sent[0] == sent[1] {
		t.Fatal("different IDs produced the same signed wire text")
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

	t.Run("old-only signed records never prove a later send", func(t *testing.T) {
		h := newCorrHarness(t)
		h.addNativeSession(t, "native")
		const body = "старая запись"
		aID := h.addMessage(t, "sender", "native", body)
		bID := h.addMessage(t, "sender", "native", body)
		aMsg, _ := h.st.GetMessage(aID)
		bMsg, _ := h.st.GetMessage(bID)
		oldAt := h.clock.now().Add(-time.Minute)
		// Seed the exact signed envelopes the sends will use, so age is the
		// only reason these records cannot prove either send.
		h.journal.appendRecord(nativePiEnvelope(aID, formatBody(aMsg)), 0, oldAt)
		h.journal.appendRecord(nativePiEnvelope(bID, formatBody(bMsg)), 0, oldAt)
		// recordFor/recordAll stay off: no matching record after the
		// boundaries, so only the old records could ever match.
		h.q.SetNativeSender(h.sender)
		h.q.Wake("native")
		corrWait(t, 5*time.Second, func() bool {
			return h.status(t, aID) == "accepted" && h.status(t, bID) == "accepted"
		}, "both sends accepted")
		sent := h.sender.callTexts()
		if len(sent) != 2 || sent[0] != nativePiEnvelope(aID, formatBody(aMsg)) || sent[1] != nativePiEnvelope(bID, formatBody(bMsg)) {
			t.Fatalf("oracle: sent texts %q do not equal the seeded signed envelopes", sent)
		}
		h.confirmPass()
		a, _ := h.st.GetMessage(aID)
		b, _ := h.st.GetMessage(bID)
		if a.Status == "delivered" || b.Status == "delivered" {
			t.Fatalf("an old record before the boundaries proved a later send: A=%+v B=%+v", a, b)
		}
		if a.DeliveryProofAt != 0 || b.DeliveryProofAt != 0 {
			t.Fatalf("an old record was recorded as proof: A=%+v B=%+v", a, b)
		}
	})

	t.Run("two equal bodies with two own records settle both", func(t *testing.T) {
		h := newCorrHarness(t)
		h.addNativeSession(t, "native")
		const body = "две записи"
		aID := h.addMessage(t, "sender", "native", body)
		bID := h.addMessage(t, "sender", "native", body)
		h.sender.recordAll = true
		h.q.SetNativeSender(h.sender)
		h.q.Wake("native")
		corrWait(t, 5*time.Second, func() bool {
			return h.status(t, aID) == "accepted" && h.status(t, bID) == "accepted"
		}, "both sends accepted")
		h.confirmPass()
		a, _ := h.st.GetMessage(aID)
		b, _ := h.st.GetMessage(bID)
		if a.Status != "delivered" || b.Status != "delivered" {
			t.Fatalf("two own records did not settle both: A=%s B=%s", a.Status, b.Status)
		}
		if a.DeliveryProofAt == 0 || b.DeliveryProofAt == 0 || a.DeliveryProofAt == b.DeliveryProofAt {
			t.Fatalf("proofs must be distinct records: A=%d B=%d", a.DeliveryProofAt, b.DeliveryProofAt)
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

// TestPiDelayedTimedOutRecordDoesNotConfirmNewMessage: a message that timed
// out before the next identical body was sent can still leave a delayed
// record. That record must never confirm the newer message, and only the
// newer message's own signed record may settle it.
func TestPiDelayedTimedOutRecordDoesNotConfirmNewMessage(t *testing.T) {
	h := newCorrHarness(t)
	h.addNativeSession(t, "native")
	const body = "поздняя запись"
	aID := h.addMessage(t, "sender", "native", body)
	h.q.SetNativeSender(h.sender)
	h.q.Wake("native")
	corrWait(t, 5*time.Second, func() bool { return h.status(t, aID) == "accepted" }, "A accepted")
	if got := h.sender.callCount(); got != 1 {
		t.Fatalf("A sends = %d, want 1", got)
	}
	delayedA := h.sender.callTexts()[0]

	h.clock.advance(31 * time.Minute)
	h.confirmPass()
	a, _ := h.st.GetMessage(aID)
	if a.Status != "failed" || a.Reason != nativeDeliveryUnconfirmed {
		t.Fatalf("A = %+v, want failed/%s before B is sent", a, nativeDeliveryUnconfirmed)
	}

	h.q.cfg.QueueTimeout = 0 // keep B pending until its own record
	bID := h.addMessage(t, "sender", "native", body)
	h.q.Wake("native")
	corrWait(t, 5*time.Second, func() bool { return h.status(t, bID) == "accepted" }, "B accepted")
	if got := h.sender.callCount(); got != 2 {
		t.Fatalf("total sends = %d, want 2 (A and B)", got)
	}
	ownB := h.sender.callTexts()[1]

	// A's delayed record arrives after B's send boundary.
	h.journal.appendRecord(delayedA, aID, h.clock.now())
	h.confirmPass()
	if got := h.status(t, bID); got == "delivered" {
		t.Fatal("delayed timed-out A record confirmed B")
	}

	h.clock.advance(time.Second)
	h.journal.appendRecord(ownB, bID, h.clock.now())
	h.confirmPass()
	b, _ := h.st.GetMessage(bID)
	if b.Status != "delivered" {
		t.Fatalf("B status = %q, want delivered by its own record", b.Status)
	}
	if reopened, _ := h.st.GetMessage(aID); reopened.Status != "failed" {
		t.Fatalf("terminal A was reopened: %+v", reopened)
	}
	if got := h.sender.callCount(); got != 2 {
		t.Fatalf("sender calls after delayed record = %d, want 2 (no resend)", got)
	}
}

// TestPiOldUnsignedNativeRecordStaysUnconfirmed: a native record written by
// the old unsigned sender must not prove the signed message; the row stays
// accepted until the existing timeout/exit path fails it honestly.
func TestPiOldUnsignedNativeRecordStaysUnconfirmed(t *testing.T) {
	h := newCorrHarness(t)
	h.addNativeSession(t, "native")
	const body = "старая неподписанная подача"
	id := h.addMessage(t, "sender", "native", body)
	h.sender.recordFor = id
	h.sender.recordText = "[from sender] " + body // old bare wire text
	h.q.SetNativeSender(h.sender)
	h.q.Wake("native")
	corrWait(t, 5*time.Second, func() bool { return h.status(t, id) == "accepted" }, "signed send accepted")
	if got := h.sender.callCount(); got != 1 {
		t.Fatalf("sends = %d, want 1", got)
	}

	h.confirmPass()
	if got := h.status(t, id); got == "delivered" {
		t.Fatal("old unsigned record confirmed the signed message")
	}

	h.clock.advance(31 * time.Minute)
	h.confirmPass()
	msg, _ := h.st.GetMessage(id)
	if msg.Status != "failed" || msg.Reason != nativeDeliveryUnconfirmed {
		t.Fatalf("message = %+v, want failed/%s", msg, nativeDeliveryUnconfirmed)
	}
	if msg.DeliveryProofAt != 0 {
		t.Fatalf("old unsigned record was stored as proof: %+v", msg)
	}
	if got := h.sender.callCount(); got != 1 {
		t.Fatalf("sender calls after terminal failure = %d, want 1 (no resend)", got)
	}
}

// TestPiDelayedTimedOutRecordAfterRecoverDoesNotConfirmNewMessage repeats the
// delayed terminal-A case across Recover: after the recovery pass, A's delayed
// signed record must still not confirm B, and only B's own record settles it.
func TestPiDelayedTimedOutRecordAfterRecoverDoesNotConfirmNewMessage(t *testing.T) {
	h := newCorrHarness(t)
	h.addNativeSession(t, "native")
	const body = "поздняя запись после Recover"
	aID := h.addMessage(t, "sender", "native", body)
	h.q.SetNativeSender(h.sender)
	h.q.Wake("native")
	corrWait(t, 5*time.Second, func() bool { return h.status(t, aID) == "accepted" }, "A accepted")
	delayedA := h.sender.callTexts()[0]

	h.clock.advance(31 * time.Minute)
	h.confirmPass()
	a, _ := h.st.GetMessage(aID)
	if a.Status != "failed" || a.Reason != nativeDeliveryUnconfirmed {
		t.Fatalf("A = %+v, want failed/%s before B is sent", a, nativeDeliveryUnconfirmed)
	}

	h.q.cfg.QueueTimeout = 0
	bID := h.addMessage(t, "sender", "native", body)
	h.q.Wake("native")
	corrWait(t, 5*time.Second, func() bool { return h.status(t, bID) == "accepted" }, "B accepted")
	ownB := h.sender.callTexts()[1]

	h.q.Recover(h.ctx)
	h.journal.appendRecord(delayedA, aID, h.clock.now())
	h.confirmPass()
	if got := h.status(t, bID); got == "delivered" {
		t.Fatal("delayed timed-out A record confirmed B after Recover")
	}

	h.clock.advance(time.Second)
	h.journal.appendRecord(ownB, bID, h.clock.now())
	h.confirmPass()
	b, _ := h.st.GetMessage(bID)
	if b.Status != "delivered" {
		t.Fatalf("B status = %q, want delivered by its own record", b.Status)
	}
	if reopened, _ := h.st.GetMessage(aID); reopened.Status != "failed" {
		t.Fatalf("terminal A was reopened after Recover: %+v", reopened)
	}
	if got := h.sender.callCount(); got != 2 {
		t.Fatalf("sender calls = %d, want 2 (no resend)", got)
	}
}

// TestPiOldUnsignedPersistedNativeRowStaysUnconfirmed claims a persisted
// native row exactly like the old sender would have left it, writes the old
// bare record after the boundary, and recovers it under the new confirmer.
// The bare record must never prove the signed message; the positive control
// with the exact signed record must still be delivered, so a disabled scan
// cannot make the negative case pass.
func TestPiOldUnsignedPersistedNativeRowStaysUnconfirmed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		signed bool
	}{
		{name: "old unsigned record is rejected"},
		{name: "positive signed record control", signed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCorrHarness(t)
			h.addNativeSession(t, "native")
			const body = "старая сохранённая подача"
			id := h.addMessage(t, "sender", "native", body)
			msg, _ := h.st.GetMessage(id)
			injectAt := h.clock.now()
			if ok, err := h.st.ClaimNativeMessage(id, NativeTransportPiUDS, injectAt.UnixMilli()); err != nil || !ok {
				t.Fatalf("ClaimNativeMessage = %v, %v", ok, err)
			}

			h.clock.advance(time.Second)
			recordText := formatBody(msg)
			if tc.signed {
				recordText = nativePiEnvelope(id, formatBody(msg))
			}
			recordAt := h.clock.now()
			h.journal.appendRecord(recordText, id, recordAt)

			h.q.Recover(h.ctx)
			if got := h.status(t, id); got != "accepted" {
				t.Fatalf("status after Recover = %q, want accepted under confirmation", got)
			}
			h.confirmPass()

			if tc.signed {
				got, _ := h.st.GetMessage(id)
				if got.Status != "delivered" || got.DeliveryProofAt != recordAt.UnixNano() {
					t.Fatalf("signed control = %+v, want delivered by its own record", got)
				}
				return
			}

			if got := h.status(t, id); got == "delivered" {
				t.Fatal("old unsigned record confirmed the persisted native row")
			}
			h.clock.advance(31 * time.Minute)
			h.confirmPass()
			got, _ := h.st.GetMessage(id)
			if got.Status != "failed" || got.Reason != nativeDeliveryUnconfirmed {
				t.Fatalf("message = %+v, want failed/%s", got, nativeDeliveryUnconfirmed)
			}
			if got.DeliveryProofAt != 0 || got.DeliveryProofText != "" {
				t.Fatalf("old bare record stored as proof: %+v", got)
			}
			if h.sender.callCount() != 0 {
				t.Fatalf("sender calls = %d, want 0 (no resend)", h.sender.callCount())
			}
		})
	}
}
