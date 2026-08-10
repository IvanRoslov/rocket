package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// draftPane is a pane rendering whose composer holds an unsent human draft
// (a real capture shape: sigil, NO-BREAK SPACE, text, closing rule).
var draftPane = strings.Join([]string{
	"✻ Brewed for 2m 38s",
	"────────────────────────────────",
	"❯ drop the deprecated always-auth key",
	"────────────────────────────────",
	"  ⏵⏵ bypass permissions on (shift+tab to cycle)",
}, "\n")

// emptyComposerPane is the same shape with nothing typed into it.
var emptyComposerPane = strings.Join([]string{
	"  ⎿  Tip: Use /btw to ask a quick side question",
	"────────────────────────────────",
	"❯ ",
	"────────────────────────────────",
	"  ⏵⏵ bypass permissions on (shift+tab to cycle)",
}, "\n")

// TestTmux_Inject_DraftGuard_Defers is the whole point of the guard: a
// human's unsent draft must survive an incoming delivery untouched — no
// C-u, no paste, no Enter — and the caller must be told with
// ErrComposerBusy rather than a generic failure.
func TestTmux_Inject_DraftGuard_Defers(t *testing.T) {
	f := &fakeTmux{pane: draftPane}
	rt := newFakeRuntime(f)

	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{})
	if !errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject: want ErrComposerBusy, got %v", err)
	}
	for _, forbidden := range []string{"send-keys", "paste-buffer", "load-buffer", "kill-buffer"} {
		if f.sent(forbidden) {
			t.Errorf("%s must not be issued while the composer holds a user draft", forbidden)
		}
	}
}

// TestTmux_Inject_DraftGuard_Force covers the queue's busy-deadline escape
// hatch: once an abandoned draft has blocked delivery long enough, the
// caller forces it through and the old behaviour (C-u first) applies.
func TestTmux_Inject_DraftGuard_Force(t *testing.T) {
	f := &fakeTmux{pane: draftPane}
	rt := newFakeRuntime(f)

	// The pane never changes, so submission is never confirmed; what matters
	// here is that the guard did not stop the attempt.
	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Force: true})
	if errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject with Force: guard must not fire, got %v", err)
	}
	if !f.sent("send-keys", "-t", paneTarget("sess"), "C-u") {
		t.Errorf("forced Inject must clear the composer with C-u")
	}
	if !f.sent("paste-buffer") {
		t.Errorf("forced Inject must paste the text")
	}
}

// TestTmux_Inject_DraftGuard_EmptyComposer pins the normal path: an empty
// composer is not busy and delivery proceeds exactly as before.
func TestTmux_Inject_DraftGuard_EmptyComposer(t *testing.T) {
	f := &fakeTmux{pane: emptyComposerPane}
	rt := newFakeRuntime(f)

	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{})
	if errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject: empty composer must not be busy, got %v", err)
	}
	if !f.sent("send-keys", "-t", paneTarget("sess"), "C-u") {
		t.Errorf("Inject must clear the composer with C-u on the normal path")
	}
}

// ghostPane is a real `capture-pane -p -e` rendering of a composer holding
// only Claude Code's ghost-text autosuggestion: the content sits entirely
// inside an SGR-2 (dim) span. Without -e it is byte-identical to draftPane.
var ghostPane = strings.Join([]string{
	"\x1b[38;5;246m✻\x1b[39m \x1b[38;5;246mBrewed for 2m 38s\x1b[39m",
	"\x1b[38;5;244m────────────────────────────────\x1b[39m",
	"\x1b[39m❯ \x1b[2mdrop the deprecated always-auth key\x1b[0m",
	"\x1b[38;5;244m────────────────────────────────\x1b[39m",
	"\x1b[39m  \x1b[38;5;211m⏵⏵ bypass permissions on\x1b[39m",
}, "\n")

// TestTmux_Inject_DraftGuard_CapturesEscapes pins the capture the guard
// depends on: only `capture-pane -e` carries the dim attribute that tells a
// typed draft from a rendered suggestion. A plain capture gives the guard
// nothing to tell them apart with, so it must never be the one it reads.
func TestTmux_Inject_DraftGuard_CapturesEscapes(t *testing.T) {
	f := &fakeTmux{pane: emptyComposerPane}
	rt := newFakeRuntime(f)

	_ = rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{})
	if !f.sent("capture-pane", "-p", "-e", "-t", paneTarget("sess")) {
		t.Errorf("the draft guard must capture with escape sequences (-e); calls: %v", f.calls)
	}
}

// TestTmux_Inject_DraftGuard_GhostText is the false positive this guard
// grew a parser for: a suggestion Claude Code rendered on its own is not a
// human draft, and delivery must not be deferred for it.
func TestTmux_Inject_DraftGuard_GhostText(t *testing.T) {
	f := &fakeTmux{pane: ghostPane}
	rt := newFakeRuntime(f)

	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{})
	if errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject: a ghost-text suggestion must not be busy, got %v", err)
	}
	if !f.sent("send-keys", "-t", paneTarget("sess"), "C-u") {
		t.Errorf("Inject must proceed past a ghost-text suggestion")
	}
}

// TestTmux_Inject_DraftGuard_CaptureFails documents the fail-open rule: if
// the pane cannot be read, the guard cannot claim a draft is there, and
// delivery must not be blocked by an unreadable pane.
func TestTmux_Inject_DraftGuard_CaptureFails(t *testing.T) {
	f := &fakeTmux{pane: draftPane, captureErr: errors.New("no such pane")}
	rt := newFakeRuntime(f)

	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{})
	if errors.Is(err, ErrComposerBusy) {
		t.Fatalf("Inject: unreadable pane must not report ErrComposerBusy, got %v", err)
	}
	if !f.sent("send-keys", "-t", paneTarget("sess"), "C-u") {
		t.Errorf("Inject must proceed when the guard's capture fails")
	}
}
