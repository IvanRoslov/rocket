package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeBindingCaptureRacesHook(t *testing.T) {
	st := openTestStore(t)
	v, _ := claudeStoreFixture(t, st)
	ctx := context.Background()
	l, a, observed, e := st.GetClaudeBinding(ctx, "s")
	if e != nil {
		t.Fatal(e)
	}
	b := *a
	b.ClaudeSessionID = "239baa3b-27eb-40ed-82e1-1d9f3dde01e5"
	b.JournalPath = filepath.Join(filepath.Dir(a.JournalPath), b.ClaudeSessionID+".jsonl")
	at := time.Now().UnixNano()
	if ok, e := st.AcceptClaudeHookForBinding(ctx, *a, l.BindingRevision, at, "waiting_input", time.Now().Unix()); !ok || e != nil {
		t.Fatalf("hook %v %v", ok, e)
	}
	if ok, e := st.AdvanceClaudeBinding(ctx, *a, b, l.BindingRevision, observed, time.Now().UnixNano()); ok || e != nil {
		t.Fatalf("racing hook lost %v %v", ok, e)
	}
	got, _ := st.GetSession("s")
	if got.Activity != "waiting_input" {
		t.Fatal(got.Activity)
	}
	if ok, e := st.AdvanceClaudeBinding(ctx, *a, b, l.BindingRevision, at, time.Now().UnixNano()); !ok || e != nil {
		t.Fatalf("fresh advance %v %v", ok, e)
	}
	n, _, barrier, _ := st.GetClaudeBinding(ctx, "s")
	if n.BindingRevision != l.BindingRevision+1 || barrier <= at {
		t.Fatal(n, barrier)
	}
	if ok, _ := st.ApplyClaudeActivityForBinding(ctx, v.Target, l.BindingRevision, at, "ready", time.Now().Unix()); ok {
		t.Fatal("old A ready cleared B")
	}
	if ok, e := st.ApplyClaudeActivityForBinding(ctx, b, n.BindingRevision, barrier, "blocked", time.Now().Add(-time.Minute).Unix()); !ok || e != nil {
		t.Fatalf("resumed older block %v %v", ok, e)
	}
	if ok, _ := st.AcceptClaudeHookForBinding(ctx, *a, l.BindingRevision, time.Now().UnixNano(), "ready", time.Now().Unix()); ok {
		t.Fatal("stale A hook applied")
	}
	got, _ = st.GetSession("s")
	if got.Activity != "blocked" {
		t.Fatal(got.Activity)
	}
}

func TestClaudeBindingRejectsStaleActivityAndCapture(t *testing.T) {
	st := openTestStore(t)
	v, _ := claudeStoreFixture(t, st)
	ctx := context.Background()
	l, _, at, _ := st.GetClaudeBinding(ctx, "s")
	if ok, _ := st.AcceptClaudeHookForBinding(ctx, v.Target, l.BindingRevision, time.Now().UnixNano(), "waiting_input", time.Now().Unix()); !ok {
		t.Fatal("hook")
	}
	if ok, _ := st.ApplyClaudeActivityForBinding(ctx, v.Target, l.BindingRevision, at, "ready", time.Now().Unix()); ok {
		t.Fatal("pre-notification poll cleared wait")
	}
	l.BindingRevision = ^uint64(0)
	raw := `{"RocketSessionID":"s","Generation":"g","BindingRevision":18446744073709551615}`
	st.db.Exec(`UPDATE claude_launches SET launch_json=? WHERE session_id='s'`, raw)
	_, _, at, _ = st.GetClaudeBinding(ctx, "s")
	b := v.Target
	b.ClaudeSessionID = "239baa3b-27eb-40ed-82e1-1d9f3dde01e5"
	b.JournalPath = filepath.Join(filepath.Dir(b.JournalPath), b.ClaudeSessionID+".jsonl")
	if ok, _ := st.AdvanceClaudeBinding(ctx, v.Target, b, l.BindingRevision, at, time.Now().UnixNano()); ok {
		t.Fatal("revision overflow")
	}
}

func TestClaudeStalePaneCompletionCannotRestoreGeneration(t *testing.T) {
	st := openTestStore(t)
	_, _ = claudeStoreFixture(t, st)
	stale, _, _ := st.GetClaudeLaunch("s")
	next := stale
	next.Generation = "replacement"
	next.StartedAt = next.StartedAt.Add(time.Nanosecond)
	next.PaneIdentity = ""
	next.PanePID = 0
	next.PaneStart = time.Time{}
	if e := st.PutClaudeLaunch(next); e != nil {
		t.Fatal(e)
	}
	_ = st.PutClaudeLaunch(stale)
	got, _, e := st.GetClaudeLaunch("s")
	if e != nil || got.Generation != "replacement" {
		t.Fatalf("stale capture rolled back generation: %+v %v", got, e)
	}
}
