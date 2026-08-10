package runtime

import "testing"

// The pane renderings below are trimmed captures of real sessions taken on
// 2026-08-10 (`tmux capture-pane -p`), not invented shapes.
func TestLooksLikeUserDraft(t *testing.T) {
	tests := []struct {
		name string
		pane string
		want bool
	}{
		{
			name: "empty composer",
			pane: "  ⎿  Tip: Use /btw to ask a quick side question\n" +
				"────────────────────────────────\n" +
				"❯ \n" +
				"────────────────────────────────\n" +
				"  ⏵⏵ bypass permissions on (shift+tab to cycle)\n",
			want: false,
		},
		{
			name: "human draft",
			pane: "✻ Brewed for 2m 38s\n" +
				"────────────────────────────────\n" +
				"❯ drop the deprecated always-auth key\n" +
				"────────────────────────────────\n" +
				"  ⏵⏵ bypass permissions on (shift+tab to cycle)\n",
			want: true,
		},
		{
			// Claude Code separates the sigil from the draft with a
			// NO-BREAK SPACE (U+00A0); an ASCII-space check missed every
			// real draft.
			name: "draft after a no-break space",
			pane: "────────────────────────────────\n" +
				"❯ drop the deprecated always-auth key\n" +
				"────────────────────────────────\n" +
				"  ⏵⏵ bypass permissions on\n",
			want: true,
		},
		{
			name: "empty composer after a no-break space",
			pane: "────────────────────────────────\n" +
				"❯ \n" +
				"────────────────────────────────\n" +
				"  ⏵⏵ bypass permissions on\n",
			want: false,
		},
		{
			name: "queued-messages placeholder",
			pane: "  ❯ ой, ладно, я понял ты катишь прод\n" +
				"  ❯ можно позже\n" +
				"────────────────────────────────\n" +
				"❯ Press up to edit queued messages\n" +
				"────────────────────────────────\n" +
				"  ⏸ manual mode on · 1 shell · esc to interrupt\n",
			want: false,
		},
		{
			name: "try placeholder",
			pane: "────────────────────────────────\n" +
				"❯ Try \"how does auth work?\"\n" +
				"────────────────────────────────\n" +
				"  ⏵⏵ bypass permissions on\n",
			want: false,
		},
		{
			name: "boxed empty composer",
			pane: "╭──────────────────╮\n" +
				"│ >                │\n" +
				"╰──────────────────╯\n" +
				"  ? for shortcuts\n",
			want: false,
		},
		{
			name: "boxed draft",
			pane: "╭──────────────────╮\n" +
				"│ > hello there    │\n" +
				"╰──────────────────╯\n" +
				"  ? for shortcuts\n",
			want: true,
		},
		{
			name: "multiline draft",
			pane: "────────────────────────────────\n" +
				"❯ first line\n" +
				"  second line\n" +
				"────────────────────────────────\n" +
				"  ⏵⏵ bypass permissions on\n",
			want: true,
		},
		{
			name: "no composer at all",
			pane: "running tests…\n  PASS ok 1.2s\n",
			want: false,
		},
		{
			name: "unrecognised bare prompt is never busy",
			pane: "you: hello\nfiller\n---footer---\n> hello\n",
			want: false,
		},
		{
			name: "history quote lines only",
			pane: "  > quoted output\n  > more output\n",
			want: false,
		},
		{
			name: "trailing blank padding does not hide the draft",
			pane: "────────────────────────────────\n" +
				"❯ draft text\n" +
				"────────────────────────────────\n" +
				"  ⏵⏵ bypass permissions on\n\n\n\n\n",
			want: true,
		},
		{
			name: "prompt without a closing rule is unrecognised",
			pane: "some output\n❯ maybe a draft, maybe not\n",
			want: false,
		},
		{
			// Real `capture-pane -p -e` bytes (session
			// gar-naming-cli-migrate, 2026-08-10): Claude Code's ghost-text
			// autosuggestion is painted entirely inside an SGR-2 (dim) span.
			// Plain capture makes it byte-identical to a human draft — this
			// is the false positive that deferred delivery for minutes.
			name: "ghost-text suggestion is not a draft",
			pane: "\x1b[38;5;246m✻\x1b[39m \x1b[38;5;246mBrewed for 2m 38s\x1b[39m\n" +
				"\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m❯ \x1b[2mdrop the deprecated always-auth key\x1b[0m\n" +
				"\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m  \x1b[38;5;211m⏵⏵ bypass permissions on\x1b[39m\n",
			want: false,
		},
		{
			// Real `capture-pane -p -e` bytes of text typed into a live
			// composer via send-keys without Enter: normal intensity, no
			// dim span anywhere.
			name: "typed draft in an escaped capture is a draft",
			pane: "\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m❯ hello there draft\n" +
				"\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m  \x1b[38;5;246m⏸ manual mode on\x1b[39m\n",
			want: true,
		},
		{
			// Human typed a prefix and the suggestion continues it: some
			// visible content is NOT dim, so the draft is real.
			name: "typed prefix with a dim suggestion tail is a draft",
			pane: "\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m❯ drop the\x1b[2m deprecated always-auth key\x1b[0m\n" +
				"\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m  \x1b[38;5;211m⏵⏵ bypass permissions on\x1b[39m\n",
			want: true,
		},
		{
			name: "escaped empty composer is not a draft",
			pane: "\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[38;5;246m❯ \x1b[39m\n" +
				"\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m  \x1b[38;5;211m⏵⏵ bypass permissions on\x1b[39m\n",
			want: false,
		},
		{
			// SGR 22 ends a dim span just like 0 does: text after it is
			// normal intensity and therefore a real draft.
			name: "dim span closed by SGR 22 leaves a real draft",
			pane: "────────────────\n" +
				"❯ \x1b[2mghost\x1b[22m typed\n" +
				"────────────────\n" +
				"  ⏵⏵ bypass permissions on\n",
			want: true,
		},
		{
			name: "escaped placeholder is not a draft",
			pane: "\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m❯ \x1b[38;5;246mTry \"how does auth work?\"\x1b[39m\n" +
				"\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m  \x1b[38;5;211m⏵⏵ bypass permissions on\x1b[39m\n",
			want: false,
		},
		{
			// Escape-only rows carry no visible text: they must count as
			// blank padding, otherwise they push the composer out of the
			// window.
			name: "escape-only trailing rows do not hide the draft",
			pane: "\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m❯ hello there draft\n" +
				"\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m  \x1b[38;5;211m⏵⏵ bypass permissions on\x1b[39m\n" +
				"\x1b[39m\n\x1b[39m\n\x1b[39m\n\x1b[39m\n",
			want: true,
		},
		{
			name: "multiline ghost suggestion is not a draft",
			pane: "\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m❯ \x1b[2mfirst dim line\x1b[0m\n" +
				// tmux re-emits every captured row's attributes, so a
				// wrapped suggestion opens its own dim span per line.
				"\x1b[2m  second dim line\x1b[0m\n" +
				"\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m  \x1b[38;5;211m⏵⏵ bypass permissions on\x1b[39m\n",
			want: false,
		},
		{
			// OSC 8 hyperlinks appear in real captures; their payload is
			// not visible text and must not be mistaken for a draft.
			name: "osc hyperlink chrome is not composer content",
			pane: "\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m❯ \x1b]8;id=1;https://example.com\x1b\\\x1b[2mghost\x1b[0m\x1b]8;;\x1b\\\n" +
				"\x1b[38;5;244m────────────────\x1b[39m\n" +
				"\x1b[39m  \x1b[38;5;211m⏵⏵ bypass permissions on\x1b[39m\n",
			want: false,
		},
		{
			name: "cursor block in an empty composer is not a draft",
			pane: "────────────────────────────────\n" +
				"❯ █\n" +
				"────────────────────────────────\n" +
				"  ⏵⏵ bypass permissions on\n",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LooksLikeUserDraft(tt.pane); got != tt.want {
				t.Errorf("LooksLikeUserDraft() = %v, want %v", got, tt.want)
			}
		})
	}
}
