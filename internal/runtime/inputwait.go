package runtime

import "strings"

// inputWaitWindow is how many bottom rows of a pane LooksLikeInputWait
// examines. A permission prompt renders at the very bottom of the pane
// (caption plus its two or three options), so a narrow window is both
// enough and safer: it cannot be fooled by a prompt-shaped line that
// scrolled past earlier in the agent's own output.
const inputWaitWindow = 10

// inputWaitMarkers are the literal strings Claude Code's tool-permission
// prompt renders while it blocks on a keystroke. Deliberately literal and
// narrow, exactly like LooksLikeQuizWidget's markers: generic
// "prompt-shaped" heuristics over TUI output are what the rule in
// docs/07-activity.md warns against.
var inputWaitMarkers = []string{
	"Do you want to proceed?",
	"Do you want to make this edit to",
	"Do you want to create",
	"Yes, and don't ask again",
	"No, and tell Claude what to do differently",
}

// LooksLikeInputWait reports whether the bottom of a pane is showing
// something that is actually blocked on a human keystroke: Claude Code's
// AskUserQuestion widget (LooksLikeQuizWidget) or its tool-permission
// prompt.
//
// It exists because the waiting_input activity state has exactly one
// source — Claude Code's Notification hook — which also fires when the
// agent has merely been idle for a while, and nothing ever un-sets it. The
// monitor uses this function to confirm or clear that suspicion on each
// sweep (see monitor.correctStaleInputWait).
//
// Only the NEGATIVE answer is ever acted on: nothing here can put a session
// INTO waiting_input, and an unrecognised prompt merely leaves today's
// status quo in place for another poll interval.
func LooksLikeInputWait(pane string) bool {
	tail := tailLines(trimTrailingBlank(pane), inputWaitWindow)
	if LooksLikeQuizWidget(tail) {
		return true
	}
	for _, marker := range inputWaitMarkers {
		if strings.Contains(tail, marker) {
			return true
		}
	}
	return false
}
