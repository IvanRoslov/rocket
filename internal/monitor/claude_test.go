package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/agent/claudecode"
	"github.com/IvanRoslov/rocket/internal/agent/deepseek"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"github.com/IvanRoslov/rocket/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type claudeMonAgent struct{ *fakeAgent }

func (claudeMonAgent) ClaudeSocketCompatible() bool { return true }

type claudeMonProbe struct {
	processes map[int]claudeinstance.Process
	pane      claudeinstance.Pane
	err       error
}

func (p *claudeMonProbe) Process(_ context.Context, pid int) (claudeinstance.Process, bool, error) {
	v, ok := p.processes[pid]
	return v, ok, p.err
}
func (p *claudeMonProbe) Pane(context.Context, string) (claudeinstance.Pane, bool, error) {
	return p.pane, true, p.err
}
func claudeMonitorFixture(t *testing.T) (*Monitor, *store.Store, *claudeMonProbe, claudeinstance.HookEvent) {
	t.Helper()
	rt := &fakeRuntime{names: []string{"s"}}
	m, st, _ := testMonitor(t, rt, &fakeProber{}, nil)
	a := claudeMonAgent{&fakeAgent{state: activity.Active, ts: time.Now()}}
	m.resolveAgent = func(string) (agent.Agent, error) { return a, nil }
	seedSession(t, st, store.Session{ID: "s", Agent: "claude", TmuxName: "s", Activity: "active"})
	at := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	l := claudeinstance.Launch{RocketSessionID: "s", Generation: "g", PaneIdentity: "s:@1.%1", PanePID: 10, PaneStart: at, StartedAt: at}
	st.PutClaudeLaunch(l)
	p := &claudeMonProbe{pane: claudeinstance.Pane{Identity: l.PaneIdentity, PID: 10, Start: at}, processes: map[int]claudeinstance.Process{10: {PID: 10, Start: at, Name: "sh"}, 11: {PID: 11, PPID: 10, Start: at, Name: "claude"}, 12: {PID: 12, PPID: 11, Start: at, Name: "sh"}}}
	m.claudeProbe = p
	m.claudeSessionsDir = t.TempDir()
	cc := socketmsg.Session{PID: 11, SessionID: "329baa3b-27eb-40ed-82e1-1d9f3dde01e5", CWD: t.TempDir(), Tmux: l.PaneIdentity, Kind: "interactive", PeerProtocol: 1, ProcStart: at.Format(time.RFC3339), MessagingSocketPath: "/tmp/fixture.sock"}
	raw, _ := json.Marshal(cc)
	os.WriteFile(filepath.Join(m.claudeSessionsDir, "11.json"), raw, 0600)
	identity := claudeinstance.Resolve(context.Background(), l, nil, []socketmsg.Session{cc}, p, filepath.Join(filepath.Dir(m.claudeSessionsDir), "projects")).Identity
	st.BindClaudeIdentity(identity)
	os.MkdirAll(filepath.Dir(identity.JournalPath), 0700)
	os.WriteFile(identity.JournalPath, []byte(`{"sessionId":"`+identity.ClaudeSessionID+`","type":"assistant","timestamp":"`+time.Now().Format(time.RFC3339Nano)+`"}`+"\n"), 0600)
	st.UpdateSessionActivity("s", "active", at.Unix())
	return m, st, p, claudeinstance.HookEvent{ClaudeSessionID: cc.SessionID, HookEventName: "SessionEnd", LaunchGeneration: "g", ProducerPID: 12, ProducerStart: at, At: time.Now().UnixNano() - 1}
}
func TestClaudeUnqualifiedExitAndPersistedCacheCannotKillLiveRoot(t *testing.T) {
	m, st, _, _ := claudeMonitorFixture(t)
	m.PushUpdate("s", activity.Exited, time.Now())
	got, _ := st.GetSession("s")
	if got.Activity == "exited" {
		t.Fatal("unqualified push killed root")
	}
	st.UpdateSessionActivity("s", "exited", time.Now().Unix())
	m.cache["s"] = activity.Exited
	sess, _ := st.GetSession("s")
	m.pollSession(context.Background(), sess, map[string]bool{"s": true}, true)
	state, known := m.Activity("s")
	if !known || state != activity.Active {
		t.Fatalf("old exited/cache survived independent active poll: %s %v", state, known)
	}
}
func TestQualifiedSessionEndClearDoesNotDeclareOSDeath(t *testing.T) {
	m, st, p, event := claudeMonitorFixture(t)
	event.Reason = "clear"
	m.PushClaudeUpdate(context.Background(), "s", activity.Exited, event)
	got, _ := st.GetSession("s")
	if got.Activity != "active" {
		t.Fatalf("logical clear killed live root: %+v", got)
	}
	delete(p.processes, 11)
	m.pollSession(context.Background(), got, map[string]bool{"s": true}, true)
	got, _ = st.GetSession("s")
	if got.Activity != "exited" {
		t.Fatalf("real root death ignored: %+v", got)
	}
}
func TestQualifiedClaudePushUsesDurableOrdering(t *testing.T) {
	m, st, _, event := claudeMonitorFixture(t)
	event.HookEventName = "Stop"
	m.PushClaudeUpdate(context.Background(), "s", activity.Ready, event)
	got, _ := st.GetSession("s")
	if got.Activity != "ready" {
		t.Fatalf("qualified event ignored: %+v", got)
	}
	event.HookEventName = "PreToolUse"
	m.PushClaudeUpdate(context.Background(), "s", activity.Active, event)
	got, _ = st.GetSession("s")
	if got.Activity != "ready" {
		t.Fatal("equal replay applied")
	}
}

func TestClaudePollCannotUseForeignNewestActivityToBypassBlocked(t *testing.T) {
	m, st, _, _ := claudeMonitorFixture(t)
	_, identity, _ := st.GetClaudeLaunch("s")
	os.MkdirAll(filepath.Dir(identity.JournalPath), 0700)
	raw := `{"sessionId":"` + identity.ClaudeSessionID + `","type":"assistant","isApiErrorMessage":true,"timestamp":"` + time.Now().Add(-time.Minute).Format(time.RFC3339Nano) + `"}` + "\n"
	os.WriteFile(identity.JournalPath, []byte(raw), 0600)
	old := time.Now().Add(-time.Minute)
	os.Chtimes(identity.JournalPath, old, old)
	// The generic adapter source claims a newer ready record from another UUID.
	a := claudeMonAgent{&fakeAgent{state: activity.Ready, ts: time.Now()}}
	m.resolveAgent = func(string) (agent.Agent, error) { return a, nil }
	sess, _ := st.GetSession("s")
	m.pollSession(context.Background(), sess, map[string]bool{"s": true}, true)
	got, _ := st.GetSession("s")
	if got.Activity != "blocked" {
		t.Fatalf("foreign newest bypassed blocked root: %+v", got)
	}
}

func TestClaudeExactJournalUnknownNeverForcesReadyOrExit(t *testing.T) {
	for _, which := range []string{"missing", "corrupt", "wrong UUID", "future timestamp", "probe error"} {
		t.Run(which, func(t *testing.T) {
			m, st, p, _ := claudeMonitorFixture(t)
			_, id, _ := st.GetClaudeLaunch("s")
			switch which {
			case "missing":
				os.Remove(id.JournalPath)
			case "corrupt":
				os.WriteFile(id.JournalPath, []byte("{broken\n"), 0600)
			case "wrong UUID":
				os.WriteFile(id.JournalPath, []byte(`{"sessionId":"foreign","type":"assistant"}`+"\n"), 0600)
			case "future timestamp":
				os.WriteFile(id.JournalPath, []byte(`{"sessionId":"`+id.ClaudeSessionID+`","type":"assistant","timestamp":"`+time.Now().Add(time.Hour).Format(time.RFC3339Nano)+`"}`+"\n"), 0600)
			case "probe error":
				p.err = errors.New("process lookup failed")
			}
			sess, _ := st.GetSession("s")
			m.pollSession(context.Background(), sess, map[string]bool{"s": true}, true)
			got, _ := st.GetSession("s")
			if got.Activity != "" {
				t.Fatalf("uncertainty became authority: %+v", got)
			}
		})
	}
}
func TestForeignBlockedCannotOverrideExactLiveActivity(t *testing.T) {
	for _, want := range []activity.State{activity.Active, activity.Ready} {
		t.Run(string(want), func(t *testing.T) {
			m, st, _, _ := claudeMonitorFixture(t)
			_, id, _ := st.GetClaudeLaunch("s")
			ts := time.Now()
			if want == activity.Ready {
				ts = ts.Add(-time.Minute)
			}
			raw := `{"sessionId":"` + id.ClaudeSessionID + `","type":"assistant","timestamp":"` + ts.Format(time.RFC3339Nano) + `"}` + "\n"
			os.WriteFile(id.JournalPath, []byte(raw), 0600)
			os.Chtimes(id.JournalPath, ts, ts)
			a := claudeMonAgent{&fakeAgent{state: activity.Blocked, ts: time.Now()}}
			m.resolveAgent = func(string) (agent.Agent, error) { return a, nil }
			sess, _ := st.GetSession("s")
			m.pollSession(context.Background(), sess, map[string]bool{"s": true}, true)
			got, _ := st.GetSession("s")
			if got.Activity != string(want) {
				t.Fatalf("foreign blocked overrode root: %+v", got)
			}
		})
	}
}

func TestClaudeRealAdaptersHaveNoGenericFailureOrStartupConsumer(t *testing.T) {
	for _, a := range []agent.Agent{claudecode.New(), deepseek.New()} {
		if _, ok := a.(agent.TurnFailureReporter); ok {
			t.Fatalf("%s now needs exact-journal failure integration", a.Name())
		}
		if _, ok := a.(agent.StartupProber); ok {
			t.Fatalf("%s now needs exact-journal startup integration", a.Name())
		}
	}
}

func TestClaudeQualifiedWaitRetainsPromptQuizAndUncertainty(t *testing.T) {
	for _, which := range []string{"prompt", "quiz", "capture error", "two misses"} {
		t.Run(which, func(t *testing.T) {
			m, st, _, event := claudeMonitorFixture(t)
			rt := &quizFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"s"}}, captureOut: plainComposerTail}
			m.rt = rt
			if which == "prompt" {
				rt.captureOut = permissionPromptTail
			}
			if which == "capture error" {
				rt.captureErr = errors.New("unavailable")
			}
			event.HookEventName = "Notification"
			m.PushClaudeUpdate(context.Background(), "s", activity.WaitingInput, event)
			if which == "quiz" {
				st.SetPendingQuiz("s", "pending")
			}
			for pass := 0; pass < 2; pass++ {
				sess, _ := st.GetSession("s")
				m.pollSession(context.Background(), sess, map[string]bool{"s": true}, true)
				got, _ := st.GetSession("s")
				want := "waiting_input"
				if which == "two misses" && pass == 1 {
					want = "active"
				}
				if got.Activity != want {
					t.Fatalf("pass %d activity=%s want=%s", pass, got.Activity, want)
				}
			}
		})
	}
}

type claudeCaptureCallback struct {
	*quizFakeRuntime
	capture func()
}

func (r *claudeCaptureCallback) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	if r.capture != nil {
		r.capture()
	}
	return r.quizFakeRuntime.Capture(ctx, h, lines)
}
func TestClaudeWaitMissesCannotCrossGenerationOrApplyStaleCapture(t *testing.T) {
	for _, race := range []bool{false, true} {
		t.Run(fmt.Sprint(race), func(t *testing.T) {
			m, st, _, event := claudeMonitorFixture(t)
			rt := &claudeCaptureCallback{quizFakeRuntime: &quizFakeRuntime{captureOut: plainComposerTail}}
			m.rt = rt
			event.HookEventName = "Notification"
			m.PushClaudeUpdate(context.Background(), "s", activity.WaitingInput, event)
			poll := func() {
				sess, _ := st.GetSession("s")
				m.pollSession(context.Background(), sess, map[string]bool{"s": true}, true)
			}
			poll() // one G1 miss
			l, id, _ := st.GetClaudeLaunch("s")
			replace := func() {
				l.Generation = "g2"

				l.StartedAt = l.StartedAt.Add(time.Nanosecond)
				if e := st.PutClaudeLaunch(l); e != nil {
					t.Fatal(e)
				}
				id.LaunchGeneration = "g2"
				if ok, e := st.BindClaudeIdentity(*id); !ok || e != nil {
					t.Fatalf("bind %v %v", ok, e)
				}
				if ok, e := st.AcceptClaudeHook("s", "g2", time.Now().UnixNano(), "waiting_input", time.Now().Unix()); !ok || e != nil {
					t.Fatalf("wait %v %v", ok, e)
				}
			}
			if race {
				rt.capture = func() { rt.capture = nil; replace() }
				poll()
			} else {
				replace()
			}
			poll() // first G2 miss must not combine with G1
			got, _ := st.GetSession("s")
			if got.Activity != "waiting_input" {
				t.Fatalf("G1 capture/miss cleared G2: %+v", got)
			}
			poll() // second G2 miss permits stale wait correction
			got, _ = st.GetSession("s")
			if got.Activity != "active" {
				t.Fatalf("G2 misses failed to clear stale wait: %+v", got)
			}
		})
	}
}

func TestClaudeOlderExactReadyCanReplaceLegacyExitHint(t *testing.T) {
	m, st, _, _ := claudeMonitorFixture(t)
	_, id, _ := st.GetClaudeLaunch("s")
	raw := `{"sessionId":"` + id.ClaudeSessionID + `","type":"assistant","timestamp":"` + time.Now().Add(-time.Minute).Format(time.RFC3339Nano) + `"}` + "\n"
	os.WriteFile(id.JournalPath, []byte(raw), 0600)
	st.UpdateSessionActivity("s", "exited", time.Now().Unix())
	m.cache["s"] = activity.Exited
	sess, _ := st.GetSession("s")
	m.pollSession(context.Background(), sess, map[string]bool{"s": true}, true)
	got, _ := st.GetSession("s")
	if got.Activity != "ready" && got.Activity != "idle" {
		t.Fatalf("unqualified newer exit blocked exact ready: %+v", got)
	}
}
func TestClaudeQualifiedStopCannotClearExactAPIErrorBlock(t *testing.T) {
	m, st, _, event := claudeMonitorFixture(t)
	_, id, _ := st.GetClaudeLaunch("s")
	raw := `{"sessionId":"` + id.ClaudeSessionID + `","type":"assistant","isApiErrorMessage":true,"timestamp":"` + time.Now().Add(-time.Minute).Format(time.RFC3339Nano) + `"}` + "\n"
	os.WriteFile(id.JournalPath, []byte(raw), 0600)
	st.UpdateSessionActivity("s", "blocked", time.Now().Add(-time.Minute).Unix())
	event.HookEventName = "Stop"
	m.PushClaudeUpdate(context.Background(), "s", activity.Ready, event)
	got, _ := st.GetSession("s")
	if got.Activity != "blocked" {
		t.Fatalf("API-error Stop bypassed genuine blocked: %+v", got)
	}
}

func TestClaudeBindingCaptureRacesNotification(t *testing.T) {
	m, st, _, event := claudeMonitorFixture(t)
	rt := &claudeCaptureCallback{quizFakeRuntime: &quizFakeRuntime{captureOut: plainComposerTail}}
	m.rt = rt
	event.HookEventName = "Notification"
	m.PushClaudeUpdate(context.Background(), "s", activity.WaitingInput, event)
	poll := func() {
		sess, _ := st.GetSession("s")
		m.pollSession(context.Background(), sess, map[string]bool{"s": true}, true)
	}
	poll() // first miss before the next notification
	rt.capture = func() {
		rt.capture = nil
		event.At = time.Now().UnixNano()
		m.PushClaudeUpdate(context.Background(), "s", activity.WaitingInput, event)
	}
	poll() // stale second capture must not clear the fresh notification
	got, _ := st.GetSession("s")
	if got.Activity != "waiting_input" {
		t.Fatalf("stale capture cleared notification: %s", got.Activity)
	}
	poll() // first miss in fresh event token
	got, _ = st.GetSession("s")
	if got.Activity != "waiting_input" {
		t.Fatal("old miss contributed to new token")
	}
	poll()
	got, _ = st.GetSession("s")
	if got.Activity == "waiting_input" {
		t.Fatal("fresh consecutive misses never clear stale wait")
	}
}
