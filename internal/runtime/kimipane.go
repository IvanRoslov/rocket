package runtime

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Kimi Code CLI 2.4.0 renders its composer as a box at the bottom of the
// pane, followed by a two-row status bar (live captures in
// testdata/kimi-*.pane):
//
//	   ❯ reply QUEUED_OK                         ← Kimi's own queue (busy only),
//	   ↑ to edit · ctrl-s to steer immediately     between a ─── rule and the box
//	╭──────────────────────────────╮
//	│ > draft text                 │
//	╰──────────────────────────────╯
//	Never Ask  K3.1 Pro thinking: high  …
//	                        context: 0% (0/1M)
//
// Tips, the spinner and the welcome lines vary between runs, so only these
// structural rows are matched.

// KimiComposer is the state of Kimi's composer read off a pane.
type KimiComposer int

const (
	// KimiEmpty: the composer is empty and nothing waits in Kimi's queue;
	// the TUI takes input.
	KimiEmpty KimiComposer = iota
	// KimiDraft: the composer holds unsent text (a human's draft).
	KimiDraft
	// KimiBusy: the composer is empty but Kimi is busy with a turn and holds
	// queued messages («❯ …» rows above the composer).
	KimiBusy
)

func (c KimiComposer) String() string {
	switch c {
	case KimiEmpty:
		return "empty"
	case KimiDraft:
		return "draft"
	case KimiBusy:
		return "busy"
	}
	return "unknown"
}

// PaneDialectKimi selects the Kimi Code pane parsers (ParseKimiComposer,
// ParseKimiStartScreen) wherever rocket reads or types into a pane.
const PaneDialectKimi = "kimi"

// kimiStatusRow is the status-bar row under Kimi's composer box.
var kimiStatusRow = regexp.MustCompile(`context: [0-9.]+% \(`)

const (
	// kimiFooterRows is how many non-blank rows may follow the box.
	kimiFooterRows = 3
	// kimiQueueScanRows bounds the walk above the box for queue rows.
	kimiQueueScanRows = 12
)

// ParseKimiComposer reads the state of Kimi's composer from the bottom of
// pane (captured with or without escapes). ok is false when the pane does
// not end in a Kimi composer box followed by its status bar — the TUI is not
// up yet, a start screen or a question widget replaces the composer, or the
// pane belongs to another agent.
func ParseKimiComposer(pane string) (KimiComposer, bool) {
	k, ok := parseKimiComposer(pane)
	return k.state, ok
}

// kimiComposer is what parseKimiComposer reads off the pane.
type kimiComposer struct {
	state KimiComposer
	rows  int      // composer content rows (≥ 1)
	queue []string // Kimi's queued items, top to bottom, «❯ » stripped
}

func parseKimiComposer(pane string) (kimiComposer, bool) {
	parsed := trimTrailingBlankLines(parsePane(pane))
	rows := make([]string, len(parsed))
	for i, l := range parsed {
		rows[i] = strings.TrimSpace(l.text())
	}

	// The box's bottom border, followed only by the status bar.
	bottom := -1
	footer := 0
	status := false
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		if strings.HasPrefix(r, "╰") && strings.HasSuffix(r, "╯") {
			bottom = i
			break
		}
		if r == "" {
			continue
		}
		if kimiStatusRow.MatchString(r) {
			status = true
		}
		footer++
		if footer > kimiFooterRows {
			return kimiComposer{}, false
		}
	}
	if bottom < 0 || !status {
		return kimiComposer{}, false
	}

	// Box content rows up to the top border; the first one is the prompt.
	top := -1
	var content []string
	for i := bottom - 1; i >= 0; i-- {
		r := rows[i]
		if strings.HasPrefix(r, "╭") && strings.HasSuffix(r, "╮") {
			top = i
			break
		}
		if !strings.HasPrefix(r, "│") || !strings.HasSuffix(r, "│") || utf8.RuneCountInString(r) < 2 {
			return kimiComposer{}, false
		}
		content = append([]string{strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(r, "│"), "│"))}, content...)
	}
	if top < 0 || len(content) == 0 {
		return kimiComposer{}, false
	}
	first, isPrompt := strings.CutPrefix(content[0], ">")
	if !isPrompt {
		return kimiComposer{}, false
	}
	content[0] = first
	k := kimiComposer{state: KimiEmpty, rows: len(content)}
	if strings.TrimSpace(strings.Join(content, " ")) != "" {
		k.state = KimiDraft
		return k, true
	}

	// Kimi's queue: «❯ …» rows directly above the box, under a ─── rule.
	var queue []string
	for i := top - 1; i >= 0 && top-i <= kimiQueueScanRows; i-- {
		r := rows[i]
		if r == "" {
			break
		}
		if strings.HasPrefix(r, "────") {
			if len(queue) > 0 {
				k.state, k.queue = KimiBusy, queue
			}
			break
		}
		if item, ok := strings.CutPrefix(r, "❯ "); ok {
			queue = append([]string{item}, queue...)
		}
	}
	return k, true
}

// LooksLikeKimiQueued reports whether pane shows text waiting in Kimi's own
// queue (the «❯ …» rows above its composer while a turn runs). Kimi cuts a
// long item at the screen edge with «…», so an item also matches when it is
// a visible start of text at least minQueuedPrefix non-space runes long.
func LooksLikeKimiQueued(pane, text string) bool {
	want := stripWhitespace(text)
	if want == "" {
		return false
	}
	k, ok := parseKimiComposer(pane)
	if !ok {
		return false
	}
	for _, item := range k.queue {
		got := stripWhitespace(item)
		if got == want {
			return true
		}
		head, cut := strings.CutSuffix(got, "…")
		if cut && utf8.RuneCountInString(head) >= minQueuedPrefix && strings.HasPrefix(want, head) {
			return true
		}
	}
	return false
}

// kimiClearKeys are the keys that empty Kimi's composer of rows rows: its
// C-u clears only the current line, and BSpace at a line start joins it to
// the line above (live, Kimi 2.4.0). Ctrl-C would clear it all at once but
// cancels a running turn, and a second press exits Kimi.
func kimiClearKeys(rows int) []string {
	keys := []string{"C-u"}
	for i := 1; i < rows; i++ {
		keys = append(keys, "BSpace", "C-u")
	}
	return keys
}
