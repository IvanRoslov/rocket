package runtime

import "testing"

// Confirmation compares the injected text's last line (the marker) against a
// pane capture. The pane rarely holds that logical line as one row, and every
// way it can break it must still count as "present": a false negative makes
// Inject report a non-delivery for text that landed, and the queue then
// redelivers the same message up to maxAttempts (live incident #1186/Q5: one
// message pasted into cto five times, the agent replying "Дубль, не реагирую").
func TestContainsMarkerIgnoresHowThePaneBrokeTheLine(t *testing.T) {
	marker := "[#1186/Q5 reply from task-1186-orch] Принял. Держу оба PR mergeable, " +
		"воркеров не трогаю, ничего не мержу до зелёного CI. Если понадобится " +
		"проверить что-то ещё на моём уровне доступа — скажи."

	cases := []struct {
		name    string
		capture string
		want    bool
	}{
		{
			name:    "single row",
			capture: "chrome\n" + marker + "\nmore",
			want:    true,
		},
		{
			// tmux's own wrap: the row is cut at the pane width, mid-word,
			// with no indent. `capture-pane -J` rejoins these.
			name: "tmux hard wrap at pane width",
			capture: "❯ [#1186/Q5 reply from task-1186-orch] Принял. Держу оба PR mergeable, воркеров не трогаю, ничего не мержу до зелёного CI. Ес\n" +
				"ли понадобится проверить что-то ещё на моём уровне доступа — скажи.",
			want: true,
		},
		{
			// Claude Code re-flows the message itself: "❯ " prefix, the
			// continuation indented, every row padded to the full width.
			// These are real newlines printed by the app, so -J leaves them
			// split — this is the shape that caused the incident.
			name: "TUI reflow with prefix, indent and padding",
			capture: "❯ [#1186/Q5 reply from task-1186-orch] Принял. Держу оба PR mergeable, воркеров не трогаю, ничего не мержу до зелёного CI. Если          \n" +
				"  понадобится проверить что-то ещё на моём уровне доступа — скажи.                                                                    ",
			want: true,
		},
		{
			name:    "genuinely absent",
			capture: "❯ \n  esc to interrupt · ← for agents",
			want:    false,
		},
		{
			name:    "empty marker is never present",
			capture: "anything at all",
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := marker
			if tc.name == "empty marker is never present" {
				m = "   "
			}
			if got := ContainsMarker(tc.capture, m); got != tc.want {
				t.Fatalf("ContainsMarker = %v, want %v", got, tc.want)
			}
		})
	}
}

// A marker must not be confirmed by unrelated pane content that merely shares
// its words: normalisation drops whitespace, and nothing more.
func TestContainsMarkerDoesNotMatchDifferentText(t *testing.T) {
	if ContainsMarker("Принял. Держу оба PR mergeable.", "Принял. Держу оба PR unmergeable.") {
		t.Fatal("ContainsMarker matched text that differs in a non-whitespace character")
	}
}
