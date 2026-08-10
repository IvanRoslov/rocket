package runtime

import "strings"

// heldDialogTitle is the caption of Claude Code's held-peer-message approval
// dialog — the transient widget that appears when the recipient's
// `crossSessionInbound` policy holds an incoming cross-session message
// (docs/design/cc-socket-protocol.md §6).
const heldDialogTitle = "Held message from another session"

// heldDialogOptions are the dialog's two selectable captions. They sit at the
// very bottom of the widget, so they survive a tail capture even when a long
// message body pushes the title out of the window.
var heldDialogOptions = []string{
	"Deliver this message to Claude",
	"Deny — drop it and tell the sender it was declined",
}

// heldBannerMarker identifies the PERMANENT scrollback banner Claude Code
// prints alongside the dialog:
//
//	⏺ Held peer message — from … ; preview: «…» — not delivered to Claude (1 held). …
//
// It is deliberately NOT a blocker: unlike the dialog, the banner never goes
// away, so blocking on it would wedge the recipient's queue forever. It is
// used only to strip the banner out of delivery-confirmation captures, whose
// «preview» quotes the injected text back and false-matches the marker.
const heldBannerMarker = "Held peer message —"

// StripHeldPeerBanner removes every held-peer-message banner block from a
// pane capture, so a delivery-confirmation search cannot match the message
// text the banner quotes back in its «preview».
//
// This is the second half of the same bug: even with the blocker in place, a
// session that ever held one of our messages keeps that banner in its
// scrollback forever, and Inject's final check looks for the marker across
// the scrollback. Matching it there turns a message that was never delivered
// into a "delivered".
//
// A banner is the line carrying heldBannerMarker plus its wrapped
// continuation rows, which run up to the next blank line.
func StripHeldPeerBanner(pane string) string {
	if !strings.Contains(pane, heldBannerMarker) {
		return pane
	}
	lines := strings.Split(pane, "\n")
	kept := make([]string, 0, len(lines))
	inBanner := false
	for _, line := range lines {
		switch {
		case inBanner && strings.TrimSpace(line) == "":
			inBanner = false
			kept = append(kept, line)
		case inBanner:
			// continuation row of the wrapped banner
		case strings.Contains(line, heldBannerMarker):
			inBanner = true
		default:
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// LooksLikeHeldPeerDialog reports whether a pane is currently showing the
// held-peer-message approval dialog.
//
// While that dialog is open, tmux injection is not merely useless but
// destructive: the keystrokes drive the dialog's selector instead of the
// composer, which dismisses it — the held copy is dropped AND the injected
// text never reaches the conversation (reproduced live on CLI 2.1.226, see
// docs/superpowers/plans/2026-08-10-held-dialog-block.md). So the queue
// treats an open dialog exactly like a pending quiz: hold the message and
// wait.
//
// Recognition is deliberately conservative in both directions:
//
//   - It keys on the dialog, never on the scrollback banner (see
//     heldBannerMarker), because the banner outlives the dialog forever.
//   - It requires the title AND one of the option captions. Any single phrase
//     can appear inside a rocket message an agent echoed into its own pane —
//     the messages discussing this very feature do — and one phrase blocking
//     delivery would deadlock that recipient's queue.
//
// An unrecognised pane is not blocked.
func LooksLikeHeldPeerDialog(pane string) bool {
	if !strings.Contains(pane, heldDialogTitle) {
		return false
	}
	for _, opt := range heldDialogOptions {
		if strings.Contains(pane, opt) {
			return true
		}
	}
	return false
}
