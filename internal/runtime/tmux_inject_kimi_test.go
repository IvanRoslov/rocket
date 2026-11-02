package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func injectCallCount(calls [][]string, command, last string) int {
	n := 0
	for _, c := range calls {
		if len(c) > 0 && c[0] == command && (last == "" || c[len(c)-1] == last) {
			n++
		}
	}
	return n
}

func TestTmux_Inject_KimiCacheHintBeforePasteStartsCompact(t *testing.T) {
	f := &fakeTmux{pane: readPane(t, "kimi-cache-hint.pane")}
	err := newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Dialect: PaneDialectKimi})
	if !errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject = %v, want ErrComposerBusy", err)
	}
	if got := injectCallCount(f.calls, "send-keys", "Enter"); got != 1 {
		t.Fatalf("Enter count = %d, want 1", got)
	}
	if f.sent("paste-buffer") {
		t.Fatal("message pasted after the resume dialog was already open")
	}
}

func TestTmux_Inject_KimiCacheHintAfterSubmitContinuesStashedMessage(t *testing.T) {
	idle := readPane(t, "kimi-idle.pane")
	draft := strings.Replace(idle, "│ >    ", "│ > [paste #1 +3 lines]", 1)
	draft = strings.Replace(draft, "[paste #1 +3 lines]                  ", "[paste #1 +3 lines]", 1)
	s := &scriptedKimi{draft: idle, idle: readPane(t, "kimi-cache-hint.pane")}
	rt := newFakeRuntime(&s.fakeTmux)
	rt.runFn = func(ctx context.Context, args ...string) (string, string, error) {
		if args[0] == "paste-buffer" {
			s.draft = draft
		}
		return s.run(ctx, args...)
	}
	if err := rt.Inject(context.Background(), Handle{Name: "sess"}, "a\nb\nc", InjectOpts{Bracketed: true, Dialect: PaneDialectKimi}); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if got := injectCallCount(s.calls, "send-keys", "Enter"); got != 2 {
		t.Fatalf("Enter count = %d, want submit + compact", got)
	}
	if got := injectCallCount(s.calls, "paste-buffer", ""); got != 1 {
		t.Fatalf("paste count = %d, want 1", got)
	}
}

func TestTmux_Inject_KimiCacheHintOtherSelectionIsUntouched(t *testing.T) {
	pane := strings.Replace(strings.Replace(readPane(t, "kimi-cache-hint.pane"), "   ❯ Compact and continue", "     Compact and continue", 1), "     Continue as-is", "   ❯ Continue as-is", 1)
	f := &fakeTmux{pane: pane}
	err := newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Dialect: PaneDialectKimi})
	if !errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject = %v, want ErrComposerBusy", err)
	}
	if f.sent("send-keys") || f.sent("paste-buffer") {
		t.Fatal("keys sent after the human moved the cursor off compact")
	}
}

func TestTmux_Inject_KimiCacheHintCompactKeyFailureIsUnconfirmed(t *testing.T) {
	idle := readPane(t, "kimi-idle.pane")
	draft := strings.Replace(idle, "│ >    ", "│ > [paste #1 +3 lines]", 1)
	draft = strings.Replace(draft, "[paste #1 +3 lines]                  ", "[paste #1 +3 lines]", 1)
	s := &scriptedKimi{draft: idle, idle: readPane(t, "kimi-cache-hint.pane")}
	rt := newFakeRuntime(&s.fakeTmux)
	rt.runFn = func(ctx context.Context, args ...string) (string, string, error) {
		if args[0] == "send-keys" && args[len(args)-1] == "Enter" && injectCallCount(s.calls, "send-keys", "Enter") == 1 {
			s.calls = append(s.calls, args)
			return "", "", errors.New("send-keys failed")
		}
		if args[0] == "paste-buffer" {
			s.draft = draft
		}
		return s.run(ctx, args...)
	}
	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "a\nb\nc", InjectOpts{Bracketed: true, Dialect: PaneDialectKimi})
	if !errors.Is(err, ErrSubmitUnconfirmed) {
		t.Fatalf("Inject = %v, want ErrSubmitUnconfirmed", err)
	}
	if got := injectCallCount(s.calls, "paste-buffer", ""); got != 1 {
		t.Fatalf("paste count = %d, want 1", got)
	}
}

// kimiSteerPane builds a Kimi pane mid-turn: the queue rule, one row per
// queued item, the queue footer, then the empty composer box and status bar.
// The footers and row shape follow the official QueuePaneComponent renderer
// (upstream-queue-pane.ts at the pinned 2.4.0 revision).
func kimiSteerPane(items []string, foot string) string {
	lines := []string{" " + strings.Repeat("─", 40)}
	for _, it := range items {
		lines = append(lines, "   ❯ "+it)
	}
	lines = append(lines,
		"   "+foot,
		" ╭──────────────────────────────╮",
		" │ >                            │",
		" ╰──────────────────────────────╯",
		" Never Ask  K3.1 Pro thinking: high",
		"                                  context: 0% (0/1M)",
	)
	return strings.Join(lines, "\n")
}

const kimiSteerFoot = "↑ to edit · ctrl-s to steer immediately"

// kimiSteerScript feeds Inject a scripted Kimi pane: empty composer until the
// paste, then (after Enter) the panes in post, one per successful capture.
// The last pane repeats if the probe polls past the end.
type kimiSteerScript struct {
	fakeTmux
	pasted, entered bool
	post            []string
	next            int
	loaded          string // text of the last load-buffer file
}

func (s *kimiSteerScript) run(ctx context.Context, args ...string) (string, string, error) {
	s.calls = append(s.calls, args)
	switch {
	case args[0] == "load-buffer":
		if b, err := os.ReadFile(args[len(args)-1]); err == nil {
			s.loaded = string(b)
		}
	case args[0] == "paste-buffer":
		s.pasted = true
	case args[0] == "send-keys" && args[len(args)-1] == "Enter":
		s.entered = true
	case args[0] == "capture-pane":
		if !s.entered || len(s.post) == 0 {
			return kimiFixture("kimi-idle.pane"), "", nil
		}
		p := s.post[s.next]
		if s.next < len(s.post)-1 {
			s.next++
		}
		return p, "", nil
	}
	return "", "", nil
}

func kimiFixture(name string) string {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		panic(err)
	}
	return string(b)
}

func runKimiSteerInject(t *testing.T, s *kimiSteerScript, text string) error {
	t.Helper()
	rt := newFakeRuntime(&s.fakeTmux)
	rt.runFn = s.run
	return rt.Inject(context.Background(), Handle{Name: "sess"}, text, InjectOpts{Bracketed: true, Dialect: PaneDialectKimi})
}

// A message Enter parked in Kimi's queue is steered into the running turn by
// exactly one Ctrl-S once the pane proves the editor is empty, the queue holds
// only our item, and Kimi advertises immediate steering.
func TestTmux_Inject_KimiSteersQueuedMessageIntoCurrentTurn(t *testing.T) {
	s := &kimiSteerScript{post: []string{kimiSteerPane([]string{"[from orch] please re-check the gate"}, kimiSteerFoot)}}
	if err := runKimiSteerInject(t, s, "[from orch] please re-check the gate"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if got := injectCallCount(s.calls, "send-keys", "C-s"); got != 1 {
		t.Fatalf("C-s count = %d, want 1", got)
	}
	if got := injectCallCount(s.calls, "paste-buffer", ""); got != 1 {
		t.Fatalf("paste count = %d, want 1", got)
	}
	if s.loaded != "[from orch] please re-check the gate" {
		t.Fatalf("pasted text = %q", s.loaded)
	}
}

// Kimi renders a multiline item as one whitespace-collapsed row; ownership is
// still provable, so the full text is steered.
func TestTmux_Inject_KimiSteersMultilineCollapsedQueueRow(t *testing.T) {
	text := "[from orch] line one\nline two\n\tline three"
	s := &kimiSteerScript{post: []string{kimiSteerPane([]string{"[from orch] line one line two line three"}, kimiSteerFoot)}}
	if err := runKimiSteerInject(t, s, text); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if got := injectCallCount(s.calls, "send-keys", "C-s"); got != 1 {
		t.Fatalf("C-s count = %d, want 1", got)
	}
	if s.loaded != text {
		t.Fatalf("pasted text = %q, want the complete multiline message", s.loaded)
	}
}

// A visible foreign queue item means ownership is not exclusive: nothing is
// pressed, and the already-queued message stays with Kimi.
func TestTmux_Inject_KimiDoesNotSteerForeignQueueItem(t *testing.T) {
	s := &kimiSteerScript{post: []string{kimiSteerPane(
		[]string{"[from orch] ours", "someone else's queued message"}, kimiSteerFoot)}}
	if err := runKimiSteerInject(t, s, "[from orch] ours"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if got := injectCallCount(s.calls, "send-keys", "C-s"); got != 0 {
		t.Fatalf("C-s count = %d, want 0", got)
	}
	if got := injectCallCount(s.calls, "paste-buffer", ""); got != 1 {
		t.Fatalf("paste count = %d, want 1", got)
	}
}

// A scrolled queue shows one row plus the total in the footer. One visible row
// is not proof that the queue holds only our message.
func TestTmux_Inject_KimiDoesNotSteerHiddenScrolledQueueItem(t *testing.T) {
	s := &kimiSteerScript{post: []string{kimiSteerPane(
		[]string{"[from orch] ours"}, "2 queued · ↑ to edit · ctrl-s to steer immediately")}}
	if err := runKimiSteerInject(t, s, "[from orch] ours"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if got := injectCallCount(s.calls, "send-keys", "C-s"); got != 0 {
		t.Fatalf("C-s count = %d, want 0", got)
	}
}

// Kimi itself says Ctrl-S will not steer now (it would wait for the current
// task): pressing it would not deliver into this turn, so nothing is pressed.
func TestTmux_Inject_KimiDoesNotSteerWhenFootSaysAfterTask(t *testing.T) {
	s := &kimiSteerScript{post: []string{kimiSteerPane(
		[]string{"[from orch] ours"}, "↑ to edit · will send after current task")}}
	if err := runKimiSteerInject(t, s, "[from orch] ours"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if got := injectCallCount(s.calls, "send-keys", "C-s"); got != 0 {
		t.Fatalf("C-s count = %d, want 0", got)
	}
}

// A narrow pane truncates our row below the 20-rune proof threshold: ownership
// is unprovable, so the message waits.
func TestTmux_Inject_KimiDoesNotSteerTruncatedOwnership(t *testing.T) {
	text := "[from orch] a message that does not fit the narrow pane"
	s := &kimiSteerScript{post: []string{kimiSteerPane([]string{"[from orch] a mess…"}, kimiSteerFoot)}}
	if err := runKimiSteerInject(t, s, text); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if got := injectCallCount(s.calls, "send-keys", "C-s"); got != 0 {
		t.Fatalf("C-s count = %d, want 0", got)
	}
}

// The human starts typing while the steering probe polls: a visible draft
// blocks the key even though the queue would have been steerable before.
func TestTmux_Inject_KimiDoesNotSteerWhenDraftAppearsDuringProbe(t *testing.T) {
	draft := strings.Replace(kimiFixture("kimi-draft.pane"), "draft not submitted", "human typing now", 1)
	s := &kimiSteerScript{post: []string{
		kimiSteerPane([]string{"[from orch] ours"}, "↑ to edit · will send after current task"),
		draft,
		kimiFixture("kimi-idle.pane"),
	}}
	if err := runKimiSteerInject(t, s, "[from orch] ours"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if got := injectCallCount(s.calls, "send-keys", "C-s"); got != 0 {
		t.Fatalf("C-s count = %d, want 0", got)
	}
}

// Kimi drained the queue itself (turn end, WaitFor pass-through): there is
// nothing to steer, no extra key, and no second paste.
func TestTmux_Inject_KimiDoesNotSteerDrainedOrIdle(t *testing.T) {
	s := &kimiSteerScript{post: []string{kimiFixture("kimi-idle.pane")}}
	if err := runKimiSteerInject(t, s, "[from orch] ours"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if got := injectCallCount(s.calls, "send-keys", "C-s"); got != 0 {
		t.Fatalf("C-s count = %d, want 0", got)
	}
	if got := injectCallCount(s.calls, "paste-buffer", ""); got != 1 {
		t.Fatalf("paste count = %d, want 1", got)
	}
}

// A failed steer key is not a delivery failure: the message is already Kimi's
// queued item, so Inject still returns nil and nothing is pasted again.
func TestTmux_Inject_KimiSteerKeyFailureIsNotRedelivered(t *testing.T) {
	s := &kimiSteerScript{post: []string{kimiSteerPane([]string{"[from orch] ours"}, kimiSteerFoot)}}
	rt := newFakeRuntime(&s.fakeTmux)
	rt.runFn = func(ctx context.Context, args ...string) (string, string, error) {
		if args[0] == "send-keys" && args[len(args)-1] == "C-s" {
			s.calls = append(s.calls, args)
			return "", "", errors.New("send-keys failed")
		}
		return s.run(ctx, args...)
	}
	if err := rt.Inject(context.Background(), Handle{Name: "sess"}, "[from orch] ours", InjectOpts{Bracketed: true, Dialect: PaneDialectKimi}); err != nil {
		t.Fatalf("Inject = %v, want nil (message stays Kimi's)", err)
	}
	if got := injectCallCount(s.calls, "paste-buffer", ""); got != 1 {
		t.Fatalf("paste count = %d, want 1", got)
	}
}

// A queue that already exists before Rocket types anything means Kimi owns
// queued input already. Appending our text would either join someone else's
// payload or duplicate a copy Kimi already has, so delivery defers before the
// clear, the paste or any key — a later C-s is not the required deferral.
func TestTmux_Inject_KimiPreExistingQueueDefersBeforePaste(t *testing.T) {
	cases := map[string]string{
		"foreign item":                kimiSteerPane([]string{"someone else's queued message"}, kimiSteerFoot),
		"foreign item not steerable":  kimiSteerPane([]string{"someone else's queued message"}, "↑ to edit · will send after current task"),
		"hidden scrolled rows":        kimiSteerPane([]string{"[from orch] ours"}, "2 queued · ↑ to edit · ctrl-s to steer immediately"),
		"our own copy already queued": kimiSteerPane([]string{"[from orch] ours"}, kimiSteerFoot),
	}
	for name, pane := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeTmux{pane: pane}
			err := newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "[from orch] ours",
				InjectOpts{Bracketed: true, Dialect: PaneDialectKimi})
			if !errors.Is(err, ErrComposerBusy) {
				t.Fatalf("Inject = %v, want ErrComposerBusy deferral", err)
			}
			if f.sent("send-keys") || f.sent("paste-buffer") {
				t.Fatalf("keys sent over a pre-existing queue: %q", f.calls)
			}
		})
	}
}

// The pre-existing-queue deferral is not a draft-deadline escape hatch: Force
// must never authorize typing into a queue Kimi already holds.
func TestTmux_Inject_KimiPreExistingQueueDefersEvenForced(t *testing.T) {
	f := &fakeTmux{pane: kimiSteerPane([]string{"someone else's queued message"}, kimiSteerFoot)}
	err := newFakeRuntime(f).Inject(context.Background(), Handle{Name: "sess"}, "[from orch] ours",
		InjectOpts{Force: true, Bracketed: true, Dialect: PaneDialectKimi})
	if !errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject = %v, want ErrComposerBusy", err)
	}
	if f.sent("send-keys") || f.sent("paste-buffer") {
		t.Fatalf("keys sent over a pre-existing queue despite Force: %q", f.calls)
	}
}
