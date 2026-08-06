package runtime

import (
	"strings"
	"testing"
)

// permissionPromptPane is a Claude Code tool-permission prompt as it renders
// at the bottom of the pane, with the composer replaced by the prompt box.
const permissionPromptPane = `⏺ Bash(rm -rf build)
  ⎿  Running…

╭──────────────────────────────────────────────────────────╮
│ Bash command                                             │
│                                                          │
│   rm -rf build                                           │
│   Remove the build directory                             │
│                                                          │
│ Do you want to proceed?                                  │
│ ❯ 1. Yes                                                 │
│   2. Yes, and don't ask again for rm commands in this dir│
│   3. No, and tell Claude what to do differently (esc)    │
╰──────────────────────────────────────────────────────────╯`

// idleComposerPane is a session that has just finished a turn: composer
// empty, no prompt anywhere.
const idleComposerPane = `⏺ Готово — тесты зелёные.

╭──────────────────────────────────────────────────────────╮
│ >                                                        │
╰──────────────────────────────────────────────────────────╯
  ⏵⏵ bypass permissions on (shift+tab to cycle)`

func TestLooksLikeInputWait(t *testing.T) {
	cases := []struct {
		name string
		pane string
		want bool
	}{
		{"permission prompt", permissionPromptPane, true},
		{"edit permission prompt", "⏺ Update(tmux.go)\n\nDo you want to make this edit to tmux.go?\n❯ 1. Yes\n  2. No", true},
		{"quiz widget", "  3. Blue\n\nEnter to select · ↑/↓ to navigate · Esc to cancel", true},
		{"quiz review screen", "Ready to submit your answers?\n❯ 1. Submit answers", true},

		{"idle composer", idleComposerPane, false},
		{"working spinner", "✻ Thinking… (12s · esc to interrupt)\n\n> ", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := LooksLikeInputWait(c.pane); got != c.want {
			t.Errorf("%s: LooksLikeInputWait = %v, want %v", c.name, got, c.want)
		}
	}
}

// A prompt that has scrolled out of the bottom rows is history, not a live
// wait: only the pane tail counts.
func TestLooksLikeInputWaitIgnoresScrolledPastPrompt(t *testing.T) {
	pane := "Do you want to proceed?\n❯ 1. Yes\n" +
		strings.Repeat("⏺ работаю дальше\n", 12) + idleComposerPane
	if LooksLikeInputWait(pane) {
		t.Errorf("LooksLikeInputWait = true for a prompt far above the pane tail, want false")
	}
}

// A pane padded with unwritten rows below the prompt (the usual shape of a
// tmux capture) must still be recognised.
func TestLooksLikeInputWaitTrimsTrailingBlankRows(t *testing.T) {
	pane := permissionPromptPane + strings.Repeat("\n", 20)
	if !LooksLikeInputWait(pane) {
		t.Errorf("LooksLikeInputWait = false for a prompt padded with blank rows, want true")
	}
}
