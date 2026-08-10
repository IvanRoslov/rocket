package runtime

import (
	"context"
	"errors"
	"testing"
)

// TestTmux_Inject_HeldDialogGuard_Defers is the race-closing half of the
// held-dialog blocker: the queue checks the pane before it decides to
// deliver, but the dialog can open in the window between that check and the
// C-u. Injecting then destroys BOTH copies of the message, so Inject must
// re-check immediately before it touches the pane and report the deferral
// without sending a single key.
func TestTmux_Inject_HeldDialogGuard_Defers(t *testing.T) {
	f := &fakeTmux{pane: fixture(t, "held-dialog-open.pane")}
	rt := newFakeRuntime(f)

	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{})
	if !errors.Is(err, ErrHeldDialog) {
		t.Fatalf("Inject: want ErrHeldDialog, got %v", err)
	}
	for _, forbidden := range []string{"send-keys", "paste-buffer", "load-buffer", "kill-buffer"} {
		if f.sent(forbidden) {
			t.Errorf("%s must not be issued while the held-message dialog is open", forbidden)
		}
	}
}

// TestTmux_Inject_HeldDialogGuard_IgnoresForce pins the difference from the
// draft guard. Force exists to let an abandoned human draft be overwritten
// after a deadline — a recoverable loss. Forcing through an open held dialog
// is never recoverable: it dismisses the dialog (dropping the held copy) and
// the injected text is swallowed. So Force must not bypass this guard.
func TestTmux_Inject_HeldDialogGuard_IgnoresForce(t *testing.T) {
	f := &fakeTmux{pane: fixture(t, "held-dialog-open.pane")}
	rt := newFakeRuntime(f)

	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{Force: true})
	if !errors.Is(err, ErrHeldDialog) {
		t.Fatalf("Inject with Force: want ErrHeldDialog, got %v", err)
	}
	if f.sent("send-keys") || f.sent("paste-buffer") {
		t.Errorf("forced Inject must still refuse to type into an open held dialog")
	}
}

// TestTmux_Inject_HeldDialogGuard_BannerOnly keeps the guard off the
// permanent scrollback banner: every session that ever held a message keeps
// that banner forever, and blocking on it would wedge the queue.
func TestTmux_Inject_HeldDialogGuard_BannerOnly(t *testing.T) {
	f := &fakeTmux{pane: fixture(t, "held-banner-only.pane")}
	rt := newFakeRuntime(f)

	err := rt.Inject(context.Background(), Handle{Name: "sess"}, "hello", InjectOpts{})
	if errors.Is(err, ErrHeldDialog) {
		t.Fatalf("Inject: scrollback banner must not block delivery, got %v", err)
	}
	if !f.sent("paste-buffer") {
		t.Errorf("Inject must proceed when only the scrollback banner is present")
	}
}
