package store

import (
	"testing"
	"time"
)

func TestClaudeHookOrderingAndGenerationAtomicActivity(t *testing.T) {
	st := openTestStore(t)
	v, _ := claudeStoreFixture(t, st)
	now := time.Now()
	at := now.UnixNano() - 2
	if ok, e := st.AcceptClaudeHook("s", "g", at, "active", now.Unix()); !ok || e != nil {
		t.Fatalf("valid event %v %v", ok, e)
	}
	if ok, _ := st.AcceptClaudeHook("s", "g", at, "ready", now.Unix()); ok {
		t.Fatal("equal event accepted")
	}
	if ok, e := st.AcceptClaudeHook("s", "g", at+1, "ready", now.Unix()); !ok || e != nil {
		t.Fatalf("1ns event lost %v %v", ok, e)
	}
	if ok, _ := st.AcceptClaudeHook("s", "g", time.Now().Add(time.Hour).UnixNano(), "ready", now.Unix()); ok {
		t.Fatal("future poisoned high water")
	}
	if ok, e := st.AcceptClaudeHook("s", "g", at+2, "active", now.Unix()); !ok || e != nil {
		t.Fatalf("valid after future %v %v", ok, e)
	}
	l, _, _ := st.GetClaudeLaunch("s")
	l.Generation = "new"

	l.StartedAt = l.StartedAt.Add(time.Nanosecond)
	st.PutClaudeLaunch(l)
	if ok, _ := st.AcceptClaudeHook("s", v.Target.LaunchGeneration, at+3, "exited", now.Unix()); ok {
		t.Fatal("old generation event applied")
	}
	if ok, _ := st.ApplyClaudeActivity("s", "g", "exited", now.Unix()); ok {
		t.Fatal("poll from old generation applied")
	}
	sess, _ := st.GetSession("s")
	if sess.Activity != "" {
		t.Fatalf("new generation inherited old activity %s", sess.Activity)
	}
}

func TestClaudeRestoreRacingValidatedEventNeverAppliesToNewGeneration(t *testing.T) {
	for i := 0; i < 10; i++ {
		st := openTestStore(t)
		v, _ := claudeStoreFixture(t, st)
		l, _, _ := st.GetClaudeLaunch("s")
		l.Generation = "replacement"

		l.StartedAt = l.StartedAt.Add(time.Nanosecond)
		start := make(chan struct{})
		done := make(chan error, 2)
		go func() {
			<-start
			_, e := st.AcceptClaudeHook("s", v.Target.LaunchGeneration, time.Now().UnixNano(), "ready", time.Now().Unix())
			done <- e
		}()
		go func() { <-start; done <- st.PutClaudeLaunch(l) }()
		close(start)
		for n := 0; n < 2; n++ {
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		}
		sess, _ := st.GetSession("s")
		if sess.Activity != "" {
			t.Fatalf("old validated event reached new generation: %+v", sess)
		}
	}
}
