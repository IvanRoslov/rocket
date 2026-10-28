package runtime

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func pasteCall(f *fakeTmux) []string {
	for _, c := range f.calls {
		if len(c) > 0 && c[0] == "paste-buffer" {
			return c
		}
	}
	return nil
}

func TestTmux_Inject_BracketedPaste(t *testing.T) {
	f := &fakeTmux{pane: emptyComposerPane}
	_ = newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Bracketed: true})
	want := []string{"paste-buffer", "-p", "-d", "-b", "rocket-sess", "-t", paneTarget("sess")}
	if got := pasteCall(f); !slices.Equal(got, want) {
		t.Fatalf("paste argv = %q, want %q", got, want)
	}
}

func TestTmux_Inject_PlainPasteUnchanged(t *testing.T) {
	f := &fakeTmux{pane: emptyComposerPane}
	_ = newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{})
	want := []string{"paste-buffer", "-d", "-b", "rocket-sess", "-t", paneTarget("sess")}
	if got := pasteCall(f); !slices.Equal(got, want) {
		t.Fatalf("paste argv = %q, want %q", got, want)
	}
}

// Kimi's boxed composer is not one LooksLikeUserDraft knows; with the Kimi
// dialect the guard reads it with ParseKimiComposer.
func TestTmux_Inject_KimiDraftGuard(t *testing.T) {
	f := &fakeTmux{pane: readPane(t, "kimi-draft.pane")}
	err := newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Dialect: PaneDialectKimi})
	if !errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject = %v, want ErrComposerBusy", err)
	}
	if f.sent("send-keys") || f.sent("paste-buffer") {
		t.Fatal("keys sent despite the draft")
	}
}

// scriptedKimi serves the draft pane until the first Enter, then the idle
// pane: a multi-line paste shows only «[paste #1 +3 lines]», so the marker
// never renders and the line count does not change.
type scriptedKimi struct {
	fakeTmux
	draft, idle string
	entered     bool
}

func (s *scriptedKimi) run(ctx context.Context, args ...string) (string, string, error) {
	s.calls = append(s.calls, args)
	switch {
	case args[0] == "send-keys" && args[len(args)-1] == "Enter":
		s.entered = true
	case args[0] == "capture-pane":
		if s.entered {
			return s.idle, "", nil
		}
		return s.draft, "", nil
	}
	return "", "", nil
}

func TestTmux_Inject_KimiConfirmsByComposer(t *testing.T) {
	idle := readPane(t, "kimi-idle.pane")
	draft := strings.Replace(idle, "│ >    ", "│ > [paste #1 +3 lines]", 1)
	draft = strings.Replace(draft, "[paste #1 +3 lines]                  ", "[paste #1 +3 lines]", 1)
	s := &scriptedKimi{draft: idle, idle: idle}
	// Before the paste the composer is empty (guard passes); after it, the
	// placeholder shows until Enter.
	pasted := false
	rt := newFakeRuntime(&s.fakeTmux)
	rt.runFn = func(ctx context.Context, args ...string) (string, string, error) {
		if args[0] == "paste-buffer" {
			pasted = true
			s.draft = draft
		}
		return s.run(ctx, args...)
	}
	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "a\nb\nc", InjectOpts{Bracketed: true, Dialect: PaneDialectKimi})
	if err != nil || !pasted {
		t.Fatalf("Inject = %v (pasted %v)", err, pasted)
	}
	enters := 0
	for _, c := range s.calls {
		if c[0] == "send-keys" && c[len(c)-1] == "Enter" {
			enters++
		}
	}
	if enters != 1 {
		t.Fatalf("Enter sent %d times, want 1", enters)
	}
}

// An unrecognized Kimi pane (question widget, dialog) is never typed into,
// not even when forced: Force is for an abandoned draft, not an unknown
// screen where Enter would pick an option.
func TestTmux_Inject_KimiUnrecognizedPaneDefersEvenForced(t *testing.T) {
	f := &fakeTmux{pane: readPane(t, "kimi-question.pane")}
	err := newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Force: true, Dialect: PaneDialectKimi})
	if !errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject = %v, want ErrComposerBusy", err)
	}
	if f.sent("send-keys") || f.sent("paste-buffer") {
		t.Fatal("keys sent to an unrecognized Kimi pane")
	}
}

// Kimi's C-u clears only the current composer line (live, 2.4.0); a forced
// delivery must clear every row of the draft.
func TestTmux_Inject_KimiForcedClearsEveryDraftRow(t *testing.T) {
	pane := strings.Join([]string{
		" ╭──────────────╮",
		" │ > first      │",
		" │   second     │",
		" │   third      │",
		" ╰──────────────╯",
		" Never Ask  K3.1 Pro",
		"            context: 0% (0/1M)",
	}, "\n")
	f := &fakeTmux{pane: pane}
	_ = newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Force: true, Dialect: PaneDialectKimi})
	want := []string{"send-keys", "-t", paneTarget("sess"), "C-u", "BSpace", "C-u", "BSpace", "C-u"}
	if len(f.calls) < 2 || !slices.Equal(f.calls[1], want) {
		t.Fatalf("clear = %q, want %q", f.calls[1], want)
	}
}

func TestLooksLikeKimiQueued(t *testing.T) {
	busy := readPane(t, "kimi-busy-queued.pane")
	if !LooksLikeKimiQueued(busy, "reply QUEUED_OK") {
		t.Error("queued item not recognized")
	}
	if LooksLikeKimiQueued(busy, "something else entirely") {
		t.Error("other text recognized as queued")
	}
	if LooksLikeKimiQueued(readPane(t, "kimi-finished.pane"), "reply QUEUED_OK") {
		t.Error("history echo recognized as queued")
	}
	long := strings.Replace(busy, "❯ reply QUEUED_OK", "❯ [from orch] a long message that Kimi cut at the …", 1)
	if !LooksLikeKimiQueued(long, "[from orch] a long message that Kimi cut at the edge of the screen and more") {
		t.Error("truncated queue item not recognized")
	}
}

// Kimi dialect: an unreadable pane is not typed into blind.
func TestTmux_Inject_KimiCaptureErrorDefers(t *testing.T) {
	f := &fakeTmux{captureErr: errors.New("tmux hiccup")}
	err := newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Dialect: PaneDialectKimi})
	if !errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject = %v, want ErrComposerBusy", err)
	}
	if f.sent("send-keys") || f.sent("paste-buffer") {
		t.Fatal("keys sent to an unreadable Kimi pane")
	}
}
