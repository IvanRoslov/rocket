package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestTmux_Inject_FinalCheckIgnoresHeldBanner is the confirmation half of the
// held-message bug: the injected text never reached the conversation, but the
// held banner's «preview» quotes it back into the scrollback, and Inject's
// final check used to find it there and report success. A non-delivery must
// stay a non-delivery.
func TestTmux_Inject_FinalCheckIgnoresHeldBanner(t *testing.T) {
	const text = "[from orch] ship the release notes"
	pane := strings.Join([]string{
		"⏺ Held peer message — from an unidentified session [verified pid 28729] (peer claims name: rocket); preview:",
		"  «" + text + "» — not delivered to Claude (1 held). The sender did not attest its",
		"  permission mode and this session bypasses prompts. Review it below, or set \"crossSessionInbound\" to \"accept\".",
		"",
		"────────────────────────────────",
		"❯ ",
		"────────────────────────────────",
		"  ⏵⏵ bypass permissions on (shift+tab to cycle)",
	}, "\n")

	f := &fakeTmux{pane: pane}
	rt := newFakeRuntime(f)

	err := rt.Inject(context.Background(), Handle{Name: "sess"}, text, InjectOpts{})
	if !errors.Is(err, ErrNotDelivered) {
		t.Fatalf("Inject: want ErrNotDelivered, got %v — the held banner must not confirm delivery", err)
	}
}
