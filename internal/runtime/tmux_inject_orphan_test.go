package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeTmux records every `tmux` invocation and answers capture-pane calls
// with a caller-supplied pane rendering, so Inject's confirmation logic can
// be driven deterministically without a real tmux server or terminal.
type fakeTmux struct {
	// pane is the full pane content returned for every capture-pane call.
	pane string
	// calls holds the argument list of every invocation, in order.
	calls [][]string
	// captureErr, when non-nil, is returned for every capture-pane call —
	// an unreadable pane.
	captureErr error
}

func (f *fakeTmux) run(ctx context.Context, args ...string) (string, string, error) {
	f.calls = append(f.calls, args)
	if len(args) > 0 && args[0] == "capture-pane" {
		if f.captureErr != nil {
			return "", "", f.captureErr
		}
		return f.pane, "", nil
	}
	return "", "", nil
}

// sent reports whether a `tmux` invocation whose arguments contain all of
// want (in order, as a contiguous run) was recorded.
func (f *fakeTmux) sent(want ...string) bool {
	for _, call := range f.calls {
		if containsRun(call, want) {
			return true
		}
	}
	return false
}

func containsRun(hay, needle []string) bool {
	if len(needle) == 0 || len(hay) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// newFakeRuntime returns a tmuxRuntime wired to f, with the poll timings
// squashed so an exhausted-attempts run completes near-instantly.
func newFakeRuntime(f *fakeTmux) *tmuxRuntime {
	return &tmuxRuntime{
		settleFn:     func(time.Duration) {},
		runFn:        f.run,
		pollInterval: time.Millisecond,
		pollTimeout:  2 * time.Millisecond,
	}
}

// TestTmux_Inject_UnconfirmedButDelivered covers the case where the retry
// loop exhausts its attempts against a chat-style TUI that keeps echoing
// the marker in its bottom rows (so neither marker-absent nor count-growth
// ever fires) while the submitted message HAS in fact landed and scrolled
// into the history area above that tail. Inject must recognise the
// delivery and return nil, leaving the composer untouched.
func TestTmux_Inject_UnconfirmedButDelivered(t *testing.T) {
	f := &fakeTmux{pane: strings.Join([]string{
		"you: hello",
		"filler-1",
		"filler-2",
		"filler-3",
		"---footer---",
		"> hello",
	}, "\n")}
	rt := newFakeRuntime(f)

	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{})
	if err != nil {
		t.Fatalf("Inject: want nil (marker found in history), got %v", err)
	}

	// The composer holds content that was delivered — never wipe it.
	if n := countCalls(f, "send-keys", "-t", paneTarget("sess"), "C-u"); n != 1 {
		t.Errorf("C-u sent %d times, want exactly 1 (the initial pre-paste clear)", n)
	}
	if f.sent("kill-buffer") {
		t.Errorf("kill-buffer must not be issued when the text was delivered")
	}
}

// TestTmux_Inject_UnconfirmedAndStuck covers the orphaned-text case: the
// attempts are exhausted and the marker appears nowhere in the pane beyond
// the composer itself, so the text really is stuck as an unsent draft.
// Inject must clear the composer and the paste buffer, and report the
// honest non-delivery.
func TestTmux_Inject_UnconfirmedAndStuck(t *testing.T) {
	f := &fakeTmux{pane: strings.Join([]string{
		"you: something else",
		"filler-1",
		"filler-2",
		"filler-3",
		"---footer---",
		"> hello",
	}, "\n")}
	rt := newFakeRuntime(f)

	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{})
	if !errors.Is(err, ErrNotDelivered) {
		t.Fatalf("Inject: want ErrNotDelivered, got %v", err)
	}
	if errors.Is(err, ErrSubmitUnconfirmed) {
		t.Errorf("a cleared composer is a known non-delivery, not the unknown case")
	}

	if n := countCalls(f, "send-keys", "-t", paneTarget("sess"), "C-u"); n != 2 {
		t.Errorf("C-u sent %d times, want 2 (initial clear + final composer clear)", n)
	}
	if !f.sent("kill-buffer", "-b", "rocket-sess") {
		t.Errorf("expected kill-buffer -b rocket-sess, calls: %v", f.calls)
	}
}

// TestTmux_Inject_UnconfirmedAndUnknown covers the third outcome: the
// attempts are exhausted and the final whole-pane capture itself fails, so
// Inject cannot tell delivery from a stuck draft. It must clear nothing
// (the text may well have gone through) and report the genuinely unknown
// case as ErrSubmitUnconfirmed.
func TestTmux_Inject_UnconfirmedAndUnknown(t *testing.T) {
	f := &fakeTmux{pane: strings.Join([]string{
		"filler-1",
		"filler-2",
		"filler-3",
		"---footer---",
		"> hello",
	}, "\n")}
	rt := newFakeRuntime(f)
	// An already-expired poll timeout makes each attempt poll exactly
	// once, so "the second capture after the fifth Enter" is
	// deterministically the final whole-pane one — the only capture this
	// fake breaks. The in-loop polling keeps working, so the attempts are
	// exhausted normally.
	rt.pollTimeout = time.Nanosecond
	enters, capturesAfterLastEnter := 0, 0
	rt.runFn = func(ctx context.Context, args ...string) (string, string, error) {
		if containsRun(args, []string{"send-keys", "-t", paneTarget("sess"), "Enter"}) {
			enters++
		}
		if len(args) > 0 && args[0] == "capture-pane" && enters >= 5 {
			capturesAfterLastEnter++
			if capturesAfterLastEnter > 1 {
				f.calls = append(f.calls, args)
				return "", "", errors.New("capture-pane: no such pane")
			}
		}
		return f.run(ctx, args...)
	}

	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{})
	if !errors.Is(err, ErrSubmitUnconfirmed) {
		t.Fatalf("Inject: want ErrSubmitUnconfirmed, got %v", err)
	}
	if errors.Is(err, ErrNotDelivered) {
		t.Errorf("an unreadable pane is not proof of non-delivery")
	}
	if n := countCalls(f, "send-keys", "-t", paneTarget("sess"), "C-u"); n != 1 {
		t.Errorf("C-u sent %d times, want exactly 1 (the initial pre-paste clear)", n)
	}
	if f.sent("kill-buffer") {
		t.Errorf("kill-buffer must not be issued when delivery could not be determined")
	}
}

func countCalls(f *fakeTmux, want ...string) int {
	n := 0
	for _, call := range f.calls {
		if containsRun(call, want) {
			n++
		}
	}
	return n
}
