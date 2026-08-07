package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scrollbackTmux answers capture-pane with two different renderings: the
// visible screen, and the screen plus scrollback when the call carries -S.
// That is the real distinction Inject has to cope with — see the test below.
type scrollbackTmux struct {
	visible    string
	scrollback string
	calls      [][]string
}

func (f *scrollbackTmux) run(ctx context.Context, args ...string) (string, string, error) {
	f.calls = append(f.calls, args)
	if len(args) > 0 && args[0] == "capture-pane" {
		for _, a := range args {
			if a == "-S" {
				return f.scrollback, "", nil
			}
		}
		return f.visible, "", nil
	}
	return "", "", nil
}

// A busy agent starts rendering its turn the moment the message is
// submitted, so the message scrolls off the visible screen within seconds.
// Inject's final check has to look at the scrollback: judging by the visible
// screen alone, a delivered message looks like a stuck draft, Inject reports
// ErrNotDelivered, and the queue redelivers it — the #1186/Q5 and
// papercuts-sdk-app-21 duplicate storms, where the marker was provably
// present in the pane's scrollback and absent from the visible screen.
func TestInjectConfirmsFromScrollbackAfterMessageScrolledOff(t *testing.T) {
	text := "papercuts-sdk-app-21-orch: already complete. Task #1154 is in review, " +
		"report uploaded (task doc, kind=report). No PRs to merge and none pending."

	// The visible screen has moved on: the agent's own output fills it and
	// the composer at the bottom is empty again. No trace of the message.
	visible := strings.Repeat("agent output line\n", 40) +
		"❯                                        \n" +
		"  esc to interrupt · ← for agents"

	// The scrollback still holds it, wrapped the way the TUI renders it.
	scrollback := "❯ papercuts-sdk-app-21-orch: already complete. Task #1154 is in review, report uploaded (task doc, kind=report). No PRs to\n" +
		"  merge and none pending.\n" +
		visible

	f := &scrollbackTmux{visible: visible, scrollback: scrollback}
	rt := newFakeRuntime(nil)
	rt.runFn = f.run

	err := rt.Inject(context.Background(), Handle{Name: "cto"}, text)

	if errors.Is(err, ErrNotDelivered) {
		t.Fatal("Inject reported a non-delivery for a message that is present in the " +
			"pane's scrollback: the queue will redeliver it and the agent sees duplicates")
	}
	if err != nil {
		t.Fatalf("Inject: unexpected error %v", err)
	}
}

// The composer-stuck case must still be reported honestly: if the text was
// never submitted it sits in the composer and appears nowhere above it, not
// even in the scrollback. Widening the final check must not blunt this.
func TestInjectStillReportsGenuineNonDelivery(t *testing.T) {
	text := "draft that never got submitted"

	stuck := strings.Repeat("older unrelated output\n", 40) +
		"❯ draft that never got submitted\n" +
		"  esc to interrupt · ← for agents"

	f := &scrollbackTmux{visible: stuck, scrollback: stuck}
	rt := newFakeRuntime(nil)
	rt.runFn = f.run

	err := rt.Inject(context.Background(), Handle{Name: "cto"}, text)
	if !errors.Is(err, ErrNotDelivered) {
		t.Fatalf("Inject = %v, want ErrNotDelivered for a draft stuck in the composer", err)
	}
}
