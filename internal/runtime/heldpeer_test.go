package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

// fixture reads a pane capture recorded from a live Claude Code session.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func TestLooksLikeHeldPeerDialogOnLiveCapture(t *testing.T) {
	if !LooksLikeHeldPeerDialog(fixture(t, "held-dialog-open.pane")) {
		t.Fatal("live capture of an open held-message dialog was not recognised")
	}
}

func TestLooksLikeHeldPeerDialogIgnoresScrollbackBanner(t *testing.T) {
	// The «⏺ Held peer message …» banner stays in the scrollback forever,
	// long after the dialog was answered. Treating it as a blocker would
	// wedge the recipient's queue permanently.
	if LooksLikeHeldPeerDialog(fixture(t, "held-banner-only.pane")) {
		t.Fatal("scrollback banner (no open dialog) was treated as an open dialog")
	}
}

func TestLooksLikeHeldPeerDialogRejectsUnrelatedPanes(t *testing.T) {
	cases := map[string]string{
		"empty": "",
		"composer": "" +
			"────────────────────────\n" +
			"❯ \n" +
			"────────────────────────\n" +
			"  ⏵⏵ bypass permissions on (shift+tab to cycle)\n",
		"quiz widget": "" +
			" Which approach?\n" +
			" ❯ 1. Rewrite\n" +
			"   2. Patch\n" +
			" Enter to select · Esc to cancel\n",
	}
	for name, pane := range cases {
		t.Run(name, func(t *testing.T) {
			if LooksLikeHeldPeerDialog(pane) {
				t.Fatalf("%s pane was treated as an open held dialog", name)
			}
		})
	}
}

func TestLooksLikeHeldPeerDialogNeedsTitleAndOption(t *testing.T) {
	// A single caption can legitimately appear inside a message an agent
	// echoed into its own pane — including the messages this very feature is
	// discussed in. One phrase must never be enough to block delivery.
	titleOnly := "⏺ I am explaining the \"Held message from another session\" dialog to you.\n"
	if LooksLikeHeldPeerDialog(titleOnly) {
		t.Fatal("prose quoting the dialog title was treated as an open dialog")
	}
	optionOnly := "⏺ The second option reads: Deliver this message to Claude\n"
	if LooksLikeHeldPeerDialog(optionOnly) {
		t.Fatal("prose quoting a dialog option was treated as an open dialog")
	}
}
