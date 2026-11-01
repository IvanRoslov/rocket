package claudeinstance

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestQualifiedHookMetadata(t *testing.T) {
	l, r, p := fixture()
	id := Resolve(context.Background(), l, nil, r, p, "/tmp/p").Identity
	now := time.Now()
	p.processes[12] = Process{PID: 12, PPID: 11, Start: now, Name: "sh"}
	e := HookEvent{ClaudeSessionID: id.ClaudeSessionID, HookEventName: "SessionEnd", LaunchGeneration: l.Generation, ProducerPID: 12, ProducerStart: now, At: now.UnixNano()}
	if err := ValidateHook(context.Background(), id, e, "exited", 0, now, p); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*HookEvent, *fakeProbe)
		state  string
	}{
		{"foreign UUID", func(e *HookEvent, p *fakeProbe) { e.ClaudeSessionID = "foreign" }, "exited"},
		{"old generation", func(e *HookEvent, p *fakeProbe) { e.LaunchGeneration = "old" }, "exited"},
		{"nested agent", func(e *HookEvent, p *fakeProbe) { e.AgentID = "nested" }, "exited"},
		{"nested process", func(e *HookEvent, p *fakeProbe) {
			p.processes[12] = Process{PID: 12, PPID: 11, Start: now, Name: "claude"}
		}, "exited"},
		{"future", func(e *HookEvent, p *fakeProbe) { e.At = now.Add(time.Minute).UnixNano() }, "exited"},
		{"replay", func(e *HookEvent, p *fakeProbe) { e.At = 1 }, "exited"},
		{"event mismatch", func(e *HookEvent, p *fakeProbe) {}, "ready"},
		{"PID reused", func(e *HookEvent, p *fakeProbe) { e.ProducerStart = now.Add(-time.Second) }, "exited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, p := fixture()
			p.processes[12] = Process{PID: 12, PPID: 11, Start: now, Name: "sh"}
			v := e
			tc.mutate(&v, &p)
			if err := ValidateHook(context.Background(), id, v, tc.state, 1, now, p); err == nil {
				t.Fatal("unqualified hook accepted")
			}
		})
	}
}
func TestBoundedHookParser(t *testing.T) {
	if _, e := ParseHook(strings.NewReader(`{"session_id":"uuid","hook_event_name":"Stop","prompt":"SECRET_PROMPT","tool_input":{"secret":"SECRET_TOOL"}}`)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{"", "{", strings.Repeat(" ", MaxHookBytes+1), `{} {}`} {
		if _, e := ParseHook(strings.NewReader(raw)); e == nil {
			t.Fatal("malformed/oversize accepted")
		}
	}
}

func TestHookNanosecondsAndZeroFutureSkew(t *testing.T) {
	l, r, p := fixture()
	id := Resolve(context.Background(), l, nil, r, p, "/tmp/p").Identity
	now := time.Now()
	p.processes[12] = Process{PID: 12, PPID: 11, Start: now.Add(-time.Second), Name: "sh"}
	e := HookEvent{ClaudeSessionID: id.ClaudeSessionID, HookEventName: "Stop", LaunchGeneration: l.Generation, ProducerPID: 12, ProducerStart: now.Add(-time.Second), At: now.UnixNano()}
	if err := ValidateHook(context.Background(), id, e, "ready", e.At-1, now, p); err != nil {
		t.Fatalf("1ns must remain distinct: %v", err)
	}
	if err := ValidateHook(context.Background(), id, e, "ready", e.At, now, p); err == nil {
		t.Fatal("equal ordering accepted")
	}
	e.At = now.Add(time.Nanosecond).UnixNano()
	if err := ValidateHook(context.Background(), id, e, "ready", 0, now, p); err == nil {
		t.Fatal("positive future skew accepted")
	}
}
