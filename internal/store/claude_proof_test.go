package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func appendClaudeIncoming(t *testing.T, v claudeinstance.Snapshot, recordID string) claudeinstance.Proof {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"type": "user", "sessionId": v.Target.ClaudeSessionID, "uuid": recordID, "message": map[string]string{"role": "user", "content": v.Envelope}})
	f, e := os.OpenFile(v.Boundary.Path, os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		t.Fatal(e)
	}
	st, _ := f.Stat()
	f.Write(append(raw, '\n'))
	f.Close()
	return claudeinstance.Proof{RecordID: recordID, Offset: st.Size(), End: st.Size() + int64(len(raw)) + 1}
}
func nextClaudeSnapshot(t *testing.T, st *Store, old claudeinstance.Snapshot, generation string) claudeinstance.Snapshot {
	t.Helper()
	l, _, e := st.GetClaudeLaunch("s")
	if e != nil {
		t.Fatal(e)
	}
	l.Generation = generation
	l.StartedAt = time.Now().UTC()
	if e = st.PutClaudeLaunch(l); e != nil {
		t.Fatal(e)
	}
	v := old
	v.Target.LaunchGeneration = generation
	v.Target.PID++
	v.Target.ProcessStart = time.Now().UTC()
	if ok, e := st.BindClaudeIdentity(v.Target); !ok || e != nil {
		t.Fatalf("bind new %v %v", ok, e)
	}
	v.MessageID, e = st.AddMessage(Message{ToSession: "s", Body: "feedback"})
	if e != nil {
		t.Fatal(e)
	}
	oldWire := v.WireID
	v.WireID, _ = socketmsg.NewMsgID()
	v.Envelope = strings.Replace(v.Envelope, oldWire, v.WireID, 1)
	v.Boundary, e = claudeinstance.PinJournal(v.Target)
	if e != nil {
		t.Fatal(e)
	}
	v.PreparedAt = time.Now().UTC()
	v.Deadline = v.PreparedAt.Add(time.Hour)
	return v
}
func TestClaudeExactProofConsumesPhysicalRecord(t *testing.T) {
	st := openTestStore(t)
	v, id := claudeStoreFixture(t, st)
	if ok, e := st.ClaimClaudeMessage(v); !ok || e != nil {
		t.Fatal(e)
	}
	p := appendClaudeIncoming(t, v, "record-a")
	if ok, e := st.ConfirmClaudeMessage(id, p, time.Now()); !ok || e != nil {
		t.Fatalf("exact proof %v %v", ok, e)
	}
	m, _ := st.GetMessage(id)
	if m.Status != "delivered" {
		t.Fatal(m.Status)
	}
	next := nextClaudeSnapshot(t, st, v, "g2")
	next.Boundary = v.Boundary // stale overlapping cursor must never reuse old physical record
	if ok, e := st.ClaimClaudeMessage(next); !ok || e != nil {
		t.Fatal(e)
	}
	if ok, e := st.ConfirmClaudeMessage(next.MessageID, p, time.Now()); ok || e != nil {
		t.Fatalf("cross-generation record reused %v %v", ok, e)
	}
	var key string
	if e := st.db.QueryRow(`SELECT target_key FROM claude_consumed_records WHERE message_id=?`, id).Scan(&key); e != nil || key != v.Target.ClaudeSessionID {
		t.Fatalf("physical key %q %v", key, e)
	}
}
func TestLateEqualEnvelopeAcrossGenerationsRemainsAmbiguousAfterPurge(t *testing.T) {
	st := openTestStore(t)
	a, id := claudeStoreFixture(t, st)
	a.EnvelopeVersion = 0
	a.Envelope = "legacy equal envelope"
	claimLegacyClaudeFixture(t, st, a)
	b := nextClaudeSnapshot(t, st, a, "g2")
	if ok, e := claimLegacyClaudeFixture(t, st, b); !ok || e != nil {
		t.Fatalf("B claim %v %v", ok, e)
	}
	st.ExpireClaudeMessage(id, a.Deadline.Add(time.Second))
	if e := st.PurgeOld(time.Now().Add(time.Minute).Unix()); e != nil {
		t.Fatal(e)
	}
	if _, e := st.GetMessage(id); e != nil {
		t.Fatal("unresolved old A was purged")
	}
	p := appendClaudeIncoming(t, b, "late-A")
	if ok, e := st.ConfirmClaudeMessage(b.MessageID, p, time.Now()); ok || e != nil {
		t.Fatalf("late G1 input assigned to G2 B %v %v", ok, e)
	}
	appendClaudeIncoming(t, b, "late-B")
	if _, ok, _ := claudeinstance.FindProof(context.Background(), b); ok {
		t.Fatal("two identical records arbitrarily allocated")
	}
}
func TestClaudeReplacementJournalAndDamagedOldHistoryPreventNewClaim(t *testing.T) {
	for _, which := range []string{"replacement inode different envelope", "missing old side row", "corrupt old snapshot"} {
		t.Run(which, func(t *testing.T) {
			st := openTestStore(t)
			a, id := claudeStoreFixture(t, st)
			st.ClaimClaudeMessage(a)
			if which == "replacement inode different envelope" {
				os.Rename(a.Boundary.Path, a.Boundary.Path+".old")
				os.WriteFile(a.Boundary.Path, nil, 0600)
			}
			b := nextClaudeSnapshot(t, st, a, "g2")
			if which == "replacement inode different envelope" {
				b.Envelope, _ = socketmsg.EnvelopeWithID(b.WireID, "", "", "different envelope")
			}
			if which == "missing old side row" {
				st.db.Exec(`DELETE FROM claude_deliveries WHERE message_id=?`, id)
			}
			if which == "corrupt old snapshot" {
				st.db.Exec(`UPDATE claude_deliveries SET snapshot_json='{' WHERE message_id=?`, id)
			}
			if ok, _ := st.ClaimClaudeMessage(b); ok {
				t.Fatal("unknown old ownership allowed new side effect")
			}
			m, _ := st.GetMessage(b.MessageID)
			if m.Status != "queued" || m.DeliveryTransport != "" {
				t.Fatalf("failed claim not atomic %+v", m)
			}
		})
	}
}
func TestExpiredClaudeClaimIsRejectedBeforeSideEffect(t *testing.T) {
	st := openTestStore(t)
	v, _ := claudeStoreFixture(t, st)
	v.PreparedAt = time.Now().Add(-time.Hour)
	v.Deadline = time.Now().Add(-time.Minute)
	if ok, _ := st.ClaimClaudeMessage(v); ok {
		t.Fatal("expired snapshot claimed")
	}
}

func TestClaudeSideRowOwnsRouteRecoveryExpiryAndRetentionWithEmptyMarker(t *testing.T) {
	st := openTestStore(t)
	v, id := claudeStoreFixture(t, st)
	st.ClaimClaudeMessage(v)
	if _, e := st.db.Exec(`UPDATE messages SET delivery_transport='' WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	got, _ := st.GetMessage(id)
	if got.DeliveryTransport != "claude-socket-v1" {
		t.Errorf("side snapshot downgraded route: %+v", got)
	}
	st.ResetDelivering()
	got, _ = st.GetMessage(id)
	if got.Status != "accepted" {
		t.Errorf("side snapshot requeued: %+v", got)
	}
	if ok, e := st.ExpireClaudeMessage(id, v.Deadline.Add(time.Second)); !ok || e != nil {
		t.Errorf("expiry lost owner %v %v", ok, e)
	}
	// A corrupt side row with an empty main marker still cannot be collected.
	st.db.Exec(`UPDATE messages SET delivery_transport='' WHERE id=?`, id)
	st.db.Exec(`UPDATE claude_deliveries SET snapshot_json='{' WHERE message_id=?`, id)
	if e := st.PurgeOld(time.Now().Add(time.Hour).Unix()); e != nil {
		t.Fatal(e)
	}
	if _, e := st.GetMessage(id); e != nil {
		t.Errorf("damaged empty-marker owner purged: %v", e)
	}
}

func TestClaudeProofRejectsDamagedOldHistoryAfterClaim(t *testing.T) {
	for _, which := range []string{"missing", "corrupt", "scalar mismatch"} {
		t.Run(which, func(t *testing.T) {
			st := openTestStore(t)
			a, id := claudeStoreFixture(t, st)
			st.ClaimClaudeMessage(a)
			b := nextClaudeSnapshot(t, st, a, "g2")
			b.Envelope, _ = socketmsg.EnvelopeWithID(b.WireID, "", "", "different envelope")
			st.ClaimClaudeMessage(b)
			switch which {
			case "missing":
				st.db.Exec(`DELETE FROM claude_deliveries WHERE message_id=?`, id)
			case "corrupt":
				st.db.Exec(`UPDATE claude_deliveries SET snapshot_json='{' WHERE message_id=?`, id)
			case "scalar mismatch":
				st.db.Exec(`UPDATE claude_deliveries SET claude_uuid='foreign' WHERE message_id=?`, id)
			}
			p := appendClaudeIncoming(t, b, "new-input")
			if ok, _ := st.ConfirmClaudeMessage(b.MessageID, p, time.Now()); ok {
				t.Fatal("bad old history hidden by envelope/UUID index")
			}
		})
	}
}

func TestClaudePhysicalConsumptionAtomicAcrossGenerationsAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rocket.db")
	st, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := claudeStoreFixture(t, st)
	st.ClaimClaudeMessage(a)
	b := nextClaudeSnapshot(t, st, a, "g2")
	b.Envelope, _ = socketmsg.EnvelopeWithID(b.WireID, "", "", "different envelope")
	if ok, e := st.ClaimClaudeMessage(b); !ok || e != nil {
		t.Fatal(e)
	}
	pa := appendClaudeIncoming(t, a, "physical-record-id")
	pb := appendClaudeIncoming(t, b, "physical-record-id")
	// Even conflicting records with an identical persisted record ID cannot
	// acquire two owners under concurrent generations.
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, pair := range []struct {
		v claudeinstance.Snapshot
		p claudeinstance.Proof
	}{{a, pa}, {b, pb}} {
		wg.Add(1)
		go func(v claudeinstance.Snapshot, p claudeinstance.Proof) {
			defer wg.Done()
			ok, e := st.ConfirmClaudeMessage(v.MessageID, p, time.Now())
			results <- ok
			errs <- e
		}(pair.v, pair.p)
	}
	wg.Wait()
	close(results)
	close(errs)
	wins := 0
	for ok := range results {
		if ok {
			wins++
		}
	}
	for e := range errs {
		if e != nil {
			var code interface{ Code() int }
			if !errors.As(e, &code) || code.Code()&255 != 5 {
				t.Fatal(e)
			}
			t.Logf("concurrent proof failed closed on SQLite writer contention: %v", e)
		}
	}
	if wins != 1 {
		t.Fatalf("physical proof winners=%d", wins)
	}
	if e := st.Close(); e != nil {
		t.Fatal(e)
	}
	st, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	st.ResetDelivering()
	if ok, e := st.ConfirmClaudeMessage(a.MessageID, pa, time.Now()); ok || e != nil {
		t.Fatalf("restart consumed again A %v %v", ok, e)
	}
	if ok, e := st.ConfirmClaudeMessage(b.MessageID, pb, time.Now()); ok || e != nil {
		t.Fatalf("restart consumed again B %v %v", ok, e)
	}
	var n int
	st.db.QueryRow(`SELECT count(*) FROM claude_consumed_records`).Scan(&n)
	if n != 1 {
		t.Fatalf("ownership rows=%d", n)
	}
}

func TestClaudePurgePreservesOnlyNeededResolvedProofAndForeignTransport(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		t.Run(fmt.Sprint(overlap), func(t *testing.T) {
			st := openTestStore(t)
			a, id := claudeStoreFixture(t, st)
			a.EnvelopeVersion = 0
			a.Envelope = "legacy equal envelope"
			claimLegacyClaudeFixture(t, st, a)
			p := appendClaudeIncoming(t, a, "record-a")
			if ok, e := st.ConfirmClaudeMessage(id, p, time.Now()); !ok || e != nil {
				t.Fatal(e)
			}
			b := nextClaudeSnapshot(t, st, a, "g2")
			if overlap {
				b.Boundary = a.Boundary
			}
			if ok, e := claimLegacyClaudeFixture(t, st, b); !ok || e != nil {
				t.Fatal(e)
			}
			legacy, _ := st.AddMessage(Message{ToSession: "s", Body: "legacy", Status: "delivered"})
			native, _ := st.AddMessage(Message{ToSession: "s", Body: "native", Status: "accepted", DeliveryTransport: "pi-uds-v1"})
			if e := st.PurgeOld(time.Now().Add(time.Minute).Unix()); e != nil {
				t.Fatal(e)
			}
			_, e := st.GetMessage(id)
			if overlap && e != nil {
				t.Fatal("overlapping consumed owner removed")
			}
			if !overlap && !errors.Is(e, ErrNotFound) {
				t.Fatalf("unrelated resolved owner not purged: %v", e)
			}
			if _, e = st.GetMessage(b.MessageID); e != nil {
				t.Fatal("unresolved owner removed")
			}
			if _, e = st.GetMessage(native); e != nil {
				t.Fatal("native exception changed")
			}
			if _, e = st.GetMessage(legacy); !errors.Is(e, ErrNotFound) {
				t.Fatal("legacy purge changed")
			}
			var count int
			st.db.QueryRow(`SELECT count(*) FROM claude_consumed_records WHERE message_id=?`, id).Scan(&count)
			want := 0
			if overlap {
				want = 1
			}
			if count != want {
				t.Fatalf("FK consumed cleanup=%d want=%d", count, want)
			}
		})
	}
}

func TestClaudeTerminalProofExpiryAndNegativeCannotReopen(t *testing.T) {
	for _, phase := range []string{"not_sent", "denied", "expired", "unconfirmed", "delivered"} {
		t.Run(phase, func(t *testing.T) {
			st := openTestStore(t)
			v, id := claudeStoreFixture(t, st)
			st.ClaimClaudeMessage(v)
			p := appendClaudeIncoming(t, v, "exact")
			switch phase {
			case "unconfirmed":
				st.ExpireClaudeMessage(id, v.Deadline.Add(time.Second))
			case "delivered":
				st.ConfirmClaudeMessage(id, p, time.Now())
			default:
				st.SetClaudePhase(id, v.WireID, v.Target, phase, time.Now())
			}
			before, _ := st.GetMessage(id)
			if ok, e := st.SetClaudePhase(id, v.WireID, v.Target, "accepted", time.Now()); ok || e != nil {
				t.Fatalf("callback reopened %v %v", ok, e)
			}
			if ok, e := st.ConfirmClaudeMessage(id, p, time.Now()); ok || e != nil {
				t.Fatalf("proof reopened %v %v", ok, e)
			}
			st.ExpireClaudeMessage(id, v.Deadline.Add(time.Second))
			after, _ := st.GetMessage(id)
			if before != after {
				t.Fatalf("terminal changed before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestClaudeContextCanceledBeforeProofHasNoSideEffect(t *testing.T) {
	st := openTestStore(t)
	v, id := claudeStoreFixture(t, st)
	st.ClaimClaudeMessage(v)
	p := appendClaudeIncoming(t, v, "exact")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, e := st.ConfirmClaudeMessageContext(ctx, id, p, time.Now()); ok || !errors.Is(e, context.Canceled) {
		t.Fatalf("cancelled proof %v %v", ok, e)
	}
	var n int
	st.db.QueryRow(`SELECT count(*) FROM claude_consumed_records`).Scan(&n)
	m, _ := st.GetMessage(id)
	if m.Status == "delivered" || n != 0 {
		t.Fatalf("cancel consumed %+v count=%d", m, n)
	}
	// Existing wrapper still confirms a live request; later cancellation cannot
	// retroactively deny its committed atomic result.
	if ok, e := st.ConfirmClaudeMessage(id, p, time.Now()); !ok || e != nil {
		t.Fatalf("wrapper %v %v", ok, e)
	}
	cancel()
	m, _ = st.GetMessage(id)
	st.db.QueryRow(`SELECT count(*) FROM claude_consumed_records`).Scan(&n)
	if m.Status != "delivered" || n != 1 {
		t.Fatalf("committed state erased %+v count=%d", m, n)
	}
}
func TestClaudeContextBoundsActualSQLLockWait(t *testing.T) {
	st := openTestStore(t)
	v, id := claudeStoreFixture(t, st)
	st.ClaimClaudeMessage(v)
	p := appendClaudeIncoming(t, v, "exact")
	tx, e := st.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(`UPDATE messages SET status=status WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	ok, e := st.ConfirmClaudeMessageContext(ctx, id, p, time.Now())
	elapsed := time.Since(start)
	tx.Rollback()
	t.Logf("actual SQLite writer-lock deadline latency=%s", elapsed)
	if ok || e == nil || elapsed > 500*time.Millisecond {
		t.Fatalf("lock cancellation responsiveness ok=%v error=%v elapsed=%s", ok, e, elapsed)
	}
	var n int
	st.db.QueryRow(`SELECT count(*) FROM claude_consumed_records`).Scan(&n)
	m, _ := st.GetMessage(id)
	if m.Status == "delivered" || n != 0 {
		t.Fatalf("cancelled lock proof mutated %+v count=%d", m, n)
	}
}

type claudeCountingCancel struct {
	context.Context
	cancel context.CancelFunc
	calls  atomic.Int64
	at     int64
}

func (c *claudeCountingCancel) Err() error {
	if c.calls.Add(1) >= c.at {
		c.cancel()
	}
	return c.Context.Err()
}
func claudeCountingContext(at int64) (*claudeCountingCancel, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	return &claudeCountingCancel{Context: ctx, cancel: cancel, at: at}, cancel
}
func assertClaudeNotConsumed(t *testing.T, st *Store, id int64) {
	t.Helper()
	var n int
	st.db.QueryRow(`SELECT count(*) FROM claude_consumed_records`).Scan(&n)
	m, _ := st.GetMessage(id)
	if m.Status == "delivered" || n != 0 {
		t.Fatalf("cancellation consumed %+v count=%d", m, n)
	}
}
func TestClaudeContextSharedBudgetCancelsDuringAtomicReread(t *testing.T) {
	st := openTestStore(t)
	v, id := claudeStoreFixture(t, st)
	st.ClaimClaudeMessage(v)
	f, e := os.OpenFile(v.Boundary.Path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 200; i++ {
		if _, e = f.WriteString("{\"type\":\"progress\"}\n"); e != nil {
			t.Fatal(e)
		}
	}
	f.Close()
	p := appendClaudeIncoming(t, v, "exact")
	// One cancellation budget spans the initial scan and the atomic re-read.
	// A Background reset in the Store would incorrectly let the latter commit.
	ctx, cancel := claudeCountingContext(350)
	defer cancel()
	initial, found, e := claudeinstance.FindProof(ctx, v)
	if !found || e != nil || initial != p {
		t.Fatalf("first scan %+v %v %v", initial, found, e)
	}
	before := ctx.calls.Load()
	ok, e := st.ConfirmClaudeMessageContext(ctx, id, p, time.Now())
	t.Logf("shared budget calls before atomic re-read=%d after=%d", before, ctx.calls.Load())
	if ok || !errors.Is(e, context.Canceled) || ctx.calls.Load() < 350 {
		t.Fatalf("shared cancellation %v %v", ok, e)
	}
	assertClaudeNotConsumed(t, st, id)
}
func TestClaudeContextCancelsLargeHistoricalValidation(t *testing.T) {
	st := openTestStore(t)
	v, id := claudeStoreFixture(t, st)
	st.ClaimClaudeMessage(v)
	p := appendClaudeIncoming(t, v, "exact")
	tx, e := st.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 300; i++ {
		// Build valid scalar/snapshot history in one fixture transaction. A broken
		// history fixture would fail before cancellation and cannot satisfy test.
		res, e := tx.Exec(`INSERT INTO messages(to_session,body,status,created_at,delivery_transport) VALUES('s','history','failed',?,'claude-socket-v1')`, time.Now().Unix())
		if e != nil {
			t.Fatal(e)
		}
		history := v
		history.MessageID, _ = res.LastInsertId()
		history.EnvelopeVersion = 0
		history.WireID = fmt.Sprintf("history-%d", i)
		history.Envelope = fmt.Sprintf("history envelope %d", i)
		raw, _ := json.Marshal(history)
		_, e = tx.Exec(`INSERT INTO claude_deliveries(message_id,transport,wire_id,snapshot_json,claude_uuid,journal_device,journal_inode,journal_path,boundary_offset,envelope_hash,phase,deadline_ns) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, history.MessageID, history.Transport, history.WireID, string(raw), history.Target.ClaudeSessionID, history.Boundary.Device, history.Boundary.Inode, history.Boundary.Path, history.Boundary.Offset, envelopeHash(history.Envelope), "not_sent", history.Deadline.UnixNano())
		if e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	read, e := st.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	history, e := claudeHistory(read)
	read.Rollback()
	if e != nil || len(history) != 301 {
		t.Fatalf("history fixture invalid %d %v", len(history), e)
	}
	ctx, cancel := claudeCountingContext(100)
	defer cancel()
	start := time.Now()
	ok, e := st.ConfirmClaudeMessageContext(ctx, id, p, time.Now())
	elapsed := time.Since(start)
	t.Logf("history cancellation latency=%s calls=%d", elapsed, ctx.calls.Load())
	if ok || !errors.Is(e, context.Canceled) || elapsed > 500*time.Millisecond {
		t.Fatalf("history cancellation %v %v elapsed=%s", ok, e, elapsed)
	}
	assertClaudeNotConsumed(t, st, id)
}

func TestClaudeProofRestoresOriginalBusyTimeoutBeforePoolReuse(t *testing.T) {
	st := openTestStore(t)
	st.db.SetMaxOpenConns(1)
	v, id := claudeStoreFixture(t, st)
	st.ClaimClaudeMessage(v)
	p := appendClaudeIncoming(t, v, "exact")
	if _, e := st.db.Exec(`CREATE TEMP TABLE claude_connection_sentinel(value INTEGER)`); e != nil {
		t.Fatal(e)
	}
	st.db.Exec(`INSERT INTO claude_connection_sentinel VALUES(1)`)
	if _, e := st.db.Exec(`PRAGMA busy_timeout=4321`); e != nil {
		t.Fatal(e)
	}
	if ok, e := st.ConfirmClaudeMessageContext(context.Background(), id, p, time.Now()); !ok || e != nil {
		t.Fatalf("proof %v %v", ok, e)
	}
	var timeout, sentinel int
	if e := st.db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); e != nil || timeout != 4321 {
		t.Fatalf("pooled timeout=%d error=%v", timeout, e)
	}
	if e := st.db.QueryRow(`SELECT value FROM claude_connection_sentinel`).Scan(&sentinel); e != nil || sentinel != 1 {
		t.Fatalf("connection was not reused: %d %v", sentinel, e)
	}
}

type claudeFailedRestore struct {
	discarded bool
	query     string
}

func (c *claudeFailedRestore) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	c.query = query
	return nil, errors.New("fixture restore failed")
}
func (c *claudeFailedRestore) Raw(fn func(any) error) error {
	e := fn(nil)
	c.discarded = errors.Is(e, driver.ErrBadConn)
	return e
}
func TestClaudeProofDiscardsConnectionWhenTimeoutRestorationFails(t *testing.T) {
	c := &claudeFailedRestore{}
	restoreClaudeProofConnection(c, 4321)
	if !c.discarded || c.query != "PRAGMA busy_timeout=4321" {
		t.Fatalf("unsafe restoration policy %+v", c)
	}
}

func TestClaudeHousekeepingExcludesAnySideEvenConflictingMarker(t *testing.T) {
	for _, marker := range []string{"", "pi-uds-v1", "future-v9"} {
		t.Run(marker, func(t *testing.T) {
			st := openTestStore(t)
			v, id := claudeStoreFixture(t, st)
			st.ClaimClaudeMessage(v)
			st.db.Exec(`UPDATE messages SET status='queued',delivery_transport=? WHERE id=?`, marker, id)
			winners, e := st.ExpireQueuedBefore(time.Now().Add(time.Hour).Unix())
			if e != nil || len(winners) != 0 {
				t.Fatalf("bulk expiry stole Claude owner: %+v %v", winners, e)
			}

			got, _ := st.GetMessage(id)
			if got.Status != "queued" {
				t.Fatalf("conflicting immutable ownership reassigned: %+v", got)
			}
		})
	}
}

func TestConflictingClaudeSideRowCannotExposeNativeOrVisualRoute(t *testing.T) {
	st := openTestStore(t)
	v, id := claudeStoreFixture(t, st)
	st.ClaimClaudeMessage(v)
	st.db.Exec(`UPDATE messages SET delivery_transport='pi-uds-v1' WHERE id=?`, id)
	m, e := st.GetMessage(id)
	if e != nil {
		t.Fatal(e)
	}
	if m.DeliveryTransport == "pi-uds-v1" || m.DeliveryTransport == "" || m.DeliveryTransport == "claude-socket-v1" {
		t.Fatalf("conflicting immutable data assigned a supported route: %+v", m)
	}
}

func TestGenericExpiryRollbackOrCommitFailureReturnsNoWinners(t *testing.T) {
	for _, commitFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(commitFailure), func(t *testing.T) {
			st := openTestStore(t)
			claudeStoreFixture(t, st)
			a, _ := st.AddMessage(Message{ToSession: "s", Body: "a"})
			b, _ := st.AddMessage(Message{ToSession: "s", Body: "b"})
			if commitFailure {
				if _, e := st.db.Exec(`CREATE TABLE expiry_commit_guard(parent INTEGER REFERENCES messages(id) DEFERRABLE INITIALLY DEFERRED)`); e != nil {
					t.Fatal(e)
				}
				if _, e := st.db.Exec(`CREATE TRIGGER fail_expiry_commit AFTER UPDATE ON messages WHEN NEW.status='failed' BEGIN INSERT INTO expiry_commit_guard VALUES(-999); END`); e != nil {
					t.Fatal(e)
				}
			} else if _, e := st.db.Exec(fmt.Sprintf(`CREATE TRIGGER fail_second_expiry BEFORE UPDATE ON messages WHEN NEW.id=%d AND NEW.status='failed' BEGIN SELECT RAISE(ABORT,'fixture expiry failure'); END`, b)); e != nil {
				t.Fatal(e)
			}
			winners, e := st.ExpireQueuedBefore(time.Now().Add(time.Hour).Unix())
			if e == nil || len(winners) != 0 {
				t.Fatalf("uncommitted winners=%+v error=%v", winners, e)
			}
			for _, id := range []int64{a, b} {
				m, _ := st.GetMessage(id)
				if m.Status != "queued" {
					t.Fatalf("partial expiry committed %+v", m)
				}
			}
		})
	}
}

func TestClaudeDamagedConflictingSideRetainsOwnershipDuringPurge(t *testing.T) {
	st := openTestStore(t)
	v, id := claudeStoreFixture(t, st)
	st.ClaimClaudeMessage(v)
	st.db.Exec(`UPDATE messages SET delivery_transport='future-v9',status='failed',reason='unsupported_delivery_transport' WHERE id=?`, id)
	st.db.Exec(`UPDATE claude_deliveries SET snapshot_json='{' WHERE message_id=?`, id)
	if e := st.PurgeOld(time.Now().Add(time.Hour).Unix()); e != nil {
		t.Fatal(e)
	}
	if _, e := st.GetMessage(id); e != nil {
		t.Fatalf("corrupt side with conflicting marker lost ownership: %v", e)
	}
}

func TestClaudeAtomicProofLargeRecordsAndLaterDuplicate(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprint(duplicate), func(t *testing.T) {
			st := openTestStore(t)
			v, id := claudeStoreFixture(t, st)
			v.Envelope, _ = socketmsg.EnvelopeWithID(v.WireID, "", "", strings.Repeat("<>&\n", 20000)) // JSON escaping enlarges a legal envelope.
			if ok, e := st.ClaimClaudeMessage(v); !ok || e != nil {
				t.Fatalf("claim %v %v", ok, e)
			}
			raw, e := json.Marshal(map[string]any{"type": "assistant", "sessionId": v.Target.ClaudeSessionID, "message": map[string]string{"role": "assistant", "content": strings.Repeat("x", 80*1024)}})
			if e != nil {
				t.Fatal(e)
			}
			f, e := os.OpenFile(v.Boundary.Path, os.O_APPEND|os.O_WRONLY, 0600)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.Write(append(raw, '\n')); e != nil {
				f.Close()
				t.Fatal(e)
			}
			if e = f.Close(); e != nil {
				t.Fatal(e)
			}
			proof := appendClaudeIncoming(t, v, "large-exact")
			if duplicate {
				appendClaudeIncoming(t, v, "large-later-duplicate")
			}
			ok, e := st.ConfirmClaudeMessageContext(context.Background(), id, proof, time.Now())
			if e != nil || ok == duplicate {
				t.Fatalf("atomic large proof confirmed=%v duplicate=%v error=%v", ok, duplicate, e)
			}
			var consumed int
			if e = st.db.QueryRow(`SELECT count(*) FROM claude_consumed_records WHERE message_id=?`, id).Scan(&consumed); e != nil {
				t.Fatal(e)
			}
			m, e := st.GetMessage(id)
			if e != nil {
				t.Fatal(e)
			}
			if duplicate && (consumed != 0 || m.Status != "delivering") {
				t.Fatalf("duplicate consumed=%d status=%s", consumed, m.Status)
			}
			if !duplicate && (consumed != 1 || m.Status != "delivered") {
				t.Fatalf("unique consumed=%d status=%s", consumed, m.Status)
			}
		})
	}
}

// Seed pre-version1 persisted history directly; new production claims never accept v0.
func claimLegacyClaudeFixture(t *testing.T, st *Store, v claudeinstance.Snapshot) (bool, error) {
	t.Helper()
	v.EnvelopeVersion = 0
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := st.db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE messages SET status='delivering',attempts=attempts+1,delivery_transport='claude-socket-v1' WHERE id=?`, v.MessageID); e != nil {
		t.Fatal(e)
	}
	ns, _ := claudeDeadlineNS(v.Deadline)
	_, e = tx.Exec(`INSERT INTO claude_deliveries(message_id,transport,wire_id,snapshot_json,claude_uuid,journal_device,journal_inode,journal_path,boundary_offset,envelope_hash,phase,deadline_ns) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, v.MessageID, v.Transport, v.WireID, string(raw), v.Target.ClaudeSessionID, v.Boundary.Device, v.Boundary.Inode, v.Boundary.Path, v.Boundary.Offset, envelopeHash(v.Envelope), "possible_write", ns)
	if e != nil {
		t.Fatal(e)
	}
	return true, tx.Commit()
}
