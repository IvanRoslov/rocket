package runtime

import (
	"context"
	"errors"
	"slices"
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

func TestTmux_Inject_PiForcedClearUsesCtrlC(t *testing.T) {
	f := &fakeTmux{pane: readPane(t, "pi-draft.pane")}
	_ = newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Force: true, Dialect: PaneDialectPi})
	want := []string{"send-keys", "-t", paneTarget("sess"), "C-c"}
	if len(f.calls) < 2 || !slices.Equal(f.calls[1], want) {
		t.Fatalf("clear = %q, want %q", f.calls[1], want)
	}
	if f.sent("C-u") {
		t.Fatal("C-u sent to Pi; recon clear is C-c")
	}
}

func TestTmux_Inject_PiBracketedPaste(t *testing.T) {
	f := &fakeTmux{pane: readPane(t, "pi-idle.pane")}
	_ = newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Bracketed: true, Dialect: PaneDialectPi})
	want := []string{"paste-buffer", "-p", "-d", "-b", "rocket-sess", "-t", paneTarget("sess")}
	if got := pasteCall(f); !slices.Equal(got, want) {
		t.Fatalf("paste argv = %q, want %q", got, want)
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
