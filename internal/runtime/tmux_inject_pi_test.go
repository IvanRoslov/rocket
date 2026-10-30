package runtime

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestTmux_Inject_PiDraftGuard(t *testing.T) {
	f := &fakeTmux{pane: readPane(t, "pi-draft.pane")}
	err := newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Dialect: PaneDialectPi})
	if !errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject = %v, want ErrComposerBusy", err)
	}
	if f.sent("send-keys") || f.sent("paste-buffer") {
		t.Fatal("keys sent despite the human draft")
	}
}

func TestTmux_Inject_PiUnrecognizedPaneDefersEvenForced(t *testing.T) {
	f := &fakeTmux{pane: readPane(t, "kimi-question.pane")}
	err := newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Force: true, Dialect: PaneDialectPi})
	if !errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject = %v, want ErrComposerBusy", err)
	}
	if f.sent("send-keys") || f.sent("paste-buffer") {
		t.Fatal("keys sent to an unrecognized Pi pane")
	}
}

func TestTmux_Inject_PiCaptureErrorDefers(t *testing.T) {
	f := &fakeTmux{captureErr: errors.New("tmux hiccup")}
	err := newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Dialect: PaneDialectPi})
	if !errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject = %v, want ErrComposerBusy", err)
	}
	if f.sent("send-keys") || f.sent("paste-buffer") {
		t.Fatal("keys sent to an unreadable Pi pane")
	}
}

func TestTmux_Inject_PiForcedDraftGuard(t *testing.T) {
	f := &fakeTmux{pane: readPane(t, "pi-draft.pane")}
	err := newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Force: true, Dialect: PaneDialectPi})
	if !errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject = %v, want ErrComposerBusy even under Force", err)
	}
	if f.sent("send-keys") || f.sent("paste-buffer") {
		t.Fatal("clear/paste/Enter sent to a human draft under Force")
	}
}

func TestTmux_Inject_PiBracketedPaste(t *testing.T) {
	f := &fakeTmux{pane: readPane(t, "pi-idle.pane")}
	_ = newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Bracketed: true, Dialect: PaneDialectPi})
	want := []string{"paste-buffer", "-p", "-d", "-b", "rocket-sess", "-t", paneTarget("sess")}
	if got := pasteCall(f); !slices.Equal(got, want) {
		t.Fatalf("paste argv = %q, want %q", got, want)
	}
	if f.sent("C-c") {
		t.Fatal("C-c sent to an empty Pi composer; only a draft needs clearing")
	}
}

func TestTmux_Inject_PiSteeringReturnsQueued(t *testing.T) {
	s := &stagedTmux{paneFn: func(enters int, _ bool) string {
		if enters == 0 {
			return readPane(t, "pi-idle.pane")
		}
		return readPane(t, "pi-busy-queued.pane")
	}}
	err := newStagedRuntime(s).Inject(context.Background(), Handle{Name: "sess"}, "<redacted-steering>", InjectOpts{Bracketed: true, Dialect: PaneDialectPi})
	if !errors.Is(err, ErrQueuedByAgent) {
		t.Fatalf("Inject = %v, want ErrQueuedByAgent", err)
	}
	if s.enters != 1 {
		t.Fatalf("Enter sent %d times, want exactly 1", s.enters)
	}
	if s.sent("kill-buffer") {
		t.Fatal("kill-buffer issued for steering owned by Pi")
	}
}

func TestTmux_Inject_PiFinalCleanupOnlyClearsDraft(t *testing.T) {
	stuck := strings.Replace(readPane(t, "pi-plain-draft.pane"), "<redacted-plain-draft>", "hello", 1)
	for _, tc := range []struct {
		name      string
		finalPane string
		wantClear bool
	}{
		{name: "empty", finalPane: readPane(t, "pi-idle.pane"), wantClear: false},
		{name: "busy", finalPane: readPane(t, "pi-busy.pane"), wantClear: false},
		{name: "unknown", finalPane: readPane(t, "kimi-question.pane"), wantClear: false},
		{name: "draft", finalPane: stuck, wantClear: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captures := 0
			s := &stagedTmux{paneFn: func(_ int, scrollback bool) string {
				if scrollback {
					return tc.finalPane
				}
				captures++
				if captures == 1 {
					return readPane(t, "pi-idle.pane")
				}
				return stuck
			}}
			err := newStagedRuntime(s).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Dialect: PaneDialectPi})
			if !errors.Is(err, ErrNotDelivered) {
				t.Fatalf("Inject = %v, want ErrNotDelivered", err)
			}
			if got := countCalls(&s.fakeTmux, "send-keys", "-t", paneTarget("sess"), "C-c"); (got > 0) != tc.wantClear {
				t.Fatalf("C-c calls = %d, wantClear=%v; calls: %v", got, tc.wantClear, s.calls)
			}
		})
	}
}
