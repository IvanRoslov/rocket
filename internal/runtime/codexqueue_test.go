package runtime

import (
	"errors"
	"strings"
	"testing"
)

// The codex-*.pane fixtures are raw `tmux capture-pane -p` output from a live
// codex-cli 0.160.0 session (--no-daemon, 100x40 pane) recorded 2026-10-28 for
// task #5713, with the probe directory path shortened to /tmp/codexprobe.

// longProbeText is the message pasted into the busy Codex for
// codex-queued.pane and codex-history.pane: one logical line that Codex wraps
// across three rows of its queue block.
const longProbeText = "[from probe-orch] MARKER-QUEUE-LONG this is a deliberately long probe message that must wrap across several rows of the Codex queue block so the wrapped rendering can be captured exactly; please reply ACK-LONG when you see it"

// multiProbeText is the multi-line message of codex-queued-multi.pane. Codex
// appended it to an already queued message and truncated the item with «…»,
// so its last line is not on screen.
const multiProbeText = "[from probe-orch] first line MULTI-ONE\nsecond line MULTI-TWO\n\nlast line MULTI-END reply ACK-MULTI"

// orchQueuedPane is the orchestrator's capture of the same state (200x50
// pane, blank lines dropped): a short message right after one Enter.
const orchQueuedPane = "" +
	"• Working (17s • esc to interrupt) · 1 background terminal running · /ps to view · /stop to close\n" +
	"• Messages to be submitted after next tool call (press esc to interrupt and send immediately)\n" +
	"  ↳ [from probe] MARKER-ALPHA please reply ACK-ALPHA\n" +
	"› Ask Codex to do anything\n" +
	"  GPT-6-Astra medium · /tmp/codexprobe · Run shell probe\n"

func TestLooksLikeAgentQueuedRealBlock(t *testing.T) {
	if !LooksLikeAgentQueued(orchQueuedPane, "[from probe] MARKER-ALPHA please reply ACK-ALPHA") {
		t.Fatal("queue block with the message on its ↳ row was not recognised")
	}
}

func TestLooksLikeAgentQueuedWrappedLongMarker(t *testing.T) {
	if !LooksLikeAgentQueued(fixture(t, "codex-queued.pane"), longProbeText) {
		t.Fatal("long message wrapped across the queue block's rows was not recognised")
	}
}

func TestLooksLikeAgentQueuedTruncatedItem(t *testing.T) {
	// Only the start of the message is visible before «…»; its last line
	// (Inject's usual marker) is not on screen at all.
	pane := fixture(t, "codex-queued-multi.pane")
	if !LooksLikeAgentQueued(pane, multiProbeText) {
		t.Fatal("truncated queue item showing the start of the text was not recognised")
	}
	if LooksLikeAgentQueued(pane, "[from probe-orch] first line MULTI-ONE\nsomething entirely different") {
		t.Fatal("a visible prefix must match the text itself, not merely share its first row")
	}
}

func TestLooksLikeAgentQueuedMarkerOnlyInHistory(t *testing.T) {
	// The model took the message: it moved into history as «› …» and the
	// queue block is gone.
	if LooksLikeAgentQueued(fixture(t, "codex-history.pane"), longProbeText) {
		t.Fatal("message echoed in history without a queue block was treated as queued")
	}
}

func TestLooksLikeAgentQueuedTextStillInComposer(t *testing.T) {
	// Pasted while busy, Enter not pressed yet: the text sits in the composer
	// above «tab to queue message».
	if LooksLikeAgentQueued(fixture(t, "codex-before-enter.pane"), longProbeText) {
		t.Fatal("text still in the composer was treated as queued")
	}
	// A block exists (an older item) but our text is still in the composer.
	pane := "" +
		"• Messages to be submitted after next tool call (press esc to interrupt and send immediately)\n" +
		"  ↳ [from probe] MARKER-ALPHA please reply ACK-ALPHA\n" +
		"› [from probe] MARKER-ALPHA please reply ACK-ALPHA\n" +
		"  tab to queue message\n"
	if LooksLikeAgentQueued(pane, "[from probe] MARKER-ALPHA please reply ACK-ALPHA") {
		t.Fatal("an identical older queue item must not confirm text still in the composer")
	}
}

func TestLooksLikeAgentQueuedOtherMarker(t *testing.T) {
	if LooksLikeAgentQueued(fixture(t, "codex-queued.pane"), "[from probe-orch] some other message entirely") {
		t.Fatal("queue block holding a different message was treated as ours")
	}
	if LooksLikeAgentQueued(fixture(t, "codex-queued.pane"), "   \n") {
		t.Fatal("blank marker must never match")
	}
}

func TestLooksLikeAgentQueuedClaudeCodeScreen(t *testing.T) {
	if LooksLikeAgentQueued(fixture(t, "permission-neg-idle.pane"), "hello") {
		t.Fatal("idle Claude Code screen was treated as an agent queue")
	}
	claude := "" +
		"❯ [from probe] MARKER-ALPHA please reply ACK-ALPHA\n" +
		"⏺ Working on it\n" +
		"────────────────────────\n" +
		"❯ \n" +
		"────────────────────────\n" +
		"  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"
	if LooksLikeAgentQueued(claude, "[from probe] MARKER-ALPHA please reply ACK-ALPHA") {
		t.Fatal("Claude Code screen with the message in history was treated as queued")
	}
}

func TestErrQueuedByAgentIsDistinct(t *testing.T) {
	for _, other := range []error{ErrNotDelivered, ErrSubmitUnconfirmed, ErrComposerBusy, ErrHeldDialog} {
		if errors.Is(ErrQueuedByAgent, other) {
			t.Fatalf("ErrQueuedByAgent must not match %v", other)
		}
	}
	if !strings.Contains(ErrQueuedByAgent.Error(), "pending queue") {
		t.Fatalf("unexpected message %q", ErrQueuedByAgent)
	}
}
