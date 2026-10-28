package runtime

import (
	"context"
	"errors"
	"testing"
)

// stagedTmux is a fakeTmux whose pane depends on how many Enters were sent
// and on whether the capture reaches into scrollback (-S), so a test can
// script what Codex shows before and after a submit.
type stagedTmux struct {
	fakeTmux
	enters int
	// paneFn returns the capture for the current state.
	paneFn func(enters int, scrollback bool) string
}

func (s *stagedTmux) run(ctx context.Context, args ...string) (string, string, error) {
	s.calls = append(s.calls, args)
	if len(args) == 0 {
		return "", "", nil
	}
	switch args[0] {
	case "send-keys":
		if args[len(args)-1] == "Enter" {
			s.enters++
		}
	case "capture-pane":
		scrollback := false
		for _, a := range args {
			if a == "-S" {
				scrollback = true
			}
		}
		return s.paneFn(s.enters, scrollback), "", nil
	}
	return "", "", nil
}

func newStagedRuntime(s *stagedTmux) *tmuxRuntime {
	rt := newFakeRuntime(&s.fakeTmux)
	rt.runFn = s.run
	return rt
}

// TestTmux_Inject_CodexQueuedReturnsQueuedByAgent: Codex is busy, the paste
// sits in its composer, and one Enter moves it into the pending-queue block.
// Inject must report ErrQueuedByAgent at once — one Enter, no C-u after the
// paste, no kill-buffer — instead of pressing Enter again or reading the
// grown tail as a plain submit.
func TestTmux_Inject_CodexQueuedReturnsQueuedByAgent(t *testing.T) {
	before := fixture(t, "codex-before-enter.pane")
	queued := fixture(t, "codex-queued.pane")
	s := &stagedTmux{paneFn: func(enters int, _ bool) string {
		if enters == 0 {
			return before
		}
		return queued
	}}

	err := newStagedRuntime(s).Inject(context.Background(), Handle{Name: "sess"}, longProbeText, InjectOpts{})
	if !errors.Is(err, ErrQueuedByAgent) {
		t.Fatalf("Inject: want ErrQueuedByAgent, got %v", err)
	}
	if s.enters != 1 {
		t.Errorf("Enter sent %d times, want exactly 1", s.enters)
	}
	if n := countCalls(&s.fakeTmux, "send-keys", "-t", paneTarget("sess"), "C-u"); n != 1 {
		t.Errorf("C-u sent %d times, want 1 (only the pre-paste clear)", n)
	}
	if s.sent("kill-buffer") {
		t.Errorf("kill-buffer must not be issued for queued text")
	}
}

// TestTmux_Inject_CodexQueuedShortMessage covers the short one-row message
// whose block makes the bottom tail grow: count-growth alone would call it a
// plain submit, but it is a queued one.
func TestTmux_Inject_CodexQueuedShortMessage(t *testing.T) {
	const text = "[from probe] MARKER-ALPHA please reply ACK-ALPHA"
	before := "" +
		"• Working (14s • esc to interrupt) · 1 background terminal running · /ps to view · /stop to close\n" +
		"› " + text + "\n" +
		"  tab to queue message                                                    97% context left\n"
	s := &stagedTmux{paneFn: func(enters int, _ bool) string {
		if enters == 0 {
			return before
		}
		return orchQueuedPane
	}}

	err := newStagedRuntime(s).Inject(context.Background(), Handle{Name: "sess"}, text, InjectOpts{})
	if !errors.Is(err, ErrQueuedByAgent) {
		t.Fatalf("Inject: want ErrQueuedByAgent, got %v", err)
	}
	if s.enters != 1 {
		t.Errorf("Enter sent %d times, want exactly 1", s.enters)
	}
}

// TestTmux_Inject_CodexQueuedSeenOnlyInFinalCheck: polling never saw the
// block (the visible screen kept showing the composer), but the final
// whole-pane check does. That is a queued message, not a stuck draft: no
// C-u, no kill-buffer, ErrQueuedByAgent.
func TestTmux_Inject_CodexQueuedSeenOnlyInFinalCheck(t *testing.T) {
	before := fixture(t, "codex-before-enter.pane")
	queued := fixture(t, "codex-queued.pane")
	s := &stagedTmux{paneFn: func(_ int, scrollback bool) string {
		if scrollback {
			return queued
		}
		return before
	}}

	err := newStagedRuntime(s).Inject(context.Background(), Handle{Name: "sess"}, longProbeText, InjectOpts{})
	if !errors.Is(err, ErrQueuedByAgent) {
		t.Fatalf("Inject: want ErrQueuedByAgent, got %v", err)
	}
	if n := countCalls(&s.fakeTmux, "send-keys", "-t", paneTarget("sess"), "C-u"); n != 1 {
		t.Errorf("C-u sent %d times, want 1 (only the pre-paste clear)", n)
	}
	if s.sent("kill-buffer") {
		t.Errorf("kill-buffer must not be issued for queued text")
	}
}

// TestTmux_Inject_CodexIdleSubmitUnchanged: an idle Codex submits at once and
// shows no queue block; Inject keeps reporting a plain success.
func TestTmux_Inject_CodexIdleSubmitUnchanged(t *testing.T) {
	const text = "[from probe] MARKER-ALPHA please reply ACK-ALPHA"
	before := "" +
		"› " + text + "\n" +
		"  GPT-6-Astra medium · /tmp/codexprobe\n" +
		"  ? for shortcuts\n"
	after := "" +
		"› " + text + "\n" +
		"• Working (0s • esc to interrupt)\n" +
		"› Ask Codex to do anything\n" +
		"  GPT-6-Astra medium · /tmp/codexprobe\n" +
		"  ? for shortcuts\n"
	s := &stagedTmux{paneFn: func(enters int, _ bool) string {
		if enters == 0 {
			return before
		}
		return after
	}}

	if err := newStagedRuntime(s).Inject(context.Background(), Handle{Name: "sess"}, text, InjectOpts{}); err != nil {
		t.Fatalf("Inject: want nil, got %v", err)
	}
}
