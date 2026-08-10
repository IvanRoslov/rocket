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
